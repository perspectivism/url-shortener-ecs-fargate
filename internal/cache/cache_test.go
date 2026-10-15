package cache

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/redis/go-redis/v9"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
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
	if err := lc.FillLink(ctx, "abc", links.CacheEntry{URL: "https://example.com", Exp: &exp, Status: links.StatusActive}, time.Hour); err != nil {
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
	_ = lc.FillLink(ctx, "gone", links.CacheEntry{NotFound: true}, time.Minute)
	if raw, _ := mr.Get("l:gone"); raw != `{"nf":true}` {
		t.Fatalf("negative wire format = %s", raw)
	}
	// Invalidation: the entry becomes a fence for links.FenceTTL that reads as
	// a miss and that a racing fill (SET NX) cannot overwrite.
	if err := lc.InvalidateLink(ctx, "abc"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := mr.Get("l:abc"); raw != fence || mr.TTL("l:abc") != links.FenceTTL {
		t.Fatalf("fence = %q ttl %v", raw, mr.TTL("l:abc"))
	}
	if _, ok, err := lc.GetLink(ctx, "abc"); ok || err != nil {
		t.Fatal("a fence reads as a miss")
	}
	if err := lc.FillLink(ctx, "abc", links.CacheEntry{URL: "https://stale.example", Status: links.StatusActive}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if raw, _ := mr.Get("l:abc"); raw != fence {
		t.Fatalf("a fill overwrote the fence: %s", raw)
	}
	mr.FastForward(links.FenceTTL)
	if err := lc.FillLink(ctx, "abc", links.CacheEntry{URL: "https://new.example", Status: links.StatusActive}, time.Hour); err != nil {
		t.Fatal(err)
	}
	if e, ok, _ := lc.GetLink(ctx, "abc"); !ok || e.URL != "https://new.example" {
		t.Fatalf("fill after the fence expired: %+v %v", e, ok)
	}
	// A fill never replaces an existing entry either.
	_ = lc.FillLink(ctx, "abc", links.CacheEntry{URL: "https://other.example", Status: links.StatusActive}, time.Hour)
	if e, _, _ := lc.GetLink(ctx, "abc"); e.URL != "https://new.example" {
		t.Fatalf("fill replaced an entry: %+v", e)
	}
	_ = mr.Set("l:corrupt", "{not json")
	if _, ok, err := lc.GetLink(ctx, "corrupt"); ok || err != nil {
		t.Fatal("corrupt entries are misses")
	}

	mr.Close()
	if _, _, err := lc.GetLink(ctx, "abc"); err == nil {
		t.Fatal("expected error when valkey is down")
	}
	if err := lc.FillLink(ctx, "abc", links.CacheEntry{}, time.Second); err == nil {
		t.Fatal("expected set error")
	}
	if err := lc.InvalidateLink(ctx, "abc"); err == nil {
		t.Fatal("expected invalidate error")
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
	if ok, err := idem.Put(ctx, "alice", "k1", "", rec); err != nil || !ok {
		t.Fatal(ok, err)
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
	if _, err := idem.Put(ctx, "alice", "k1", "", rec); err == nil {
		t.Fatal("expected error")
	}
}

// Regression (review finding 4): concurrent requests with one key across API
// tasks: exactly one claims it; the others see the claim, then the response.
func TestIdempotencyClaimIsAtomic(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	idem := NewIdempotency(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}))
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		wins int
	)
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec, claim, err := idem.Claim(ctx, "alice", "k", "fp")
			if err != nil {
				t.Error(err)
				return
			}
			if claim == "" && (rec.Status != 0 || rec.Fingerprint != "fp") {
				t.Errorf("loser saw %+v, want the pending claim", rec)
			}
			if claim != "" {
				mu.Lock()
				wins++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("%d requests claimed the key, want 1", wins)
	}
	if ttl := mr.TTL(idemKey("alice", "k")); ttl != IdemClaimTTL {
		t.Fatalf("claim ttl = %v", ttl)
	}
	// Release lets a retry claim the key again.
	raw, _ := mr.Get(idemKey("alice", "k"))
	_ = idem.Release(ctx, "alice", "k", raw)
	_, c2, _ := idem.Claim(ctx, "alice", "k", "fp")
	if c2 == "" {
		t.Fatal("released key must be claimable")
	}
	if ok, err := idem.Put(ctx, "alice", "k", c2, IdemRecord{Fingerprint: "fp", Status: 201, Body: []byte(`{}`)}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if rec, claim, _ := idem.Claim(ctx, "alice", "k", "fp"); claim != "" || rec.Status != 201 {
		t.Fatalf("after Put: %+v claim=%q", rec, claim)
	}
	if ttl := mr.TTL(idemKey("alice", "k")); ttl != IdempotencyTTL {
		t.Fatalf("response ttl = %v", ttl)
	}
	// An abandoned claim expires.
	_, _, _ = idem.Claim(ctx, "alice", "k2", "fp")
	mr.FastForward(IdemClaimTTL)
	if _, claim, _ := idem.Claim(ctx, "alice", "k2", "fp"); claim == "" {
		t.Fatal("expired claim must be claimable")
	}
}

// Regression (second review, finding 2): request A outlives its claim; B
// claims the key and finishes. A's late Release or Put must not delete or
// overwrite B's record.
func TestIdempotencyClaimOwnership(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	idem := NewIdempotency(redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1}))
	_, a, _ := idem.Claim(ctx, "alice", "k", "fp")
	mr.FastForward(IdemClaimTTL) // A's lease expires
	_, b, _ := idem.Claim(ctx, "alice", "k", "fp")
	if a == "" || b == "" || a == b {
		t.Fatalf("claims a=%q b=%q must both succeed with distinct tokens", a, b)
	}
	if ok, _ := idem.Put(ctx, "alice", "k", b, IdemRecord{Fingerprint: "fp", Status: 201, Body: []byte(`{"code":"bbb"}`)}); !ok {
		t.Fatal("B must store its response")
	}
	if err := idem.Release(ctx, "alice", "k", a); err != nil {
		t.Fatal(err)
	}
	if ok, _ := idem.Put(ctx, "alice", "k", a, IdemRecord{Fingerprint: "fp", Status: 201, Body: []byte(`{"code":"aaa"}`)}); ok {
		t.Fatal("A's late Put must not overwrite B's record")
	}
	got, found, _ := idem.Get(ctx, "alice", "k")
	if !found || string(got.Body) != `{"code":"bbb"}` {
		t.Fatalf("B's record = %+v found=%v, want B's response", got, found)
	}
	// B's own release would also not delete a finished response.
	_ = idem.Release(ctx, "alice", "k", b)
	if _, found, _ := idem.Get(ctx, "alice", "k"); !found {
		t.Fatal("release must never delete a finished response")
	}
	if IdemWorkDeadline >= IdemClaimTTL {
		t.Fatal("claimed work must be bounded inside the claim lease")
	}
}
