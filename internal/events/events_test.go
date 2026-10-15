package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

func ev(i int) ClickEvent {
	return ClickEvent{V: 1, EventID: fmt.Sprintf("e-%d", i), Code: "abc", TS: time.Unix(1_800_000_000, 0).UTC()}
}

func TestDecode(t *testing.T) {
	b, _ := json.Marshal(ev(1))
	if e, err := Decode(b); err != nil || e.EventID != "e-1" {
		t.Fatalf("decode: %+v %v", e, err)
	}
	for name, raw := range map[string]string{
		"json":    "{",
		"version": `{"v":2,"event_id":"x","code":"c","ts":"2026-10-14T00:00:00Z"}`,
		"id":      `{"v":1,"code":"c","ts":"2026-10-14T00:00:00Z"}`,
		"code":    `{"v":1,"event_id":"x","ts":"2026-10-14T00:00:00Z"}`,
		"ts":      `{"v":1,"event_id":"x","code":"c"}`,
	} {
		if _, err := Decode([]byte(raw)); !errors.Is(err, ErrPoison) {
			t.Errorf("%s: err = %v, want ErrPoison", name, err)
		}
	}
}

type fakeSQS struct {
	mu       sync.Mutex
	bodies   []string
	calls    int
	failIDs  map[string]int // entry id -> remaining failures
	err      error
	block    chan struct{}
	maxBatch int
}

func (f *fakeSQS) SendMessageBatch(ctx context.Context, in *sqs.SendMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(in.Entries) > f.maxBatch {
		f.maxBatch = len(in.Entries)
	}
	if f.err != nil {
		return nil, f.err
	}
	out := &sqs.SendMessageBatchOutput{}
	for _, e := range in.Entries {
		id := aws.ToString(e.Id)
		if f.failIDs[id] > 0 {
			f.failIDs[id]--
			out.Failed = append(out.Failed, types.BatchResultErrorEntry{Id: e.Id})
			continue
		}
		f.bodies = append(f.bodies, aws.ToString(e.MessageBody))
		out.Successful = append(out.Successful, types.SendMessageBatchResultEntry{Id: e.Id})
	}
	return out, nil
}

func (f *fakeSQS) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func newProducer(s SQSSender, buffer int, m Metrics) *Producer {
	return NewProducer(s, ProducerOptions{QueueURL: "q", Buffer: buffer, FlushEvery: 20 * time.Millisecond, Senders: 2}, m, slog.New(slog.DiscardHandler))
}

func TestBatchingAndFlush(t *testing.T) {
	s := &fakeSQS{}
	m := &linkstest.Counter{}
	p := newProducer(s, 1000, m)
	p.Start()
	for i := range 25 {
		if !p.Enqueue(ev(i)) {
			t.Fatal("enqueue failed")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.count() < 25 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if s.count() != 25 || s.maxBatch > 10 || m.Get(telemetry.ClickEventsEnqueued) != 25 {
		t.Fatalf("sent=%d maxBatch=%d enqueued=%v", s.count(), s.maxBatch, m.Get(telemetry.ClickEventsEnqueued))
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Enqueue(ev(99)) || m.Get(telemetry.ClickEventsDropped) != 1 {
		t.Fatal("enqueue after shutdown must drop")
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal("second shutdown is a no-op")
	}
}

func TestShutdownDrainsBuffer(t *testing.T) {
	s := &fakeSQS{}
	p := NewProducer(s, ProducerOptions{QueueURL: "q", Buffer: 1000, FlushEvery: time.Hour, Senders: 1}, &linkstest.Counter{}, slog.New(slog.DiscardHandler))
	p.Start()
	for i := range 7 { // partial batch, flush interval far away
		p.Enqueue(ev(i))
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.count() != 7 {
		t.Fatalf("drained %d of 7", s.count())
	}
}

func TestBufferFullDrops(t *testing.T) {
	s := &fakeSQS{block: make(chan struct{})}
	m := &linkstest.Counter{}
	p := NewProducer(s, ProducerOptions{QueueURL: "q", Buffer: 5, FlushEvery: time.Millisecond, Senders: 1}, m, slog.New(slog.DiscardHandler))
	p.Start()
	start := time.Now()
	accepted := 0
	for i := range 100 {
		if p.Enqueue(ev(i)) {
			accepted++
		}
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Enqueue must never block")
	}
	if m.Get(telemetry.ClickEventsDropped) == 0 || accepted > 16 {
		t.Fatalf("accepted=%d dropped=%v", accepted, m.Get(telemetry.ClickEventsDropped))
	}
	close(s.block)
	_ = p.Shutdown(context.Background())
}

func TestRetryAndFailure(t *testing.T) {
	// Entry "0" fails once then succeeds on retry; "1" always fails.
	s := &fakeSQS{failIDs: map[string]int{"0": 1, "1": 99}}
	m := &linkstest.Counter{}
	p := NewProducer(s, ProducerOptions{QueueURL: "q", Buffer: 10, FlushEvery: time.Hour, Senders: 1}, m, slog.New(slog.DiscardHandler))
	p.Start()
	p.Enqueue(ev(0))
	p.Enqueue(ev(1))
	p.Enqueue(ev(2))
	_ = p.Shutdown(context.Background())
	if s.count() != 2 || m.Get(telemetry.ClickEventsDropped) != 1 || m.Get(telemetry.ClickEventsEnqueued) != 2 {
		t.Fatalf("sent=%d dropped=%v enqueued=%v", s.count(), m.Get(telemetry.ClickEventsDropped), m.Get(telemetry.ClickEventsEnqueued))
	}

	// Queue down: everything is dropped and counted; nothing blocks.
	down := &fakeSQS{err: errors.New("connection refused")}
	m2 := &linkstest.Counter{}
	p2 := NewProducer(down, ProducerOptions{QueueURL: "q", Buffer: 10, FlushEvery: time.Hour, Senders: 1}, m2, slog.New(slog.DiscardHandler))
	p2.Start()
	for i := range 3 {
		p2.Enqueue(ev(i))
	}
	_ = p2.Shutdown(context.Background())
	if m2.Get(telemetry.ClickEventsDropped) != 3 || down.calls != 2 {
		t.Fatalf("dropped=%v calls=%d", m2.Get(telemetry.ClickEventsDropped), down.calls)
	}
}

func TestShutdownDeadline(t *testing.T) {
	s := &fakeSQS{block: make(chan struct{})}
	m := &linkstest.Counter{}
	p := NewProducer(s, ProducerOptions{QueueURL: "q", Buffer: 10, FlushEvery: time.Hour, Senders: 1}, m, slog.New(slog.DiscardHandler))
	p.Start()
	p.Enqueue(ev(1))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := p.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if m.Get(telemetry.ClickEventsDropped) != 1 {
		t.Fatal("aborted events must be counted as dropped")
	}
	// Defaults.
	d := NewProducer(s, ProducerOptions{}, m, slog.New(slog.DiscardHandler))
	if cap(d.ch) != 10_000 || d.opts.Senders != 4 || d.opts.FlushEvery != time.Second {
		t.Fatal("defaults")
	}
}
