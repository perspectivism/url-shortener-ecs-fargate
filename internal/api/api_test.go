package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shortener/url-shortener-ecs-fargate/internal/analytics"
	"github.com/shortener/url-shortener-ecs-fargate/internal/auth"
	"github.com/shortener/url-shortener-ecs-fargate/internal/cache"
	"github.com/shortener/url-shortener-ecs-fargate/internal/contract"
	"github.com/shortener/url-shortener-ecs-fargate/internal/httpx"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/shortener/url-shortener-ecs-fargate/internal/ratelimit"
	"github.com/shortener/url-shortener-ecs-fargate/internal/scanner"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

type fakeAuth struct{ err error }

var principals = map[string]auth.Principal{
	"alice-rw": {OwnerID: "alice", Scopes: []string{auth.ScopeLinksRead, auth.ScopeLinksWrite}, KeyID: "a1"},
	"alice-r":  {OwnerID: "alice", Scopes: []string{auth.ScopeLinksRead}, KeyID: "a2"},
	"bob-rw":   {OwnerID: "bob", Scopes: []string{auth.ScopeLinksRead, auth.ScopeLinksWrite}, KeyID: "b1"},
	"admin":    {OwnerID: "ops", Scopes: []string{auth.ScopeAdmin}, KeyID: "o1"},
	"limited":  {OwnerID: "limited", Scopes: []string{auth.ScopeLinksRead, auth.ScopeLinksWrite}, KeyID: "l1"},
}

func (f fakeAuth) Authenticate(_ context.Context, h string) (auth.Principal, error) {
	if f.err != nil {
		return auth.Principal{}, f.err
	}
	p, ok := principals[strings.TrimPrefix(h, "Bearer ")]
	if !ok {
		return auth.Principal{}, auth.ErrUnauthorized
	}
	return p, nil
}

type fakeLimiter struct{}

func (fakeLimiter) Allow(_ context.Context, owner, _ string, limit int, _ time.Duration) ratelimit.Decision {
	if owner == "limited" {
		return ratelimit.Decision{Allowed: false, Limit: limit, RetryAfter: 1500 * time.Millisecond}
	}
	return ratelimit.Decision{Allowed: true, Limit: limit, Remaining: limit - 1}
}

type fakeIdem struct {
	mu  sync.Mutex
	m   map[string]cache.IdemRecord
	err error
}

func (f *fakeIdem) Get(_ context.Context, owner, key string) (cache.IdemRecord, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return cache.IdemRecord{}, false, f.err
	}
	r, ok := f.m[owner+"|"+key]
	return r, ok, nil
}

func (f *fakeIdem) Put(_ context.Context, owner, key string, r cache.IdemRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.m[owner+"|"+key] = r
	return nil
}

type harness struct {
	t        *testing.T
	h        http.Handler
	contract *contract.Validator
	repo     *linkstest.Repo
	idem     *fakeIdem
	now      *time.Time
}

var (
	specOnce sync.Once
	spec     *contract.Validator
	errSpec  error
)

func loadSpec(t *testing.T) *contract.Validator {
	t.Helper()
	specOnce.Do(func() {
		b, err := os.ReadFile("../../api/openapi.yaml")
		if err != nil {
			errSpec = err
			return
		}
		spec, errSpec = contract.Parse(b)
	})
	if errSpec != nil {
		t.Fatal(errSpec)
	}
	return spec
}

func newHarness(t *testing.T, svc LinkService, a Authenticator) *harness {
	t.Helper()
	now := t0
	h := &harness{t: t, contract: loadSpec(t), repo: linkstest.NewRepo(), idem: &fakeIdem{m: map[string]cache.IdemRecord{}}, now: &now}
	clock := func() time.Time { return *h.now }
	if svc == nil {
		svc = links.NewService(links.ServiceDeps{
			Repo: h.repo, Cache: linkstest.NewCache(), Scanner: scanner.Fake{}, Codes: &linkstest.SeqCodes{},
			Now: clock, BlockedHosts: []string{"sho.rt"},
		})
	}
	if a == nil {
		a = fakeAuth{}
	}
	base, _ := url.Parse("https://sho.rt")
	h.h = New(Deps{
		Links: svc, Stats: &fakeStats{}, Auth: a, Limiter: fakeLimiter{}, Idempotency: h.idem, Metrics: &linkstest.Counter{},
		Logger: slog.New(slog.DiscardHandler), Now: clock, BaseURL: base, WriteLimit: 60, ReadLimit: 300,
	}).Handler()
	return h
}

