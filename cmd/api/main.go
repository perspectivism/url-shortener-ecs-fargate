// Command api serves the /api/v1 management API (the write path).
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/shortener/url-shortener-ecs-fargate/internal/api"
	"github.com/shortener/url-shortener-ecs-fargate/internal/auth"
	"github.com/shortener/url-shortener-ecs-fargate/internal/cache"
	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
	"github.com/shortener/url-shortener-ecs-fargate/internal/httpx"
	"github.com/shortener/url-shortener-ecs-fargate/internal/keygen"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
	"github.com/shortener/url-shortener-ecs-fargate/internal/platform"
	"github.com/shortener/url-shortener-ecs-fargate/internal/ratelimit"
	"github.com/shortener/url-shortener-ecs-fargate/internal/scanner"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
)

func main() { os.Exit(run(os.Args[1:])) }

func newScanner(cfg config.API) links.Scanner {
	switch cfg.Scanner {
	case config.ScannerSafeBrowsing:
		client := &http.Client{
			Timeout:   2 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, Proxy: http.ProxyFromEnvironment},
		}
		return scanner.NewSafeBrowsing(cfg.SafeBrowsingEndpoint, cfg.SafeBrowsingKey, client)
	case config.ScannerFake:
		return scanner.Fake{}
	default:
		return scanner.Noop{}
	}
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		return httpx.Healthcheck(context.Background(), platform.HealthcheckAddr(), &http.Client{Timeout: 2 * time.Second})
	}
	cfg, err := config.LoadAPI(os.LookupEnv)
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

	repo := store.NewLinks(dynamodb.NewFromConfig(awsCfg), cfg.LinksTable, 200*time.Millisecond)
	gen, err := keygen.New(repo, cfg.KeyBlockSize, cfg.SqidsAlphabet, links.IsReserved, tel.Metrics)
	if err != nil {
		log.Error("startup failed", "err", err)
		return 1
	}
	svc := links.NewService(links.ServiceDeps{
		Repo: repo, Cache: cache.NewLinkCache(rc), Scanner: newScanner(cfg), Codes: gen,
		Metrics: tel.Metrics, Logger: log, Now: time.Now, BlockedHosts: cfg.BlockedHosts,
	})
	server := api.New(api.Deps{
		Links:       svc,
		Auth:        auth.NewAuthenticator(repo, cfg.Env, cfg.AuthCacheTTL, time.Now, log),
		Limiter:     ratelimit.New(rc, time.Now, tel.Metrics, log),
		Idempotency: cache.NewIdempotency(rc),
		Metrics:     tel.Metrics,
		Logger:      log,
		Now:         time.Now,
		BaseURL:     cfg.BaseURL,
		WriteLimit:  cfg.WriteLimitPerMin,
		ReadLimit:   cfg.ReadLimitPerMin,
	})

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		log.Error("listen failed", "err", err)
		return 1
	}

	metricsCtx, stopMetrics := context.WithCancel(context.WithoutCancel(ctx))
	metricsDone := make(chan struct{})
	go func() { tel.Metrics.Run(metricsCtx, cfg.MetricsInterval, log); close(metricsDone) }()
	log.Info("api listening", "addr", ln.Addr().String(), "scanner", cfg.Scanner)
	code := 0
	if err := httpx.Serve(ctx, httpx.NewServer(cfg.HTTPAddr, server.Handler(), log), ln, cfg.ShutdownTimeout); err != nil {
		log.Error("server error", "err", err)
		code = 1
	}
	stopMetrics()
	<-metricsDone
	log.Info("api stopped")
	return code
}
