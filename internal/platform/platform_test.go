package platform

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/shortener/url-shortener-ecs-fargate/internal/config"
)

func TestTelemetry(t *testing.T) {
	var buf bytes.Buffer
	tel := NewTelemetry(&buf, config.Common{Service: "api", Env: "test", MetricsNamespace: "Shortener/test", LogLevel: slog.LevelInfo})
	tel.Logger.Info("hello")
	tel.Metrics.Count("X", 1)
	if err := tel.Metrics.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `"service":"api"`) || !strings.Contains(out, `"Namespace":"Shortener/test"`) || strings.Count(out, "\n") != 2 {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestAWSConfigAndCache(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "local")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "local")
	cfg, err := AWSConfig(context.Background(), "eu-west-1")
	if err != nil || cfg.Region != "eu-west-1" {
		t.Fatalf("cfg: %v %v", cfg.Region, err)
	}

	mr := miniredis.RunT(t)
	c, err := NewCache(config.Cache{Addr: mr.Addr(), Mode: config.CacheModeStandalone, Auth: config.CacheAuthNone}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}

	iamCfg := aws.Config{Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "x", "")}
	c2, err := NewCache(config.Cache{Addr: "cache.example:6379", Mode: config.CacheModeCluster, Auth: config.CacheAuthIAM, TLS: true, User: "u", CacheName: "c", Region: "us-east-1"}, iamCfg)
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.Close()
	if _, err := NewCache(config.Cache{Mode: "bogus"}, cfg); err == nil {
		t.Fatal("expected error")
	}
}

func TestHealthcheckAddrAndSignals(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	if HealthcheckAddr() != ":8080" {
		t.Fatal("default addr")
	}
	t.Setenv("HTTP_ADDR", ":9999")
	if HealthcheckAddr() != ":9999" {
		t.Fatal("env addr")
	}
	ctx, stop := SignalContext()
	stop()
	<-ctx.Done()
}
