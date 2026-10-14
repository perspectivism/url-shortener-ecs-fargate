package ctl

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type fakeDDB struct {
	puts    []*dynamodb.PutItemInput
	updates []*dynamodb.UpdateItemInput
	tables  []string
	err     error
}

func (f *fakeDDB) GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return &dynamodb.GetItemOutput{}, nil
}

func (f *fakeDDB) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.puts = append(f.puts, in)
	return &dynamodb.PutItemOutput{}, f.err
}

func (f *fakeDDB) UpdateItem(_ context.Context, in *dynamodb.UpdateItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error) {
	f.updates = append(f.updates, in)
	return &dynamodb.UpdateItemOutput{}, f.err
}

func (f *fakeDDB) Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return &dynamodb.QueryOutput{}, nil
}

func (f *fakeDDB) Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error) {
	return &dynamodb.ScanOutput{}, nil
}

func (f *fakeDDB) TransactWriteItems(context.Context, *dynamodb.TransactWriteItemsInput, ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	return &dynamodb.TransactWriteItemsOutput{}, nil
}

func (f *fakeDDB) CreateTable(_ context.Context, in *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
	f.tables = append(f.tables, *in.TableName)
	return &dynamodb.CreateTableOutput{}, f.err
}

func (f *fakeDDB) DescribeTable(context.Context, *dynamodb.DescribeTableInput, ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	return &dynamodb.DescribeTableOutput{Table: &types.TableDescription{TableStatus: types.TableStatusActive}}, nil
}

func (f *fakeDDB) UpdateTimeToLive(context.Context, *dynamodb.UpdateTimeToLiveInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateTimeToLiveOutput, error) {
	return &dynamodb.UpdateTimeToLiveOutput{}, nil
}

type fakeSQS struct {
	created []*sqs.CreateQueueInput
	err     error
	noArn   bool
}

func (f *fakeSQS) CreateQueue(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	f.created = append(f.created, in)
	if f.err != nil {
		return nil, f.err
	}
	return &sqs.CreateQueueOutput{QueueUrl: aws.String("http://sqs/" + *in.QueueName)}, nil
}

func (f *fakeSQS) GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	if f.noArn {
		return &sqs.GetQueueAttributesOutput{}, nil
	}
	return &sqs.GetQueueAttributesOutput{Attributes: map[string]string{"QueueArn": "arn:aws:sqs:us-east-1:000000000000:dlq"}}, nil
}

type fakeS3 struct{ err error }

func (f *fakeS3) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, f.err
}

type world struct {
	ddb    *fakeDDB
	sqs    *fakeSQS
	s3     *fakeS3
	out    bytes.Buffer
	errOut bytes.Buffer
	vars   map[string]string
	newErr error
}

func (w *world) env() Env {
	return Env{
		Getenv: func(k string) string { return w.vars[k] },
		NewClients: func(context.Context) (Clients, error) {
			return Clients{DynamoDB: w.ddb, SQS: w.sqs, S3: w.s3}, w.newErr
		},
		Stdout: &w.out, Stderr: &w.errOut, Rand: rand.Reader,
		Now: func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) },
	}
}

func newWorld() *world {
	return &world{ddb: &fakeDDB{}, sqs: &fakeSQS{}, s3: &fakeS3{}, vars: map[string]string{"APP_ENV": "local", "LINKS_TABLE": "links"}}
}

