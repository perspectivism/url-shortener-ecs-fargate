package keygen

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/shortener/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
)

const alphabet = "k3G7QAe51FCsPW92uEOyq4Bg6Sp8YzVTmnU0liwDdHXLajZrfxNhobJIRcMvKt"

// counter emulates the DynamoDB ADD counter (atomic, shared).
type counter struct {
	mu     sync.Mutex
	v      int64
	claims int
	err    error
	bogus  bool
}

func (c *counter) ClaimIDBlock(_ context.Context, size int64) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	if c.bogus {
		return size - 1, nil
	}
	c.v += size
	c.claims++
	return c.v, nil
}

func TestRangeAllocation(t *testing.T) {
	c := &counter{}
	m := &linkstest.Counter{}
	g, err := New(c, 3, alphabet, nil, m)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint64
	for range 7 {
		id, err := g.NextID(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	want := []uint64{1, 2, 3, 4, 5, 6, 7}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if c.claims != 3 || m.Get(telemetry.KeyBlocksAllocated) != 3 {
		t.Fatalf("claims = %d, metric = %v", c.claims, m.Get(telemetry.KeyBlocksAllocated))
	}
}

func TestConcurrentGeneratorsNeverCollide(t *testing.T) {
	c := &counter{}
	var gens []*Generator
	for range 4 {
		g, err := New(c, 50, alphabet, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		gens = append(gens, g)
	}
	var (
		mu   sync.Mutex
		seen = map[string]bool{}
		wg   sync.WaitGroup
	)
	codeRE := regexp.MustCompile(`^[A-Za-z0-9]{6,}$`)
	for _, g := range gens {
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 500 {
					code, err := g.NextCode(context.Background())
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					if seen[code] {
						t.Errorf("duplicate code %s", code)
					}
					if !codeRE.MatchString(code) {
						t.Errorf("bad code %q", code)
					}
					seen[code] = true
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	if len(seen) != 4*4*500 {
		t.Fatalf("got %d unique codes", len(seen))
	}
}

func TestSkipsReserved(t *testing.T) {
	g, err := New(&counter{}, 10, alphabet, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := g.Encode(1)
	second, _ := g.Encode(2)
	g2, _ := New(&counter{}, 10, alphabet, func(c string) bool { return c == first }, nil)
	got, err := g2.NextCode(context.Background())
	if err != nil || got != second {
		t.Fatalf("NextCode = %q, %v; want %q (reserved %q skipped)", got, err, second, first)
	}
}

func TestErrors(t *testing.T) {
	if _, err := New(&counter{}, 0, alphabet, nil, nil); err == nil {
		t.Error("block size 0 must fail")
	}
	if _, err := New(&counter{}, 10, "aab", nil, nil); err == nil {
		t.Error("bad alphabet must fail")
	}
	g, _ := New(&counter{err: errors.New("throttled")}, 10, alphabet, nil, nil)
	if _, err := g.NextCode(context.Background()); err == nil {
		t.Error("claim error must propagate")
	}
	g, _ = New(&counter{bogus: true}, 10, alphabet, nil, nil)
	if _, err := g.NextID(context.Background()); err == nil {
		t.Error("counter below block size must fail")
	}
}
