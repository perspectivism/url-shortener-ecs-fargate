package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixed = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return fixed }

func TestLoggerFields(t *testing.T) {
	var buf bytes.Buffer
	l := NewLogger(&buf, "redirect", "dev", "abc123", slog.LevelInfo)
	l.Debug("hidden")
	l.Info("hello", "code", "xyz")
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not one JSON line: %q: %v", buf.String(), err)
	}
	for k, want := range map[string]string{"level": "info", "msg": "hello", "service": "redirect", "env": "dev", "version": "abc123", "code": "xyz"} {
		if got[k] != want {
			t.Errorf("%s = %v, want %v", k, got[k], want)
		}
	}
	if _, ok := got["ts"]; !ok {
		t.Error("missing ts")
	}
	if _, ok := got["time"]; ok {
		t.Error("time key should be renamed to ts")
	}
}

func TestMetricsFlushEMF(t *testing.T) {
	var buf bytes.Buffer
	m := NewMetrics(&buf, "Shortener/dev", "redirect", clock)
	if err := m.Flush(); err != nil || buf.Len() != 0 {
		t.Fatalf("empty flush wrote %q (err %v)", buf.String(), err)
	}
	m.Count(CacheHit, 1)
	m.Count(CacheHit, 2)
	m.Count(CacheMiss, 1)
	for i := range 250 {
		m.Observe(RedirectLatencyMs, float64(i))
	}
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		AWS struct {
			Timestamp         int64
			CloudWatchMetrics []struct {
				Namespace  string
				Dimensions [][]string
				Metrics    []metricDef
			}
		} `json:"_aws"`
		Service           string
		CacheHit          float64
		CacheMiss         float64
		RedirectLatencyMs []float64
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("invalid EMF JSON: %v", err)
	}
	if doc.AWS.Timestamp != fixed.UnixMilli() || doc.Service != "redirect" || doc.CacheHit != 3 || doc.CacheMiss != 1 {
		t.Fatalf("unexpected doc: %+v", doc)
	}
	if n := len(doc.RedirectLatencyMs); n != maxSamples {
		t.Fatalf("samples = %d, want %d", n, maxSamples)
	}
	d := doc.AWS.CloudWatchMetrics[0]
	if d.Namespace != "Shortener/dev" || len(d.Dimensions) != 1 || d.Dimensions[0][0] != "Service" || len(d.Metrics) != 3 {
		t.Fatalf("bad directive: %+v", d)
	}
	for _, md := range d.Metrics {
		want := "Count"
		if md.Name == RedirectLatencyMs {
			want = "Milliseconds"
		}
		if md.Unit != want {
			t.Errorf("%s unit = %s, want %s", md.Name, md.Unit, want)
		}
	}
	buf.Reset()
	if err := m.Flush(); err != nil || buf.Len() != 0 {
		t.Fatal("flush should reset state")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestRunFlushesOnCancel(t *testing.T) {
	var buf bytes.Buffer
	w := NewSyncWriter(&buf)
	m := NewMetrics(w, "Shortener/test", "api", clock)
	m.Count(LinksCreated, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx, time.Millisecond, slog.New(slog.DiscardHandler)); close(done) }()
	time.Sleep(20 * time.Millisecond)
	m.Count(LinksCreated, 1)
	cancel()
	<-done
	if c := strings.Count(buf.String(), `"LinksCreated":1`); c != 2 {
		t.Fatalf("expected two flushed documents, got %d in %q", c, buf.String())
	}

	// A failing writer must not panic or block.
	bad := NewMetrics(errWriter{}, "n", "s", clock)
	bad.Count("X", 1)
	if err := bad.Flush(); err == nil {
		t.Fatal("expected write error")
	}
	bad.Count("X", 1)
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	bad.Run(ctx2, time.Hour, slog.New(slog.DiscardHandler))
}

func TestConcurrentUse(t *testing.T) {
	var buf bytes.Buffer
	m := NewMetrics(NewSyncWriter(&buf), "n", "s", clock)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1000 {
				m.Count(CacheHit, 1)
				m.Observe(RedirectLatencyMs, float64(i))
			}
		}()
	}
	wg.Wait()
	if err := m.Flush(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"CacheHit":8000`) {
		t.Fatalf("lost increments: %s", buf.String()[:200])
	}
	var d Discard
	d.Count("x", 1)
	d.Observe("x", 1)
}
