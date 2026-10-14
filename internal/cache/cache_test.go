package cache

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/redis/go-redis/v9"

	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
)

func TestIAMToken(t *testing.T) {
	p := &IAMTokenProvider{
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "secretkeyexample", ""),
		UserID:      "shortener-dev-app",
		CacheName:   "shortener-dev",
		Region:      "us-east-1",
		Serverless:  true,
		Now:         func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) },
	}
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "shortener-dev/?") || strings.HasPrefix(tok, "http") {
		t.Fatalf("token must be host/?query without scheme: %s", tok)
	}
	q, err := url.ParseQuery(tok[strings.Index(tok, "?")+1:])
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Action":              "connect",
		"User":                "shortener-dev-app",
		"ResourceType":        "ServerlessCache",
		"X-Amz-Algorithm":     "AWS4-HMAC-SHA256",
		"X-Amz-Credential":    "AKIDEXAMPLE/20261014/us-east-1/elasticache/aws4_request",
		"X-Amz-Date":          "20261014T120000Z",
		"X-Amz-Expires":       "900",
		"X-Amz-SignedHeaders": "host",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
	if len(q.Get("X-Amz-Signature")) != 64 {
		t.Error("missing signature")
	}
	// Deterministic for fixed inputs; different for a different user.
	tok2, _ := p.Token(context.Background())
	p.UserID = "other"
	tok3, _ := p.Token(context.Background())
	if tok != tok2 || tok == tok3 {
		t.Fatal("signature should depend only on inputs")
	}
	// Node-based caches omit ResourceType.
	p.Serverless = false
	tok4, _ := p.Token(context.Background())
	if strings.Contains(tok4, "ResourceType") {
		t.Fatal("ResourceType only for serverless")
	}
	bad := &IAMTokenProvider{Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{}, errors.New("no creds")
	})}
	if _, err := bad.Token(context.Background()); err == nil {
		t.Fatal("expected credentials error")
	}
	badName := &IAMTokenProvider{Credentials: p.Credentials, CacheName: "bad host\x7f"}
	if _, err := badName.Token(context.Background()); err == nil {
		t.Fatal("expected request error")
	}
}

