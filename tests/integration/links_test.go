//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateRedirectHeadersAndHead(t *testing.T) {
	link := createLink(t, "alice", `{"url":"https://example.com/landing?utm=1"}`)
	code := str(link, "code")
	if str(link, "short_url") != env.redirect.url+"/"+code || len(code) < 6 {
		t.Fatalf("link = %v", link)
	}

	r := call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", map[string]string{"User-Agent": "Mozilla/5.0 integration", "Referer": "https://news.ycombinator.com/item?id=1"})
	expectStatus(t, r, http.StatusFound, "")
	for k, v := range map[string]string{
		"Location":        "https://example.com/landing?utm=1",
		"Cache-Control":   "private, no-store, max-age=0",
		"X-Robots-Tag":    "noindex",
		"Referrer-Policy": "no-referrer-when-downgrade",
	} {
		if got := r.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// The first GET populated the positive cache.
	if v, err := env.valkey.Get(context.Background(), "l:"+code).Result(); err != nil || !strings.Contains(v, `"st":"active"`) {
		t.Fatalf("positive cache entry = %q, %v", v, err)
	}

	head := call(t, http.MethodHead, env.redirect.url+"/"+code, "", "", nil)
	expectStatus(t, head, http.StatusFound, "")

	// GET produced exactly one click; HEAD produced none.
	waitStats(t, "alice", code, 1, 0)
	evs := waitS3Events(t, code, 1)
	e := evs[0]
	if e.ReferrerHost != "news.ycombinator.com" || e.V != 1 || len(e.Visitor) != 64 {
		t.Fatalf("event = %+v", e)
	}
	raw, _ := json.Marshal(e)
	if strings.Contains(string(raw), "127.0.0.1") {
		t.Fatal("raw IP must never appear in click events")
	}
	time.Sleep(2 * time.Second) // give a late (wrong) HEAD event time to show up
	waitStats(t, "alice", code, 1, 0)
}

func TestNotFoundAndNegativeCache(t *testing.T) {
	code := "nope" + env.runID
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusNotFound, "not_found")
	v, err := env.valkey.Get(context.Background(), "l:"+code).Result()
	if err != nil || v != `{"nf":true}` {
		t.Fatalf("negative cache entry = %q, %v", v, err)
	}
	if ttl := env.valkey.TTL(context.Background(), "l:"+code).Val(); ttl <= 0 || ttl > 60*time.Second {
		t.Fatalf("negative TTL = %v", ttl)
	}
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusNotFound, "not_found")

	// Creating a link with that alias clears the negative entry immediately.
	createLink(t, "alice", `{"url":"https://example.com/neg","alias":"`+code+`"}`)
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusFound, "")
}

func TestGoneStates(t *testing.T) {
	// Expired.
	exp := time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	expiring := str(createLink(t, "alice", `{"url":"https://example.com/expiring","expires_at":"`+exp+`"}`), "code")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+expiring, "", "", nil), http.StatusFound, "")
	time.Sleep(3 * time.Second)
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+expiring, "", "", nil), http.StatusGone, "gone")

	// Disabled by an admin (cache invalidated immediately).
	code := str(createLink(t, "alice", `{"url":"https://example.com/to-disable"}`), "code")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusFound, "")
	expectStatus(t, call(t, http.MethodPost, env.api.url+"/api/v1/admin/links/"+code+"/disable", env.keys["admin"], `{"reason":"integration test"}`, nil), http.StatusOK, "")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusGone, "gone")
	expectStatus(t, call(t, http.MethodPost, env.api.url+"/api/v1/admin/links/"+code+"/enable", env.keys["admin"], "", nil), http.StatusOK, "")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusFound, "")

	// Deleted by the owner.
	r := call(t, http.MethodDelete, env.api.url+"/api/v1/links/"+code, env.keys["alice"], "", nil)
	expectStatus(t, r, http.StatusNoContent, "")
	expectStatus(t, call(t, http.MethodGet, env.redirect.url+"/"+code, "", "", nil), http.StatusGone, "gone")
}

func TestValidationAndAuthErrors(t *testing.T) {
	api := env.api.url + "/api/v1/links"
	alias := "alias-" + env.runID
	createLink(t, "alice", `{"url":"https://example.com","alias":"`+alias+`"}`)
	expectStatus(t, call(t, http.MethodPost, api, env.keys["bob"], `{"url":"https://example.com","alias":"`+alias+`"}`, nil), http.StatusConflict, "alias_taken")
	expectStatus(t, call(t, http.MethodPost, api, env.keys["alice"], `{"url":"https://example.com","alias":"admin"}`, nil), http.StatusBadRequest, "invalid_request")
	for _, bad := range []string{"ftp://example.com", "http://127.0.0.1/", "http://169.254.169.254/latest", "https://user:pw@example.com", "https://sho.rt/x", "not a url", ""} {
		expectStatus(t, call(t, http.MethodPost, api, env.keys["alice"], `{"url":"`+bad+`"}`, nil), http.StatusUnprocessableEntity, "invalid_url")
	}
	expectStatus(t, call(t, http.MethodPost, api, env.keys["alice"], `{"url":"https://malware.testing.google.test/testing/malware/"}`, nil), http.StatusUnprocessableEntity, "unsafe_url")
	expectStatus(t, call(t, http.MethodPost, api, "", `{"url":"https://example.com"}`, nil), http.StatusUnauthorized, "unauthorized")
	expectStatus(t, call(t, http.MethodPost, api, "shortener_test_"+strings.Repeat("A", 43), `{"url":"https://example.com"}`, nil), http.StatusUnauthorized, "unauthorized")
	expectStatus(t, call(t, http.MethodPost, api, env.keys["alice-ro"], `{"url":"https://example.com"}`, nil), http.StatusForbidden, "forbidden")
	expectStatus(t, call(t, http.MethodPost, env.api.url+"/api/v1/admin/links/"+alias+"/disable", env.keys["alice"], `{"reason":"x"}`, nil), http.StatusForbidden, "forbidden")
	expectStatus(t, call(t, http.MethodPost, api, env.keys["alice"], `{"url":"https://example.com/`+strings.Repeat("a", 9000)+`"}`, nil), http.StatusRequestEntityTooLarge, "payload_too_large")

	// Cross-owner access is indistinguishable from "does not exist".
	expectStatus(t, call(t, http.MethodGet, api+"/"+alias, env.keys["bob"], "", nil), http.StatusNotFound, "not_found")
	expectStatus(t, call(t, http.MethodPatch, api+"/"+alias, env.keys["bob"], `{"url":"https://evil.example"}`, nil), http.StatusNotFound, "not_found")
	expectStatus(t, call(t, http.MethodDelete, api+"/"+alias, env.keys["bob"], "", nil), http.StatusNotFound, "not_found")
	expectStatus(t, call(t, http.MethodGet, api+"/"+alias, env.keys["alice-ro"], "", nil), http.StatusOK, "")
}

