// Package telemetry provides structured JSON logging (log/slog) and CloudWatch
// metrics via the Embedded Metric Format (EMF) written to stdout. The awslogs
// driver ships stdout to CloudWatch Logs, which extracts EMF metrics; there are
// no PutMetricData calls and no extra network endpoint.
package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metric names (SPEC §13).
const (
	RedirectLatencyMs       = "RedirectLatencyMs"
	CacheHit                = "CacheHit"
	CacheMiss               = "CacheMiss"
	NegativeCacheHit        = "NegativeCacheHit"
	CacheErrors             = "CacheErrors"
	ClickEventsEnqueued     = "ClickEventsEnqueued"
	ClickEventsDropped      = "ClickEventsDropped"
	LinksCreated            = "LinksCreated"
	URLScanFlagged          = "URLScanFlagged"
	URLScanUnavailable      = "URLScanUnavailable"
	URLScanSkipped          = "URLScanSkipped"
	LinksDisabled           = "LinksDisabled"
	WorkerEventsProcessed   = "WorkerEventsProcessed"
	WorkerDuplicateEvents   = "WorkerDuplicateEvents"
	KeyBlocksAllocated      = "KeyBlocksAllocated"
	CacheInvalidationFailed = "CacheInvalidationFailed"
	RateLimited             = "RateLimited"
)

// maxSamples is the EMF limit for array-valued metrics.
const maxSamples = 100

// SyncWriter serialises writes so log lines and EMF documents never interleave.
type SyncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewSyncWriter wraps w.
func NewSyncWriter(w io.Writer) *SyncWriter { return &SyncWriter{w: w} }

func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// NewLogger returns a JSON logger with the standard fields (SPEC §13): ts,
// level, msg, service, env, version.
func NewLogger(w io.Writer, service, env, version string, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				a.Key = "ts"
				a.Value = slog.StringValue(a.Value.Time().UTC().Format(time.RFC3339Nano))
			}
			if len(groups) == 0 && a.Key == slog.LevelKey {
				a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(h).With("service", service, "env", env, "version", version)
}

// Recorder is the metrics interface consumers depend on.
type Recorder interface {
	Count(name string, v float64)
	Observe(name string, v float64)
}

// Discard is a Recorder that drops everything.
type Discard struct{}

// Count implements Recorder.
func (Discard) Count(string, float64) {}

// Observe implements Recorder.
func (Discard) Observe(string, float64) {}

type reservoir struct {
	seen    int
	samples []float64
}

// Metrics aggregates counters and latency samples in memory and writes one EMF
// document per flush.
type Metrics struct {
	mu        sync.Mutex
	w         io.Writer
	namespace string
	service   string
	now       func() time.Time
	rnd       *rand.Rand
	counters  map[string]float64
	samples   map[string]*reservoir
}

// NewMetrics returns an EMF emitter writing to w.
func NewMetrics(w io.Writer, namespace, service string, now func() time.Time) *Metrics {
	return &Metrics{
		w:         w,
		namespace: namespace,
		service:   service,
		now:       now,
		rnd:       rand.New(rand.NewPCG(uint64(now().UnixNano()), 0x5eed)), //nolint:gosec // sampling only, not security relevant
		counters:  map[string]float64{},
		samples:   map[string]*reservoir{},
	}
}

// Count adds v to a counter metric.
func (m *Metrics) Count(name string, v float64) {
	m.mu.Lock()
	m.counters[name] += v
	m.mu.Unlock()
}

// Observe records a sample for a distribution metric (reservoir-sampled to 100
// values per flush, so CloudWatch percentiles are computed over a uniform sample).
func (m *Metrics) Observe(name string, v float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.samples[name]
	if r == nil {
		r = &reservoir{}
		m.samples[name] = r
	}
	r.seen++
	if len(r.samples) < maxSamples {
		r.samples = append(r.samples, v)
		return
	}
	if j := m.rnd.IntN(r.seen); j < maxSamples {
		r.samples[j] = v
	}
}

type metricDef struct {
	Name string `json:"Name"`
	Unit string `json:"Unit"`
}

// Flush writes the aggregated data as one EMF document and resets it. It is a
// no-op when nothing was recorded.
func (m *Metrics) Flush() error {
	m.mu.Lock()
	counters, samples := m.counters, m.samples
	m.counters, m.samples = map[string]float64{}, map[string]*reservoir{}
	m.mu.Unlock()
	if len(counters) == 0 && len(samples) == 0 {
		return nil
	}

	doc := map[string]any{"Service": m.service}
	var defs []metricDef
	for name, v := range counters {
		doc[name] = v
		defs = append(defs, metricDef{Name: name, Unit: "Count"})
	}
	for name, r := range samples {
		doc[name] = r.samples
		defs = append(defs, metricDef{Name: name, Unit: unitFor(name)})
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	doc["_aws"] = map[string]any{
		"Timestamp": m.now().UnixMilli(),
		"CloudWatchMetrics": []map[string]any{{
			"Namespace":  m.namespace,
			"Dimensions": [][]string{{"Service"}},
			"Metrics":    defs,
		}},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = m.w.Write(append(b, '\n'))
	return err
}

func unitFor(name string) string {
	if strings.HasSuffix(name, "Ms") {
		return "Milliseconds"
	}
	return "None"
}

// Run flushes every interval until ctx is done, then flushes once more.
func (m *Metrics) Run(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := m.Flush(); err != nil {
				logger.ErrorContext(ctx, "metrics flush failed", "err", err)
			}
			return
		case <-t.C:
			if err := m.Flush(); err != nil {
				logger.ErrorContext(ctx, "metrics flush failed", "err", err)
			}
		}
	}
}