func TestNewClient(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := New(Options{Addr: mr.Addr(), Mode: ModeStandalone, Auth: AuthNone})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Ping(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}

	// IAM-style auth: the credentials provider supplies user + token per connection.
	mr2 := miniredis.RunT(t)
	mr2.RequireUserAuth("app", "token-1")
	calls := 0
	tokenFn := func(context.Context) (string, error) { calls++; return "token-1", nil }
	c2, err := New(Options{Addr: mr2.Addr(), Mode: ModeStandalone, Auth: AuthIAM, User: "app", Token: tokenFn, TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = c2.Close() // TLS against miniredis is not possible; construction is what we verify here.
	stdOpts := redisOptionsForTest(t, mr2.Addr(), tokenFn)
	if err := stdOpts.Ping(context.Background()).Err(); err != nil || calls == 0 {
		t.Fatalf("auth via credentials provider failed: %v (calls=%d)", err, calls)
	}

	cl, err := New(Options{Addr: "cache.example:6379", Mode: ModeCluster, Auth: AuthIAM, User: "u", Token: tokenFn, TLS: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = cl.Close()

	for name, o := range map[string]Options{
		"no addr":      {Mode: ModeStandalone, Auth: AuthNone},
		"iam no tls":   {Addr: "x:1", Mode: ModeCluster, Auth: AuthIAM, User: "u", Token: tokenFn},
		"iam no token": {Addr: "x:1", Mode: ModeCluster, Auth: AuthIAM, User: "u", TLS: true},
		"bad auth":     {Addr: "x:1", Mode: ModeCluster, Auth: "password"},
		"bad mode":     {Addr: "x:1", Mode: "sentinel", Auth: AuthNone},
		"tls bad addr": {Addr: "nohostport", Mode: ModeCluster, Auth: AuthNone, TLS: true},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

// redisOptionsForTest builds a non-TLS client using the same credentials
// provider wiring New uses, to prove the per-connection token flow.
func redisOptionsForTest(t *testing.T, addr string, token TokenFunc) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: addr, CredentialsProviderContext: func(ctx context.Context) (string, string, error) {
		tok, err := token(ctx)
		return "app", tok, err
	}})
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestLinkCache(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	lc := NewLinkCache(c)

	if _, ok, err := lc.GetLink(ctx, "abc"); ok || err != nil {
		t.Fatalf("miss: ok=%v err=%v", ok, err)
	}
	exp := int64(1_900_000_000)
	if err := lc.SetLink(ctx, "abc", links.CacheEntry{URL: "https://example.com", Exp: &exp, Status: links.StatusActive}, time.Hour); err != nil {
		t.Fatal(err)
	}
	raw, _ := mr.Get("l:abc")
	if raw != `{"u":"https://example.com","exp":1900000000,"st":"active"}` {
		t.Fatalf("wire format = %s", raw)
	}
	if ttl := mr.TTL("l:abc"); ttl != time.Hour {
		t.Fatalf("ttl = %v", ttl)
	}
	e, ok, err := lc.GetLink(ctx, "abc")
	if !ok || err != nil || e.URL != "https://example.com" || *e.Exp != exp {
		t.Fatalf("get: %+v %v %v", e, ok, err)
	}
	_ = lc.SetLink(ctx, "gone", links.CacheEntry{NotFound: true}, time.Minute)
	if raw, _ := mr.Get("l:gone"); raw != `{"nf":true}` {
		t.Fatalf("negative wire format = %s", raw)
	}
	if err := lc.DeleteLink(ctx, "abc"); err != nil || mr.Exists("l:abc") {
		t.Fatal("delete failed")
	}
	_ = mr.Set("l:corrupt", "{not json")
	if _, ok, err := lc.GetLink(ctx, "corrupt"); ok || err != nil {
		t.Fatal("corrupt entries are misses")
	}

	mr.Close()
	if _, _, err := lc.GetLink(ctx, "abc"); err == nil {
		t.Fatal("expected error when valkey is down")
	}
	if err := lc.SetLink(ctx, "abc", links.CacheEntry{}, time.Second); err == nil {
		t.Fatal("expected set error")
	}
	if err := lc.DeleteLink(ctx, "abc"); err == nil {
		t.Fatal("expected del error")
	}
}

func TestIdempotency(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	idem := NewIdempotency(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}))
	if _, ok, err := idem.Get(ctx, "alice", "k1"); ok || err != nil {
		t.Fatal("expected miss")
	}
	rec := IdemRecord{Fingerprint: "fp", Status: 201, Body: json.RawMessage(`{"code":"abc"}`)}
	if err := idem.Put(ctx, "alice", "k1", rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := idem.Get(ctx, "alice", "k1")
	if !ok || err != nil || got.Status != 201 || string(got.Body) != `{"code":"abc"}` {
		t.Fatalf("get: %+v %v %v", got, ok, err)
	}
	if _, ok, _ := idem.Get(ctx, "bob", "k1"); ok {
		t.Fatal("keys must be scoped per owner")
	}
	key := idemKey("alice", "k1\r\nFLUSHALL")
	if strings.ContainsAny(key, "\r\n ") || !strings.HasPrefix(key, "idem:alice:") {
		t.Fatalf("unsafe key %q", key)
	}
	if ttl := mr.TTL(idemKey("alice", "k1")); ttl != IdempotencyTTL {
		t.Fatalf("ttl = %v", ttl)
	}
	_ = mr.Set(idemKey("alice", "bad"), "{")
	if _, _, err := idem.Get(ctx, "alice", "bad"); err == nil {
		t.Fatal("expected decode error")
	}
	mr.Close()
	if _, _, err := idem.Get(ctx, "alice", "k1"); err == nil {
		t.Fatal("expected error")
	}
	if err := idem.Put(ctx, "alice", "k1", rec); err == nil {
		t.Fatal("expected error")
	}
}
