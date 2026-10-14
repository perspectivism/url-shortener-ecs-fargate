package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) Problem {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != ProblemContentType {
		t.Fatalf("content-type = %q", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("bad problem body: %v", err)
	}
	return p
}

func TestWriteProblem(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r = r.WithContext(WithRequestID(r.Context(), "req-1"))
	rec := httptest.NewRecorder()
	WriteProblem(rec, r, http.StatusConflict, CodeAliasTaken, "alias already in use")
	p := decodeProblem(t, rec)
	if rec.Code != 409 || p.Status != 409 || p.Code != "alias_taken" || p.Title != "Conflict" || p.RequestID != "req-1" || p.Type != "urn:shortener:problem:alias_taken" {
		t.Fatalf("unexpected problem: %+v", p)
	}
	rec = httptest.NewRecorder()
	WriteJSON(rec, 201, map[string]string{"a": "b"})
	if rec.Code != 201 || rec.Header().Get("Content-Type") != "application/json" || !strings.Contains(rec.Body.String(), `"a":"b"`) {
		t.Fatalf("WriteJSON: %d %s", rec.Code, rec.Body)
	}
}

func TestRequestID(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { seen = RequestIDFrom(r.Context()) }))
	tests := []struct {
		name, header string
		wantSame     bool
	}{
		{"cloudfront id", "abcDEF123_-==.x", true},
		{"none", "", false},
		{"log injection", "abc\ninjected", false},
		{"too long", strings.Repeat("a", 200), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("X-Amz-Cf-Id", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)
			if tt.wantSame && seen != tt.header {
				t.Fatalf("id = %q, want %q", seen, tt.header)
			}
			if !tt.wantSame && (seen == tt.header || len(seen) != 36) {
				t.Fatalf("expected generated UUID, got %q", seen)
			}
			if rec.Header().Get("X-Request-Id") != seen {
				t.Fatal("X-Request-Id not echoed")
			}
		})
	}
	if RequestIDFrom(context.Background()) != "" {
		t.Fatal("empty context should have no id")
	}
}

func TestRecover(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("kaboom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if p := decodeProblem(t, rec); p.Status != 500 || p.Detail != "" {
		t.Fatalf("problem = %+v", p)
	}
	if !strings.Contains(logs.String(), "kaboom") {
		t.Fatal("panic not logged")
	}

	abort := Recover(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		err, _ := recover().(error)
		if !errors.Is(err, http.ErrAbortHandler) {
			t.Fatalf("ErrAbortHandler must be re-panicked, got %v", err)
		}
	}()
	abort.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	ticks := []time.Time{time.Unix(0, 0), time.Unix(0, int64(1500*time.Microsecond))}
	now := func() time.Time { t := ticks[0]; ticks = ticks[1:]; return t }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x/{id}", func(w http.ResponseWriter, r *http.Request) {
		AddLogAttrs(r.Context(), "code", r.PathValue("id"))
		w.WriteHeader(http.StatusTeapot)
		w.WriteHeader(http.StatusOK) // second call must not change the recorded status
	})
	mux.HandleFunc("GET /healthz", Healthz)
	h := Chain(mux, RequestID, AccessLog(logger, now))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x/abc?secret=1", nil))
	var line map[string]any
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("log line: %v (%s)", err, logs.String())
	}
	if line["route"] != "GET /x/{id}" || line["status"] != float64(418) || line["latency_ms"] != 1.5 || line["code"] != "abc" {
		t.Fatalf("unexpected log: %v", line)
	}
	if strings.Contains(logs.String(), "secret=1") {
		t.Fatal("query string must not be logged")
	}
	logs.Reset()
	ticks = []time.Time{time.Unix(0, 0), time.Unix(0, 0)}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if logs.Len() != 0 {
		t.Fatal("health checks must not be logged")
	}
	AddLogAttrs(context.Background(), "k", "v") // no-op without middleware
}

func TestStatusRecorderDefaults(t *testing.T) {
	rec := &StatusRecorder{ResponseWriter: httptest.NewRecorder()}
	_, _ = rec.Write([]byte("x"))
	if rec.Status != 200 || rec.Unwrap() == nil {
		t.Fatal("implicit 200 not recorded")
	}
}

type payload struct {
	URL string `json:"url"`
	N   int    `json:"n"`
}

