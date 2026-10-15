package nightly

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/scanner"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

var now = time.Date(2026, 10, 15, 3, 15, 0, 0, time.UTC)

type fakeUniques struct {
	active   []string
	counts   map[string]int64
	countErr error
	scanErr  error
}

func (f *fakeUniques) ActiveCodes(_ context.Context, _ string, fn func([]string) error) error {
	if f.scanErr != nil {
		return f.scanErr
	}
	return fn(f.active)
}

func (f *fakeUniques) Count(_ context.Context, code string, dates []string) (map[string]int64, error) {
	if f.countErr != nil {
		return nil, f.countErr
	}
	return map[string]int64{dates[0]: f.counts[code]}, nil
}

type fakeStats struct {
	mu  sync.Mutex
	got map[string]int64
	err error
}

func (f *fakeStats) SetUniques(_ context.Context, code, date string, n int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.got[code+"|"+date] = n
	return nil
}

// memRescan serves rescan candidates from the shared in-memory repo.
type memRescan struct {
	repo *linkstest.Repo
	all  []string
	err  error
}

func (m *memRescan) ScanRescan(ctx context.Context, seg, total int32, cutoff time.Time, fn func([]links.Link) error) error {
	if m.err != nil {
		return m.err
	}
	var page []links.Link
	for i, code := range m.all {
		if int32(i)%total != seg {
			continue
		}
		l, err := m.repo.GetLink(ctx, code)
		if err != nil || l.Status != links.StatusActive {
			continue
		}
		if l.ScanStatus == links.ScanPending || l.LastScannedAt == nil || l.LastScannedAt.Before(cutoff) {
			page = append(page, l)
		}
	}
	if len(page) == 0 {
		return nil
	}
	return fn(page)
}

type world struct {
	job   *Job
	u     *fakeUniques
	stats *fakeStats
	repo  *linkstest.Repo
	cache *linkstest.Cache
	scan  *memRescan
	m     *linkstest.Counter
}

func newWorld(sc links.Scanner, n int) *world {
	w := &world{
		u:     &fakeUniques{active: []string{"a", "b"}, counts: map[string]int64{"a": 5, "b": 0}},
		stats: &fakeStats{got: map[string]int64{}},
		repo:  linkstest.NewRepo(), cache: linkstest.NewCache(), m: &linkstest.Counter{},
	}
	w.scan = &memRescan{repo: w.repo}
	old := now.AddDate(0, 0, -30)
	for i := range n {
		code := fmt.Sprintf("link%03d", i)
		url := fmt.Sprintf("https://ok%d.example/", i)
		if i%10 == 0 {
			url = "https://malware.testing.google.test/testing/malware/" + code
		}
		st := links.ScanPending
		var scanned *time.Time
		if i%3 == 0 {
			st, scanned = links.ScanClean, &old
		}
		w.repo.Put(links.Link{Code: code, LongURL: url, OwnerID: "o", Status: links.StatusActive, ScanStatus: st, LastScannedAt: scanned, UpdatedAt: old})
		w.scan.all = append(w.scan.all, code)
		_ = w.cache.SetLink(context.Background(), code, links.CacheEntry{URL: url, Status: links.StatusActive}, time.Hour)
	}
	// A fresh, clean link that must not be rescanned and a deleted one.
	w.repo.Put(links.Link{Code: "fresh", LongURL: "https://fresh.example/", Status: links.StatusActive, ScanStatus: links.ScanClean, LastScannedAt: &now})
	w.repo.Put(links.Link{Code: "gone", LongURL: "https://malware.testing.google.test/x", Status: links.StatusDeleted, ScanStatus: links.ScanPending})
	w.scan.all = append(w.scan.all, "fresh", "gone")

	svc := links.NewService(links.ServiceDeps{Repo: w.repo, Cache: w.cache, Scanner: sc, Codes: &linkstest.SeqCodes{}, Metrics: w.m, Now: func() time.Time { return now }})
	w.job = New(Deps{
		Uniques: w.u, Stats: w.stats, Links: w.scan, Scanner: sc, Apply: svc, Metrics: w.m,
		Logger: slog.New(slog.DiscardHandler), Now: func() time.Time { return now }, RescanAge: 7 * 24 * time.Hour, Segments: 3,
	})
	return w
}

