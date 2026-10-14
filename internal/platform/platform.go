// Package platform holds the startup wiring shared by every binary: telemetry,
// AWS configuration, the Valkey client and signal handling. cmd/* packages
// call these and contain no logic of their own.
package platform

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/redis/go-redis/v9"

	"github.com/shortener/url-shortener-ecs-fargate/internal/cache"
	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
	"github.com/shortener/url-shortener-ecs-fargate/internal/telemetry"
	"github.com/shortener/url-shortener-ecs-fargate/internal/version"
)

// Telemetry bundles the logger and metrics emitter for a service.
type Telemetry struct {
	Logger  *slog.Logger
	Metrics *telemetry.Metrics
}

// NewTelemetry writes logs and EMF metrics to out (stdout in production).
func NewTelemetry(out io.Writer, c config.Common) Telemetry {
	w := telemetry.NewSyncWriter(out)
	return Telemetry{
		Logger:  telemetry.NewLogger(w, c.Service, c.Env, version.String(), c.LogLevel),
		Metrics: telemetry.NewMetrics(w, c.MetricsNamespace, c.Service, time.Now),
	}
}

// SignalContext is cancelled on SIGTERM (ECS stop) or SIGINT.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
}

// AWSConfig loads the default credential chain (the ECS task role in AWS).
// Service endpoints for local emulators come from AWS_ENDPOINT_URL_* env vars.
func AWSConfig(ctx context.Context, region string) (aws.Config, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithRetryer(func() aws.Retryer {
			return retry.NewStandard(func(o *retry.StandardOptions) { o.MaxAttempts = 3 })
		}),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}
	return cfg, nil
}

// NewCache builds the Valkey client from config; IAM auth signs a fresh token
// per connection using the task role's credentials.
func NewCache(c config.Cache, awsCfg aws.Config) (redis.UniversalClient, error) {
	opts := cache.Options{Addr: c.Addr, Mode: c.Mode, Auth: c.Auth, TLS: c.TLS, User: c.User}
	if c.Auth == config.CacheAuthIAM {
		tp := &cache.IAMTokenProvider{
			Credentials: awsCfg.Credentials,
			UserID:      c.User,
			CacheName:   c.CacheName,
			Region:      c.Region,
			Serverless:  true,
		}
		opts.Token = tp.Token
	}
	client, err := cache.New(opts)
	if err != nil {
		return nil, fmt.Errorf("cache client: %w", err)
	}
	return client, nil
}

// HealthcheckAddr returns the listen address for the healthcheck subcommand.
func HealthcheckAddr() string {
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		return v
	}
	return ":8080"
}
