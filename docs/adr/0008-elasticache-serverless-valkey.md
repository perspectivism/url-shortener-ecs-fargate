# ADR-0008: ElastiCache Serverless for Valkey (vs DAX, vs no cache)

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

Redirects are ~100× more frequent than creates and follow a power law: a few
links take most clicks. The redirect target is p99 < 50 ms on a cache hit
(SPEC N1). We also need a shared store for rate-limit counters, idempotency
records and HyperLogLog unique-visitor counters (SPEC §7).

## Decision Drivers

- Sub-millisecond reads for hot links; protect DynamoDB from hot partitions
- Negative caching to absorb code-scanning (enumeration) traffic
- Data structures beyond key-value: counters with TTL, HyperLogLog
- No node sizing or patching; scales to zero-ish in dev
- Failure of the cache must never fail a redirect

## Considered Options

1. No cache (DynamoDB only)
2. DynamoDB Accelerator (DAX)
3. ElastiCache Serverless for Valkey
4. Node-based ElastiCache (Valkey/Redis OSS replication group)
5. In-process LRU per task only

## Decision

Chosen option: **3 — ElastiCache Serverless for Valkey**, engine major
version 9 (ElastiCache supports Valkey 7.2 → 9.1 as of 2026-10-14; local
compose uses `valkey/valkey:9.0.6`), TLS required, IAM authentication
(ADR-0009), CMK encryption, `cache_usage_limits` for storage and ECPU caps.

| Key | Value | TTL |
| --- | --- | --- |
| `l:{code}` | `{"u","exp","st"}` | min(1 h, time to expiry) |
| `l:{code}` | `{"nf":true}` | 60 s (negative cache) |
| `rl:{owner}:{route}:{window}` | counter | window + 1 s |
| `idem:{owner}:{sha256(key)}` | stored 201 response | 24 h |
| `hll:{{code}}:{date}` | HyperLogLog | 8 days |

Behaviour (`internal/links/resolver.go`, `internal/cache`):

- **Cache-aside** with a 50 ms read deadline; on miss or error, read DynamoDB.
- **Fail open:** cache errors increment `CacheErrors` and fall through to
  DynamoDB; rate limiting also fails open (WAF is the backstop).
- **Stampede protection:** `singleflight` per code per task, on a context
  detached from any single caller so one cancelled request cannot fail the rest.
- **Invalidation:** writes go to DynamoDB first, then `DEL l:{code}`, retried
  once; on failure `CacheInvalidationFailed` is emitted and staleness is
  bounded by the 1 h positive TTL.
- Gone links (disabled/deleted/expired) are cached **without** the destination.
- Locally, Valkey runs as a single standalone node without TLS/auth
  (`CACHE_MODE=standalone`); AWS uses the cluster client. Hash-tagged HLL keys
  keep multi-key commands in one slot in both modes.

## Consequences

### Positive

- Hot links are served from memory; DynamoDB sees roughly one read per code per hour.
- Code scanning hits the negative cache, not DynamoDB.
- One store for caching, rate limits, idempotency and uniques.

### Negative

- Serverless has a minimum billable data/ECPU footprint — a material share of
  dev idle cost (quantified in `docs/cost.md`).
- Another network hop on misses; another dependency to monitor.
- Up to 1 h staleness if both invalidation attempts fail (rare, alarmed).

**Why not DAX:** it only caches DynamoDB items — no negative caching of
arbitrary keys, no counters, no HyperLogLog — and is node-based with a higher
minimum cost.

## Interview Angle

> "Reads follow a power law, so cache-aside in Valkey makes the hot path a
> memory lookup. I cache misses too, so scanning random codes can't hammer the
> database, and the cache fails open — if it's down, redirects get slower, not broken."