func TestIdempotentCreate(t *testing.T) {
	hdr := map[string]string{"Idempotency-Key": "it-" + env.runID}
	body := `{"url":"https://example.com/idempotent"}`
	a := call(t, http.MethodPost, env.api.url+"/api/v1/links", env.keys["alice"], body, hdr)
	b := call(t, http.MethodPost, env.api.url+"/api/v1/links", env.keys["alice"], body, hdr)
	expectStatus(t, a, http.StatusCreated, "")
	expectStatus(t, b, http.StatusCreated, "")
	if string(a.Raw) != string(b.Raw) || b.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay differs:\n%s\n%s", a.Raw, b.Raw)
	}
	expectStatus(t, call(t, http.MethodPost, env.api.url+"/api/v1/links", env.keys["alice"], `{"url":"https://example.com/other"}`, hdr), http.StatusBadRequest, "invalid_request")
}

func TestRateLimit(t *testing.T) {
	var limited *reply
	for range 10 { // limit is 3/min; 10 requests exceed it even across a window boundary
		r := call(t, http.MethodPost, env.apiLimit.url+"/api/v1/links", env.keys["throttled"], `{"url":"https://example.com/rl"}`, nil)
		if r.Status == http.StatusTooManyRequests {
			limited = &r
			break
		}
		expectStatus(t, r, http.StatusCreated, "")
	}
	if limited == nil {
		t.Fatal("never rate limited")
	}
	if limited.Body["code"] != "rate_limited" || limited.Header.Get("Retry-After") == "" {
		t.Fatalf("429 = %s retry-after=%q", limited.Raw, limited.Header.Get("Retry-After"))
	}
}

func TestListPagination(t *testing.T) {
	for i := range 5 {
		createLink(t, "bob", fmt.Sprintf(`{"url":"https://example.com/page/%d"}`, i))
	}
	seen := map[string]bool{}
	cursor := ""
	for range 10 {
		url := env.api.url + "/api/v1/links?limit=2"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		r := call(t, http.MethodGet, url, env.keys["bob"], "", nil)
		expectStatus(t, r, http.StatusOK, "")
		items, _ := r.Body["items"].([]any)
		for _, it := range items {
			m, _ := it.(map[string]any)
			seen[str(m, "code")] = true
		}
		cursor = str(r.Body, "next_cursor")
		if cursor == "" {
			break
		}
	}
	if len(seen) < 5 {
		t.Fatalf("paginated through %d links, want >= 5", len(seen))
	}
	// Alice cannot use Bob's cursor.
	r := call(t, http.MethodGet, env.api.url+"/api/v1/links?limit=1", env.keys["bob"], "", nil)
	if c := str(r.Body, "next_cursor"); c != "" {
		expectStatus(t, call(t, http.MethodGet, env.api.url+"/api/v1/links?cursor="+c, env.keys["alice"], "", nil), http.StatusBadRequest, "invalid_request")
	}
}

// TestKeyRangeAllocationNoDuplicates creates >= 10,000 links concurrently
// through two API instances that claim ID blocks from the same counter
// (block size 100, so they interleave ~100 times) and asserts every code is unique.
func TestKeyRangeAllocationNoDuplicates(t *testing.T) {
	const total, workers = 10_000, 32
	instances := []string{env.apiB.url, env.apiC.url}
	var (
		mu    sync.Mutex
		codes = make(map[string]int, total)
		fails int
		wg    sync.WaitGroup
		next  = make(chan int, total)
	)
	for i := range total {
		next <- i
	}
	close(next)
	start := time.Now()
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			base := instances[w%2]
			for i := range next {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/api/v1/links",
					strings.NewReader(fmt.Sprintf(`{"url":"https://example.com/kgs/%d"}`, i)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+env.keys["bulk"])
				resp, err := noRedirect.Do(req)
				if err != nil {
					mu.Lock()
					fails++
					mu.Unlock()
					continue
				}
				var body map[string]any
				_ = json.NewDecoder(resp.Body).Decode(&body)
				resp.Body.Close()
				mu.Lock()
				if resp.StatusCode == http.StatusCreated {
					codes[str(body, "code")]++
				} else {
					fails++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	dups := 0
	for _, n := range codes {
		if n > 1 {
			dups++
		}
	}
	t.Logf("created %d links via 2 instances in %s (%d failures)", len(codes), time.Since(start).Round(time.Millisecond), fails)
	if fails != 0 || dups != 0 || len(codes) != total {
		t.Fatalf("unique=%d dups=%d failures=%d, want %d unique, 0 dups, 0 failures", len(codes), dups, fails, total)
	}
}
