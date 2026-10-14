// Command nightly persists unique visitors and rescans destinations
// (scheduled by EventBridge Scheduler at 03:15 UTC).
package main

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/shortener/url-shortener-ecs-fargate/internal/cache"
	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
	"github.com/shortener/url-shortener-ecs-fargate/internal/nightly"
	"github.com/shortener/url-shortener-ecs-fargate/internal/platform"
	"github.com/shortener/url-shortener-ecs-fargate/internal/scanner"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
)

func main() { os.Exit(run(os.Args[1:])) }

func newScanner(cfg config.Nightly) links.Scanner {
	switch cfg.Scanner {
	case config.ScannerSafeBrowsing:
		client := &http.Client{
			Timeout:   10 * time.Second,
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
		return 0 // batch job: alive while running; failures surface as a non-zero exit
	}
	cfg, err := config.LoadNightly(os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx, stop := platform.SignalContext()
	defer stop()
	tel := platform.NewTelemetry(os.Stdout, cfg.Common)
	log := tel.Logger
	defer func() { _ = tel.Metrics.Flush() }()

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

	ddb := dynamodb.NewFromConfig(awsCfg)
	linkStore := store.NewLinks(ddb, cfg.LinksTable, 5*time.Second)
	svc := links.NewService(links.ServiceDeps{
		Repo: linkStore, Cache: cache.NewLinkCache(rc), Scanner: newScanner(cfg),
		Metrics: tel.Metrics, Logger: log, Now: time.Now,
	})
	job := nightly.New(nightly.Deps{
		Uniques:   cache.NewUniques(rc),
		Stats:     store.NewAnalytics(ddb, cfg.AnalyticsTable, 5*time.Second),
		Links:     linkStore,
		Scanner:   newScanner(cfg),
		Apply:     svc,
		Metrics:   tel.Metrics,
		Logger:    log,
		Now:       time.Now,
		RescanAge: cfg.RescanAge,
		Segments:  cfg.ScanSegments,
	})
	date := nightly.TargetDate(cfg.Date, time.Now())
	if _, err := job.Run(ctx, date); err != nil {
		log.Error("nightly failed", "date", date, "err", err)
		return 1
	}
	return 0
}
