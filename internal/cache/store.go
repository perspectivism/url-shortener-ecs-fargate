package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/shortener/url-shortener-ecs-fargate/internal/links"
)

// LinkCache stores redirect entries under l:{code} (SPEC §7).
type LinkCache struct{ c redis.Cmdable }

// NewLinkCache returns a LinkCache.
func NewLinkCache(c redis.Cmdable) *LinkCache { return &LinkCache{c: c} }

func linkKey(code string) string { return "l:" + code }

// GetLink implements links.ResolverCache.
func (lc *LinkCache) GetLink(ctx context.Context, code string) (links.CacheEntry, bool, error) {
	b, err := lc.c.Get(ctx, linkKey(code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return links.CacheEntry{}, false, nil
	}
	if err != nil {
		return links.CacheEntry{}, false, fmt.Errorf("cache get: %w", err)
	}
	var e links.CacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		// A corrupt entry is treated as a miss and overwritten by the reload.
		return links.CacheEntry{}, false, nil
	}
	return e, true, nil
}

// SetLink implements links.ResolverCache.
func (lc *LinkCache) SetLink(ctx context.Context, code string, e links.CacheEntry, ttl time.Duration) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cache encode: %w", err)
	}
	if err := lc.c.Set(ctx, linkKey(code), b, ttl).Err(); err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

// DeleteLink implements links.Invalidator.
func (lc *LinkCache) DeleteLink(ctx context.Context, code string) error {
	if err := lc.c.Del(ctx, linkKey(code)).Err(); err != nil {
		return fmt.Errorf("cache del: %w", err)
	}
	return nil
}

// IdempotencyTTL is how long create responses are replayable (SPEC §7).
const IdempotencyTTL = 24 * time.Hour

// IdemRecord is a stored response for an Idempotency-Key.
type IdemRecord struct {
	Fingerprint string          `json:"f"`
	Status      int             `json:"s"`
	Body        json.RawMessage `json:"b"`
}

// Idempotency stores create responses under idem:{owner}:{sha256(key)}. The
// client-supplied key is hashed so arbitrary header bytes never form Valkey keys.
type Idempotency struct{ c redis.Cmdable }

// NewIdempotency returns an Idempotency store.
func NewIdempotency(c redis.Cmdable) *Idempotency { return &Idempotency{c: c} }

func idemKey(owner, key string) string {
	sum := sha256.Sum256([]byte(key))
	return "idem:" + owner + ":" + hex.EncodeToString(sum[:16])
}

// Get returns the stored record, if any.
func (i *Idempotency) Get(ctx context.Context, owner, key string) (IdemRecord, bool, error) {
	b, err := i.c.Get(ctx, idemKey(owner, key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return IdemRecord{}, false, nil
	}
	if err != nil {
		return IdemRecord{}, false, fmt.Errorf("idempotency get: %w", err)
	}
	var r IdemRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return IdemRecord{}, false, fmt.Errorf("idempotency decode: %w", err)
	}
	return r, true, nil
}

// Put stores rec for 24 h.
func (i *Idempotency) Put(ctx context.Context, owner, key string, rec IdemRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("idempotency encode: %w", err)
	}
	if err := i.c.Set(ctx, idemKey(owner, key), b, IdempotencyTTL).Err(); err != nil {
		return fmt.Errorf("idempotency set: %w", err)
	}
	return nil
}
