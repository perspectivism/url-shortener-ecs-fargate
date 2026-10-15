package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

// SQSSender is the subset of the SQS client the producer uses.
type SQSSender interface {
	SendMessageBatch(ctx context.Context, in *sqs.SendMessageBatchInput, opts ...func(*sqs.Options)) (*sqs.SendMessageBatchOutput, error)
}

// Metrics records counters.
type Metrics interface {
	Count(name string, v float64)
}

// maxBatch is the SQS SendMessageBatch limit.
const maxBatch = 10

// sendTimeout bounds each SQS call (AGENTS §8: SQS 1 s).
const sendTimeout = time.Second

// ProducerOptions configures a Producer.
type ProducerOptions struct {
	QueueURL   string
	Buffer     int           // bounded channel size (default 10 000)
	FlushEvery time.Duration // max time an event waits in a partial batch (default 1 s)
	Senders    int           // concurrent batch senders (default 4)
}

// Producer buffers click events and sends them to SQS in batches. Enqueue
// never blocks: when the buffer is full the event is dropped and counted.
// Redirects therefore never wait on, or fail because of, analytics (SPEC N3).
type Producer struct {
	sqs     SQSSender
	opts    ProducerOptions
	metrics Metrics
	logger  *slog.Logger

	ch      chan ClickEvent
	mu      sync.RWMutex
	closed  bool
	wg      sync.WaitGroup
	sendCtx context.Context
	abort   context.CancelFunc
}

// NewProducer returns a Producer; call Start before Enqueue.
func NewProducer(client SQSSender, opts ProducerOptions, metrics Metrics, logger *slog.Logger) *Producer {
	if opts.Buffer <= 0 {
		opts.Buffer = 10_000
	}
	if opts.FlushEvery <= 0 {
		opts.FlushEvery = time.Second
	}
	if opts.Senders <= 0 {
		opts.Senders = 4
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Producer{
		sqs: client, opts: opts, metrics: metrics, logger: logger,
		ch: make(chan ClickEvent, opts.Buffer), sendCtx: ctx, abort: cancel,
	}
}

// Start launches the sender goroutines.
func (p *Producer) Start() {
	for range p.opts.Senders {
		p.wg.Add(1)
		go p.run()
	}
}

// Enqueue adds e to the buffer without blocking. It returns false (and counts
// ClickEventsDropped) when the buffer is full or the producer is shutting down.
func (p *Producer) Enqueue(e ClickEvent) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.metrics.Count(telemetry.ClickEventsDropped, 1)
		return false
	}
	select {
	case p.ch <- e:
		return true
	default:
		p.metrics.Count(telemetry.ClickEventsDropped, 1)
		return false
	}
}

// Shutdown stops accepting events and drains the buffer to SQS. If ctx expires
// first, in-flight sends are aborted and the remainder is counted as dropped.
func (p *Producer) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.ch)
	}
	p.mu.Unlock()

	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
		p.abort()
		return nil
	case <-ctx.Done():
		p.abort()
		<-done
		return ctx.Err()
	}
}

func (p *Producer) run() {
	defer p.wg.Done()
	batch := make([]ClickEvent, 0, maxBatch)
	timer := time.NewTimer(p.opts.FlushEvery)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-p.ch:
			if !ok {
				p.send(batch)
				return
			}
			batch = append(batch, e)
			if len(batch) == maxBatch {
				p.send(batch)
				batch = batch[:0]
				timer.Reset(p.opts.FlushEvery)
			}
		case <-timer.C:
			p.send(batch)
			batch = batch[:0]
			timer.Reset(p.opts.FlushEvery)
		}
	}
}

// send delivers one batch, retrying failed entries once; the rest are dropped.
func (p *Producer) send(batch []ClickEvent) {
	if len(batch) == 0 {
		return
	}
	pending := make(map[string]string, len(batch))
	for i, e := range batch {
		b, err := json.Marshal(e)
		if err != nil { // cannot happen for this struct; never fail the redirect path
			p.metrics.Count(telemetry.ClickEventsDropped, 1)
			continue
		}
		pending[strconv.Itoa(i)] = string(b)
	}
	for attempt := 0; attempt < 2 && len(pending) > 0; attempt++ {
		entries := make([]types.SendMessageBatchRequestEntry, 0, len(pending))
		for id, body := range pending {
			entries = append(entries, types.SendMessageBatchRequestEntry{Id: aws.String(id), MessageBody: aws.String(body)})
		}
		ctx, cancel := context.WithTimeout(p.sendCtx, sendTimeout)
		out, err := p.sqs.SendMessageBatch(ctx, &sqs.SendMessageBatchInput{QueueUrl: &p.opts.QueueURL, Entries: entries})
		cancel()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				p.logger.Warn("click batch send failed", "events", len(pending), "attempt", attempt+1, "err", err)
			}
			continue
		}
		for _, ok := range out.Successful {
			delete(pending, aws.ToString(ok.Id))
		}
		p.metrics.Count(telemetry.ClickEventsEnqueued, float64(len(out.Successful)))
	}
	if n := len(pending); n > 0 {
		p.metrics.Count(telemetry.ClickEventsDropped, float64(n))
		p.logger.Warn("click events dropped after retry", "events", n)
	}
}
