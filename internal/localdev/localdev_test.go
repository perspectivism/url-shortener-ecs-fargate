package localdev

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type sqsFake struct {
	createErr, attrErr error
	failSecond         bool
	n                  int
}

func (f *sqsFake) CreateQueue(_ context.Context, in *sqs.CreateQueueInput, _ ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	f.n++
	if f.createErr != nil || (f.failSecond && f.n == 2) {
		return nil, errors.New("create failed")
	}
	return &sqs.CreateQueueOutput{QueueUrl: aws.String("http://q/" + *in.QueueName)}, nil
}

func (f *sqsFake) GetQueueAttributes(context.Context, *sqs.GetQueueAttributesInput, ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	if f.attrErr != nil {
		return nil, f.attrErr
	}
	return &sqs.GetQueueAttributesOutput{Attributes: map[string]string{"QueueArn": "arn:dlq"}}, nil
}

func TestEnsureQueues(t *testing.T) {
	q, d, err := EnsureQueues(context.Background(), &sqsFake{}, "clicks", "clicks-dlq")
	if err != nil || q != "http://q/clicks" || d != "http://q/clicks-dlq" {
		t.Fatalf("%s %s %v", q, d, err)
	}
	for name, f := range map[string]*sqsFake{
		"dlq":   {createErr: errors.New("x")},
		"attrs": {attrErr: errors.New("x")},
		"queue": {failSecond: true},
	} {
		if _, _, err := EnsureQueues(context.Background(), f, "a", "b"); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

type s3Fake struct{ err error }

func (f s3Fake) CreateBucket(context.Context, *s3.CreateBucketInput, ...func(*s3.Options)) (*s3.CreateBucketOutput, error) {
	return &s3.CreateBucketOutput{}, f.err
}

func TestEnsureBucket(t *testing.T) {
	for _, err := range []error{nil, &s3types.BucketAlreadyOwnedByYou{}, &s3types.BucketAlreadyExists{}} {
		if got := EnsureBucket(context.Background(), s3Fake{err: err}, "b"); got != nil {
			t.Errorf("EnsureBucket(%v) = %v", err, got)
		}
	}
	if EnsureBucket(context.Background(), s3Fake{err: errors.New("denied")}, "b") == nil {
		t.Error("expected error")
	}
}
