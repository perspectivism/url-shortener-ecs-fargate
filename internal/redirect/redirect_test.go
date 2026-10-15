package redirect

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/config"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/events"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/visitor"
)

type fakeResolver struct {
	res links.Resolution
	err error
}

func (f fakeResolver) Resolve(context.Context, string) (links.Resolution, error) { return f.res, f.err }

type recordingQueue struct {
	mu     sync.Mutex
	events []events.ClickEvent
}

func (q *recordingQueue) Enqueue(e events.ClickEvent) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = append(q.events, e)
	return true
}

type samples struct{ n int }

func (s *samples) Count(string, float64)   {}
func (s *samples) Observe(string, float64) { s.n++ }

var now = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func newHandler(res fakeResolver, q *recordingQueue, logs *bytes.Buffer, m *samples) http.Handler {
	return New(Deps{
		Resolver: res, Clicks: q, Hasher: visitor.NewHasher(config.NewSecret(strings.Repeat("s", 32))),
		FromCloudFront: true, Metrics: m, Logger: slog.New(slog.NewJSONHandler(logs, nil)),
		Now: func() time.Time { return now }, NewEventID: func() string { return "evt-1" },
	}).Routes()
}

func TestRedirectFound(t *testing.T) {
	q := &recordingQueue{}
	var logs bytes.Buffer
	m := &samples{}
	h := newHandler(fakeResolver{res: links.Resolution{Outcome: links.OutcomeFound, URL: "https://example.com/secret-path?token=abc"}}, q, &logs, m)

	r := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh)")
	r.Header.Set("Referer", "https://news.ycombinator.com/item?id=1")
	r.Header.Set("CloudFront-Viewer-Address", "203.0.113.9:4431")
	r.Header.Set("CloudFront-Viewer-Country", "de")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://example.com/secret-path?token=abc" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for k, v := range map[string]string{
		"Cache-Control":   "private, no-store, max-age=0",
		"X-Robots-Tag":    "noindex",
		"Referrer-Policy": "no-referrer-when-downgrade",
	} {
		if rec.Header().Get(k) != v {
			t.Errorf("%s = %q, want %q", k, rec.Header().Get(k), v)
		}
	}
	if len(q.events) != 1 {
		t.Fatalf("events = %d", len(q.events))
	}
	e := q.events[0]
	if e.V != 1 || e.EventID != "evt-1" || e.Code != "abc123" || e.ReferrerHost != "news.ycombinator.com" || e.Country != "DE" || e.IsBot || len(e.Visitor) != 64 || !e.TS.Equal(now) {
		t.Fatalf("event = %+v", e)
	}
	if m.n != 1 {
		t.Fatal("latency not observed")
	}
	// Privacy: neither the IP nor the destination URL is logged.
	if strings.Contains(logs.String(), "203.0.113.9") || strings.Contains(logs.String(), "secret-path") {
		t.Fatalf("log leaked data: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"code":"abc123"`) || !strings.Contains(logs.String(), `"status":302`) {
		t.Fatalf("access log missing fields: %s", logs.String())
	}
}

func TestHeadEmitsNoEvent(t *testing.T) {
	q := &recordingQueue{}
	h := newHandler(fakeResolver{res: links.Resolution{Outcome: links.OutcomeFound, URL: "https://example.com"}}, q, &bytes.Buffer{}, &samples{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/abc123", nil))
	if rec.Code != http.StatusFound || len(q.events) != 0 {
		t.Fatalf("HEAD: %d events=%d", rec.Code, len(q.events))
	}
}

func TestRedirectErrors(t *testing.T) {
	tests := []struct {
		name   string
		res    fakeResolver
		path   string
		method string
		want   int
	}{
		{"unknown", fakeResolver{res: links.Resolution{Outcome: links.OutcomeNotFound}}, "/nope", http.MethodGet, 404},
		{"gone", fakeResolver{res: links.Resolution{Outcome: links.OutcomeGone}}, "/old", http.MethodGet, 410},
		{"backend down", fakeResolver{err: errors.New("dynamodb down")}, "/abc", http.MethodGet, 503},
		{"invalid code", fakeResolver{res: links.Resolution{Outcome: links.OutcomeFound, URL: "x"}}, "/" + strings.Repeat("a", 40), http.MethodGet, 404},
		{"bad chars", fakeResolver{res: links.Resolution{Outcome: links.OutcomeFound, URL: "x"}}, "/a.b", http.MethodGet, 404},
		{"nested path", fakeResolver{}, "/a/b", http.MethodGet, 404},
		{"root", fakeResolver{}, "/", http.MethodGet, 404},
		{"post", fakeResolver{}, "/abc", http.MethodPost, 404},
		{"healthz", fakeResolver{}, "/healthz", http.MethodGet, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &recordingQueue{}
			rec := httptest.NewRecorder()
			newHandler(tt.res, q, &bytes.Buffer{}, &samples{}).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.want || len(q.events) != 0 {
				t.Fatalf("status = %d (want %d), events = %d", rec.Code, tt.want, len(q.events))
			}
			if tt.want >= 400 && rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
			}
		})
	}
}

func TestDefaultEventIDAndBot(t *testing.T) {
	q := &recordingQueue{}
	h := New(Deps{
		Resolver: fakeResolver{res: links.Resolution{Outcome: links.OutcomeFound, URL: "https://example.com"}},
		Clicks:   q, Hasher: visitor.NewHasher(config.NewSecret(strings.Repeat("s", 32))),
		Metrics: &samples{}, Logger: slog.New(slog.DiscardHandler), Now: time.Now,
	}).Routes()
	r := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	r.Header.Set("User-Agent", "curl/8.5.0")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if len(q.events) != 1 || len(q.events[0].EventID) != 36 || !q.events[0].IsBot || q.events[0].Country != "unknown" {
		t.Fatalf("event = %+v", q.events)
	}
}
