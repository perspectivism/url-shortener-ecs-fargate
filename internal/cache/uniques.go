package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// UniquesTTL keeps a day's HyperLogLog and active set for 8 days (SPEC §7),
// long enough for the nightly job and the stats endpoint's recent-days view.
const UniquesTTL = 8 * 24 * time.Hour

// Uniques tracks unique visitors with HyperLogLog and the set of codes that
// had clicks on a day (so the nightly job knows which uniques to persist).
type Uniques struct{ c redis.Cmdable }

// NewUniques returns a Uniques store.
func NewUniques(c redis.Cmdable) *Uniques { return &Uniques{c: c} }

// hllKey uses the hash tag {code} so all of a link's daily HLLs share one
// cluster slot (multi-key PFCOUNT/PFMERGE work in cluster mode).
func hllKey(code, date string) string { return "hll:{" + code + "}:" + date }

func activeKey(date string) string { return "active:" + date }

// Add records a click: the visitor joins the day's HLL (humans only) and the
// code joins the day's active set. Pipelined, not transactional: keys live in
// different cluster slots, and each command is idempotent.
func (u *Uniques) Add(ctx context.Context, code, date, visitor string, human bool) error {
	_, err := u.c.Pipelined(ctx, func(p redis.Pipeliner) error {
		if human {
			p.PFAdd(ctx, hllKey(code, date), visitor)
			p.Expire(ctx, hllKey(code, date), UniquesTTL)
		}
		p.SAdd(ctx, activeKey(date), code)
		p.Expire(ctx, activeKey(date), UniquesTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("uniques add: %w", err)
	}
	return nil
}

// Count returns PFCOUNT for each date (missing keys count 0).
func (u *Uniques) Count(ctx context.Context, code string, dates []string) (map[string]int64, error) {
	cmds := make([]*redis.IntCmd, len(dates))
	_, err := u.c.Pipelined(ctx, func(p redis.Pipeliner) error {
		for i, d := range dates {
			cmds[i] = p.PFCount(ctx, hllKey(code, d))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("uniques count: %w", err)
	}
	out := make(map[string]int64, len(dates))
	for i, d := range dates {
		out[d] = cmds[i].Val()
	}
	return out, nil
}

// ActiveCodes iterates the codes active on date in batches (SSCAN).
func (u *Uniques) ActiveCodes(ctx context.Context, date string, fn func([]string) error) error {
	var cursor uint64
	for {
		codes, next, err := u.c.SScan(ctx, activeKey(date), cursor, "", 500).Result()
		if err != nil {
			return fmt.Errorf("active codes: %w", err)
		}
		if len(codes) > 0 {
			if err := fn(codes); err != nil {
				return err
			}
		}
		if next == 0 {
			return nil
		}
		cursor = next
	}
}
