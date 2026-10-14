// Command worker consumes click events: exactly-once counters in DynamoDB,
// HyperLogLog uniques in Valkey, raw events to S3.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"

	"github.com/shortener/url-shortener-ecs-fargate/internal/cache"
	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
	"github.com/shortener/url-shortener-ecs-fargate/internal/httpx"
	"github.com/shortener/url-shortener-ecs-fargate/internal/platform"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
	"github.com/shortener/url-shortener-ecs-fargate/internal/worker"
)

func main() { os.Exit(run(os.Args[1:])) }

func taskID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		return httpx.Healthcheck(context.Background(), platform.HealthcheckAddr(), &http.Client{Timeout: 2 * time.Second})
	}
	cfg, err := config.LoadWorker(os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := platform.SignalContext()
	defer stop()
	tel := platform.NewTelemetry(os.Stdout, cfg.Common)
	log := tel.Logger

	awsCfg, err := platform.AWSConfig(ctx, cfg.AWSRegion)
	if err != nil {
		log.Error("startup failed", "err", err)
		return 1
	}
	rc, err := platform.NewCache(cfg.Cache, awsCfg)
	if err != nil {
		log.Error("startup failed", "err", err)
		return 1
	}
	defer rc.Close()

	w := worker.New(worker.Options{
		QueueURL: cfg.ClickQueueURL, Bucket: cfg.EventsBucket, TaskID: taskID(),
		FlushInterval: cfg.FlushInterval, MaxBatchBytes: cfg.MaxBatchBytes,
		Concurrency: cfg.Concurrency, WaitSeconds: cfg.WaitSeconds,
	}, worker.Deps{
		SQS:      sqs.NewFromConfig(awsCfg),
		S3:       s3.NewFromConfig(awsCfg, func(o *s3.Options) { o.UsePathStyle = true }),
		Recorder: store.NewAnalytics(dynamodb.NewFromConfig(awsCfg), cfg.AnalyticsTable, 2*time.Second),
		Uniques:  cache.NewUniques(rc),
		Metrics:  tel.Metrics,
		Logger:   log,
		Now:      time.Now,
		NewID:    func() string { return uuid.Must(uuid.NewV7()).String() },
	})

	// Liveness endpoint for the container health check: healthy while the
	// receive loop is cycling (long polls return at least every 20 s).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, r *http.Request) {
		if !w.Healthy(2 * time.Minute) {
			httpx.WriteProblem(rw, r, http.StatusServiceUnavailable, httpx.CodeServiceUnavailable, "receive loop stalled")
			return
		}
		httpx.Healthz(rw, r)
	})
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		log.Error("listen failed", "err", err)
		return 1
	}
	go func() { _ = httpx.Serve(ctx, httpx.NewServer(cfg.HTTPAddr, mux, log), ln, time.Second) }()

	metricsCtx, stopMetrics := context.WithCancel(context.WithoutCancel(ctx))
	metricsDone := make(chan struct{})
	go func() { tel.Metrics.Run(metricsCtx, cfg.MetricsInterval, log); close(metricsDone) }()

	log.Info("worker started", "queue", cfg.ClickQueueURL, "bucket", cfg.EventsBucket)
	_ = w.Run(ctx, cfg.ShutdownTimeout)
	stopMetrics()
	<-metricsDone
	log.Info("worker stopped")
	return 0
}