func TestDecodeJSON(t *testing.T) {
	tests := []struct {
		name, ct, body string
		wantStatus     int
		wantDetail     string
	}{
		{"ok", "application/json; charset=utf-8", `{"url":"x","n":1}`, 0, ""},
		{"wrong type", "text/plain", `{}`, 415, ""},
		{"missing type", "", `{}`, 415, ""},
		{"empty", "application/json", ``, 400, "empty"},
		{"syntax", "application/json", `{"url":`, 400, "not valid JSON"},
		{"bad syntax", "application/json", `{"url" "x"}`, 400, "not valid JSON"},
		{"field type", "application/json", `{"n":"x"}`, 400, `field "n"`},
		{"unknown", "application/json", `{"zzz":1}`, 400, "unknown field"},
		{"trailing", "application/json", `{"url":"x"} {"url":"y"}`, 400, "single JSON object"},
		{"too large", "application/json", `{"url":"` + strings.Repeat("a", MaxBodyBytes) + `"}`, 413, ""},
		{"too large after object", "application/json", `{"url":"a"}` + strings.Repeat(" ", MaxBodyBytes), 413, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			r.ContentLength = -1 // force the streaming limit path
			if tt.ct != "" {
				r.Header.Set("Content-Type", tt.ct)
			}
			rec := httptest.NewRecorder()
			r.Body = http.MaxBytesReader(rec, r.Body, MaxBodyBytes)
			var p payload
			err := DecodeJSON(r, &p)
			if tt.wantStatus == 0 {
				if err != nil || p.URL != "x" {
					t.Fatalf("err=%v p=%+v", err, p)
				}
				return
			}
			WriteDecodeError(rec, r, err)
			pr := decodeProblem(t, rec)
			if rec.Code != tt.wantStatus || !strings.Contains(pr.Detail, tt.wantDetail) {
				t.Fatalf("got %d %q, want %d containing %q", rec.Code, pr.Detail, tt.wantStatus, tt.wantDetail)
			}
		})
	}
	rec := httptest.NewRecorder()
	WriteDecodeError(rec, httptest.NewRequest(http.MethodPost, "/", nil), errors.New("other"))
	if rec.Code != 400 {
		t.Fatalf("default mapping = %d", rec.Code)
	}
	if (&InvalidJSONError{Detail: "d"}).Error() != "d" {
		t.Fatal("Error()")
	}
}

func TestLimitBodyContentLength(t *testing.T) {
	called := false
	h := LimitBody(10)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 11))))
	if rec.Code != 413 || called {
		t.Fatalf("status %d called %v", rec.Code, called)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("ok")))
	if !called {
		t.Fatal("small body rejected")
	}
}

func TestClientIP(t *testing.T) {
	tests := []struct {
		name, header, remote string
		cf                   bool
		want                 string
		ok                   bool
	}{
		{"cf v4", "203.0.113.9:443", "10.0.0.1:1", true, "203.0.113.9", true},
		{"cf v6 unbracketed", "2001:db8::1:46532", "10.0.0.1:1", true, "2001:db8::1", true},
		{"cf v6 bracketed", "[2001:db8::2]:443", "", true, "2001:db8::2", true},
		{"cf missing", "", "10.0.0.1:1", true, "", false},
		{"cf garbage", "nonsense:1", "", true, "", false},
		{"remote", "203.0.113.9:443", "192.0.2.4:5555", false, "192.0.2.4", true},
		{"remote mapped", "", "[::ffff:192.0.2.5]:1", false, "192.0.2.5", true},
		{"remote no port", "", "192.0.2.6", false, "192.0.2.6", true},
		{"remote garbage", "", "pipe", false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remote
			if tt.header != "" {
				r.Header.Set("CloudFront-Viewer-Address", tt.header)
			}
			a, ok := ClientIP(r, tt.cf)
			if ok != tt.ok || (ok && a.String() != tt.want) {
				t.Fatalf("got %v %v, want %s %v", a, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestServeAndHealthcheck(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", Healthz)
	logger := slog.New(slog.DiscardHandler)
	srv := NewServer(ln.Addr().String(), mux, logger)
	if srv.ReadHeaderTimeout == 0 || srv.MaxHeaderBytes == 0 || srv.IdleTimeout <= 60*time.Second {
		t.Fatal("server timeouts not set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, srv, ln, 10*time.Second) }()

	// No keep-alives: a pooled or speculatively dialed connection would make
	// Shutdown wait (up to 5 s for connections that never sent a request).
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	if code := Healthcheck(context.Background(), ln.Addr().String(), client); code != 0 {
		t.Fatalf("healthcheck = %d", code)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	if code := Healthcheck(context.Background(), ":"+port, client); code != 0 {
		t.Fatalf("healthcheck :port = %d", code)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if code := Healthcheck(context.Background(), ln.Addr().String(), client); code != 1 {
		t.Fatal("healthcheck should fail once the server is down")
	}
	if code := Healthcheck(context.Background(), "bad\x7f", client); code != 1 {
		t.Fatal("bad address should fail")
	}

	// A non-200 health endpoint fails the probe.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer bad.Close()
	if code := Healthcheck(context.Background(), bad.Listener.Addr().String(), client); code != 1 {
		t.Fatal("503 should fail the probe")
	}

	// Serve returns listener errors.
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	_ = ln2.Close()
	if err := Serve(context.Background(), NewServer("", mux, logger), ln2, time.Second); err == nil {
		t.Fatal("expected error from closed listener")
	}
}