func TestNightlyRunAndRerunIsNoop(t *testing.T) {
	w := newWorld(scanner.Fake{}, 120)
	sum, err := w.job.Run(context.Background(), "2026-10-14")
	if err != nil {
		t.Fatal(err)
	}
	if w.stats.got["a|2026-10-14"] != 5 || len(w.stats.got) != 2 || sum.UniquesWritten != 2 {
		t.Fatalf("uniques: %v %+v", w.stats.got, sum)
	}
	if sum.Candidates != 120 || sum.Scanned != 120 || sum.Flagged != 12 || sum.Disabled != 12 {
		t.Fatalf("summary = %+v", sum)
	}
	if w.m.Get(telemetry.LinksDisabled) != 12 || w.m.Get(telemetry.URLScanFlagged) != 12 {
		t.Fatal("metrics")
	}
	l, _ := w.repo.GetLink(context.Background(), "link000")
	if l.Status != links.StatusDisabled || l.DisabledReason != links.DisabledReasonUnsafe || l.ScanStatus != links.ScanFlagged {
		t.Fatalf("flagged link = %+v", l)
	}
	if _, ok := w.cache.Entry("link000"); ok {
		t.Fatal("cache entry of the disabled link must be removed")
	}
	if l, _ := w.repo.GetLink(context.Background(), "link001"); l.ScanStatus != links.ScanClean || l.LastScannedAt == nil || !l.LastScannedAt.Equal(now) {
		t.Fatalf("clean link = %+v", l)
	}

	// Re-running is a no-op: same uniques, nothing left to scan or disable.
	sum2, err := w.job.Run(context.Background(), "2026-10-14")
	if err != nil || sum2.Candidates != 0 || sum2.Disabled != 0 || w.stats.got["a|2026-10-14"] != 5 || w.m.Get(telemetry.LinksDisabled) != 12 {
		t.Fatalf("rerun = %+v %v", sum2, err)
	}
}

func TestNightlyScannerDisabledAndFailures(t *testing.T) {
	w := newWorld(scanner.Noop{}, 5)
	sum, err := w.job.Run(context.Background(), "2026-10-14")
	if err != nil || !sum.ScanSkipped || sum.Scanned != 0 || w.m.Get(telemetry.URLScanSkipped) == 0 {
		t.Fatalf("noop: %+v %v", sum, err)
	}

	w = newWorld(errScanner{}, 5)
	sum, err = w.job.Run(context.Background(), "2026-10-14")
	if err == nil || sum.ScanUnavailable == 0 || w.m.Get(telemetry.URLScanUnavailable) == 0 {
		t.Fatalf("scanner down must fail the job (alarm): %+v %v", sum, err)
	}

	for name, mutate := range map[string]func(*world){
		"active codes": func(w *world) { w.u.scanErr = errors.New("valkey down") },
		"count":        func(w *world) { w.u.countErr = errors.New("valkey down") },
		"set uniques":  func(w *world) { w.stats.err = errors.New("throttled") },
		"scan":         func(w *world) { w.scan.err = errors.New("scan failed") },
		"apply":        func(w *world) { w.job.d.Apply = failingApply{} },
	} {
		w := newWorld(scanner.Fake{}, 3)
		mutate(w)
		if _, err := w.job.Run(context.Background(), "2026-10-14"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

type failingApply struct{}

func (failingApply) ApplyScan(context.Context, string, bool) (bool, error) {
	return false, errors.New("dynamodb down")
}

type errScanner struct{}

func (errScanner) Check(context.Context, []string) (map[string]scanner.Verdict, error) {
	return nil, errors.New("safe browsing unavailable")
}

func TestTargetDate(t *testing.T) {
	if TargetDate("", now) != "2026-10-14" || TargetDate("2026-01-01", now) != "2026-01-01" {
		t.Fatal("TargetDate")
	}
	if New(Deps{}).d.Segments != 1 {
		t.Fatal("default segments")
	}
}
