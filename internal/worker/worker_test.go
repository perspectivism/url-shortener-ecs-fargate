package worker

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/events"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links/linkstest"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/store"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/telemetry"
)

type fakeQueue struct {
	mu        sync.Mutex
	onReceive func(call int) // test hook, called before each receive
	calls     int
	batches   [][]sqstypes.Message
	deleted   []string
	visible0  []string
	recvErr   error
	deleteErr error
	failedDel bool
	cancel    context.CancelFunc
}

func (q *fakeQueue) ReceiveMessage(ctx context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	if q.onReceive != nil {
		q.onReceive(q.calls)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.calls++
	if q.recvErr != nil {
		err := q.recvErr
		q.recvErr = nil
		return nil, err
	}
	if len(q.batches) == 0 {
		if q.cancel != nil {
			q.cancel()
		}
		return nil, ctx.Err()
	}
	b := q.batches[0]
	q.batches = q.batches[1:]
	return &sqs.ReceiveMessageOutput{Messages: b}, nil
}

func (q *fakeQueue) DeleteMessageBatch(_ context.Context, in *sqs.DeleteMessageBatchInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageBatchOutput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.deleteErr != nil {
		return nil, q.deleteErr
	}
	out := &sqs.DeleteMessageBatchOutput{}
	if q.failedDel {
		out.Failed = []sqstypes.BatchResultErrorEntry{{Id: aws.String("0"), Message: aws.String("receipt expired")}}
	}
	for _, e := range in.Entries {
		q.deleted = append(q.deleted, aws.ToString(e.ReceiptHandle))
	}
	return out, nil
}

func (q *fakeQueue) ChangeMessageVisibility(_ context.Context, in *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.visible0 = append(q.visible0, aws.ToString(in.ReceiptHandle))
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	err     error
}

func (f *fakeS3) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	b, _ := io.ReadAll(in.Body)
	f.objects[aws.ToString(in.Key)] = b
	return &s3.PutObjectOutput{}, nil
}

type fakeRecorder struct {
	mu       sync.Mutex
	seen     map[string]bool
	err      error           // every click fails
	fail     map[string]bool // these event IDs fail
	batches  []int           // size of every RecordClicks call
	onCommit func()          // test hook, called when RecordClicks runs
}

func (f *fakeRecorder) RecordClicks(_ context.Context, clicks []store.Click) (store.BatchResult, error) {
	if f.onCommit != nil {
		f.onCommit()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, len(clicks))
	res := store.BatchResult{Results: make([]store.ClickResult, len(clicks))}
	var err error
	for i, c := range clicks {
		switch {
		case f.err != nil:
			res.Results[i], err = store.Failed, f.err
		case f.fail[c.EventID]:
			res.Results[i], err = store.Failed, errors.New("throttled")
		case f.seen[c.EventID]:
			res.Results[i] = store.Duplicate
		default:
			f.seen[c.EventID] = true
			res.Results[i] = store.Recorded
		}
	}
	for _, r := range res.Results {
		if r == store.Recorded {
			res.Transactions = 1
		}
	}
	return res, err
}

type fakeUniques struct {
	mu    sync.Mutex
	adds  int
	human int
	err   error
}

func (f *fakeUniques) Add(_ context.Context, _, _, _ string, human bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.adds++
	if human {
		f.human++
	}
	return f.err
}

func msg(id string, ev any) sqstypes.Message {
	var body string
	switch v := ev.(type) {
	case string:
		body = v
	default:
		b, _ := json.Marshal(v)
		body = string(b)
	}
	return sqstypes.Message{MessageId: aws.String(id), ReceiptHandle: aws.String("rh-" + id), Body: aws.String(body)}
}

func event(id string, hour int, bot bool) events.ClickEvent {
	return events.ClickEvent{V: 1, EventID: id, Code: "abc", TS: time.Date(2026, 10, 14, hour, 30, 0, 0, time.UTC), Visitor: "v-" + id, IsBot: bot, Country: "DE"}
}

