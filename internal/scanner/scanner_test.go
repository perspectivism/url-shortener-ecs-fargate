package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/config"
)

func TestNoopAndFake(t *testing.T) {
	if _, err := (Noop{}).Check(context.Background(), []string{"https://a.example"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("noop err = %v", err)
	}
	v, err := (Fake{}).Check(context.Background(), []string{"https://ok.example/", "https://malware.testing.google.test/testing/malware/"})
	if err != nil || v["https://ok.example/"].Unsafe || !v["https://malware.testing.google.test/testing/malware/"].Unsafe {
		t.Fatalf("fake verdicts = %+v, %v", v, err)
	}
}

type threat struct {
	URL         string   `json:"url"`
	ThreatTypes []string `json:"threatTypes"`
}

// fakeAPI emulates urls:search. flagged maps URL -> threat URL to return.
func fakeAPI(t *testing.T, flagged map[string]string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v5/urls:search" || r.Method != http.MethodGet {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Goog-Api-Key") != "test-key" || r.URL.Query().Get("key") != "" {
			http.Error(w, "auth", http.StatusForbidden)
			return
		}
		urls := r.URL.Query()["urls"]
		if len(urls) > MaxBatch {
			http.Error(w, "too many", http.StatusBadRequest)
			return
		}
		var resp struct {
			Threats       []threat `json:"threats"`
			CacheDuration string   `json:"cacheDuration"`
		}
		resp.CacheDuration = "300s"
		for _, u := range urls {
			if tu, ok := flagged[u]; ok {
				resp.Threats = append(resp.Threats, threat{URL: tu, ThreatTypes: []string{"MALWARE"}})
			}
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func TestSafeBrowsing(t *testing.T) {
	var calls atomic.Int32
	bad := "https://evil.example/x"
	prefixBad := "https://phish.example/a/b"
	srv := fakeAPI(t, map[string]string{bad: bad, prefixBad: "https://phish.example/"}, &calls)
	defer srv.Close()
	sb := NewSafeBrowsing(srv.URL+"/", config.NewSecret("test-key"), srv.Client())

	// Single URL: an expression match (not the exact URL) still flags it.
	v, err := sb.Check(context.Background(), []string{prefixBad})
	if err != nil || !v[prefixBad].Unsafe {
		t.Fatalf("single: %+v %v", v, err)
	}

	// 120 URLs -> 3 batches; one exact match.
	var urls []string
	for i := range 119 {
		urls = append(urls, fmt.Sprintf("https://ok%d.example/", i))
	}
	urls = append(urls, bad)
	calls.Store(0)
	v, err = sb.Check(context.Background(), urls)
	if err != nil || len(v) != 120 || !v[bad].Unsafe || v[urls[0]].Unsafe || calls.Load() != 3 {
		t.Fatalf("batch: unsafe=%v len=%d calls=%d err=%v", v[bad].Unsafe, len(v), calls.Load(), err)
	}

	// Unattributable match in a batch triggers per-URL rechecks.
	calls.Store(0)
	v, err = sb.Check(context.Background(), []string{"https://ok.example/", prefixBad})
	if err != nil || !v[prefixBad].Unsafe || v["https://ok.example/"].Unsafe || calls.Load() != 3 {
		t.Fatalf("recheck: %+v calls=%d err=%v", v, calls.Load(), err)
	}
}

func TestSafeBrowsingErrors(t *testing.T) {
	var calls atomic.Int32
	srv := fakeAPI(t, nil, &calls)
	defer srv.Close()

	wrongKey := NewSafeBrowsing(srv.URL, config.NewSecret("nope"), srv.Client())
	if _, err := wrongKey.Check(context.Background(), []string{"https://a.example/"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected status error, got %v", err)
	}

	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("{not json")) }))
	defer garbage.Close()
	if _, err := NewSafeBrowsing(garbage.URL, config.NewSecret("k"), garbage.Client()).Check(context.Background(), []string{"https://a.example/"}); err == nil {
		t.Fatal("expected decode error")
	}

	slow := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := NewSafeBrowsing(slow.URL, config.NewSecret("k"), slow.Client()).Check(ctx, []string{"https://secret-path.example/token"})
	if err == nil || strings.Contains(err.Error(), "secret-path") {
		t.Fatalf("timeout error must not include the scanned URL: %v", err)
	}

	if _, err := NewSafeBrowsing("http://bad host", config.NewSecret("k"), http.DefaultClient).Check(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected request build error")
	}
	if got := redactURLError(errors.New("plain")); got.Error() != "plain" {
		t.Fatal("non-url errors pass through")
	}
}
