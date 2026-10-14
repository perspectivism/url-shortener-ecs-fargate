package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// Environment names.
const (
	EnvLocal = "local"
	EnvTest  = "test"
	EnvDev   = "dev"
	EnvProd  = "prod"
)

// Cache modes and auth types.
const (
	CacheModeCluster    = "cluster"
	CacheModeStandalone = "standalone"
	CacheAuthIAM        = "iam"
	CacheAuthNone       = "none"
)

// Common holds settings shared by every service.
type Common struct {
	Service          string
	Env              string
	LogLevel         slog.Level
	HTTPAddr         string
	AWSRegion        string
	LinksTable       string
	MetricsNamespace string
	MetricsInterval  time.Duration
	ShutdownTimeout  time.Duration
}

// Cache holds Valkey connection settings.
type Cache struct {
	Addr      string
	Mode      string
	Auth      string
	TLS       bool
	User      string
	CacheName string
	Region    string
}

// IsAWSEnv reports whether env runs in AWS (where stricter rules apply).
func IsAWSEnv(env string) bool { return env == EnvDev || env == EnvProd }

func readCommon(r *Reader, service string) Common {
	c := Common{
		Service:         service,
		Env:             r.OneOf("APP_ENV", "", EnvLocal, EnvTest, EnvDev, EnvProd),
		HTTPAddr:        r.String("HTTP_ADDR", ":8080"),
		AWSRegion:       r.String("AWS_REGION", "us-east-1"),
		LinksTable:      r.Required("LINKS_TABLE"),
		MetricsInterval: r.Duration("METRICS_FLUSH_INTERVAL", 10*time.Second),
		ShutdownTimeout: r.Duration("SHUTDOWN_TIMEOUT", 20*time.Second),
	}
	if c.Env == "" {
		r.Problemf("APP_ENV is required")
	}
	c.MetricsNamespace = "Shortener/" + c.Env
	switch r.OneOf("LOG_LEVEL", "info", "debug", "info", "warn", "error") {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		c.LogLevel = slog.LevelInfo
	}
	return c
}

func readCache(r *Reader, env, region string) Cache {
	c := Cache{
		Addr:   r.Required("CACHE_ADDR"),
		Mode:   r.OneOf("CACHE_MODE", CacheModeCluster, CacheModeCluster, CacheModeStandalone),
		Auth:   r.OneOf("CACHE_AUTH", CacheAuthIAM, CacheAuthIAM, CacheAuthNone),
		TLS:    r.Bool("CACHE_TLS", true),
		Region: region,
	}
	if c.Auth == CacheAuthIAM {
		c.User = r.Required("CACHE_USER")
		// ElastiCache lowercases cache names at creation; the signed token must match.
		c.CacheName = strings.ToLower(r.Required("CACHE_NAME"))
		if !c.TLS {
			r.Problemf("CACHE_TLS must be true when CACHE_AUTH=iam (ElastiCache IAM auth requires TLS)")
		}
	}
	if IsAWSEnv(env) && c.Auth != CacheAuthIAM {
		r.Problemf("CACHE_AUTH must be iam when APP_ENV is dev or prod")
	}
	return c
}

// Redirect configures the redirect service.
type Redirect struct {
	Common
	Cache              Cache
	ClickQueueURL      string
	VisitorSalt        Secret
	ClientIPSource     string
	ClickBuffer        int
	ClickFlushInterval time.Duration
	ClickSenders       int
}

// Client IP sources.
const (
	IPSourceCloudFront = "cloudfront"
	IPSourceRemoteAddr = "remote_addr"
)

// LoadRedirect parses and validates the redirect service configuration.
func LoadRedirect(lookup LookupFunc) (Redirect, error) {
	r := NewReader(lookup)
	c := Redirect{Common: readCommon(r, "redirect")}
	c.Cache = readCache(r, c.Env, c.AWSRegion)
	c.ClickQueueURL = r.Required("CLICK_QUEUE_URL")
	c.VisitorSalt = r.Secret("VISITOR_HASH_SALT", true)
	if c.VisitorSalt.IsSet() && len(c.VisitorSalt.Reveal()) < 32 {
		r.Problemf("VISITOR_HASH_SALT must be at least 32 characters")
	}
	c.ClientIPSource = r.OneOf("CLIENT_IP_SOURCE", IPSourceCloudFront, IPSourceCloudFront, IPSourceRemoteAddr)
	if IsAWSEnv(c.Env) && c.ClientIPSource != IPSourceCloudFront {
		r.Problemf("CLIENT_IP_SOURCE must be cloudfront when APP_ENV is dev or prod")
	}
	c.ClickBuffer = r.Int("CLICK_BUFFER", 10000, 10, 1_000_000)
	c.ClickFlushInterval = r.Duration("CLICK_FLUSH_INTERVAL", time.Second)
	c.ClickSenders = r.Int("CLICK_SENDERS", 4, 1, 64)
	return c, r.Err()
}