type rig struct {
	w   *Worker
	q   *fakeQueue
	s3  *fakeS3
	rec *fakeRecorder
	u   *fakeUniques
	m   *linkstest.Counter
	now *time.Time
}

func newRig(flushEvery time.Duration) *rig {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	r := &rig{q: &fakeQueue{}, s3: &fakeS3{objects: map[string][]byte{}}, rec: &fakeRecorder{seen: map[string]bool{}}, u: &fakeUniques{}, m: &linkstest.Counter{}, now: &now}
	n := 0
	r.w = New(Options{QueueURL: "q", Bucket: "b", TaskID: "task1", FlushInterval: flushEvery, Concurrency: 4}, Deps{
		SQS: r.q, S3: r.s3, Recorder: r.rec, Uniques: r.u, Metrics: r.m, Logger: slog.New(slog.DiscardHandler),
		Now:   func() time.Time { return *r.now },
		NewID: func() string { n++; return fmt.Sprintf("obj%d", n) },
	})
	return r
}

func (r *rig) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r.q.cancel = cancel
	if err := r.w.Run(ctx, time.Second); err != nil {
		t.Fatal(err)
	}
}

func gunzipLines(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(zr)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func TestProcessesFlushesAndDeletes(t *testing.T) {
	r := newRig(time.Hour)
	r.q.batches = [][]sqstypes.Message{
		{msg("1", event("e1", 10, false)), msg("2", event("e2", 11, false)), msg("3", event("e3", 11, true))},
		{msg("4", event("e1", 10, false))}, // redelivered duplicate
	}
	r.run(t)
	if len(r.q.deleted) != 4 {
		t.Fatalf("deleted = %v, want all 4 after the shutdown flush", r.q.deleted)
	}
	if r.m.Get(telemetry.WorkerEventsProcessed) != 3 || r.m.Get(telemetry.WorkerDuplicateEvents) != 1 {
		t.Fatalf("processed=%v dup=%v", r.m.Get(telemetry.WorkerEventsProcessed), r.m.Get(telemetry.WorkerDuplicateEvents))
	}
	if r.u.adds != 4 || r.u.human != 3 {
		t.Fatalf("uniques adds=%d human=%d (bots are not humans)", r.u.adds, r.u.human)
	}
	// One object per hourly partition; duplicates are written too (Athena dedupes).
	var total int
	for key, body := range r.s3.objects {
		if !strings.HasPrefix(key, "events/dt=2026-10-14/hour=1") || !strings.HasSuffix(key, ".json.gz") || !strings.Contains(key, "task1-obj") {
			t.Errorf("bad key %s", key)
		}
		total += len(gunzipLines(t, body))
	}
	if len(r.s3.objects) != 2 || total != 4 {
		t.Fatalf("objects=%d lines=%d", len(r.s3.objects), total)
	}
}

func TestNoDeleteBeforeFlushAndFlushOnInterval(t *testing.T) {
	r := newRig(time.Minute)
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	ctx := context.Background()
	out, _ := r.q.ReceiveMessage(ctx, nil)
	r.w.decode(ctx, out.Messages)
	r.w.count(ctx)
	if r.w.due() || len(r.q.deleted) != 0 {
		t.Fatal("must not flush or delete before the interval")
	}
	*r.now = r.now.Add(time.Minute)
	if !r.w.due() {
		t.Fatal("flush is due after the interval")
	}
	r.w.flush(ctx)
	if len(r.q.deleted) != 1 || len(r.s3.objects) != 1 {
		t.Fatalf("deleted=%v objects=%d", r.q.deleted, len(r.s3.objects))
	}

	// Size-triggered flush.
	r2 := newRig(time.Hour)
	r2.w.o.MaxBatchBytes = 10
	r2.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	out, _ = r2.q.ReceiveMessage(ctx, nil)
	r2.w.decode(ctx, out.Messages)
	r2.w.count(ctx)
	if !r2.w.due() {
		t.Fatal("flush is due when the buffer exceeds the byte limit")
	}
}

func TestS3FailureMeansNoDelete(t *testing.T) {
	r := newRig(time.Hour)
	r.s3.err = errors.New("s3 down")
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false)), msg("2", event("e2", 10, false))}}
	r.run(t)
	if len(r.q.deleted) != 0 || r.m.Get(WorkerS3FlushFailed) != 1 {
		t.Fatalf("deleted=%v flushFailed=%v: messages must be redelivered when S3 fails", r.q.deleted, r.m.Get(WorkerS3FlushFailed))
	}
	if len(r.w.receipts) != 0 || r.w.bufBytes != 0 {
		t.Fatal("failed batch must be discarded")
	}
}

