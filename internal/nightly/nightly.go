// Package nightly is the scheduled batch job (SPEC §8.3): persist yesterday's
// unique visitors from HyperLogLog, rescan destinations with Safe Browsing and
// auto-disable flagged links. Every step is idempotent, so re-running is safe.
package nightly

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/scanner"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// Uniques reads the day's active codes and HyperLogLog counts.
type Uniques interface {
	ActiveCodes(ctx context.Context, date string, fn func([]string) error) error
	Count(ctx context.Context, code string, dates []string) (map[string]int64, error)
}

// UniquesWriter persists daily unique counts.
type UniquesWriter interface {
	SetUniques(ctx context.Context, code, date string, n int64) error
}

// RescanSource yields links due for a rescan (one Scan segment).
type RescanSource interface {
	ScanRescan(ctx context.Context, segment, totalSegments int32, cutoff time.Time, fn func([]links.Link) error) error
}

// ScanApplier records verdicts (links.Service).
type ScanApplier interface {
	ApplyScan(ctx context.Context, code, scannedURL string, unsafe bool) (bool, error)
}

// Deps wires a Job.
type Deps struct {
	Uniques   Uniques
	Stats     UniquesWriter
	Links     RescanSource
	Scanner   links.Scanner
	Apply     ScanApplier
	Metrics   telemetry.Recorder
	Logger    *slog.Logger
	Now       func() time.Time
	RescanAge time.Duration
	Segments  int32
}

// Summary is the job's outcome (logged as one line).
type Summary struct {
	Date            string
	UniquesWritten  int
	Candidates      int
	Scanned         int
	Flagged         int
	Disabled        int
	ScanSkipped     bool
	ScanUnavailable int
}

// Job runs the nightly steps.
type Job struct{ d Deps }

// New returns a Job.
func New(d Deps) *Job {
	if d.Segments < 1 {
		d.Segments = 1
	}
	return &Job{d: d}
}

// Run processes date (yyyy-mm-dd). A non-nil error means at least one step
// failed (the task exits non-zero, which raises the nightly alarm).
func (j *Job) Run(ctx context.Context, date string) (Summary, error) {
	sum := Summary{Date: date}
	errU := j.persistUniques(ctx, date, &sum)
	errS := j.rescan(ctx, &sum)
	j.d.Logger.InfoContext(ctx, "nightly summary",
		"date", sum.Date, "uniques_written", sum.UniquesWritten, "rescan_candidates", sum.Candidates,
		"scanned", sum.Scanned, "flagged", sum.Flagged, "disabled", sum.Disabled,
		"scan_skipped", sum.ScanSkipped, "scan_unavailable_batches", sum.ScanUnavailable)
	return sum, errors.Join(errU, errS)
}

func (j *Job) persistUniques(ctx context.Context, date string, sum *Summary) error {
	return j.d.Uniques.ActiveCodes(ctx, date, func(codes []string) error {
		for _, code := range codes {
			counts, err := j.d.Uniques.Count(ctx, code, []string{date})
			if err != nil {
				return fmt.Errorf("uniques %s: %w", code, err)
			}
			if err := j.d.Stats.SetUniques(ctx, code, date, counts[date]); err != nil {
				return fmt.Errorf("persist uniques %s: %w", code, err)
			}
			sum.UniquesWritten++
		}
		return nil
	})
}

func (j *Job) rescan(ctx context.Context, sum *Summary) error {
	cutoff := j.d.Now().Add(-j.d.RescanAge)
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		errs    []error
		skipped bool
	)
	for seg := range j.d.Segments {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := j.d.Links.ScanRescan(ctx, seg, j.d.Segments, cutoff, func(page []links.Link) error {
				for start := 0; start < len(page); start += scanner.MaxBatch {
					batch := page[start:min(start+scanner.MaxBatch, len(page))]
					mu.Lock()
					sum.Candidates += len(batch)
					stop := skipped
					mu.Unlock()
					if stop {
						return nil
					}
					if err := j.scanBatch(ctx, batch, sum, &mu); err != nil {
						if errors.Is(err, scanner.ErrDisabled) {
							mu.Lock()
							skipped = true
							mu.Unlock()
							return nil
						}
						return err
					}
				}
				return nil
			})
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if skipped {
		sum.ScanSkipped = true
		j.d.Metrics.Count(telemetry.URLScanSkipped, float64(sum.Candidates))
	}
	return errors.Join(errs...)
}

func (j *Job) scanBatch(ctx context.Context, batch []links.Link, sum *Summary, mu *sync.Mutex) error {
	urls := make([]string, len(batch))
	for i, l := range batch {
		urls[i] = l.LongURL
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	verdicts, err := j.d.Scanner.Check(sctx, urls)
	cancel()
	if errors.Is(err, scanner.ErrDisabled) {
		return err
	}
	if err != nil {
		j.d.Metrics.Count(telemetry.URLScanUnavailable, 1)
		mu.Lock()
		sum.ScanUnavailable++
		mu.Unlock()
		return fmt.Errorf("rescan batch: %w", err)
	}
	for _, l := range batch {
		unsafe := verdicts[l.LongURL].Unsafe
		disabled, err := j.d.Apply.ApplyScan(ctx, l.Code, l.LongURL, unsafe)
		if errors.Is(err, links.ErrNotFound) || errors.Is(err, links.ErrStaleScan) {
			continue // deleted or given a new destination meanwhile
		}
		if err != nil {
			return fmt.Errorf("apply scan %s: %w", l.Code, err)
		}
		mu.Lock()
		sum.Scanned++
		if unsafe {
			sum.Flagged++
			j.d.Metrics.Count(telemetry.URLScanFlagged, 1)
		}
		if disabled {
			sum.Disabled++
			j.d.Logger.InfoContext(ctx, "link disabled: unsafe destination", "code", l.Code)
		}
		mu.Unlock()
	}
	return nil
}

// TargetDate returns override, or yesterday (UTC) relative to now.
func TargetDate(override string, now time.Time) string {
	if override != "" {
		return override
	}
	return now.UTC().AddDate(0, 0, -1).Format(time.DateOnly)
}