// Scanner kinds.
const (
	ScannerSafeBrowsing = "safebrowsing"
	ScannerFake         = "fake"
	ScannerNoop         = "noop"
)

// API configures the API service.
type API struct {
	Common
	Cache                Cache
	AnalyticsTable       string
	BaseURL              *url.URL
	BlockedHosts         []string
	KeyBlockSize         int
	SqidsAlphabet        string
	Scanner              string
	SafeBrowsingKey      Secret
	SafeBrowsingEndpoint string
	WriteLimitPerMin     int
	ReadLimitPerMin      int
	AuthCacheTTL         time.Duration
}

// LoadAPI parses and validates the API service configuration.
func LoadAPI(lookup LookupFunc) (API, error) {
	r := NewReader(lookup)
	c := API{Common: readCommon(r, "api")}
	c.Cache = readCache(r, c.Env, c.AWSRegion)
	c.AnalyticsTable = r.Required("ANALYTICS_TABLE")
	c.BaseURL = parseBaseURL(r, "BASE_URL")
	c.BlockedHosts = r.List("BLOCKED_HOSTS")
	if c.BaseURL != nil {
		c.BlockedHosts = append(c.BlockedHosts, c.BaseURL.Hostname())
	}
	c.KeyBlockSize = r.Int("KEY_BLOCK_SIZE", 1000, 1, 1_000_000)
	c.SqidsAlphabet = r.Required("SQIDS_ALPHABET")
	if c.SqidsAlphabet != "" {
		if err := ValidateAlphabet(c.SqidsAlphabet); err != nil {
			r.Problemf("SQIDS_ALPHABET %v", err)
		}
	}
	c.Scanner, c.SafeBrowsingKey, c.SafeBrowsingEndpoint = readScanner(r)
	c.WriteLimitPerMin = r.Int("RATE_LIMIT_WRITE_PER_MIN", 60, 1, 1_000_000)
	c.ReadLimitPerMin = r.Int("RATE_LIMIT_READ_PER_MIN", 300, 1, 1_000_000)
	c.AuthCacheTTL = r.Duration("AUTH_CACHE_TTL", 60*time.Second)
	return c, r.Err()
}

func readScanner(r *Reader) (kind string, key Secret, endpoint string) {
	kind = r.OneOf("SCANNER", ScannerNoop, ScannerSafeBrowsing, ScannerFake, ScannerNoop)
	key = r.Secret("SAFE_BROWSING_API_KEY", kind == ScannerSafeBrowsing)
	endpoint = r.String("SAFE_BROWSING_ENDPOINT", "https://safebrowsing.googleapis.com")
	if u, err := url.Parse(endpoint); err != nil || u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		r.Problemf("SAFE_BROWSING_ENDPOINT must be an absolute http(s) URL")
	}
	return kind, key, endpoint
}

func parseBaseURL(r *Reader, key string) *url.URL {
	raw := r.Required(key)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		r.Problemf("%s must be an absolute http(s) URL without path or query", key)
		return nil
	}
	u.Path = ""
	return u
}

// ValidateAlphabet checks a Sqids alphabet: unique ASCII letters/digits, at least 3.
func ValidateAlphabet(a string) error {
	if len(a) < 3 {
		return fmt.Errorf("must have at least 3 characters")
	}
	seen := make(map[rune]bool, len(a))
	for _, ch := range a {
		isAlnum := (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
		if !isAlnum {
			return fmt.Errorf("must contain only ASCII letters and digits")
		}
		if seen[ch] {
			return fmt.Errorf("must not repeat characters")
		}
		seen[ch] = true
	}
	return nil
}
