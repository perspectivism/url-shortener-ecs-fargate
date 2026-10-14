package links_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/shortener/url-shortener-ecs-fargate/internal/scanner"
	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
)

var t0 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

type errScanner struct{ err error }

func (e errScanner) Check(context.Context, []string) (map[string]scanner.Verdict, error) {
	return nil, e.err
}

type env struct {
	svc     *links.Service
	repo    *linkstest.Repo
	cache   *linkstest.Cache
	metrics *linkstest.Counter
	now     *time.Time
}

func newEnv(t *testing.T, sc links.Scanner, codes links.CodeSource) env {
	t.Helper()
	now := t0
	e := env{repo: linkstest.NewRepo(), cache: linkstest.NewCache(), metrics: &linkstest.Counter{}, now: &now}
	if sc == nil {
		sc = scanner.Fake{}
	}
	if codes == nil {
		codes = &linkstest.SeqCodes{}
	}
	e.svc = links.NewService(links.ServiceDeps{
		Repo: e.repo, Cache: e.cache, Scanner: sc, Codes: codes, Metrics: e.metrics,
		Now: func() time.Time { return *e.now }, BlockedHosts: []string{"sho.rt"},
	})
	return e
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	exp := t0.Add(48 * time.Hour).Add(123 * time.Millisecond)
	l, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://Example.com/a", ExpiresAt: &exp})
	if err != nil {
		t.Fatal(err)
	}
	if l.Code != "code000001" || l.LongURL != "https://example.com/a" || l.OwnerID != "alice" || l.Custom ||
		l.Status != links.StatusActive || l.ScanStatus != links.ScanClean || l.LastScannedAt == nil || !l.CreatedAt.Equal(t0) ||
		l.ExpiresAt.Nanosecond() != 0 {
		t.Fatalf("unexpected link %+v", l)
	}
	if e.cache.Deletes != 1 || e.metrics.Get(telemetry.LinksCreated) != 1 {
		t.Fatalf("invalidate/metric: deletes=%d created=%v", e.cache.Deletes, e.metrics.Get(telemetry.LinksCreated))
	}

	// Alias: success then conflict (409).
	if l, err = e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com", Alias: "my-alias"}); err != nil || l.Code != "my-alias" || !l.Custom {
		t.Fatalf("alias create: %+v %v", l, err)
	}
	if _, err = e.svc.Create(ctx, "bob", links.CreateInput{URL: "https://example.com", Alias: "my-alias"}); !errors.Is(err, links.ErrAliasTaken) {
		t.Fatalf("want ErrAliasTaken, got %v", err)
	}

	// Validation failures.
	for name, in := range map[string]links.CreateInput{
		"bad url":      {URL: "ftp://x"},
		"own domain":   {URL: "https://sho.rt/x"},
		"bad alias":    {URL: "https://example.com", Alias: "a b"},
		"reserved":     {URL: "https://example.com", Alias: "admin"},
		"past expiry":  {URL: "https://example.com", ExpiresAt: &t0},
		"unsafe (422)": {URL: "https://malware.testing.google.test/testing/malware/"},
	} {
		if _, err := e.svc.Create(ctx, "alice", in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	if e.metrics.Get(telemetry.URLScanFlagged) != 1 {
		t.Fatal("URLScanFlagged not counted")
	}
}

func TestCreateScanDegradation(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		sc     links.Scanner
		metric string
	}{
		"scanner down":     {errScanner{errors.New("timeout")}, telemetry.URLScanUnavailable},
		"scanner disabled": {scanner.Noop{}, telemetry.URLScanSkipped},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.sc, nil)
			l, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"})
			if err != nil || l.ScanStatus != links.ScanPending || l.LastScannedAt != nil || e.metrics.Get(tc.metric) != 1 {
				t.Fatalf("link=%+v err=%v metric=%v", l, err, e.metrics.Get(tc.metric))
			}
		})
	}
}

