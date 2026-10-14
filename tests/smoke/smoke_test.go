//go:build smoke

// Package smoke runs end-to-end checks against a deployed environment (or the
// local stack). Configuration comes from the environment — never from files:
//
//	SMOKE_BASE_URL         public base URL (CloudFront), required
//	SMOKE_API_URL          API base URL (default: SMOKE_BASE_URL; local: http://localhost:8081)
//	SMOKE_API_KEY          API key with links:read and links:write, required
//	SMOKE_API_KEY_2        optional second owner's key (cross-owner checks)
//	SMOKE_EXPECT_SCANNING  "true" if the environment scans URLs (Google's test URL must be rejected)
//	SMOKE_STATS_TIMEOUT    how long to wait for click counts (default 2m, SPEC N4)
package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// googleTestURL is Safe Browsing's documented malware test page.
const googleTestURL = "https://malware.testing.google.test/testing/malware/"

const browserUA = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0 shortener-smoke"

type cfg struct {
	base, api, key, key2 string
	scanning             bool
	statsTimeout         time.Duration
}

func load(t *testing.T) cfg {
	t.Helper()
	c := cfg{
		base:     strings.TrimSuffix(os.Getenv("SMOKE_BASE_URL"), "/"),
		api:      strings.TrimSuffix(os.Getenv("SMOKE_API_URL"), "/"),
		key:      os.Getenv("SMOKE_API_KEY"),
		key2:     os.Getenv("SMOKE_API_KEY_2"),
		scanning: os.Getenv("SMOKE_EXPECT_SCANNING") == "true",
	}
	if c.api == "" {
		c.api = c.base
	}
	c.statsTimeout = 2 * time.Minute
	if v := os.Getenv("SMOKE_STATS_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("SMOKE_STATS_TIMEOUT: %v", err)
		}
		c.statsTimeout = d
	}
	if c.base == "" || c.key == "" {
		t.Fatal("SMOKE_BASE_URL and SMOKE_API_KEY are required")
	}
	return c
}

var client = &http.Client{
	Timeout:       15 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type resp struct {
	status int
	header http.Header
	body   map[string]any
	raw    string
}

func do(t *testing.T, method, url, key, body string, hdr map[string]string) resp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = bytes.NewBufferString(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, url, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", browserUA)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{status: r.StatusCode, header: r.Header, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func want(t *testing.T, r resp, status int, what string) {
	t.Helper()
	if r.status != status {
		t.Fatalf("%s: status %d, want %d: %s", what, r.status, status, r.raw)
	}
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func create(t *testing.T, c cfg, key, url string) resp {
	t.Helper()
	return do(t, http.MethodPost, c.api+"/api/v1/links", key, fmt.Sprintf(`{"url":%q}`, url), nil)
}

// TestSmoke is the SPEC §16 smoke flow: create → 302 → stats increment within
// 2 min → 404 → 410 → 401 → 422 unsafe URL.
func TestSmoke(t *testing.T) {
	c := load(t)

	want(t, do(t, http.MethodGet, c.base+"/healthz", "", "", nil), http.StatusOK, "healthz through the edge")

	created := create(t, c, c.key, fmt.Sprintf("https://example.com/smoke/%d", time.Now().UnixNano()))
	want(t, created, http.StatusCreated, "create")
	code, short := str(created.body, "code"), str(created.body, "short_url")
	if code == "" || !strings.HasSuffix(short, "/"+code) {
		t.Fatalf("create response: %s", created.raw)
	}
	t.Logf("created %s", short)

	r := do(t, http.MethodGet, short, "", "", nil)
	want(t, r, http.StatusFound, "redirect")
	if !strings.HasPrefix(r.header.Get("Location"), "https://example.com/smoke/") || !strings.Contains(r.header.Get("Cache-Control"), "no-store") {
		t.Fatalf("redirect headers: Location=%q Cache-Control=%q", r.header.Get("Location"), r.header.Get("Cache-Control"))
	}

	// Stats increment within 2 minutes (SPEC N4).
	deadline := time.Now().Add(c.statsTimeout)
	start := time.Now()
	for {
		s := do(t, http.MethodGet, c.api+"/api/v1/links/"+code+"/stats", c.key, "", nil)
		want(t, s, http.StatusOK, "stats")
		if n, _ := s.body["total_clicks"].(float64); n >= 1 {
			t.Logf("click counted after %s", time.Since(start).Round(time.Second))
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("click not counted within %s", c.statsTimeout)
		}
		time.Sleep(5 * time.Second)
	}

	want(t, do(t, http.MethodGet, c.base+"/nosuchcode"+fmt.Sprint(time.Now().Unix()%100000), "", "", nil), http.StatusNotFound, "unknown code")

	want(t, do(t, http.MethodDelete, c.api+"/api/v1/links/"+code, c.key, "", nil), http.StatusNoContent, "delete")
	want(t, do(t, http.MethodGet, short, "", "", nil), http.StatusGone, "deleted link")

	want(t, do(t, http.MethodPost, c.api+"/api/v1/links", "", `{"url":"https://example.com"}`, nil), http.StatusUnauthorized, "no API key")
	want(t, do(t, http.MethodGet, c.api+"/api/v1/links", "shortener_dev_"+strings.Repeat("A", 43), "", nil), http.StatusUnauthorized, "invalid API key")

	unsafe := create(t, c, c.key, googleTestURL)
	if c.scanning {
		want(t, unsafe, http.StatusUnprocessableEntity, "Google Safe Browsing test URL")
		if unsafe.body["code"] != "unsafe_url" {
			t.Fatalf("unsafe URL problem code = %v", unsafe.body["code"])
		}
	} else {
		// Scanning disabled (dev default): accepted as pending and rescanned once enabled.
		want(t, unsafe, http.StatusCreated, "create without scanning")
		if str(unsafe.body, "scan_status") != "pending" {
			t.Fatalf("scan_status = %q, want pending", str(unsafe.body, "scan_status"))
		}
		want(t, do(t, http.MethodDelete, c.api+"/api/v1/links/"+str(unsafe.body, "code"), c.key, "", nil), http.StatusNoContent, "cleanup")
	}
}

// TestSecurityNegatives covers the request-level security checks that do not
// need AWS-side tooling (oversize body, cross-owner isolation).
func TestSecurityNegatives(t *testing.T) {
	c := load(t)

	big := fmt.Sprintf(`{"url":"https://example.com/%s"}`, strings.Repeat("a", 9000))
	r := do(t, http.MethodPost, c.api+"/api/v1/links", c.key, big, nil)
	want(t, r, http.StatusRequestEntityTooLarge, "oversize body")

	if c.key2 == "" {
		t.Log("SMOKE_API_KEY_2 not set: cross-owner check needs a second owner's key")
		return
	}
	created := create(t, c, c.key, fmt.Sprintf("https://example.com/owner/%d", time.Now().UnixNano()))
	want(t, created, http.StatusCreated, "create")
	code := str(created.body, "code")
	defer do(t, http.MethodDelete, c.api+"/api/v1/links/"+code, c.key, "", nil)
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		want(t, do(t, m, c.api+"/api/v1/links/"+code, c.key2, "", nil), http.StatusNotFound, "cross-owner "+m)
	}
	want(t, do(t, http.MethodGet, c.api+"/api/v1/links/"+code+"/stats", c.key2, "", nil), http.StatusNotFound, "cross-owner stats")
}
