package links_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

func newResolver(repo links.LinkReader, cache links.ResolverCache, m *linkstest.Counter, now time.Time) *links.Resolver {
	return links.NewResolver(cache, repo, m, slog.New(slog.DiscardHandler), func() time.Time { return now })
}

func TestResolveCacheAside(t *testing.T) {
	ctx := context.Background()
	repo, cache, m := linkstest.NewRepo(), linkstest.NewCache(), &linkstest.Counter{}
	exp := t0.Add(10 * time.Minute)
	repo.Put(links.Link{Code: "live", LongURL: "https://example.com", Status: links.StatusActive, ExpiresAt: &exp})
	repo.Put(links.Link{Code: "noexp", LongURL: "https://example.org", Status: links.StatusActive})
	r := newResolver(repo, cache, m, t0)

	// Miss -> DynamoDB -> cached with TTL = time to expiry.
	res, err := r.Resolve(ctx, "live")
	if err != nil || res.Outcome != links.OutcomeFound || res.URL != "https://example.com" {
		t.Fatalf("resolve: %+v %v", res, err)
	}
	if cache.TTLs["live"] != 10*time.Minute || m.Get(telemetry.CacheMiss) != 1 {
		t.Fatalf("ttl=%v miss=%v", cache.TTLs["live"], m.Get(telemetry.CacheMiss))
	}
	// Hit -> no DynamoDB read.
	gets := repo.Gets
	if res, _ = r.Resolve(ctx, "live"); res.Outcome != links.OutcomeFound || repo.Gets != gets || m.Get(telemetry.CacheHit) != 1 {
		t.Fatal("expected cache hit without DynamoDB read")
	}
	// No expiry -> 1 h TTL.
	_, _ = r.Resolve(ctx, "noexp")
	if cache.TTLs["noexp"] != links.PositiveTTL {
		t.Fatalf("ttl = %v", cache.TTLs["noexp"])
	}
	// Unknown -> negative entry for 60 s, then negative hit.
	if res, _ = r.Resolve(ctx, "ghost"); res.Outcome != links.OutcomeNotFound || cache.TTLs["ghost"] != links.NegativeTTL {
		t.Fatalf("negative: %+v ttl=%v", res, cache.TTLs["ghost"])
	}
	gets = repo.Gets
	if res, _ = r.Resolve(ctx, "ghost"); res.Outcome != links.OutcomeNotFound || repo.Gets != gets || m.Get(telemetry.NegativeCacheHit) != 1 {
		t.Fatal("expected negative cache hit")
	}
}

func TestResolveGoneStates(t *testing.T) {
	ctx := context.Background()
	past := t0.Add(-time.Minute)
	future := t0.Add(time.Minute)
	repo := linkstest.NewRepo()
	repo.Put(links.Link{Code: "expired", LongURL: "https://a.example", Status: links.StatusActive, ExpiresAt: &past})
	repo.Put(links.Link{Code: "disabled", LongURL: "https://a.example", Status: links.StatusDisabled})
	repo.Put(links.Link{Code: "deleted", LongURL: "https://a.example", Status: links.StatusDeleted})
	repo.Put(links.Link{Code: "soon", LongURL: "https://a.example", Status: links.StatusActive, ExpiresAt: &future})
	cache := linkstest.NewCache()
	r := newResolver(repo, cache, &linkstest.Counter{}, t0)
	for _, code := range []string{"expired", "disabled", "deleted"} {
		res, err := r.Resolve(ctx, code)
		if err != nil || res.Outcome != links.OutcomeGone {
			t.Errorf("%s: %+v %v", code, res, err)
		}
		if e, _ := cache.Entry(code); e.URL != "" {
			t.Errorf("%s: gone links must not cache a destination", code)
		}
	}
	// A cached live entry is re-checked against the clock on every request.
	if res, _ := r.Resolve(ctx, "soon"); res.Outcome != links.OutcomeFound {
		t.Fatal("soon should resolve before expiry")
	}
	later := newResolver(repo, cache, &linkstest.Counter{}, t0.Add(2*time.Minute))
	if res, _ := later.Resolve(ctx, "soon"); res.Outcome != links.OutcomeGone {
		t.Fatal("cached entry past exp must be gone")
	}
}

func TestResolveFailOpen(t *testing.T) {
	ctx := context.Background()
	repo, cache, m := linkstest.NewRepo(), linkstest.NewCache(), &linkstest.Counter{}
	repo.Put(links.Link{Code: "live", LongURL: "https://example.com", Status: links.StatusActive})
	cache.GetErr = errors.New("valkey down")
	cache.SetErr = errors.New("valkey down")
	r := newResolver(repo, cache, m, t0)
	res, err := r.Resolve(ctx, "live")
	if err != nil || res.Outcome != links.OutcomeFound || m.Get(telemetry.CacheErrors) != 2 {
		t.Fatalf("fail-open: %+v %v errors=%v", res, err, m.Get(telemetry.CacheErrors))
	}
	// DynamoDB failure surfaces as an error.
	repo.Err = errors.New("dynamodb down")
	if _, err := r.Resolve(ctx, "live"); err == nil {
		t.Fatal("expected error when both cache and DynamoDB fail")
	}
}

