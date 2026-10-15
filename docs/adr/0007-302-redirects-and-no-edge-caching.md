# ADR-0007: 302 redirects and no edge caching of redirects

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

A redirect can be answered with 301 (permanent, cacheable by browsers and
proxies) or 302 (temporary). The product needs click analytics for every visit
(SPEC F6) and the ability to change a destination, disable a malicious link or
expire a link at any time (SPEC F4, F5, F8).

## Decision Drivers

- Every click must reach the origin to be counted
- Disabling a link must take effect immediately for all visitors
- Edge caching would cut latency and cost but bypass both of the above

## Considered Options

1. 301 Moved Permanently
2. 302 Found, cached at CloudFront for a short TTL
3. 302 Found, `Cache-Control: private, no-store`, CloudFront `CachingDisabled`
4. 307/308 (method-preserving variants)

## Decision

Chosen option: **3 — 302, never cached at the edge.** Both CloudFront
behaviors use the managed `CachingDisabled` policy (M5); the origin sends:

| Header | Value | Why |
| --- | --- | --- |
| `Location` | normalized destination | the redirect |
| `Cache-Control` | `private, no-store, max-age=0` | no browser/proxy caching, so every click is counted |
| `X-Robots-Tag` | `noindex` | short links should not be indexed |
| `Referrer-Policy` | `no-referrer-when-downgrade` | destination sees the referrer except on HTTPS→HTTP |

Expiry is checked on every request from the cached `exp`, never relying on
DynamoDB TTL deletion timing. `HEAD` resolves identically but records no click.

## Consequences

### Positive

- Accurate analytics; immediate effect of disable/update/expiry.
- Simple mental model: the edge never decides where a code goes.

### Negative

- Every visit hits the origin, so latency and cost scale with clicks. The
  Valkey cache ([ADR-0008](0008-elasticache-serverless-valkey.md)) keeps the origin fast and DynamoDB cheap.
- No browser caching for repeat visitors.

## Summary

301 is cheaper but browsers cache it forever, which would lose analytics and the
ability to kill a phishing link. 302 with no-store means every click comes
back to the origin; the cache sits behind the edge instead of in it.

## Future Work

- CloudFront Functions + KeyValueStore for a small set of very hot, never-changing
  links, at the cost of edge-side analytics (SPEC §20).