func TestCreateGeneratedCollisions(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, &linkstest.Codes{List: []string{"taken1", "taken2", "free01"}})
	for _, c := range []string{"taken1", "taken2"} {
		e.repo.Put(links.Link{Code: c, OwnerID: "x"})
	}
	l, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"})
	if err != nil || l.Code != "free01" {
		t.Fatalf("got %+v %v", l, err)
	}

	// Five consecutive collisions give up.
	e = newEnv(t, nil, &linkstest.Codes{List: []string{"a1", "a2", "a3", "a4", "a5", "a6"}})
	for _, c := range []string{"a1", "a2", "a3", "a4", "a5"} {
		e.repo.Put(links.Link{Code: c})
	}
	if _, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"}); err == nil || !strings.Contains(err.Error(), "collisions") {
		t.Fatalf("expected collision exhaustion, got %v", err)
	}

	// Code source and repository failures propagate.
	e = newEnv(t, nil, &linkstest.Codes{})
	if _, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"}); err == nil {
		t.Fatal("expected code source error")
	}
	e = newEnv(t, nil, nil)
	e.repo.Err = errors.New("dynamodb down")
	if _, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"}); err == nil {
		t.Fatal("expected repo error")
	}
	if _, err := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com", Alias: "alias1"}); err == nil || errors.Is(err, links.ErrAliasTaken) {
		t.Fatalf("expected repo error for alias, got %v", err)
	}
}

func TestInvalidationFailure(t *testing.T) {
	e := newEnv(t, nil, nil)
	e.cache.DelErr = errors.New("valkey down")
	if _, err := e.svc.Create(context.Background(), "alice", links.CreateInput{URL: "https://example.com"}); err != nil {
		t.Fatalf("create must succeed when invalidation fails: %v", err)
	}
	if e.cache.Deletes != 2 || e.metrics.Get(telemetry.CacheInvalidationFailed) != 1 {
		t.Fatalf("deletes=%d failed=%v (want retry once + metric)", e.cache.Deletes, e.metrics.Get(telemetry.CacheInvalidationFailed))
	}
}

func TestGetListDelete(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	l, _ := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"})
	if _, err := e.svc.Get(ctx, "alice", l.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, "bob", l.Code); !errors.Is(err, links.ErrNotFound) {
		t.Fatalf("cross-owner get = %v, want not found", err)
	}
	if _, err := e.svc.Get(ctx, "alice", "nope"); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("missing link")
	}
	if p, err := e.svc.List(ctx, "alice", 10, ""); err != nil || len(p.Items) != 1 {
		t.Fatalf("list: %+v %v", p, err)
	}
	if err := e.svc.Delete(ctx, "bob", l.Code); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("cross-owner delete must be not found")
	}
	if err := e.svc.Delete(ctx, "alice", l.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, "alice", l.Code); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("deleted link must be not found")
	}
	if err := e.svc.Delete(ctx, "alice", l.Code); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("double delete must be not found")
	}
	if stored, _ := e.repo.GetLink(ctx, l.Code); stored.Status != links.StatusDeleted {
		t.Fatal("delete must be soft")
	}
}

func TestUpdate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	l, _ := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com"})
	*e.now = t0.Add(time.Minute)
	newURL := "https://example.org/new"
	exp := t0.Add(24 * time.Hour)
	got, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{URL: &newURL, ExpiresAt: links.OptionalTime{Set: true, Value: &exp}})
	if err != nil || got.LongURL != newURL || got.ExpiresAt == nil || !got.UpdatedAt.Equal(t0.Add(time.Minute)) {
		t.Fatalf("update: %+v %v", got, err)
	}
	// Clear expiry.
	if got, err = e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{ExpiresAt: links.OptionalTime{Set: true}}); err != nil || got.ExpiresAt != nil {
		t.Fatalf("clear expiry: %+v %v", got, err)
	}
	// Same clock: updated_at still strictly increases.
	if got2, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{}); err != nil || !got2.UpdatedAt.After(got.UpdatedAt) {
		t.Fatalf("updated_at must increase: %v", err)
	}
	// Lost race is retried transparently.
	e.repo.ConflictOnce = true
	if _, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{URL: &newURL}); err != nil {
		t.Fatalf("conflict retry: %v", err)
	}
	// Errors.
	bad := "https://malware.testing.google.test/x"
	if _, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{URL: &bad}); !errors.Is(err, links.ErrUnsafeURL) {
		t.Fatalf("unsafe update = %v", err)
	}
	invalidURL := "nope"
	if _, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{URL: &invalidURL}); !errors.Is(err, links.ErrInvalidURL) {
		t.Fatalf("invalid url update = %v", err)
	}
	past := t0
	if _, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{ExpiresAt: links.OptionalTime{Set: true, Value: &past}}); !errors.Is(err, links.ErrInvalidInput) {
		t.Fatalf("past expiry = %v", err)
	}
	if _, err := e.svc.Update(ctx, "bob", l.Code, links.UpdateInput{URL: &newURL}); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("cross-owner update must be not found")
	}
	e.repo.Err = errors.New("boom")
	if _, err := e.svc.Update(ctx, "alice", l.Code, links.UpdateInput{}); err == nil {
		t.Fatal("repo error must propagate")
	}
}

