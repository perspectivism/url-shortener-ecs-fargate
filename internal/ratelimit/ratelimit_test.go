package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

func TestFixedWindow(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	now := time.Date(2026, 10, 14, 12, 0, 50, 0, time.UTC)
	m := &linkstest.Counter{}
	l := New(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}), func() time.Time { return now }, m, slog.New(slog.DiscardHandler))

	for i := range 3 {
		d := l.Allow(ctx, "alice", "write", 3, time.Minute)
		if !d.Allowed || d.Remaining != 2-i || d.Limit != 3 {
			t.Fatalf("request %d: %+v", i, d)
		}
	}
	d := l.Allow(ctx, "alice", "write", 3, time.Minute)
	if d.Allowed || d.RetryAfter != 10*time.Second || m.Get(telemetry.RateLimited) != 1 {
		t.Fatalf("over limit: %+v", d)
	}
	// Other owners and routes are independent.
	if !l.Allow(ctx, "bob", "write", 3, time.Minute).Allowed || !l.Allow(ctx, "alice", "read", 3, time.Minute).Allowed {
		t.Fatal("limits must be per owner and route")
	}
	key := fmt.Sprintf("rl:alice:write:%d", now.Unix()/60)
	if v, _ := mr.Get(key); v != "4" {
		t.Fatalf("counter %s = %q", key, v)
	}
	if ttl := mr.TTL(key); ttl != 61*time.Second {
		t.Fatalf("ttl = %v", ttl)
	}
	// Next window resets.
	now = now.Add(15 * time.Second)
	if d := l.Allow(ctx, "alice", "write", 3, time.Minute); !d.Allowed || d.Remaining != 2 {
		t.Fatalf("new window: %+v", d)
	}
	// Retry-After is at least one second.
	now = time.Date(2026, 10, 14, 12, 1, 59, 900_000_000, time.UTC)
	for range 3 {
		l.Allow(ctx, "carol", "write", 2, time.Minute)
	}
	if d := l.Allow(ctx, "carol", "write", 2, time.Minute); d.RetryAfter != time.Second {
		t.Fatalf("retry floor: %v", d.RetryAfter)
	}
}

func TestFailOpen(t *testing.T) {
	mr := miniredis.RunT(t)
	m := &linkstest.Counter{}
	l := New(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}), time.Now, m, slog.New(slog.DiscardHandler))
	mr.Close()
	d := l.Allow(context.Background(), "alice", "write", 1, time.Minute)
	if !d.Allowed || m.Get(telemetry.CacheErrors) != 1 {
		t.Fatalf("must fail open: %+v", d)
	}
}
