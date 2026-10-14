package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) LookupFunc {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func merge(base map[string]string, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if v == "<unset>" {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

const testSalt = "0123456789abcdef0123456789abcdef"

var redirectBase = map[string]string{
	"APP_ENV":           "local",
	"LINKS_TABLE":       "links",
	"CACHE_ADDR":        "localhost:6379",
	"CACHE_AUTH":        "none",
	"CACHE_TLS":         "false",
	"CACHE_MODE":        "standalone",
	"CLICK_QUEUE_URL":   "http://localhost:9324/000000000000/clicks",
	"VISITOR_HASH_SALT": testSalt,
	"CLIENT_IP_SOURCE":  "remote_addr",
}

var apiBase = map[string]string{
	"APP_ENV":         "local",
	"LINKS_TABLE":     "links",
	"ANALYTICS_TABLE": "analytics",
	"CACHE_ADDR":      "localhost:6379",
	"CACHE_AUTH":      "none",
	"CACHE_TLS":       "false",
	"BASE_URL":        "http://localhost:8080",
	"SQIDS_ALPHABET":  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
	"SCANNER":         "fake",
}

func TestLoadRedirect(t *testing.T) {
	tests := []struct {
		name    string
		over    map[string]string
		wantErr []string
		check   func(t *testing.T, c Redirect)
	}{
		{name: "valid local", check: func(t *testing.T, c Redirect) {
			if c.Service != "redirect" || c.MetricsNamespace != "Shortener/local" || c.ClickBuffer != 10000 {
				t.Fatalf("unexpected config: %+v", c)
			}
			if c.ClickFlushInterval != time.Second || c.ShutdownTimeout != 20*time.Second {
				t.Fatalf("unexpected durations: %+v", c)
			}
		}},
		{name: "missing required", over: map[string]string{"LINKS_TABLE": "<unset>", "CLICK_QUEUE_URL": "<unset>", "VISITOR_HASH_SALT": "<unset>", "APP_ENV": "<unset>"},
			wantErr: []string{"LINKS_TABLE is required", "CLICK_QUEUE_URL is required", "VISITOR_HASH_SALT is required", "APP_ENV is required"}},
		{name: "short salt", over: map[string]string{"VISITOR_HASH_SALT": "short"}, wantErr: []string{"VISITOR_HASH_SALT must be at least 32"}},
		{name: "aws env requires iam and cloudfront ip", over: map[string]string{"APP_ENV": "prod"},
			wantErr: []string{"CACHE_AUTH must be iam", "CLIENT_IP_SOURCE must be cloudfront"}},
		{name: "iam requires tls user and name", over: map[string]string{"CACHE_AUTH": "iam"},
			wantErr: []string{"CACHE_USER is required", "CACHE_NAME is required", "CACHE_TLS must be true"}},
		{name: "iam valid lowercases cache name", over: map[string]string{"APP_ENV": "dev", "CACHE_AUTH": "iam", "CACHE_TLS": "true", "CACHE_USER": "app", "CACHE_NAME": "Shortener-Dev", "CLIENT_IP_SOURCE": "cloudfront"},
			check: func(t *testing.T, c Redirect) {
				if c.Cache.CacheName != "shortener-dev" || c.Cache.User != "app" {
					t.Fatalf("cache = %+v", c.Cache)
				}
			}},
		{name: "bad types", over: map[string]string{"CLICK_BUFFER": "lots", "CLICK_FLUSH_INTERVAL": "-1s", "CACHE_TLS": "maybe", "LOG_LEVEL": "loud", "CLICK_SENDERS": "1000"},
			wantErr: []string{"CLICK_BUFFER must be an integer", "CLICK_FLUSH_INTERVAL must be a positive duration", "CACHE_TLS must be a boolean", "LOG_LEVEL must be one of", "CLICK_SENDERS must be between"}},
		{name: "log levels", over: map[string]string{"LOG_LEVEL": "debug"}, check: func(t *testing.T, c Redirect) {
			if c.LogLevel != slog.LevelDebug {
				t.Fatalf("level = %v", c.LogLevel)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := LoadRedirect(env(merge(redirectBase, tt.over)))
			assertErr(t, err, tt.wantErr)
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

func TestLoadAPI(t *testing.T) {
	tests := []struct {
		name    string
		over    map[string]string
		wantErr []string
		check   func(t *testing.T, c API)
	}{
		{name: "valid", check: func(t *testing.T, c API) {
			if c.BaseURL.String() != "http://localhost:8080" || c.KeyBlockSize != 1000 || c.WriteLimitPerMin != 60 || c.ReadLimitPerMin != 300 {
				t.Fatalf("unexpected: %+v", c)
			}
			if len(c.BlockedHosts) != 1 || c.BlockedHosts[0] != "localhost" {
				t.Fatalf("blocked hosts = %v", c.BlockedHosts)
			}
		}},
		{name: "blocked hosts list", over: map[string]string{"BLOCKED_HOSTS": " a.example , ,b.example"}, check: func(t *testing.T, c API) {
			if strings.Join(c.BlockedHosts, ",") != "a.example,b.example,localhost" {
				t.Fatalf("blocked hosts = %v", c.BlockedHosts)
			}
		}},
		{name: "base url with path", over: map[string]string{"BASE_URL": "https://x.example/app"}, wantErr: []string{"BASE_URL must be an absolute"}},
		{name: "base url relative", over: map[string]string{"BASE_URL": "x.example"}, wantErr: []string{"BASE_URL must be an absolute"}},
		{name: "base url trailing slash ok", over: map[string]string{"BASE_URL": "https://x.example/"}, check: func(t *testing.T, c API) {
			if c.BaseURL.String() != "https://x.example" {
				t.Fatalf("base = %s", c.BaseURL)
			}
		}},
		{name: "safebrowsing requires key", over: map[string]string{"SCANNER": "safebrowsing"}, wantErr: []string{"SAFE_BROWSING_API_KEY is required"}},
		{name: "bad endpoint", over: map[string]string{"SAFE_BROWSING_ENDPOINT": "ftp://x"}, wantErr: []string{"SAFE_BROWSING_ENDPOINT must be"}},
		{name: "bad alphabet", over: map[string]string{"SQIDS_ALPHABET": "aab"}, wantErr: []string{"SQIDS_ALPHABET must not repeat"}},
		{name: "missing alphabet", over: map[string]string{"SQIDS_ALPHABET": "<unset>"}, wantErr: []string{"SQIDS_ALPHABET is required"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := LoadAPI(env(merge(apiBase, tt.over)))
			assertErr(t, err, tt.wantErr)
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

func TestErrorsNeverContainValues(t *testing.T) {
	secretish := "s3cr3t-value-that-must-not-leak"
	_, err := LoadAPI(env(merge(apiBase, map[string]string{
		"KEY_BLOCK_SIZE": secretish, "SQIDS_ALPHABET": secretish, "BASE_URL": secretish, "SCANNER": secretish,
	})))
	if err == nil || strings.Contains(err.Error(), secretish) {
		t.Fatalf("error leaked a value or was nil: %v", err)
	}
}

func TestSecretNeverPrints(t *testing.T) {
	s := NewSecret("hunter2-hunter2")
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "salt", s)
	b, _ := json.Marshal(struct{ S Secret }{s})
	for _, out := range []string{fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s", s, s, s, s), buf.String(), string(b)} {
		if strings.Contains(out, "hunter2") {
			t.Fatalf("secret leaked: %s", out)
		}
	}
	if s.Reveal() != "hunter2-hunter2" || !s.IsSet() || NewSecret("").IsSet() {
		t.Fatal("Reveal/IsSet broken")
	}
}

func TestValidateAlphabet(t *testing.T) {
	for _, a := range []string{"ab", "ab-c", "abca"} {
		if ValidateAlphabet(a) == nil {
			t.Errorf("ValidateAlphabet(%q) = nil, want error", a)
		}
	}
	if err := ValidateAlphabet("abc123XYZ"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func assertErr(t *testing.T, err error, want []string) {
	t.Helper()
	if len(want) == 0 {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %v", want)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}
}

var workerBase = map[string]string{
	"APP_ENV":         "local",
	"CACHE_ADDR":      "localhost:6379",
	"CACHE_AUTH":      "none",
	"CACHE_TLS":       "false",
	"ANALYTICS_TABLE": "analytics",
	"CLICK_QUEUE_URL": "http://localhost:9324/000000000000/clicks",
	"EVENTS_BUCKET":   "events",
}

func TestLoadWorker(t *testing.T) {
	c, err := LoadWorker(env(workerBase))
	if err != nil || c.FlushInterval != time.Minute || c.MaxBatchBytes != 5<<20 || c.Concurrency != 10 || c.WaitSeconds != 20 || c.Service != "worker" {
		t.Fatalf("worker: %+v %v", c, err)
	}
	_, err = LoadWorker(env(merge(workerBase, map[string]string{"WORKER_FLUSH_INTERVAL": "2m", "EVENTS_BUCKET": "<unset>", "WORKER_WAIT_SECONDS": "x"})))
	assertErr(t, err, []string{"WORKER_FLUSH_INTERVAL must be at most 90s", "EVENTS_BUCKET is required", "WORKER_WAIT_SECONDS must be an integer"})
	_, err = LoadWorker(env(merge(workerBase, map[string]string{"WORKER_WAIT_SECONDS": "21"})))
	assertErr(t, err, []string{"WORKER_WAIT_SECONDS must be between 0 and 20"})
	if c, _ := LoadWorker(env(merge(workerBase, map[string]string{"WORKER_WAIT_SECONDS": "1"}))); c.WaitSeconds != 1 {
		t.Fatal("WORKER_WAIT_SECONDS override")
	}
}

func TestLoadNightly(t *testing.T) {
	base := merge(workerBase, map[string]string{"LINKS_TABLE": "links", "SCANNER": "fake"})
	c, err := LoadNightly(env(base))
	if err != nil || c.Date != "" || c.RescanAge != 7*24*time.Hour || c.ScanSegments != 4 || c.Scanner != "fake" {
		t.Fatalf("nightly: %+v %v", c, err)
	}
	c, err = LoadNightly(env(merge(base, map[string]string{"NIGHTLY_DATE": "2026-10-13"})))
	if err != nil || c.Date != "2026-10-13" {
		t.Fatalf("date: %v", err)
	}
	_, err = LoadNightly(env(merge(base, map[string]string{"NIGHTLY_DATE": "yesterday", "SCANNER": "safebrowsing"})))
	assertErr(t, err, []string{"NIGHTLY_DATE must be a date", "SAFE_BROWSING_API_KEY is required"})
}
