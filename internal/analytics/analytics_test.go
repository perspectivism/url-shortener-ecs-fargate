package analytics

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/events"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/store"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

func TestClassify(t *testing.T) {
	cases := map[string][2]string{
		"": {"unknown", "unknown"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36":                         {"desktop", "Chrome"},
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": {"mobile", "Safari"},
		"Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1":          {"tablet", "Safari"},
		"Googlebot/2.1 (+http://www.google.com/bot.html)":                        {"bot", "Googlebot"},
		"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0": {"desktop", "Firefox"},
		"something-unrecognised":                                                 {"other", "something-unrecognised"},
	}
	for ua, want := range cases {
		d, b := Classify(ua)
		if d != want[0] || b != want[1] {
			t.Errorf("Classify(%q) = %s/%s, want %s/%s", ua, d, b, want[0], want[1])
		}
	}
}

func TestToClick(t *testing.T) {
	ts := time.Date(2026, 10, 14, 23, 59, 59, 0, time.FixedZone("x", 3600))
	c := ToClick(events.ClickEvent{V: 1, EventID: "e1", Code: "abc", TS: ts, Visitor: "v", Country: "unknown",
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"})
	if c.Date != "2026-10-14" || c.Referrer != DirectReferrer || c.Device != "desktop" || c.Browser != "Firefox" || c.Country != "unknown" || c.TS.Location() != time.UTC {
		t.Fatalf("click = %+v", c)
	}
}

type fakeRepo struct {
	days []store.DayRow
	dims map[string][]store.DimRow
	err  error
	derr error
}

func (f fakeRepo) Days(context.Context, string, string, string) ([]store.DayRow, error) {
	return f.days, f.err
}

func (f fakeRepo) Dimension(_ context.Context, _, dim, _, _ string) ([]store.DimRow, error) {
	return f.dims[dim], f.derr
}

type fakeHLL struct {
	counts map[string]int64
	err    error
	asked  []string
}

func (f *fakeHLL) Count(_ context.Context, _ string, dates []string) (map[string]int64, error) {
	f.asked = append(f.asked, dates...)
	return f.counts, f.err
}

func TestReport(t *testing.T) {
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC)
	repo := fakeRepo{
		days: []store.DayRow{
			{Date: "2026-10-01", Clicks: 10, BotClicks: 2, Uniques: 7, HasUniques: true},
			{Date: "2026-10-13", Clicks: 4},
			{Date: "2026-10-14", Clicks: 3, BotClicks: 1},
		},
		dims: map[string][]store.DimRow{
			DimReferrer: {{Date: "2026-10-01", Value: "t.co", Clicks: 5}, {Date: "2026-10-13", Value: "t.co", Clicks: 2}, {Date: "2026-10-13", Value: "direct", Clicks: 7}, {Date: "2026-10-14", Value: "a.example", Clicks: 7}},
		},
	}
	for i := range 15 {
		repo.dims[DimCountry] = append(repo.dims[DimCountry], store.DimRow{Date: "2026-10-14", Value: string(rune('A'+i)) + "X", Clicks: int64(i)})
	}
	hll := &fakeHLL{counts: map[string]int64{"2026-10-13": 3, "2026-10-14": 2}}
	m := &linkstest.Counter{}
	s := NewService(repo, hll, func() time.Time { return now }, m, slog.New(slog.DiscardHandler))

	rep, err := s.Report(context.Background(), "abc", time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Daily) != 14 || rep.From != "2026-10-01" || rep.To != "2026-10-14" || rep.TotalClicks != 17 || rep.TotalBotClicks != 3 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.Daily[0].Uniques != 7 || rep.Daily[12].Uniques != 3 || rep.Daily[13].Uniques != 2 || rep.Daily[5].Clicks != 0 {
		t.Fatalf("daily = %+v", rep.Daily)
	}
	// Only recent days without persisted uniques use the live HLL.
	for _, d := range hll.asked {
		if d < "2026-10-07" {
			t.Fatalf("asked HLL for old day %s", d)
		}
	}
	if r := rep.Top.Referrers; len(r) != 3 || r[0].Value != "a.example" || r[1].Value != "direct" || r[2].Value != "t.co" || r[2].Clicks != 7 {
		t.Fatalf("referrers = %+v (ties broken by value)", r)
	}
	if len(rep.Top.Countries) != TopN || rep.Top.Countries[0].Clicks != 14 || rep.Top.Devices == nil || len(rep.Top.Devices) != 0 {
		t.Fatalf("countries = %+v devices = %#v", rep.Top.Countries, rep.Top.Devices)
	}

	// Live HLL failure degrades to zero uniques.
	hll.err = errors.New("valkey down")
	rep, err = s.Report(context.Background(), "abc", now, now)
	if err != nil || rep.Daily[0].Uniques != 0 || m.Get(telemetry.CacheErrors) != 1 {
		t.Fatalf("degraded: %+v %v", rep, err)
	}

	for name, rng := range map[string][2]time.Time{
		"reversed": {now, now.AddDate(0, 0, -1)},
		"too long": {now.AddDate(0, 0, -90), now},
	} {
		if _, err := s.Report(context.Background(), "abc", rng[0], rng[1]); !errors.Is(err, ErrInvalidRange) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Report(context.Background(), "abc", now.AddDate(0, 0, -89), now); err != nil {
		t.Fatalf("90 days inclusive must be allowed: %v", err)
	}

	bad := NewService(fakeRepo{err: errors.New("boom")}, hll, func() time.Time { return now }, m, slog.New(slog.DiscardHandler))
	if _, err := bad.Report(context.Background(), "abc", now, now); err == nil {
		t.Fatal("days error")
	}
	bad = NewService(fakeRepo{derr: errors.New("boom")}, &fakeHLL{}, func() time.Time { return now }, m, slog.New(slog.DiscardHandler))
	if _, err := bad.Report(context.Background(), "abc", now, now); err == nil {
		t.Fatal("dimension error")
	}
}
