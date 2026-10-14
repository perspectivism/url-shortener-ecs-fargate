package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"
)

type fakeStore struct {
	mu       sync.Mutex
	keys     map[string]KeyRecord
	gets     int
	touches  []string
	getErr   error
	touchErr error
}

func (f *fakeStore) GetAPIKey(_ context.Context, hash string) (KeyRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if f.getErr != nil {
		return KeyRecord{}, f.getErr
	}
	r, ok := f.keys[hash]
	if !ok {
		return KeyRecord{}, ErrKeyNotFound
	}
	return r, nil
}

func (f *fakeStore) TouchAPIKey(_ context.Context, hash, day string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touches = append(f.touches, day)
	if f.touchErr != nil {
		return f.touchErr
	}
	r := f.keys[hash]
	r.LastUsedDay = day
	f.keys[hash] = r
	return nil
}

func TestGenerateKey(t *testing.T) {
	key, hash, err := GenerateKey("dev", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "shortener_dev_") || len(key) != len("shortener_dev_")+43 || !wellFormed(key, "dev") {
		t.Fatalf("bad key shape: %q", key)
	}
	if HashKey(key) != hash || len(hash) != 64 || KeyID(hash) != hash[:12] || KeyID("abc") != "abc" {
		t.Fatal("hash mismatch")
	}
	if _, _, err := GenerateKey("dev", iotest.ErrReader(errors.New("no entropy"))); err == nil {
		t.Fatal("expected entropy error")
	}
	k2, _, _ := GenerateKey("dev", bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	if k2 == key {
		t.Fatal("keys must differ")
	}
}

func setup(t *testing.T) (*Authenticator, *fakeStore, string, *time.Time) {
	t.Helper()
	key, hash, _ := GenerateKey("dev", rand.Reader)
	st := &fakeStore{keys: map[string]KeyRecord{hash: {Hash: hash, OwnerID: "alice", Scopes: []string{ScopeLinksRead}, Status: StatusActive}}}
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	a := NewAuthenticator(st, "dev", time.Minute, func() time.Time { return now }, slog.New(slog.DiscardHandler))
	return a, st, key, &now
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	a, st, key, now := setup(t)

	p, err := a.Authenticate(ctx, "Bearer "+key)
	if err != nil || p.OwnerID != "alice" || !p.Has(ScopeLinksRead) || p.Has(ScopeAdmin) || len(p.KeyID) != 12 {
		t.Fatalf("principal = %+v, %v", p, err)
	}
	if len(st.touches) != 1 || st.touches[0] != "2026-10-14" {
		t.Fatalf("touches = %v", st.touches)
	}
	// Cached: no second read, no second touch the same day.
	if _, err := a.Authenticate(ctx, "Bearer "+key); err != nil || st.gets != 1 || len(st.touches) != 1 {
		t.Fatalf("expected cache hit: gets=%d touches=%d", st.gets, len(st.touches))
	}
	// Next day: touched again (cache still valid within TTL? no — TTL passed, reload).
	*now = now.Add(24 * time.Hour)
	if _, err := a.Authenticate(ctx, "Bearer "+key); err != nil || st.gets != 2 || len(st.touches) != 2 {
		t.Fatalf("next day: gets=%d touches=%d", st.gets, len(st.touches))
	}

	// Revocation takes effect once the cache entry expires.
	st.keys[HashKey(key)] = KeyRecord{OwnerID: "alice", Status: StatusRevoked}
	if _, err := a.Authenticate(ctx, "Bearer "+key); err != nil {
		t.Fatal("still cached within TTL")
	}
	*now = now.Add(2 * time.Minute)
	if _, err := a.Authenticate(ctx, "Bearer "+key); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked key = %v", err)
	}
}

func TestAuthenticateRejects(t *testing.T) {
	ctx := context.Background()
	a, st, key, _ := setup(t)
	unknown, _, _ := GenerateKey("dev", rand.Reader)
	prodKey, _, _ := GenerateKey("prod", rand.Reader)
	for name, h := range map[string]string{
		"empty":          "",
		"basic":          "Basic " + key,
		"no prefix":      key,
		"other env":      "Bearer " + prodKey,
		"truncated":      "Bearer " + key[:len(key)-1],
		"bad base64":     "Bearer shortener_dev_" + strings.Repeat("*", 43),
		"unknown key":    "Bearer " + unknown,
		"lowercase auth": "bearer " + key,
	} {
		if _, err := a.Authenticate(ctx, h); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if st.gets != 1 {
		t.Fatalf("malformed keys must not reach storage (gets=%d)", st.gets)
	}
	st.getErr = errors.New("dynamodb down")
	other, otherHash, _ := GenerateKey("dev", rand.Reader)
	st.keys[otherHash] = KeyRecord{Status: StatusActive}
	if _, err := a.Authenticate(ctx, "Bearer "+other); err == nil || errors.Is(err, ErrUnauthorized) {
		t.Fatalf("storage errors must not look like 401: %v", err)
	}
}

func TestTouchFailureAndEviction(t *testing.T) {
	a, st, key, _ := setup(t)
	st.touchErr = errors.New("throttled")
	if _, err := a.Authenticate(context.Background(), "Bearer "+key); err != nil {
		t.Fatalf("touch failure must not fail auth: %v", err)
	}
	a.maxEntries = 1
	k2, h2, _ := GenerateKey("dev", rand.Reader)
	st.keys[h2] = KeyRecord{OwnerID: "bob", Status: StatusActive, Scopes: []string{ScopeAdmin}}
	if p, err := a.Authenticate(context.Background(), "Bearer "+k2); err != nil || p.OwnerID != "bob" {
		t.Fatal(err)
	}
	if len(a.cache) != 1 {
		t.Fatalf("cache size = %d, want bounded to 1", len(a.cache))
	}
	if !ValidScope(ScopeAdmin) || ValidScope("root") {
		t.Fatal("ValidScope")
	}
}