type slowRepo struct {
	*linkstest.Repo
	mu      sync.Mutex
	calls   int
	release chan struct{}
}

func (s *slowRepo) GetLink(ctx context.Context, code string) (links.Link, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	<-s.release
	return s.Repo.GetLink(ctx, code)
}

func TestResolveSingleflight(t *testing.T) {
	base := linkstest.NewRepo()
	base.Put(links.Link{Code: "hot", LongURL: "https://example.com", Status: links.StatusActive})
	repo := &slowRepo{Repo: base, release: make(chan struct{})}
	r := newResolver(repo, linkstest.NewCache(), &linkstest.Counter{}, t0)
	var wg sync.WaitGroup
	results := make(chan links.Resolution, 50)
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := r.Resolve(context.Background(), "hot")
			if err == nil {
				results <- res
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(repo.release)
	wg.Wait()
	close(results)
	n := 0
	for res := range results {
		if res.Outcome != links.OutcomeFound {
			t.Fatal("wrong outcome")
		}
		n++
	}
	if n != 50 || repo.calls != 1 {
		t.Fatalf("results=%d dynamodb calls=%d, want 50 and 1", n, repo.calls)
	}
}

func TestResolveCallerDeadline(t *testing.T) {
	base := linkstest.NewRepo()
	repo := &slowRepo{Repo: base, release: make(chan struct{})}
	defer close(repo.release)
	r := newResolver(repo, linkstest.NewCache(), &linkstest.Counter{}, t0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := r.Resolve(ctx, "x"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestEntryFor(t *testing.T) {
	almost := t0.Add(100 * time.Millisecond)
	e, ttl := links.EntryFor(links.Link{LongURL: "u", Status: links.StatusActive, ExpiresAt: &almost}, t0)
	if ttl != time.Second || e.Exp == nil {
		t.Fatalf("min ttl: %v %+v", ttl, e)
	}
}

// pausingRepo reads the link, then waits: the window in which a mutation can
// land between a fill's DynamoDB read and its cache write.
type pausingRepo struct {
	*linkstest.Repo
	read, release chan struct{}
}

func (p *pausingRepo) GetLink(ctx context.Context, code string) (links.Link, error) {
	l, err := p.Repo.GetLink(ctx, code)
	close(p.read)
	<-p.release
	return l, err
}

// Regression (review finding 2): a fill that read the active link must not
// write it back after an admin disable invalidated the cache.
func TestResolveFillRacingDisable(t *testing.T) {
	base := linkstest.NewRepo()
	base.Put(links.Link{Code: "evil", OwnerID: "o", LongURL: "https://phish.example", Status: links.StatusActive, CreatedAt: t0, UpdatedAt: t0})
	cache := linkstest.NewCache()
	repo := &pausingRepo{Repo: base, read: make(chan struct{}), release: make(chan struct{})}
	r := newResolver(repo, cache, &linkstest.Counter{}, t0)
	svc := links.NewService(links.ServiceDeps{Repo: base, Cache: cache, Now: func() time.Time { return t0.Add(time.Second) }})

	done := make(chan links.Resolution)
	go func() {
		res, _ := r.Resolve(context.Background(), "evil")
		done <- res
	}()
	<-repo.read // the fill has read the active link
	if _, err := svc.Disable(context.Background(), "evil", "phishing"); err != nil {
		t.Fatal(err)
	}
	close(repo.release) // ...and now tries to cache it
	<-done              // this in-flight request was ordered before the disable

	if e, ok := cache.Entry("evil"); ok {
		t.Fatalf("stale entry cached after disable: %+v", e)
	}
	r2 := newResolver(base, cache, &linkstest.Counter{}, t0.Add(2*time.Second))
	if res, _ := r2.Resolve(context.Background(), "evil"); res.Outcome != links.OutcomeGone {
		t.Fatalf("after disable: outcome %v, want gone", res.Outcome)
	}
	cache.ExpireFences()
	if res, _ := r2.Resolve(context.Background(), "evil"); res.Outcome != links.OutcomeGone {
		t.Fatalf("after the fence: outcome %v, want gone", res.Outcome)
	}
	if e, ok := cache.Entry("evil"); !ok || e.Status != links.StatusDisabled || e.URL != "" {
		t.Fatalf("after the fence the disabled entry is cached: %+v %v", e, ok)
	}
}

// A fill slower than half the fence (a paused task) skips the cache write.
func TestResolveSlowFillSkipsCache(t *testing.T) {
	repo := linkstest.NewRepo()
	repo.Put(links.Link{Code: "slow", LongURL: "https://example.com", Status: links.StatusActive})
	cache := linkstest.NewCache()
	now := t0
	calls := 0
	clock := func() time.Time { // the second reading is after the DynamoDB read
		calls++
		if calls > 1 {
			return now.Add(links.FenceTTL/2 + time.Millisecond)
		}
		return now
	}
	r := links.NewResolver(cache, repo, &linkstest.Counter{}, slog.New(slog.DiscardHandler), clock)
	if res, err := r.Resolve(context.Background(), "slow"); err != nil || res.Outcome != links.OutcomeFound {
		t.Fatalf("resolve: %+v %v", res, err)
	}
	if _, ok := cache.Entry("slow"); ok {
		t.Fatal("a slow fill must not write the cache")
	}
}