type call struct {
	method, path, key, body string
	headers                 map[string]string
}

type result struct {
	Status int
	Header http.Header
}

// do sends a request, validates the response against the OpenAPI contract
// (and the request too when validReq), and returns status + decoded body.
func (h *harness) do(c call, validReq bool) (result, map[string]any) {
	h.t.Helper()
	req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	if c.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	if validReq {
		if err := h.contract.Request(req); err != nil {
			h.t.Fatalf("request: %v", err)
		}
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	resp := rec.Result()
	defer resp.Body.Close()
	if err := h.contract.Response(req, resp); err != nil {
		h.t.Fatalf("%s %s: %v\nbody: %s", c.method, c.path, err, rec.Body.String())
	}
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return result{Status: resp.StatusCode, Header: resp.Header}, body
}

// raw returns the exact response body bytes.
func (h *harness) raw(c call) string {
	req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec.Body.String()
}

func items(body map[string]any) []any {
	v, _ := body["items"].([]any)
	return v
}

func (h *harness) expect(c call, validReq bool, status int, code string) map[string]any {
	h.t.Helper()
	resp, body := h.do(c, validReq)
	if resp.Status != status {
		h.t.Fatalf("%s %s = %d, want %d (%v)", c.method, c.path, resp.Status, status, body)
	}
	if code != "" && body["code"] != code {
		h.t.Fatalf("%s %s code = %v, want %s", c.method, c.path, body["code"], code)
	}
	return body
}

func TestCreateLinkContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	body := h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://Example.com/a","expires_at":"2026-10-20T00:00:00Z"}`}, true, 201, "")
	if body["code"] != "code000001" || body["short_url"] != "https://sho.rt/code000001" || body["url"] != "https://example.com/a" || body["expires_at"] != "2026-10-20T00:00:00.000Z" {
		t.Fatalf("body = %v", body)
	}
	resp, _ := h.do(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com/b","alias":"my-link"}`}, true)
	if resp.Status != http.StatusCreated || resp.Header.Get("Location") != "/api/v1/links/my-link" {
		t.Fatalf("alias create = %d %s", resp.Status, resp.Header.Get("Location"))
	}
	h.expect(call{method: "POST", path: "/api/v1/links", key: "bob-rw", body: `{"url":"https://example.com","alias":"my-link"}`}, true, 409, "alias_taken")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","alias":"admin"}`}, true, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","alias":"a b"}`}, false, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"ftp://example.com"}`}, true, 422, "invalid_url")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://sho.rt/loop"}`}, true, 422, "invalid_url")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://malware.testing.google.test/testing/malware/"}`}, true, 422, "unsafe_url")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","expires_at":"2020-01-01T00:00:00Z"}`}, true, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","expires_at":"tomorrow"}`}, false, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","extra":1}`}, false, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":`}, false, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com"}`, headers: map[string]string{"Content-Type": "text/plain"}}, false, 415, "unsupported_media_type")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com/` + strings.Repeat("a", 9000) + `"}`}, false, 413, "payload_too_large")
}

func TestAuthAndRateLimitContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	resp, body := h.do(call{method: "POST", path: "/api/v1/links", body: `{"url":"https://example.com"}`}, false)
	if resp.Status != http.StatusUnauthorized || body["code"] != "unauthorized" || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("no key: %d %v", resp.Status, body)
	}
	h.expect(call{method: "GET", path: "/api/v1/links", key: "wrong"}, false, 401, "unauthorized")
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-r", body: `{"url":"https://example.com"}`}, true, 403, "forbidden")
	h.expect(call{method: "POST", path: "/api/v1/admin/links/abcd/disable", key: "alice-rw", body: `{"reason":"x"}`}, true, 403, "forbidden")
	resp, body = h.do(call{method: "POST", path: "/api/v1/links", key: "limited", body: `{"url":"https://example.com"}`}, true)
	if resp.Status != http.StatusTooManyRequests || body["code"] != "rate_limited" || resp.Header.Get("Retry-After") != "2" {
		t.Fatalf("rate limit: %d %v retry=%q", resp.Status, body, resp.Header.Get("Retry-After"))
	}
	h503 := newHarness(t, nil, fakeAuth{err: errors.New("dynamodb down")})
	h503.expect(call{method: "GET", path: "/api/v1/links", key: "alice-rw"}, true, 503, "unavailable")
}

func TestIdempotencyContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	c := call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com"}`, headers: map[string]string{"Idempotency-Key": "req-123"}}
	first := h.expect(c, true, 201, "")
	rawFirst := h.raw(c)
	if rawSecond := h.raw(c); rawFirst != rawSecond {
		t.Fatalf("replay must be byte-identical:\n%q\n%q", rawFirst, rawSecond)
	}
	resp, second := h.do(c, true)
	if resp.Status != http.StatusCreated || resp.Header.Get("Idempotent-Replayed") != "true" || first["code"] != second["code"] {
		t.Fatalf("replay: %d %v vs %v", resp.Status, first, second)
	}
	if p := h.expect(call{method: "GET", path: "/api/v1/links", key: "alice-r"}, true, 200, ""); len(items(p)) != 1 {
		t.Fatalf("a replayed create must not create a second link: %v", p)
	}
	// Another owner using the same key gets their own link.
	other := c
	other.key = "bob-rw"
	if b := h.expect(other, true, 201, ""); b["code"] == first["code"] {
		t.Fatal("idempotency keys must be scoped per owner")
	}
	// Same key, different body.
	diff := c
	diff.body = `{"url":"https://example.org"}`
	h.expect(diff, true, 400, "invalid_request")
	// Malformed key.
	bad := c
	bad.headers = map[string]string{"Idempotency-Key": "has space"}
	h.expect(bad, false, 400, "invalid_request")
	// Store failures fail open.
	h.idem.err = errors.New("valkey down")
	fresh := c
	fresh.headers = map[string]string{"Idempotency-Key": "req-999"}
	h.expect(fresh, true, 201, "")
}

func TestReadUpdateDeleteContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	for i := range 3 {
		*h.now = t0.Add(time.Duration(i) * time.Second)
		h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com/` + string(rune('a'+i)) + `","expires_at":"2027-01-01T00:00:00Z"}`}, true, 201, "")
	}
	page := h.expect(call{method: "GET", path: "/api/v1/links?limit=2", key: "alice-r"}, true, 200, "")
	got := items(page)
	cursor, _ := page["next_cursor"].(string)
	if len(got) != 2 || cursor == "" {
		t.Fatalf("page 1 = %v", page)
	}
	if first, _ := got[0].(map[string]any); first["code"] != "code000003" {
		t.Fatalf("newest first violated: %v", got[0])
	}
	page = h.expect(call{method: "GET", path: "/api/v1/links?limit=2&cursor=" + cursor, key: "alice-r"}, true, 200, "")
	if len(items(page)) != 1 || page["next_cursor"] != nil {
		t.Fatalf("page 2 = %v", page)
	}
	h.expect(call{method: "GET", path: "/api/v1/links?limit=0", key: "alice-r"}, false, 400, "invalid_request")
	h.expect(call{method: "GET", path: "/api/v1/links?cursor=%25%25", key: "alice-r"}, true, 400, "invalid_request")
	if p := h.expect(call{method: "GET", path: "/api/v1/links", key: "bob-rw"}, true, 200, ""); len(items(p)) != 0 {
		t.Fatal("bob must not see alice's links")
	}

	h.expect(call{method: "GET", path: "/api/v1/links/code000001", key: "alice-r"}, true, 200, "")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001", key: "bob-rw"}, true, 404, "not_found")
	h.expect(call{method: "GET", path: "/api/v1/links/nothere", key: "alice-r"}, true, 404, "not_found")

	b := h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"url":"https://example.net/x"}`}, true, 200, "")
	if b["url"] != "https://example.net/x" {
		t.Fatalf("patch url: %v", b)
	}
	if b = h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"expires_at":null}`}, true, 200, ""); b["expires_at"] != nil {
		t.Fatalf("clear expiry: %v", b)
	}
	if b = h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"expires_at":"2027-06-01T00:00:00+02:00"}`}, true, 200, ""); b["expires_at"] != "2027-05-31T22:00:00.000Z" {
		t.Fatalf("set expiry: %v", b)
	}
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{}`}, false, 400, "invalid_request")
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"expires_at":5}`}, false, 400, "invalid_request")
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"expires_at":"soon"}`}, false, 400, "invalid_request")
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `{"url":"https://malware.testing.google.test/x"}`}, true, 422, "unsafe_url")
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "bob-rw", body: `{"url":"https://example.org"}`}, true, 404, "not_found")
	h.expect(call{method: "PATCH", path: "/api/v1/links/code000001", key: "alice-rw", body: `x`, headers: map[string]string{"Content-Type": "text/plain"}}, false, 415, "unsupported_media_type")

	h.expect(call{method: "DELETE", path: "/api/v1/links/code000001", key: "bob-rw"}, true, 404, "not_found")
	resp, _ := h.do(call{method: "DELETE", path: "/api/v1/links/code000001", key: "alice-rw"}, true)
	if resp.Status != http.StatusNoContent {
		t.Fatalf("delete = %d", resp.Status)
	}
	h.expect(call{method: "GET", path: "/api/v1/links/code000001", key: "alice-r"}, true, 404, "not_found")
	h.expect(call{method: "DELETE", path: "/api/v1/links/code000001", key: "alice-rw"}, true, 404, "not_found")
}

func TestAdminContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com","expires_at":"2026-10-15T00:00:00Z"}`}, true, 201, "")
	b := h.expect(call{method: "POST", path: "/api/v1/admin/links/code000001/disable", key: "admin", body: `{"reason":"phishing report #42"}`}, true, 200, "")
	if b["status"] != "disabled" || b["disabled_reason"] != "phishing report #42" {
		t.Fatalf("disable: %v", b)
	}
	h.expect(call{method: "POST", path: "/api/v1/admin/links/code000001/disable", key: "admin", body: `{"reason":"  "}`}, true, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/admin/links/code000001/disable", key: "admin", body: `nope`}, false, 400, "invalid_request")
	h.expect(call{method: "POST", path: "/api/v1/admin/links/missing/disable", key: "admin", body: `{"reason":"x"}`}, true, 404, "not_found")
	if b = h.expect(call{method: "POST", path: "/api/v1/admin/links/code000001/enable", key: "admin"}, true, 200, ""); b["status"] != "active" {
		t.Fatalf("enable: %v", b)
	}
	*h.now = t0.Add(48 * time.Hour)
	h.expect(call{method: "POST", path: "/api/v1/admin/links/code000001/enable", key: "admin"}, true, 410, "gone")
	h.expect(call{method: "POST", path: "/api/v1/admin/links/missing/enable", key: "admin"}, true, 404, "not_found")
}

type brokenService struct {
	LinkService
	err error
}

func (b brokenService) Get(context.Context, string, string) (links.Link, error) {
	return links.Link{}, b.err
}

func TestServerErrorsContract(t *testing.T) {
	h := newHarness(t, brokenService{err: errors.New("boom: internal detail")}, nil)
	b := h.expect(call{method: "GET", path: "/api/v1/links/abcd", key: "alice-r"}, true, 500, "internal")
	if d, _ := b["detail"].(string); strings.Contains(d, "internal detail") {
		t.Fatal("internal error text leaked")
	}
	h = newHarness(t, brokenService{err: context.DeadlineExceeded}, nil)
	h.expect(call{method: "GET", path: "/api/v1/links/abcd", key: "alice-r"}, true, 503, "unavailable")
}

