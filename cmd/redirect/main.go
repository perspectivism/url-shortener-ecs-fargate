// Command redirect serves GET/HEAD /{code} (the hot read path).
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/cache"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/config"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/events"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/httpx"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/platform"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/redirect"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/store"
	"github.com/perspectivism/url-shortener-ecs-fargate/internal/visitor"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		return httpx.Healthcheck(context.Background(), platform.HealthcheckAddr(), &http.Client{Timeout: 2 * time.Second})
	}
	cfg, err := config.LoadRedirect(os.LookupEnv)
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

	producer := events.NewProducer(sqs.NewFromConfig(awsCfg), events.ProducerOptions{
		QueueURL: cfg.ClickQueueURL, Buffer: cfg.ClickBuffer, FlushEvery: cfg.ClickFlushInterval, Senders: cfg.ClickSenders,
	}, tel.Metrics, log)
	producer.Start()

	repo := store.NewLinks(dynamodb.NewFromConfig(awsCfg), cfg.LinksTable, 200*time.Millisecond)
	h := redirect.New(redirect.Deps{
		Resolver:       links.NewResolver(cache.NewLinkCache(rc), repo, tel.Metrics, log, time.Now),
		Clicks:         producer,
		Hasher:         visitor.NewHasher(cfg.VisitorSalt),
		FromCloudFront: cfg.ClientIPSource == config.IPSourceCloudFront,
		Metrics:        tel.Metrics,
		Logger:         log,
		Now:            time.Now,
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		log.Error("listen failed", "err", err)
		return 1
	}

	metricsCtx, stopMetrics := context.WithCancel(context.WithoutCancel(ctx))
	metricsDone := make(chan struct{})
	go func() { tel.Metrics.Run(metricsCtx, cfg.MetricsInterval, log); close(metricsDone) }()
	log.Info("redirect listening", "addr", ln.Addr().String())
	code := 0
	if err := httpx.Serve(ctx, httpx.NewServer(cfg.HTTPAddr, h.Routes(), log), ln, cfg.ShutdownTimeout/2); err != nil {
		log.Error("server error", "err", err)
		code = 1
	}

	// SIGTERM: HTTP has stopped accepting and drained; now flush buffered clicks.
	drainCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout/2)
	defer cancel()
	if err := producer.Shutdown(drainCtx); err != nil {
		log.Error("click buffer not fully drained", "err", err)
	}
	stopMetrics()
	<-metricsDone
	log.Info("redirect stopped")
	return code
}
