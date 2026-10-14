package cache

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestUniques(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	u := NewUniques(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}))

	for i := range 5 {
		if err := u.Add(ctx, "abc", "2026-10-14", fmt.Sprintf("visitor-%d", i%3), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := u.Add(ctx, "abc", "2026-10-14", "bot-visitor", false); err != nil {
		t.Fatal(err)
	}
	for i := range 700 {
		_ = u.Add(ctx, fmt.Sprintf("code%d", i), "2026-10-14", "v", false)
	}
	got, err := u.Count(ctx, "abc", []string{"2026-10-14", "2026-10-13"})
	if err != nil || got["2026-10-14"] != 3 || got["2026-10-13"] != 0 {
		t.Fatalf("count = %v %v (bots must not count)", got, err)
	}
	if mr.TTL("hll:{abc}:2026-10-14") != UniquesTTL || mr.TTL("active:2026-10-14") != UniquesTTL {
		t.Fatal("ttl not set")
	}

	var seen []string
	if err := u.ActiveCodes(ctx, "2026-10-14", func(c []string) error { seen = append(seen, c...); return nil }); err != nil {
		t.Fatal(err)
	}
	sort.Strings(seen)
	uniq := map[string]bool{}
	for _, c := range seen {
		uniq[c] = true
	}
	if len(uniq) != 701 || !uniq["abc"] {
		t.Fatalf("active codes = %d", len(uniq))
	}
	boom := errors.New("stop")
	if err := u.ActiveCodes(ctx, "2026-10-14", func([]string) error { return boom }); !errors.Is(err, boom) {
		t.Fatal("callback error must propagate")
	}

	mr.Close()
	if err := u.Add(ctx, "abc", "d", "v", true); err == nil {
		t.Fatal("expected add error")
	}
	if _, err := u.Count(ctx, "abc", []string{"d"}); err == nil {
		t.Fatal("expected count error")
	}
	if err := u.ActiveCodes(ctx, "d", func([]string) error { return nil }); err == nil {
		t.Fatal("expected sscan error")
	}
}
