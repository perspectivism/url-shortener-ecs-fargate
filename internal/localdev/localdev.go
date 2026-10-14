// Package localdev provisions the local emulator resources that Terraform
// creates in AWS: the click queue with its DLQ and redrive policy, and the raw
// events bucket. Settings mirror infra/modules/messaging (SPEC §8.2).
package localdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// Queue settings (SPEC §8.2).
const (
	VisibilityTimeoutSeconds = 120
	MaxReceiveCount          = 5
	DLQRetentionSeconds      = 14 * 24 * 3600
)

// SQSAdmin is the subset of SQS used for provisioning.
type SQSAdmin interface {
	CreateQueue(ctx context.Context, in *sqs.CreateQueueInput, opts ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	GetQueueAttributes(ctx context.Context, in *sqs.GetQueueAttributesInput, opts ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// S3Admin is the subset of S3 used for provisioning.
type S3Admin interface {
	CreateBucket(ctx context.Context, in *s3.CreateBucketInput, opts ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
}

// EnsureQueues creates the DLQ and the click queue with a redrive policy.
func EnsureQueues(ctx context.Context, c SQSAdmin, queue, dlq string) (queueURL, dlqURL string, err error) {
	d, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName:  aws.String(dlq),
		Attributes: map[string]string{string(sqstypes.QueueAttributeNameMessageRetentionPeriod): strconv.Itoa(DLQRetentionSeconds)},
	})
	if err != nil {
		return "", "", fmt.Errorf("create dlq: %w", err)
	}
	attrs, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       d.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", "", fmt.Errorf("dlq arn: %w", err)
	}
	arn := attrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]
	if arn == "" {
		return "", "", errors.New("dlq arn: empty")
	}
	redrive, err := json.Marshal(map[string]string{"deadLetterTargetArn": arn, "maxReceiveCount": strconv.Itoa(MaxReceiveCount)})
	if err != nil {
		return "", "", err
	}
	q, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queue),
		Attributes: map[string]string{
			string(sqstypes.QueueAttributeNameVisibilityTimeout):             strconv.Itoa(VisibilityTimeoutSeconds),
			string(sqstypes.QueueAttributeNameReceiveMessageWaitTimeSeconds): "20",
			string(sqstypes.QueueAttributeNameRedrivePolicy):                 string(redrive),
		},
	})
	if err != nil {
		return "", "", fmt.Errorf("create queue: %w", err)
	}
	return aws.ToString(q.QueueUrl), aws.ToString(d.QueueUrl), nil
}

// EnsureBucket creates the bucket (already existing is fine).
func EnsureBucket(ctx context.Context, c S3Admin, name string) error {
	_, err := c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(name)})
	var owned *s3types.BucketAlreadyOwnedByYou
	var exists *s3types.BucketAlreadyExists
	if err != nil && !errors.As(err, &owned) && !errors.As(err, &exists) {
		return fmt.Errorf("create bucket: %w", err)
	}
	return nil
}