func TestPoisonAndTransientFailures(t *testing.T) {
	r := newRig(time.Hour)
	r.q.batches = [][]sqstypes.Message{{
		msg("bad-json", "{not json"),
		msg("bad-version", `{"v":9,"event_id":"x","code":"c","ts":"2026-10-14T00:00:00Z"}`),
		msg("good", event("e1", 10, false)),
	}}
	r.run(t)
	if len(r.q.visible0) != 2 || r.m.Get(WorkerPoisonMessages) != 2 {
		t.Fatalf("poison: visible0=%v", r.q.visible0)
	}
	if len(r.q.deleted) != 1 || r.q.deleted[0] != "rh-good" {
		t.Fatalf("only the good message may be deleted: %v", r.q.deleted)
	}

	// DynamoDB failure: no delete, counted as a retry.
	r = newRig(time.Hour)
	r.rec.err = errors.New("throttled")
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	r.run(t)
	if len(r.q.deleted) != 0 || r.m.Get(WorkerRetries) != 1 {
		t.Fatalf("transient failure must not delete: %v", r.q.deleted)
	}

	// Valkey failure degrades uniques but still counts and deletes.
	r = newRig(time.Hour)
	r.u.err = errors.New("valkey down")
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	r.run(t)
	if len(r.q.deleted) != 1 || r.m.Get(telemetry.CacheErrors) != 1 {
		t.Fatalf("valkey failure: deleted=%v", r.q.deleted)
	}
}

func TestReceiveErrorsAndDeleteFailures(t *testing.T) {
	r := newRig(time.Hour)
	r.q.recvErr = errors.New("throttled")
	r.q.deleteErr = errors.New("delete failed")
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	r.run(t) // must survive a receive error and log delete failures
	if len(r.s3.objects) != 1 {
		t.Fatal("event must still be flushed")
	}
	r = newRig(time.Hour)
	r.q.failedDel = true
	r.q.batches = [][]sqstypes.Message{{msg("1", event("e1", 10, false))}}
	r.run(t)

	// More than 10 receipts are deleted in chunks of 10.
	r = newRig(time.Hour)
	var batch []sqstypes.Message
	for i := range 10 {
		batch = append(batch, msg(fmt.Sprint(i), event(fmt.Sprint("e", i), 10, false)))
	}
	r.q.batches = [][]sqstypes.Message{batch, {msg("x", event("ex", 10, false))}}
	r.run(t)
	if len(r.q.deleted) != 11 {
		t.Fatalf("deleted = %d", len(r.q.deleted))
	}
}

func TestHealthyAndDefaults(t *testing.T) {
	r := newRig(0)
	if !r.w.Healthy(time.Minute) {
		t.Fatal("fresh worker is healthy")
	}
	*r.now = r.now.Add(3 * time.Minute)
	if r.w.Healthy(2 * time.Minute) {
		t.Fatal("stalled loop is unhealthy")
	}
	if r.w.o.FlushInterval != time.Minute || r.w.o.MaxBatchBytes != 5<<20 || r.w.o.CountInterval != 2*time.Second || r.w.o.CountMaxEvents != 1000 {
		t.Fatal("defaults")
	}
	w := New(Options{}, Deps{Now: time.Now})
	if w.o.Concurrency != 10 {
		t.Fatal("default concurrency")
	}
}

func batchOf(prefix string, n int) []sqstypes.Message {
	out := make([]sqstypes.Message, n)
	for i := range out {
		id := fmt.Sprintf("%s%d", prefix, i)
		out[i] = msg(id, event("e-"+id, 10, false))
	}
	return out
}

