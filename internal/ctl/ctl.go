// Package ctl implements shortenerctl, the operator CLI: local emulator
// initialisation and API key management.
package ctl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shortener/url-shortener-ecs-fargate/internal/auth"
	"github.com/shortener/url-shortener-ecs-fargate/internal/localdev"
	"github.com/shortener/url-shortener-ecs-fargate/internal/store"
)

// DynamoDB is what ctl needs from DynamoDB (data plane + table creation).
type DynamoDB interface {
	store.DynamoDB
	store.SchemaAPI
}

// Clients are the AWS clients ctl uses (emulators locally).
type Clients struct {
	DynamoDB DynamoDB
	SQS      localdev.SQSAdmin
	S3       localdev.S3Admin
}

// Env is the CLI's environment: I/O, configuration and injectable sources.
type Env struct {
	Getenv     func(string) string
	NewClients func(ctx context.Context) (Clients, error)
	Stdout     io.Writer
	Stderr     io.Writer
	Rand       io.Reader
	Now        func() time.Time
	// InitAttempts bounds local-init retries while emulators start (default 60, 1 s apart).
	InitAttempts int
	InitDelay    time.Duration
}

const usage = `usage: shortenerctl <command> [flags]

commands:
  local-init                 create local tables, queues and bucket (emulators only)
  apikey create              create an API key (printed once)
  apikey revoke              revoke an API key by hash
`

// Run executes the CLI and returns the process exit code.
func Run(ctx context.Context, args []string, e Env) int {
	if len(args) == 0 {
		fmt.Fprint(e.Stderr, usage)
		return 2
	}
	var err error
	switch {
	case args[0] == "local-init":
		err = localInit(ctx, args[1:], e)
	case args[0] == "apikey" && len(args) > 1 && args[1] == "create":
		err = apikeyCreate(ctx, args[2:], e)
	case args[0] == "apikey" && len(args) > 1 && args[1] == "revoke":
		err = apikeyRevoke(ctx, args[2:], e)
	default:
		fmt.Fprint(e.Stderr, usage)
		return 2
	}
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(e.Stderr, "error:", err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(e.Stderr, "error:", err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

func newFlags(name string, e Env) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	return fs
}

func def(e Env, key, fallback string) string {
	if v := e.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func localInit(ctx context.Context, args []string, e Env) error {
	fs := newFlags("local-init", e)
	linksTable := fs.String("links-table", def(e, "LINKS_TABLE", "shortener-local-links"), "links table name")
	analyticsTable := fs.String("analytics-table", def(e, "ANALYTICS_TABLE", "shortener-local-analytics"), "analytics table name")
	queue := fs.String("queue", def(e, "CLICK_QUEUE_NAME", "shortener-local-click-events"), "click queue name")
	dlq := fs.String("dlq", def(e, "CLICK_DLQ_NAME", "shortener-local-click-events-dlq"), "dead-letter queue name")
	bucket := fs.String("bucket", def(e, "EVENTS_BUCKET", "shortener-local-events"), "raw events bucket")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if env := e.Getenv("APP_ENV"); env != "local" && env != "test" {
		return usageError{"local-init only runs with APP_ENV=local or APP_ENV=test (AWS resources are created by Terraform)"}
	}
	attempts, delay := e.InitAttempts, e.InitDelay
	if attempts < 1 {
		attempts, delay = 60, time.Second
	}
	var err error
	for i := range attempts {
		if err = initResources(ctx, e, *linksTable, *analyticsTable, *queue, *dlq, *bucket); err == nil {
			return nil
		}
		if i < attempts-1 {
			fmt.Fprintf(e.Stderr, "local-init: %v (retrying)\n", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}
	return err
}

// initResources creates every local resource (each step is idempotent).
func initResources(ctx context.Context, e Env, linksTable, analyticsTable, queue, dlq, bucket string) error {
	c, err := e.NewClients(ctx)
	if err != nil {
		return err
	}
	for _, in := range []struct {
		name string
		fn   func(string) error
	}{
		{linksTable, func(n string) error {
			return store.EnsureTable(ctx, c.DynamoDB, store.LinksTableInput(n), 200*time.Millisecond)
		}},
		{analyticsTable, func(n string) error {
			return store.EnsureTable(ctx, c.DynamoDB, store.AnalyticsTableInput(n), 200*time.Millisecond)
		}},
	} {
		if err := in.fn(in.name); err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "table   %s ready\n", in.name)
	}
	qURL, dURL, err := localdev.EnsureQueues(ctx, c.SQS, queue, dlq)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "queue   %s\ndlq     %s\n", qURL, dURL)
	if err := localdev.EnsureBucket(ctx, c.S3, bucket); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "bucket  %s ready\n", bucket)
	return nil
}

func apikeyCreate(ctx context.Context, args []string, e Env) error {
	fs := newFlags("apikey create", e)
	owner := fs.String("owner", "", "owner id (required)")
	scopes := fs.String("scopes", auth.ScopeLinksRead+","+auth.ScopeLinksWrite, "comma-separated scopes: links:read, links:write, admin")
	name := fs.String("name", "", "human-readable key name")
	table := fs.String("table", def(e, "LINKS_TABLE", ""), "links table name")
	env := fs.String("env", def(e, "APP_ENV", ""), "environment (key prefix)")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if *owner == "" || *table == "" || *env == "" {
		return usageError{"--owner, --table (or LINKS_TABLE) and --env (or APP_ENV) are required"}
	}
	var list []string
	for _, s := range strings.Split(*scopes, ",") {
		s = strings.TrimSpace(s)
		if !auth.ValidScope(s) {
			return usageError{fmt.Sprintf("unknown scope %q", s)}
		}
		list = append(list, s)
	}
	key, hash, err := auth.GenerateKey(*env, e.Rand)
	if err != nil {
		return err
	}
	c, err := e.NewClients(ctx)
	if err != nil {
		return err
	}
	st := store.NewLinks(c.DynamoDB, *table, 5*time.Second)
	if err := st.PutAPIKey(ctx, auth.KeyRecord{Hash: hash, OwnerID: *owner, Name: *name, Scopes: list, Status: auth.StatusActive, CreatedAt: e.Now().UTC()}); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "api_key: %s\nkey_hash: %s\nowner: %s\nscopes: %s\n", key, hash, *owner, strings.Join(list, ","))
	fmt.Fprintln(e.Stderr, "Store the api_key now; it cannot be shown again. Revoke with: shortenerctl apikey revoke --hash <key_hash>")
	return nil
}

func apikeyRevoke(ctx context.Context, args []string, e Env) error {
	fs := newFlags("apikey revoke", e)
	hash := fs.String("hash", "", "key hash printed at creation (required)")
	table := fs.String("table", def(e, "LINKS_TABLE", ""), "links table name")
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if len(*hash) != 64 || *table == "" {
		return usageError{"--hash (64 hex chars) and --table (or LINKS_TABLE) are required"}
	}
	c, err := e.NewClients(ctx)
	if err != nil {
		return err
	}
	if err := store.NewLinks(c.DynamoDB, *table, 5*time.Second).RevokeAPIKey(ctx, *hash); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "revoked %s (takes effect within the API's auth cache TTL, 60 s)\n", auth.KeyID(*hash))
	return nil
}