func TestRoutingFallbacks(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.expect(call{method: "GET", path: "/healthz"}, true, 200, "")

	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/v1/links/abcd", nil))
	var p httpx.Problem
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 405 || p.Code != "method_not_allowed" || !strings.Contains(rec.Header().Get("Allow"), "PATCH") || rec.Header().Get("Content-Type") != httpx.ProblemContentType {
		t.Fatalf("405: %d %+v allow=%q", rec.Code, p, rec.Header().Get("Allow"))
	}
	for _, path := range []string{"/nope", "/api/v2/links", "/api/v1/links/a/b/c"} {
		rec = httptest.NewRecorder()
		h.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 404 || rec.Header().Get("Content-Type") != httpx.ProblemContentType {
			t.Errorf("%s = %d %s", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	// A body that fails mid-read (client went away) is a 400, not a 413.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", errReader{})
	req.Header.Set("Authorization", "Bearer alice-rw")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("read error = %d", rec.Code)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

type fakeStats struct{ err error }

func (f *fakeStats) Report(_ context.Context, code string, from, to time.Time) (analytics.Report, error) {
	if f.err != nil {
		return analytics.Report{}, f.err
	}
	if to.Sub(from) > 89*24*time.Hour || from.After(to) {
		return analytics.Report{}, analytics.ErrInvalidRange
	}
	rep := analytics.Report{Code: code, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), TotalClicks: 3, TotalBotClicks: 1,
		Daily: []analytics.Day{{Date: to.Format(time.DateOnly), Clicks: 3, BotClicks: 1, Uniques: 2}},
		Top:   analytics.Top{Referrers: []analytics.Entry{{Value: "t.co", Clicks: 3}}, Countries: []analytics.Entry{}, Devices: []analytics.Entry{}, Browsers: []analytics.Entry{}}}
	return rep, nil
}

func TestStatsContract(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com"}`}, true, 201, "")
	b := h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats", key: "alice-r"}, true, 200, "")
	if b["from"] != "2026-10-08" || b["to"] != "2026-10-14" || b["total_clicks"] != float64(3) {
		t.Fatalf("default range: %v", b)
	}
	if b = h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?to=2026-10-10", key: "alice-r"}, true, 200, ""); b["from"] != "2026-10-04" {
		t.Fatalf("to only: %v", b)
	}
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?from=2026-09-01&to=2026-10-14", key: "alice-r"}, true, 200, "")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?from=2026-01-01&to=2026-10-14", key: "alice-r"}, true, 400, "invalid_request")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?from=2026-10-14&to=2026-10-01", key: "alice-r"}, true, 400, "invalid_request")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?from=yesterday", key: "alice-r"}, false, 400, "invalid_request")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats?to=x", key: "alice-r"}, false, 400, "invalid_request")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats", key: "bob-rw"}, true, 404, "not_found")
	h.expect(call{method: "GET", path: "/api/v1/links/missing/stats", key: "alice-r"}, true, 404, "not_found")
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats", key: "admin"}, true, 403, "forbidden")
}

func TestStatsBackendError(t *testing.T) {
	h := newHarness(t, nil, nil)
	h.expect(call{method: "POST", path: "/api/v1/links", key: "alice-rw", body: `{"url":"https://example.com"}`}, true, 201, "")
	base, _ := url.Parse("https://sho.rt")
	h.h = New(Deps{
		Links: links.NewService(links.ServiceDeps{Repo: h.repo, Cache: linkstest.NewCache(), Scanner: scanner.Fake{}, Codes: &linkstest.SeqCodes{}}),
		Stats: &fakeStats{err: errors.New("dynamodb down")}, Auth: fakeAuth{}, Limiter: fakeLimiter{}, Idempotency: h.idem,
		Metrics: &linkstest.Counter{}, Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return t0 }, BaseURL: base, WriteLimit: 60, ReadLimit: 300,
	}).Handler()
	h.expect(call{method: "GET", path: "/api/v1/links/code000001/stats", key: "alice-r"}, true, 500, "internal")
}
