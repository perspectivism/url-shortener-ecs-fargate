package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/perspectivism/url-shortener-ecs-fargate/internal/links"
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
	if string(b) == fence {
		return links.CacheEntry{}, false, nil
	}
	var e links.CacheEntry
	if err := json.Unmarshal(b, &e); err != nil {
		// A corrupt entry is treated as a miss; it expires with its TTL.
		return links.CacheEntry{}, false, nil
	}
	return e, true, nil
}

// fence marks a code as recently mutated (links.FenceTTL). It is not JSON, so
// GetLink reads it as a miss, and fills (SET NX) cannot overwrite it.
const fence = "!"

// FillLink implements links.ResolverCache: SET NX, so a fill never replaces an
// invalidation fence (or an entry another fill already wrote).
func (lc *LinkCache) FillLink(ctx context.Context, code string, e links.CacheEntry, ttl time.Duration) error {
	b, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("cache encode: %w", err)
	}
	if err := lc.c.SetNX(ctx, linkKey(code), b, ttl).Err(); err != nil {
		return fmt.Errorf("cache set: %w", err)
	}
	return nil
}

// InvalidateLink implements links.Invalidator: the entry is replaced by a
// fence for links.FenceTTL.
func (lc *LinkCache) InvalidateLink(ctx context.Context, code string) error {
	if err := lc.c.Set(ctx, linkKey(code), fence, links.FenceTTL).Err(); err != nil {
		return fmt.Errorf("cache invalidate: %w", err)
	}
	return nil
}

// IdempotencyTTL is how long create responses are replayable (SPEC §7).
const IdempotencyTTL = 24 * time.Hour

// IdemClaimTTL is the claim lease: how long a claimed key blocks repeats if
// the request that claimed it dies. The API bounds the claimed work to
// IdemWorkDeadline, well inside the lease, so a live request never outlives it.
const IdemClaimTTL = 30 * time.Second

// IdemWorkDeadline bounds the work done under a claim (see IdemClaimTTL).
const IdemWorkDeadline = IdemClaimTTL / 3

// IdemRecord is a stored response for an Idempotency-Key. Status 0 marks a
// claim: the first request is still being processed.
type IdemRecord struct {
	Fingerprint string          `json:"f"`
	Status      int             `json:"s"`
	Body        json.RawMessage `json:"b"`
	// Token identifies the request holding a claim (claims only).
	Token string `json:"t,omitempty"`
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

// Claim atomically reserves owner/key for the request with fingerprint (SET NX,
// IdemClaimTTL). If this request now owns the key, claim is the exact value it
// wrote (with a unique token): pass it to Put or Release, which act only while
// the key still holds that claim. Otherwise claim is empty and rec is the
// existing record (another claim, Status 0, or a finished response). Two
// concurrent requests cannot both proceed.
func (i *Idempotency) Claim(ctx context.Context, owner, key, fingerprint string) (rec IdemRecord, claim string, err error) {
	b, err := json.Marshal(IdemRecord{Fingerprint: fingerprint, Token: uuid.NewString()})
	if err != nil {
		return IdemRecord{}, "", fmt.Errorf("idempotency encode: %w", err)
	}
	ok, err := i.c.SetNX(ctx, idemKey(owner, key), b, IdemClaimTTL).Result()
	if err != nil {
		return IdemRecord{}, "", fmt.Errorf("idempotency claim: %w", err)
	}
	if ok {
		return IdemRecord{}, string(b), nil
	}
	rec, found, err := i.Get(ctx, owner, key)
	if err != nil {
		return IdemRecord{}, "", err
	}
	if !found { // expired between SETNX and GET: try once more
		return i.Claim(ctx, owner, key, fingerprint)
	}
	return rec, "", nil
}

// releaseScript deletes the key only while it holds this request's claim.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

// finishScript stores the response while the key holds this request's claim,
// or is empty (the claim expired and nobody else claimed it: the link exists,
// so its response must be replayable). Never over another request's record.
var finishScript = redis.NewScript(`
local v = redis.call("GET", KEYS[1])
if v == ARGV[1] or v == false then
  redis.call("SET", KEYS[1], ARGV[2], "EX", ARGV[3])
  return 1
end
return 0`)

// Release drops this request's claim (its create failed) so a retry can
// proceed. A key that no longer holds the claim is left alone.
func (i *Idempotency) Release(ctx context.Context, owner, key, claim string) error {
	if err := releaseScript.Run(ctx, i.c, []string{idemKey(owner, key)}, claim).Err(); err != nil {
		return fmt.Errorf("idempotency release: %w", err)
	}
	return nil
}

// Put stores the finished response rec for 24 h in place of this request's
// claim. It reports false, storing nothing, if another request's record holds
// the key.
func (i *Idempotency) Put(ctx context.Context, owner, key, claim string, rec IdemRecord) (bool, error) {
	b, err := json.Marshal(rec)
	if err != nil {
		return false, fmt.Errorf("idempotency encode: %w", err)
	}
	n, err := finishScript.Run(ctx, i.c, []string{idemKey(owner, key)}, claim, b, int(IdempotencyTTL/time.Second)).Int()
	if err != nil {
		return false, fmt.Errorf("idempotency set: %w", err)
	}
	return n == 1, nil
}
