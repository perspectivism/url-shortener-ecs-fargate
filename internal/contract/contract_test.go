package contract

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const specPath = "../../api/openapi.yaml"

func Load(path string) (*Validator, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

func respond(t *testing.T, status int, ct, body string, hdr map[string]string) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	if ct != "" {
		rec.Header().Set("Content-Type", ct)
	}
	for k, v := range hdr {
		rec.Header().Set(k, v)
	}
	rec.WriteHeader(status)
	_, _ = rec.WriteString(body)
	resp := rec.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestValidatorCatchesViolations(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/v1/links/abcd", nil)
	good := `{"code":"abcd","short_url":"https://sho.rt/abcd","url":"https://example.com","status":"active","custom":true,"scan_status":"clean","created_at":"2026-10-14T12:00:00.000Z","updated_at":"2026-10-14T12:00:00.000Z"}`
	resp := respond(t, 200, "application/json", good, nil)
	if err := v.Response(get, resp); err != nil {
		t.Fatalf("valid response rejected: %v", err)
	}
	if b, _ := io.ReadAll(resp.Body); string(b) != good {
		t.Fatal("body must remain readable after validation")
	}

	bad := map[string]*http.Response{
		"missing field":     respond(t, 200, "application/json", `{"code":"abcd"}`, nil),
		"extra field":       respond(t, 200, "application/json", strings.Replace(good, `"custom":true`, `"custom":true,"secret":1`, 1), nil),
		"bad enum":          respond(t, 200, "application/json", strings.Replace(good, `"active"`, `"zombie"`, 1), nil),
		"bad timestamp":     respond(t, 200, "application/json", strings.Replace(good, "12:00:00.000Z", "12:00:00Z", 1), nil),
		"undocumented 418":  respond(t, 418, "application/json", `{}`, nil),
		"problem not json":  respond(t, 404, "text/plain", "not found", nil),
		"problem bad code":  respond(t, 404, "application/problem+json", `{"type":"urn:shortener:problem:nope","title":"x","status":404,"code":"nope"}`, nil),
		"problem bad type":  respond(t, 404, "application/problem+json", `{"type":"about:blank","title":"x","status":404,"code":"not_found"}`, nil),
		"wrong media type":  respond(t, 200, "text/html", good, nil),
		"redirect no cache": respond(t, 302, "", "", map[string]string{"Location": "https://example.com"}),
	}
	for name, r := range bad {
		req := get
		if name == "redirect no cache" {
			req = httptest.NewRequest(http.MethodGet, "/abcd", nil)
		}
		if err := v.Response(req, r); err == nil {
			t.Errorf("%s: violation not detected", name)
		}
	}

	post := httptest.NewRequest(http.MethodPost, "/api/v1/links", strings.NewReader(`{"url":"https://example.com","nope":1}`))
	post.Header.Set("Content-Type", "application/json")
	if err := v.Request(post); err == nil {
		t.Error("invalid request not detected")
	}
	if b, _ := io.ReadAll(post.Body); !strings.Contains(string(b), "nope") {
		t.Error("request body must remain readable")
	}
	ok := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	ok.Body = nil
	if err := v.Request(ok); err != nil {
		t.Errorf("valid request rejected: %v", err)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load("does-not-exist.yaml"); err == nil {
		t.Error("missing file")
	}
	dir := t.TempDir()
	for name, content := range map[string]string{
		"garbage.yaml": "::: not yaml :::\n\t-",
		"invalid.yaml": "openapi: 3.1.0\ninfo: {}\npaths: {}\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failingBody) Close() error             { return nil }

func TestBodyReadErrors(t *testing.T) {
	v, err := Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/links", nil)
	req.Body = failingBody{}
	if err := v.Request(req); err == nil {
		t.Error("request body read error")
	}
	resp := respond(t, 200, "application/json", "{}", nil)
	resp.Body = failingBody{}
	if err := v.Response(httptest.NewRequest(http.MethodGet, "/healthz", nil), resp); err == nil {
		t.Error("response body read error")
	}
}
