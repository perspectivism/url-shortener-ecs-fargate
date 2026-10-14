package links

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
)

// Cache TTLs (SPEC §7).
const (
	PositiveTTL = time.Hour
	NegativeTTL = 60 * time.Second
)

// CacheEntry is what the redirect path caches per code: either a resolved link
// ({"u","exp","st"}) or a negative entry ({"nf":true}).
type CacheEntry struct {
	URL      string `json:"u,omitempty"`
	Exp      *int64 `json:"exp,omitempty"`
	Status   Status `json:"st,omitempty"`
	NotFound bool   `json:"nf,omitempty"`
}

// ResolverCache is the redirect path's view of Valkey.
type ResolverCache interface {
	GetLink(ctx context.Context, code string) (CacheEntry, bool, error)
	SetLink(ctx context.Context, code string, e CacheEntry, ttl time.Duration) error
}

// LinkReader reads a link by code.
type LinkReader interface {
	GetLink(ctx context.Context, code string) (Link, error)
}

// Outcome of resolving a code.
type Outcome int

// Outcomes.
const (
	OutcomeNotFound Outcome = iota
	OutcomeFound
	OutcomeGone
)

// Resolution is the redirect decision for a code.
type Resolution struct {
	Outcome Outcome
	URL     string
}

// Recorder records counters.
type Recorder interface {
	Count(name string, v float64)
}

// Resolver implements cache-aside lookup with negative caching and per-code
// request coalescing (singleflight) on misses.
type Resolver struct {
	cache        ResolverCache
	repo         LinkReader
	metrics      Recorder
	logger       *slog.Logger
	now          func() time.Time
	cacheTimeout time.Duration
	dbTimeout    time.Duration
	group        singleflight.Group
}

// NewResolver returns a Resolver. Timeouts: Valkey 50 ms, DynamoDB 200 ms (AGENTS §8).
func NewResolver(cache ResolverCache, repo LinkReader, metrics Recorder, logger *slog.Logger, now func() time.Time) *Resolver {
	return &Resolver{
		cache: cache, repo: repo, metrics: metrics, logger: logger, now: now,
		cacheTimeout: 50 * time.Millisecond,
		dbTimeout:    200 * time.Millisecond,
	}
}

// Resolve decides how to answer GET /{code}. Cache failures fail open to
// DynamoDB; a DynamoDB failure is returned as an error (503).
func (r *Resolver) Resolve(ctx context.Context, code string) (Resolution, error) {
	cctx, cancel := context.WithTimeout(ctx, r.cacheTimeout)
	e, ok, err := r.cache.GetLink(cctx, code)
	cancel()
	switch {
	case err != nil:
		r.metrics.Count(telemetry.CacheErrors, 1)
		r.logger.WarnContext(ctx, "cache read failed; falling back to dynamodb", "code", code, "err", err)
	case ok && e.NotFound:
		r.metrics.Count(telemetry.NegativeCacheHit, 1)
		return Resolution{Outcome: OutcomeNotFound}, nil
	case ok:
		r.metrics.Count(telemetry.CacheHit, 1)
		return r.decide(e), nil
	default:
		r.metrics.Count(telemetry.CacheMiss, 1)
	}

	ch := r.group.DoChan(code, func() (any, error) { return r.load(ctx, code) })
	select {
	case <-ctx.Done():
		return Resolution{}, fmt.Errorf("resolve %s: %w", code, ctx.Err())
	case res := <-ch:
		if res.Err != nil {
			return Resolution{}, res.Err
		}
		e, _ := res.Val.(CacheEntry)
		return r.decide(e), nil
	}
}

// load reads DynamoDB and populates the cache. It runs once per code per
// in-flight burst, on a context detached from any single caller.
func (r *Resolver) load(ctx context.Context, code string) (CacheEntry, error) {
	base := context.WithoutCancel(ctx)
	dctx, cancel := context.WithTimeout(base, r.dbTimeout)
	l, err := r.repo.GetLink(dctx, code)
	cancel()
	var (
		e   CacheEntry
		ttl time.Duration
	)
	switch {
	case errors.Is(err, ErrNotFound):
		e, ttl = CacheEntry{NotFound: true}, NegativeTTL
	case err != nil:
		return CacheEntry{}, fmt.Errorf("resolve %s: %w", code, err)
	default:
		e, ttl = EntryFor(l, r.now())
	}
	cctx, cancel := context.WithTimeout(base, r.cacheTimeout)
	defer cancel()
	if err := r.cache.SetLink(cctx, code, e, ttl); err != nil {
		r.metrics.Count(telemetry.CacheErrors, 1)
		r.logger.WarnContext(ctx, "cache write failed", "code", code, "err", err)
	}
	return e, nil
}

// EntryFor builds the cache entry and TTL for a link: min(1 h, time to expiry)
// for live links, 1 h for links that are already gone.
func EntryFor(l Link, now time.Time) (CacheEntry, time.Duration) {
	e := CacheEntry{URL: l.LongURL, Status: l.Status}
	ttl := PositiveTTL
	if l.ExpiresAt != nil {
		exp := l.ExpiresAt.Unix()
		e.Exp = &exp
		if left := l.ExpiresAt.Sub(now); left > 0 && left < ttl && l.Status == StatusActive {
			ttl = left
		}
	}
	if ttl < time.Second {
		ttl = time.Second
	}
	if l.Status != StatusActive || l.Expired(now) {
		e.URL = "" // never cache a destination we will not redirect to
	}
	return e, ttl
}

// decide applies status and expiry on every request (never trusting TTL timing).
func (r *Resolver) decide(e CacheEntry) Resolution {
	if e.NotFound {
		return Resolution{Outcome: OutcomeNotFound}
	}
	if e.Status != StatusActive || (e.Exp != nil && r.now().Unix() >= *e.Exp) || e.URL == "" {
		return Resolution{Outcome: OutcomeGone}
	}
	return Resolution{Outcome: OutcomeFound, URL: e.URL}
}
