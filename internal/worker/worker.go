// Package worker consumes click events from SQS (ADR-0010, ADR-0024): it
// counts each event exactly once in DynamoDB, adds human visitors to the day's
// HyperLogLog, and writes raw events to S3 as gzip NDJSON.
//
// Counting is batched: decoded events wait up to CountInterval (or until
// CountMaxEvents) and are then committed together, so a hot link's counters
// are written once per transaction instead of once per click. A message is
// deleted only after BOTH its counting transaction and the S3 flush containing
// it succeed; otherwise it is redelivered and the dedupe marker absorbs the
// replay.
package worker

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/shortener/url-shortener-ecs-fargate/internal/analytics"
	"github.com/shortener/url-shortener-ecs-fargate/internal/events"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
)

// Metric names specific to the worker (in addition to SPEC §13's list).
const (
	WorkerPoisonMessages = "WorkerPoisonMessages"
	WorkerRetries        = "WorkerRetries"
	WorkerS3FlushFailed  = "WorkerS3FlushFailed"
	// WorkerCountTransactions counts committed counting transactions; divided
	// by WorkerEventsProcessed it shows how many clicks each write carries.
	WorkerCountTransactions = "WorkerCountTransactions"
)

// SQS is the subset of the SQS client the worker uses.
type SQS interface {
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, opts ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessageBatch(ctx context.Context, in *sqs.DeleteMessageBatchInput, opts ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error)
	ChangeMessageVisibility(ctx context.Context, in *sqs.ChangeMessageVisibilityInput, opts ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

// ObjectPutter writes S3 objects.
type ObjectPutter interface {
	PutObject(ctx context.Context, in *s3.PutObjectInput, opts ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// ClickRecorder counts clicks idempotently in batches (store.Analytics).
type ClickRecorder interface {
	RecordClicks(ctx context.Context, clicks []store.Click) (store.BatchResult, error)
}

// UniqueAdder adds visitors to the day's HyperLogLog (cache.Uniques).
type UniqueAdder interface {
	Add(ctx context.Context, code, date, visitor string, human bool) error
}

// Options configures a Worker.
type Options struct {
	QueueURL      string
	Bucket        string
	TaskID        string
	FlushInterval time.Duration // default 60 s
	MaxBatchBytes int           // default 5 MiB (uncompressed)
	Concurrency   int           // parallel HyperLogLog updates (default 10)
	WaitSeconds   int32         // long-poll wait (default 20)
	// CountInterval is how long decoded events may wait before their counting
	// transaction (default 2 s); CountMaxEvents commits earlier (default 1000).
	CountInterval  time.Duration
	CountMaxEvents int
}

// Deps wires a Worker.
type Deps struct {
	SQS      SQS
	S3       ObjectPutter
	Recorder ClickRecorder
	Uniques  UniqueAdder
	Metrics  telemetry.Recorder
	Logger   *slog.Logger
	Now      func() time.Time
	NewID    func() string
}

// Worker is the consumer loop.
type Worker struct {
	o         Options
	d         Deps
	heartbeat atomic.Int64 // unix nanos of the last receive attempt

	// Decoded events waiting for their counting transaction.
	pending        []pendingEvent
	pendingStarted time.Time

	// Counted events waiting for the S3 flush (and then deletion).
	buf      map[string]*bytes.Buffer // partition -> NDJSON
	bufBytes int
	receipts []string
	started  time.Time
}

type pendingEvent struct {
	ev      events.ClickEvent
	click   store.Click
	receipt string
}

// New returns a Worker.
func New(o Options, d Deps) *Worker {
	if o.FlushInterval <= 0 {
		o.FlushInterval = 60 * time.Second
	}
	if o.MaxBatchBytes <= 0 {
		o.MaxBatchBytes = 5 << 20
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 10
	}
	if o.CountInterval <= 0 {
		o.CountInterval = 2 * time.Second
	}
	if o.CountMaxEvents <= 0 {
		o.CountMaxEvents = 1000
	}
	w := &Worker{o: o, d: d, buf: map[string]*bytes.Buffer{}}
	w.heartbeat.Store(d.Now().UnixNano())
	return w
}

// Healthy reports whether the receive loop ran within maxAge.
func (w *Worker) Healthy(maxAge time.Duration) bool {
	return w.d.Now().Sub(time.Unix(0, w.heartbeat.Load())) <= maxAge
}

// Run consumes until ctx is cancelled, then flushes the buffer and deletes
// the messages it covers (within drainTimeout).
func (w *Worker) Run(ctx context.Context, drainTimeout time.Duration) error {
	for ctx.Err() == nil {
		w.heartbeat.Store(w.d.Now().UnixNano())
		out, err := w.d.SQS.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(w.o.QueueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     w.o.WaitSeconds,
		})
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			w.d.Logger.WarnContext(ctx, "receive failed", "err", err)
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		w.decode(ctx, out.Messages)
		// Count when the window is full or old, and as soon as the queue is
		// quiet (an empty long poll) so stats stay fresh at low traffic.
		if len(w.pending) > 0 && (len(out.Messages) == 0 || w.countDue()) {
			w.count(ctx)
		}
		if w.due() {
			w.count(ctx) // the flush covers everything decoded so far
			w.flush(ctx)
		}
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
	defer cancel()
	w.count(dctx)
	if len(w.receipts) > 0 {
		w.flush(dctx)
	}
	return nil
}

func (w *Worker) countDue() bool {
	return len(w.pending) >= w.o.CountMaxEvents || w.d.Now().Sub(w.pendingStarted) >= w.o.CountInterval
}

func (w *Worker) due() bool {
	return len(w.receipts) > 0 && (w.bufBytes >= w.o.MaxBatchBytes || w.d.Now().Sub(w.started) >= w.o.FlushInterval)
}

// decode parses a receive batch into pending events. Poison messages are
// never deleted: they become visible again at once so SQS moves them to the
// DLQ after maxReceiveCount instead of after 5 × 120 s.
func (w *Worker) decode(ctx context.Context, msgs []sqstypes.Message) {
	for _, m := range msgs {
		ev, err := events.Decode([]byte(aws.ToString(m.Body)))
		if err != nil {
			w.d.Metrics.Count(WorkerPoisonMessages, 1)
			w.d.Logger.WarnContext(ctx, "poison message; leaving for DLQ", "message_id", aws.ToString(m.MessageId), "err", err)
			if _, verr := w.d.SQS.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl: aws.String(w.o.QueueURL), ReceiptHandle: m.ReceiptHandle, VisibilityTimeout: 0,
			}); verr != nil {
				w.d.Logger.WarnContext(ctx, "change visibility failed", "err", verr)
			}
			continue
		}
		if len(w.pending) == 0 {
			w.pendingStarted = w.d.Now()
		}
		w.pending = append(w.pending, pendingEvent{ev: ev, click: analytics.ToClick(ev), receipt: aws.ToString(m.ReceiptHandle)})
	}
}

// count commits the pending events in batched transactions. Counted and
// duplicate events move to the S3 buffer (their messages are deleted after
// the flush); failed ones are dropped here and redelivered by SQS after the
// visibility timeout.
//
// Visitors are added to the HyperLogLog BEFORE the counters commit, so stats
// never show a click whose visitor is missing from today's uniques. PFADD is
// idempotent: a click that fails to count and is redelivered re-adds harmlessly.
func (w *Worker) count(ctx context.Context) {
	if len(w.pending) == 0 {
		return
	}
	batch := w.pending
	w.pending = nil
	w.addUniques(ctx, batch)
	clicks := make([]store.Click, len(batch))
	for i, p := range batch {
		clicks[i] = p.click
	}
	res, err := w.d.Recorder.RecordClicks(ctx, clicks)
	if res.Transactions > 0 {
		w.d.Metrics.Count(WorkerCountTransactions, float64(res.Transactions))
	}
	if err != nil {
		w.d.Logger.WarnContext(ctx, "counting failed for some events; they will be redelivered", "events", len(batch), "err", err)
	}
	var kept []pendingEvent
	for i, p := range batch {
		switch r := result(res, i); r {
		case store.Failed:
			// Transient (throttling, timeouts): leave it; SQS redelivers it.
			w.d.Metrics.Count(WorkerRetries, 1)
			continue
		case store.Duplicate:
			w.d.Metrics.Count(telemetry.WorkerDuplicateEvents, 1)
		default:
			w.d.Metrics.Count(telemetry.WorkerEventsProcessed, 1)
		}
		kept = append(kept, p)
	}
	for _, p := range kept {
		if len(w.receipts) == 0 {
			w.started = w.d.Now()
		}
		w.append(p.ev)
		w.receipts = append(w.receipts, p.receipt)
	}
}

// result is the outcome for event i; a recorder that returned fewer results
// (e.g. on a hard error) leaves the rest failed.
func result(res store.BatchResult, i int) store.ClickResult {
	if i < len(res.Results) {
		return res.Results[i]
	}
	return store.Failed
}

// addUniques adds visitors to the day's HyperLogLog in parallel. Uniques
// degrade gracefully: a Valkey outage must not push valid events to the DLQ.
// PFADD is idempotent, so a duplicate replay re-adds harmlessly.
func (w *Worker) addUniques(ctx context.Context, batch []pendingEvent) {
	sem := make(chan struct{}, w.o.Concurrency)
	var wg sync.WaitGroup
	for _, p := range batch {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := w.d.Uniques.Add(ctx, p.click.Code, p.click.Date, p.ev.Visitor, !p.ev.IsBot); err != nil {
				w.d.Metrics.Count(telemetry.CacheErrors, 1)
				w.d.Logger.WarnContext(ctx, "unique visitor update failed", "code", p.click.Code, "err", err)
			}
		}()
	}
	wg.Wait()
}

// append adds the raw event to its hourly partition buffer. Duplicates are
// written too: a redelivered message means its earlier S3 flush did not
// complete, and Athena queries deduplicate by event_id.
func (w *Worker) append(ev events.ClickEvent) {
	ts := ev.TS.UTC()
	part := "dt=" + ts.Format(time.DateOnly) + "/hour=" + ts.Format("15")
	b := w.buf[part]
	if b == nil {
		b = &bytes.Buffer{}
		w.buf[part] = b
	}
	line, _ := json.Marshal(ev) // a decoded ClickEvent always marshals
	b.Write(line)
	b.WriteByte('\n')
	w.bufBytes += len(line) + 1
}

// flush writes every partition to S3 and, only if all succeed, deletes the
// covered messages. On failure the batch is discarded and SQS redelivers it.
func (w *Worker) flush(ctx context.Context) {
	defer w.reset()
	for part, b := range w.buf {
		if err := w.put(ctx, part, b.Bytes()); err != nil {
			w.d.Metrics.Count(WorkerS3FlushFailed, 1)
			w.d.Logger.ErrorContext(ctx, "s3 flush failed; batch will be redelivered", "partition", part, "messages", len(w.receipts), "err", err)
			return
		}
	}
	w.delete(ctx, w.receipts)
}

func (w *Worker) reset() {
	w.buf = map[string]*bytes.Buffer{}
	w.bufBytes = 0
	w.receipts = nil
}

func (w *Worker) put(ctx context.Context, partition string, ndjson []byte) error {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(ndjson); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	key := fmt.Sprintf("events/%s/%s-%s.json.gz", partition, w.o.TaskID, w.d.NewID())
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := w.d.S3.PutObject(pctx, &s3.PutObjectInput{
		Bucket:      aws.String(w.o.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(gz.Bytes()),
		ContentType: aws.String("application/gzip"),
	})
	if err != nil {
		return fmt.Errorf("put %s: %w", key, err)
	}
	return nil
}

func (w *Worker) delete(ctx context.Context, receipts []string) {
	for start := 0; start < len(receipts); start += 10 {
		chunk := receipts[start:min(start+10, len(receipts))]
		entries := make([]sqstypes.DeleteMessageBatchRequestEntry, len(chunk))
		for i, r := range chunk {
			entries[i] = sqstypes.DeleteMessageBatchRequestEntry{Id: aws.String(strconv.Itoa(i)), ReceiptHandle: aws.String(r)}
		}
		dctx, cancel := context.WithTimeout(ctx, time.Second*5)
		out, err := w.d.SQS.DeleteMessageBatch(dctx, &sqs.DeleteMessageBatchInput{QueueUrl: aws.String(w.o.QueueURL), Entries: entries})
		cancel()
		if err == nil && len(out.Failed) > 0 {
			err = errors.New(aws.ToString(out.Failed[0].Message))
		}
		if err != nil {
			// Harmless: undeleted messages are redelivered and deduplicated.
			w.d.Logger.WarnContext(ctx, "delete messages failed; they will be redelivered", "messages", len(chunk), "err", err)
		}
	}
}
