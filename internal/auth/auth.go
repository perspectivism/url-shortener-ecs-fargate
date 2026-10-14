// Package auth implements API-key authentication (ADR-0015).
//
// Keys are "shortener_{env}_" + base64url(32 random bytes), shown once at
// creation and stored only as SHA-256 hex. A fast hash is appropriate: with 256
// bits of entropy, brute-forcing the hash is infeasible, so slow password
// hashing (bcrypt/argon2) would only add latency to every request.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
)

// Scopes.
const (
	ScopeLinksRead  = "links:read"
	ScopeLinksWrite = "links:write"
	ScopeAdmin      = "admin"
)

// ValidScope reports whether s is a known scope.
func ValidScope(s string) bool {
	return s == ScopeLinksRead || s == ScopeLinksWrite || s == ScopeAdmin
}

// Key statuses.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// keyRandomBytes is the key entropy (SPEC §10).
const keyRandomBytes = 32

// Errors.
var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrKeyNotFound  = errors.New("api key not found")
)

// Principal is an authenticated caller.
type Principal struct {
	OwnerID string
	Scopes  []string
	// KeyID is a short, non-secret identifier (hash prefix) for logs.
	KeyID string
}

// Has reports whether the principal holds scope.
func (p Principal) Has(scope string) bool { return slices.Contains(p.Scopes, scope) }

// KeyRecord is a stored API key (hash only).
type KeyRecord struct {
	Hash        string
	OwnerID     string
	Name        string
	Scopes      []string
	Status      string
	CreatedAt   time.Time
	LastUsedDay string
}

// Prefix returns the key prefix for env.
func Prefix(env string) string { return "shortener_" + env + "_" }

// GenerateKey creates a new key for env from rnd (crypto/rand.Reader in
// production). It returns the plaintext (show once) and its hash (store).
func GenerateKey(env string, rnd io.Reader) (key, hash string, err error) {
	b := make([]byte, keyRandomBytes)
	if _, err := io.ReadFull(rnd, b); err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	key = Prefix(env) + base64.RawURLEncoding.EncodeToString(b)
	return key, HashKey(key), nil
}

// HashKey returns the hex SHA-256 of key.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// KeyID returns the short identifier used in logs for a key hash.
func KeyID(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// wellFormed checks the key shape for env without touching storage.
func wellFormed(key, env string) bool {
	rest, ok := strings.CutPrefix(key, Prefix(env))
	if !ok || len(rest) != base64.RawURLEncoding.EncodedLen(keyRandomBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(rest)
	return err == nil
}

// KeyStore reads and touches stored keys.
type KeyStore interface {
	GetAPIKey(ctx context.Context, hash string) (KeyRecord, error)
	TouchAPIKey(ctx context.Context, hash, day string) error
}

type cached struct {
	rec     KeyRecord
	expires time.Time
}

// Authenticator validates bearer tokens with a short-lived in-process cache.
// Revocation therefore takes effect within the cache TTL (60 s by default).
type Authenticator struct {
	store      KeyStore
	env        string
	ttl        time.Duration
	now        func() time.Time
	logger     *slog.Logger
	maxEntries int

	mu    sync.Mutex
	cache map[string]cached
}

// NewAuthenticator returns an Authenticator for env.
func NewAuthenticator(store KeyStore, env string, ttl time.Duration, now func() time.Time, logger *slog.Logger) *Authenticator {
	return &Authenticator{store: store, env: env, ttl: ttl, now: now, logger: logger, maxEntries: 10_000, cache: map[string]cached{}}
}

// Authenticate validates an Authorization header value.
func (a *Authenticator) Authenticate(ctx context.Context, header string) (Principal, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || !wellFormed(token, a.env) {
		return Principal{}, ErrUnauthorized
	}
	hash := HashKey(token)
	now := a.now()

	rec, hit := a.lookupCache(hash, now)
	if !hit {
		var err error
		rec, err = a.store.GetAPIKey(ctx, hash)
		if errors.Is(err, ErrKeyNotFound) {
			return Principal{}, ErrUnauthorized
		}
		if err != nil {
			return Principal{}, fmt.Errorf("authenticate: %w", err)
		}
		a.storeCache(hash, rec, now)
	}
	if rec.Status != StatusActive {
		return Principal{}, ErrUnauthorized
	}
	a.touch(ctx, hash, rec, now)
	return Principal{OwnerID: rec.OwnerID, Scopes: rec.Scopes, KeyID: KeyID(hash)}, nil
}

func (a *Authenticator) lookupCache(hash string, now time.Time) (KeyRecord, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.cache[hash]
	if !ok || now.After(c.expires) {
		return KeyRecord{}, false
	}
	return c.rec, true
}

func (a *Authenticator) storeCache(hash string, rec KeyRecord, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= a.maxEntries {
		clear(a.cache) // bounded memory; entries are cheap to reload
	}
	a.cache[hash] = cached{rec: rec, expires: now.Add(a.ttl)}
}

// touch records last_used_day at most once per key per day per task.
func (a *Authenticator) touch(ctx context.Context, hash string, rec KeyRecord, now time.Time) {
	day := now.UTC().Format(time.DateOnly)
	if rec.LastUsedDay == day {
		return
	}
	tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 200*time.Millisecond)
	defer cancel()
	if err := a.store.TouchAPIKey(tctx, hash, day); err != nil {
		a.logger.WarnContext(ctx, "api key last_used_day update failed", "key_id", KeyID(hash), "err", err)
		return
	}
	a.mu.Lock()
	if c, ok := a.cache[hash]; ok {
		c.rec.LastUsedDay = day
		a.cache[hash] = c
	}
	a.mu.Unlock()
}
