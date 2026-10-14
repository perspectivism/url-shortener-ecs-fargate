// Package cache wraps Valkey (ElastiCache Serverless) access: client
// construction with IAM authentication, the redirect link cache, idempotency
// records and HyperLogLog unique-visitor counters.
package cache

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
)

// Modes and auth types (mirrors config constants to avoid an import cycle).
const (
	ModeCluster    = "cluster"
	ModeStandalone = "standalone"
	AuthIAM        = "iam"
	AuthNone       = "none"
)

// ConnMaxLifetime is below ElastiCache's 12 h limit for IAM-authenticated
// connections, so the pool recycles them (with a fresh token) before AWS drops them.
const ConnMaxLifetime = 11 * time.Hour

// TokenFunc returns a fresh IAM auth token.
type TokenFunc func(ctx context.Context) (string, error)

// Options configures New.
type Options struct {
	Addr         string
	Mode         string
	Auth         string
	TLS          bool
	User         string
	Token        TokenFunc // required when Auth is AuthIAM
	PoolSize     int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// New returns a Valkey client. Serverless is cluster-mode, so AWS uses
// ModeCluster; local development uses a single standalone node.
func New(o Options) (redis.UniversalClient, error) {
	if o.Addr == "" {
		return nil, errors.New("cache: address is required")
	}
	if o.DialTimeout == 0 {
		o.DialTimeout = time.Second
	}
	if o.ReadTimeout == 0 {
		o.ReadTimeout = 200 * time.Millisecond
	}
	if o.WriteTimeout == 0 {
		o.WriteTimeout = 200 * time.Millisecond
	}
	var tlsCfg *tls.Config
	if o.TLS {
		host, _, err := net.SplitHostPort(o.Addr)
		if err != nil {
			return nil, fmt.Errorf("cache: address: %w", err)
		}
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}
	}
	var creds func(context.Context) (string, string, error)
	switch o.Auth {
	case AuthIAM:
		if o.Token == nil || o.User == "" || !o.TLS {
			return nil, errors.New("cache: IAM auth requires a user, a token provider and TLS")
		}
		user, token := o.User, o.Token
		// Called for every new connection: each gets a freshly signed token.
		creds = func(ctx context.Context) (string, string, error) {
			t, err := token(ctx)
			return user, t, err
		}
	case AuthNone:
	default:
		return nil, fmt.Errorf("cache: unknown auth %q", o.Auth)
	}

	switch o.Mode {
	case ModeCluster:
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:                      []string{o.Addr},
			TLSConfig:                  tlsCfg,
			CredentialsProviderContext: creds,
			DialTimeout:                o.DialTimeout,
			ReadTimeout:                o.ReadTimeout,
			WriteTimeout:               o.WriteTimeout,
			ContextTimeoutEnabled:      true,
			PoolSize:                   o.PoolSize,
			ConnMaxLifetime:            ConnMaxLifetime,
			MaxRetries:                 1,
		}), nil
	case ModeStandalone:
		return redis.NewClient(&redis.Options{
			Addr:                       o.Addr,
			TLSConfig:                  tlsCfg,
			CredentialsProviderContext: creds,
			DialTimeout:                o.DialTimeout,
			ReadTimeout:                o.ReadTimeout,
			WriteTimeout:               o.WriteTimeout,
			ContextTimeoutEnabled:      true,
			PoolSize:                   o.PoolSize,
			ConnMaxLifetime:            ConnMaxLifetime,
			MaxRetries:                 1,
		}), nil
	default:
		return nil, fmt.Errorf("cache: unknown mode %q", o.Mode)
	}
}
