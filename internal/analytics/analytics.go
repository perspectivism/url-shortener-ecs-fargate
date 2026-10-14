// Package analytics turns click events into normalized dimension values and
// assembles per-link statistics reports (ADR-0011).
package analytics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/mileusna/useragent"

	"github.com/shortener/url-shortener-ecs-fargate/internal/events"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
	"github.com/shortener/url-shortener-ecs-fargate/internal/visitor"
)

// Dimension types (sort-key component DIM#{type}#...).
const (
	DimReferrer = "ref"
	DimCountry  = "country"
	DimDevice   = "device"
	DimBrowser  = "browser"
)

// MaxRangeDays is the longest stats range (SPEC §9.2).
const MaxRangeDays = 90

// TopN is how many values each top list returns.
const TopN = 10

// DirectReferrer is the referrer value for visits without a Referer header.
const DirectReferrer = "direct"

// Classify maps a user agent to a device class and browser name.
func Classify(ua string) (device, browser string) {
	if strings.TrimSpace(ua) == "" {
		return visitor.Unknown, visitor.Unknown
	}
	p := useragent.Parse(ua)
	switch {
	case p.Bot:
		device = "bot"
	case p.Tablet:
		device = "tablet"
	case p.Mobile:
		device = "mobile"
	case p.Desktop:
		device = "desktop"
	default:
		device = "other"
	}
	browser = visitor.Normalize(p.Name)
	return device, browser
}

// ToClick converts a click event into the normalized record the worker stores.
func ToClick(e events.ClickEvent) store.Click {
	ts := e.TS.UTC()
	device, browser := Classify(e.UserAgent)
	ref := e.ReferrerHost
	if ref == "" {
		ref = DirectReferrer
	}
	return store.Click{
		EventID:  e.EventID,
		Code:     e.Code,
		Date:     ts.Format(time.DateOnly),
		TS:       ts,
		IsBot:    e.IsBot,
		Referrer: visitor.Normalize(ref),
		Country:  visitor.Normalize(e.Country),
		Device:   device,
		Browser:  browser,
	}
}

// Day is one point of the daily series.
type Day struct {
	Date      string `json:"date"`
	Clicks    int64  `json:"clicks"`
	BotClicks int64  `json:"bot_clicks"`
	Uniques   int64  `json:"uniques"`
}

// Entry is one value in a top list.
type Entry struct {
	Value  string `json:"value"`
	Clicks int64  `json:"clicks"`
}

// Top holds the top values per dimension.
type Top struct {
	Referrers []Entry `json:"referrers"`
	Countries []Entry `json:"countries"`
	Devices   []Entry `json:"devices"`
	Browsers  []Entry `json:"browsers"`
}

// Report is a link's statistics for a date range.
type Report struct {
	Code           string `json:"code"`
	From           string `json:"from"`
	To             string `json:"to"`
	TotalClicks    int64  `json:"total_clicks"`
	TotalBotClicks int64  `json:"total_bot_clicks"`
	Daily          []Day  `json:"daily"`
	Top            Top    `json:"top"`
}

// Repo reads analytics counters.
type Repo interface {
	Days(ctx context.Context, code, from, to string) ([]store.DayRow, error)
	Dimension(ctx context.Context, code, dim, from, to string) ([]store.DimRow, error)
}

// UniqueCounter reads live HyperLogLog counts.
type UniqueCounter interface {
	Count(ctx context.Context, code string, dates []string) (map[string]int64, error)
}

// ErrInvalidRange is returned for an empty or too-long range.
var ErrInvalidRange = errors.New("invalid date range")

// Service builds reports.
type Service struct {
	repo    Repo
	uniques UniqueCounter
	now     func() time.Time
	metrics telemetry.Recorder
	logger  *slog.Logger
}

// NewService returns a stats Service.
func NewService(repo Repo, uniques UniqueCounter, now func() time.Time, metrics telemetry.Recorder, logger *slog.Logger) *Service {
	return &Service{repo: repo, uniques: uniques, now: now, metrics: metrics, logger: logger}
}

// Report returns stats for code over [from, to] (UTC dates, inclusive).
func (s *Service) Report(ctx context.Context, code string, from, to time.Time) (Report, error) {
	from, to = from.UTC().Truncate(24*time.Hour), to.UTC().Truncate(24*time.Hour)
	days := int(to.Sub(from)/(24*time.Hour)) + 1
	if days < 1 || days > MaxRangeDays {
		return Report{}, fmt.Errorf("%w: from must not be after to and the range is at most %d days", ErrInvalidRange, MaxRangeDays)
	}
	f, t := from.Format(time.DateOnly), to.Format(time.DateOnly)

	rows, err := s.repo.Days(ctx, code, f, t)
	if err != nil {
		return Report{}, err
	}
	byDate := make(map[string]store.DayRow, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}

	// Uniques: persisted nightly; recent days not yet persisted come from the live HLL.
	recentFrom := s.now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -7)
	var live []string
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format(time.DateOnly)
		if !byDate[ds].HasUniques && !d.Before(recentFrom) {
			live = append(live, ds)
		}
	}
	counts := map[string]int64{}
	if len(live) > 0 {
		if counts, err = s.uniques.Count(ctx, code, live); err != nil {
			s.metrics.Count(telemetry.CacheErrors, 1)
			s.logger.WarnContext(ctx, "live unique counts unavailable", "code", code, "err", err)
			counts = map[string]int64{}
		}
	}

	rep := Report{Code: code, From: f, To: t, Daily: make([]Day, 0, days)}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		ds := d.Format(time.DateOnly)
		r := byDate[ds]
		u := r.Uniques
		if !r.HasUniques {
			u = counts[ds]
		}
		rep.Daily = append(rep.Daily, Day{Date: ds, Clicks: r.Clicks, BotClicks: r.BotClicks, Uniques: u})
		rep.TotalClicks += r.Clicks
		rep.TotalBotClicks += r.BotClicks
	}

	for _, dim := range []struct {
		name string
		dst  *[]Entry
	}{
		{DimReferrer, &rep.Top.Referrers}, {DimCountry, &rep.Top.Countries},
		{DimDevice, &rep.Top.Devices}, {DimBrowser, &rep.Top.Browsers},
	} {
		dr, err := s.repo.Dimension(ctx, code, dim.name, f, t)
		if err != nil {
			return Report{}, err
		}
		*dim.dst = top(dr, TopN)
	}
	return rep, nil
}

// top sums dimension rows across days and returns the n largest (ties by value).
func top(rows []store.DimRow, n int) []Entry {
	sums := map[string]int64{}
	for _, r := range rows {
		sums[r.Value] += r.Clicks
	}
	out := make([]Entry, 0, len(sums))
	for v, c := range sums {
		out = append(out, Entry{Value: v, Clicks: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Clicks != out[j].Clicks {
			return out[i].Clicks > out[j].Clicks
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
