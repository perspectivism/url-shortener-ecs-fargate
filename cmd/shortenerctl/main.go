// Command shortenerctl is the operator CLI (local init, API key management).
package main

import (
	"context"
	"crypto/rand"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/shortener/url-shortener-ecs-fargate/internal/ctl"
	"github.com/shortener/url-shortener-ecs-fargate/internal/platform"
)

func main() {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	ctx, stop := platform.SignalContext()
	code := ctl.Run(ctx, os.Args[1:], ctl.Env{
		Getenv: os.Getenv,
		NewClients: func(ctx context.Context) (ctl.Clients, error) {
			cfg, err := platform.AWSConfig(ctx, region)
			if err != nil {
				return ctl.Clients{}, err
			}
			return ctl.Clients{
				DynamoDB: dynamodb.NewFromConfig(cfg),
				SQS:      sqs.NewFromConfig(cfg),
				// S3Mock serves path-style URLs only.
				S3: s3.NewFromConfig(cfg, func(o *s3.Options) { o.UsePathStyle = true }),
			}, nil
		},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Rand:   rand.Reader,
		Now:    time.Now,
	})
	stop()
	os.Exit(code)
}