func TestLocalInit(t *testing.T) {
	w := newWorld()
	if code := Run(context.Background(), []string{"local-init", "--bucket", "b1"}, w.env()); code != 0 {
		t.Fatalf("exit %d: %s", code, w.errOut.String())
	}
	if len(w.ddb.tables) != 2 || w.ddb.tables[0] != "links" || len(w.sqs.created) != 2 {
		t.Fatalf("tables=%v queues=%d", w.ddb.tables, len(w.sqs.created))
	}
	q := w.sqs.created[1]
	if q.Attributes["VisibilityTimeout"] != "120" || !strings.Contains(q.Attributes["RedrivePolicy"], `"maxReceiveCount":"5"`) {
		t.Fatalf("queue attrs = %v", q.Attributes)
	}
	if !strings.Contains(w.out.String(), "bucket  b1 ready") {
		t.Fatalf("output = %s", w.out.String())
	}

	w = newWorld()
	w.vars["APP_ENV"] = "prod"
	if code := Run(context.Background(), []string{"local-init"}, w.env()); code != 2 {
		t.Fatal("local-init must refuse non-local environments")
	}
	for name, mutate := range map[string]func(*world){
		"clients": func(w *world) { w.newErr = errors.New("no creds") },
		"table":   func(w *world) { w.ddb.err = errors.New("boom") },
		"queue":   func(w *world) { w.sqs.err = errors.New("boom") },
		"no arn":  func(w *world) { w.sqs.noArn = true },
		"bucket":  func(w *world) { w.s3.err = errors.New("boom") },
	} {
		w := newWorld()
		mutate(w)
		if code := Run(context.Background(), []string{"local-init"}, w.env()); code != 1 {
			t.Errorf("%s: exit %d", name, code)
		}
	}
}

func TestAPIKeyCommands(t *testing.T) {
	w := newWorld()
	if code := Run(context.Background(), []string{"apikey", "create", "--owner", "demo", "--scopes", "links:read, admin", "--name", "ci"}, w.env()); code != 0 {
		t.Fatalf("exit %d: %s", code, w.errOut.String())
	}
	out := w.out.String()
	if !strings.Contains(out, "api_key: shortener_local_") || !strings.Contains(out, "scopes: links:read,admin") || len(w.ddb.puts) != 1 {
		t.Fatalf("output = %s", out)
	}
	if strings.Contains(w.errOut.String(), "shortener_local_") {
		t.Fatal("key must only go to stdout")
	}
	hash := strings.TrimSpace(strings.SplitN(strings.SplitN(out, "key_hash: ", 2)[1], "\n", 2)[0])

	w.out.Reset()
	if code := Run(context.Background(), []string{"apikey", "revoke", "--hash", hash}, w.env()); code != 0 || len(w.ddb.updates) != 1 {
		t.Fatalf("revoke exit %d: %s", code, w.errOut.String())
	}

	for name, args := range map[string][]string{
		"no args":       {},
		"unknown":       {"frobnicate"},
		"apikey alone":  {"apikey"},
		"missing owner": {"apikey", "create"},
		"bad scope":     {"apikey", "create", "--owner", "x", "--scopes", "root"},
		"bad flag":      {"apikey", "create", "--nope"},
		"short hash":    {"apikey", "revoke", "--hash", "abc"},
		"revoke flag":   {"apikey", "revoke", "--nope"},
		"init flag":     {"local-init", "--nope"},
	} {
		w := newWorld()
		if code := Run(context.Background(), args, w.env()); code != 2 {
			t.Errorf("%s: exit %d, want 2", name, code)
		}
	}
	for name, mutate := range map[string]func(*world){
		"clients": func(w *world) { w.newErr = errors.New("no creds") },
		"store":   func(w *world) { w.ddb.err = errors.New("boom") },
	} {
		w := newWorld()
		mutate(w)
		if code := Run(context.Background(), []string{"apikey", "create", "--owner", "x"}, w.env()); code != 1 {
			t.Errorf("create %s: exit %d", name, code)
		}
		w = newWorld()
		mutate(w)
		if code := Run(context.Background(), []string{"apikey", "revoke", "--hash", strings.Repeat("a", 64)}, w.env()); code != 1 {
			t.Errorf("revoke %s: exit %d", name, code)
		}
	}
	w = newWorld()
	e := w.env()
	e.Rand = bytes.NewReader(nil)
	if code := Run(context.Background(), []string{"apikey", "create", "--owner", "x"}, e); code != 1 {
		t.Error("entropy failure must fail")
	}
}
