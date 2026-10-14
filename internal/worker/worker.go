// Package worker consumes click events from SQS (ADR-0010): it records each
// event exactly once in DynamoDB, adds human visitors to the day's
// HyperLogLog, and writes raw events to S3 as gzip NDJSON. A message is deleted
// only after BOTH its DynamoDB write and the S3 flush containing it succeed;
// otherwise it is redelivered and the dedupe marker absorbs the replay.
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

// ClickRecorder records clicks idempotently (store.Analytics).
type ClickRecorder interface {
	RecordClick(ctx context.Context, c store.Click) (duplicate bool, err error)
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
	Concurrency   int           // messages processed in parallel per receive (default 10)
	WaitSeconds   int32         // long-poll wait (default 20)
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

	buf      map[string]*bytes.Buffer // partition -> NDJSON
	bufBytes int
	receipts []string
	started  time.Time
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
		w.process(ctx, out.Messages)
		if w.due() {
			w.flush(ctx)
		}
	}
	dctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
	defer cancel()
	if len(w.receipts) > 0 {
		w.flush(dctx)
	}
	return nil
}

func (w *Worker) due() bool {
	return len(w.receipts) > 0 && (w.bufBytes >= w.o.MaxBatchBytes || w.d.Now().Sub(w.started) >= w.o.FlushInterval)
}

type outcome struct {
	ev      events.ClickEvent
	receipt string
	ok      bool
}

// process handles one receive batch concurrently and buffers the successes.
func (w *Worker) process(ctx context.Context, msgs []sqstypes.Message) {
	results := make([]outcome, len(msgs))
	sem := make(chan struct{}, w.o.Concurrency)
	var wg sync.WaitGroup
	for i, m := range msgs {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = w.handle(ctx, m)
		}()
	}
	wg.Wait()
	for _, r := range results {
		if !r.ok {
			continue
		}
		if len(w.receipts) == 0 {
			w.started = w.d.Now()
		}
		w.append(r.ev)
		w.receipts = append(w.receipts, r.receipt)
	}
}

func (w *Worker) handle(ctx context.Context, m sqstypes.Message) outcome {
	ev, err := events.Decode([]byte(aws.ToString(m.Body)))
	if err != nil {
		// Poison: never delete. Make it visible again immediately so SQS moves
		// it to the DLQ after maxReceiveCount instead of after 5 × 120 s.
		w.d.Metrics.Count(WorkerPoisonMessages, 1)
		w.d.Logger.WarnContext(ctx, "poison message; leaving for DLQ", "message_id", aws.ToString(m.MessageId), "err", err)
		if _, verr := w.d.SQS.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
			QueueUrl: aws.String(w.o.QueueURL), ReceiptHandle: m.ReceiptHandle, VisibilityTimeout: 0,
		}); verr != nil {
			w.d.Logger.WarnContext(ctx, "change visibility failed", "err", verr)
		}
		return outcome{}
	}
	c := analytics.ToClick(ev)
	dup, err := w.d.Recorder.RecordClick(ctx, c)
	if err != nil {
		// Transient (throttling, timeouts): leave it; SQS redelivers after the visibility timeout.
		w.d.Metrics.Count(WorkerRetries, 1)
		w.d.Logger.WarnContext(ctx, "record click failed; will retry", "event_id", ev.EventID, "err", err)
		return outcome{}
	}
	if dup {
		w.d.Metrics.Count(telemetry.WorkerDuplicateEvents, 1)
	} else {
		w.d.Metrics.Count(telemetry.WorkerEventsProcessed, 1)
	}
	// Uniques degrade gracefully: a Valkey outage must not push valid events
	// to the DLQ. PFADD is idempotent, so a duplicate replay re-adds harmlessly.
	if err := w.d.Uniques.Add(ctx, c.Code, c.Date, ev.Visitor, !ev.IsBot); err != nil {
		w.d.Metrics.Count(telemetry.CacheErrors, 1)
		w.d.Logger.WarnContext(ctx, "unique visitor update failed", "code", c.Code, "err", err)
	}
	return outcome{ev: ev, receipt: aws.ToString(m.ReceiptHandle), ok: true}
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