type alwaysConflict struct{ *linkstest.Repo }

func (alwaysConflict) UpdateLink(context.Context, links.Link, time.Time) error {
	return links.ErrConflict
}

type failingUpdate struct{ *linkstest.Repo }

func (failingUpdate) UpdateLink(context.Context, links.Link, time.Time) error {
	return errors.New("throttled")
}

func TestMutateErrors(t *testing.T) {
	ctx := context.Background()
	base := linkstest.NewRepo()
	base.Put(links.Link{Code: "abcd", OwnerID: "alice", Status: links.StatusActive, UpdatedAt: t0})
	for name, repo := range map[string]links.Repository{"conflict": alwaysConflict{base}, "failure": failingUpdate{base}} {
		svc := links.NewService(links.ServiceDeps{Repo: repo, Cache: linkstest.NewCache(), Scanner: scanner.Fake{}, Codes: &linkstest.SeqCodes{}})
		if _, err := svc.Disable(ctx, "abcd", "x"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestAdminDisableEnable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, nil, nil)
	exp := t0.Add(time.Hour)
	l, _ := e.svc.Create(ctx, "alice", links.CreateInput{URL: "https://example.com", ExpiresAt: &exp})
	got, err := e.svc.Disable(ctx, l.Code, "phishing report")
	if err != nil || got.Status != links.StatusDisabled || got.DisabledReason != "phishing report" {
		t.Fatalf("disable: %+v %v", got, err)
	}
	if got, err = e.svc.Enable(ctx, l.Code); err != nil || got.Status != links.StatusActive || got.DisabledReason != "" {
		t.Fatalf("enable: %+v %v", got, err)
	}
	*e.now = t0.Add(2 * time.Hour)
	if _, err := e.svc.Enable(ctx, l.Code); !errors.Is(err, links.ErrGone) {
		t.Fatalf("enable expired = %v, want gone", err)
	}
	_ = e.svc.Delete(ctx, "alice", l.Code)
	if _, err := e.svc.Disable(ctx, l.Code, "x"); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("disable deleted must be not found")
	}
	if _, err := e.svc.Enable(ctx, l.Code); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("enable deleted must be not found")
	}
	if _, err := e.svc.Disable(ctx, "missing", "x"); !errors.Is(err, links.ErrNotFound) {
		t.Fatal("disable missing must be not found")
	}
}

func TestValidationErrorFormatting(t *testing.T) {
	_, err := links.NormalizeURL("ftp://x", nil)
	if !strings.HasPrefix(err.Error(), "invalid url: ") {
		t.Fatalf("Error() = %q", err)
	}
	// Defaults are filled in.
	svc := links.NewService(links.ServiceDeps{Repo: linkstest.NewRepo(), Cache: linkstest.NewCache(), Scanner: scanner.Fake{}, Codes: &linkstest.SeqCodes{}})
	if _, err := svc.Create(context.Background(), "o", links.CreateInput{URL: "https://example.com"}); err != nil {
		t.Fatal(err)
	}
}