// Events from several receives are counted together: one RecordClicks call
// (and here one transaction) for 30 clicks, not 30.
func TestCountsAcrossReceivesInOneBatch(t *testing.T) {
	r := newRig(time.Hour)
	r.w.o.CountMaxEvents = 25
	r.q.batches = [][]sqstypes.Message{batchOf("a", 10), batchOf("b", 10), batchOf("c", 10)}
	r.run(t)
	if len(r.rec.batches) != 1 || r.rec.batches[0] != 30 {
		t.Fatalf("RecordClicks batches = %v, want one batch of 30", r.rec.batches)
	}
	if r.m.Get(WorkerCountTransactions) != 1 || r.m.Get(telemetry.WorkerEventsProcessed) != 30 || len(r.q.deleted) != 30 {
		t.Fatalf("transactions=%v processed=%v deleted=%d", r.m.Get(WorkerCountTransactions), r.m.Get(telemetry.WorkerEventsProcessed), len(r.q.deleted))
	}
}

// The counting window closes after CountInterval or as soon as a long poll
// comes back empty, so stats stay fresh when traffic stops.
func TestCountWindowIntervalAndQuietQueue(t *testing.T) {
	r := newRig(time.Hour)
	ctx := context.Background()
	r.w.decode(ctx, batchOf("a", 2))
	if r.w.countDue() {
		t.Fatal("a fresh window is not due")
	}
	*r.now = r.now.Add(2 * time.Second)
	if !r.w.countDue() {
		t.Fatal("window is due after CountInterval")
	}

	r = newRig(time.Hour)
	r.q.batches = [][]sqstypes.Message{batchOf("a", 3), {}, batchOf("b", 1)}
	var countedBeforeThirdReceive int
	r.q.onReceive = func(call int) {
		if call == 2 {
			r.rec.mu.Lock()
			countedBeforeThirdReceive = len(r.rec.batches)
			r.rec.mu.Unlock()
		}
	}
	r.run(t)
	if countedBeforeThirdReceive != 1 || len(r.rec.batches) != 2 {
		t.Fatalf("empty poll must count the window at once: before=%d batches=%v", countedBeforeThirdReceive, r.rec.batches)
	}
}

// One failed click in a batch is left for redelivery; the rest are deleted.
func TestPartialCountingFailure(t *testing.T) {
	r := newRig(time.Hour)
	r.rec.fail = map[string]bool{"e-a1": true}
	r.q.batches = [][]sqstypes.Message{batchOf("a", 3)}
	r.run(t)
	if r.m.Get(WorkerRetries) != 1 || r.m.Get(telemetry.WorkerEventsProcessed) != 2 || len(r.q.deleted) != 2 {
		t.Fatalf("retries=%v processed=%v deleted=%v", r.m.Get(WorkerRetries), r.m.Get(telemetry.WorkerEventsProcessed), r.q.deleted)
	}
	for _, d := range r.q.deleted {
		if d == "rh-a1" {
			t.Fatal("the failed click must not be deleted")
		}
	}
	if r.u.adds != 3 {
		t.Fatalf("visitors are added before counting (idempotent on redelivery): adds=%d", r.u.adds)
	}
}

// A recorder that returns no results at all (hard failure) fails every click.
func TestRecorderWithoutResults(t *testing.T) {
	if got := result(store.BatchResult{}, 0); got != store.Failed {
		t.Fatalf("missing result = %v, want Failed", got)
	}
}

// Visitors are in the HyperLogLog before the counters commit, so stats never
// show a counted click whose visitor is missing from uniques.
func TestUniquesAddedBeforeCounting(t *testing.T) {
	r := newRig(time.Hour)
	r.q.batches = [][]sqstypes.Message{batchOf("a", 5)}
	var addsAtCommit int
	r.rec.onCommit = func() {
		r.u.mu.Lock()
		addsAtCommit = r.u.adds
		r.u.mu.Unlock()
	}
	r.run(t)
	if addsAtCommit != 5 {
		t.Fatalf("visitors added before the counting commit = %d, want 5", addsAtCommit)
	}
}
