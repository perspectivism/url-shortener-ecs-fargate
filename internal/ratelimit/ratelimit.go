// Package ratelimit implements per-owner fixed-window rate limits in Valkey
// (key rl:{owner}:{route}:{window}). It fails open when Valkey is unavailable:
// the WAF's per-IP rate rules are the backstop (SPEC §7).
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// Metrics records counters.
type Metrics interface {
	Count(name string, v float64)
}

// Decision is the outcome of a rate-limit check.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Limiter checks fixed-window limits.
type Limiter struct {
	c       redis.Cmdable
	now     func() time.Time
	metrics Metrics
	logger  *slog.Logger
	timeout time.Duration
}

// New returns a Limiter.
func New(c redis.Cmdable, now func() time.Time, metrics Metrics, logger *slog.Logger) *Limiter {
	return &Limiter{c: c, now: now, metrics: metrics, logger: logger, timeout: 50 * time.Millisecond}
}

// Allow counts one request for owner on route against limit per window.
func (l *Limiter) Allow(ctx context.Context, owner, route string, limit int, window time.Duration) Decision {
	now := l.now()
	win := now.Unix() / int64(window.Seconds())
	key := "rl:" + owner + ":" + route + ":" + strconv.FormatInt(win, 10)
	windowEnd := time.Unix((win+1)*int64(window.Seconds()), 0)

	cctx, cancel := context.WithTimeout(ctx, l.timeout)
	defer cancel()
	var incr *redis.IntCmd
	_, err := l.c.TxPipelined(cctx, func(p redis.Pipeliner) error {
		incr = p.Incr(cctx, key)
		p.ExpireNX(cctx, key, window+time.Second)
		return nil
	})
	if err != nil {
		l.metrics.Count(telemetry.CacheErrors, 1)
		l.logger.WarnContext(ctx, "rate limiter unavailable; failing open", "route", route, "err", fmt.Errorf("rate limit: %w", err))
		return Decision{Allowed: true, Limit: limit, Remaining: limit}
	}
	n := int(incr.Val())
	if n > limit {
		l.metrics.Count(telemetry.RateLimited, 1)
		retry := windowEnd.Sub(now)
		if retry < time.Second {
			retry = time.Second
		}
		return Decision{Allowed: false, Limit: limit, Remaining: 0, RetryAfter: retry}
	}
	return Decision{Allowed: true, Limit: limit, Remaining: limit - n}
}
