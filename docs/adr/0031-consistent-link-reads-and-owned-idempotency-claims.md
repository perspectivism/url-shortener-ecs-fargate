# ADR-0031: Strongly consistent link reads and owned idempotency claims

- **Status:** Accepted (supersedes the eventually consistent redirect reads of [ADR-0004](0004-dynamodb-for-the-link-store.md) and refines the idempotency claim of [ADR-0030](0030-consistency-under-concurrency.md))
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

A second external review of the [ADR-0030](0030-consistency-under-concurrency.md)
fixes found two gaps:

1. **The fence stopped stale cache writes, not stale reads.** Redirects that
   miss the cache read DynamoDB with eventually consistent `GetItem`
   ([ADR-0004](0004-dynamodb-for-the-link-store.md): "half the cost"). Right
   after a disable, such a read can still return the active item and
   redirect. The runbook promises that a disable takes effect at once.
2. **Idempotency claims had no owner.** If request A outlived its 30-second
   claim, request B could claim the key and finish. A's late `Release` then
   deleted B's stored response, or A's `Put` overwrote it.

## Decision Drivers

- A phishing disable must take effect immediately, for every request
- A request may only finish or release its own claim
- Small, local changes with regression tests

## Considered Options

- For 1: keep eventually consistent reads and soften the runbook; **strongly consistent `GetItem`**.
- For 2: longer leases (still unowned); **a unique token per claim, with finish and release conditional on it**.

## Decision

- **Strongly consistent link reads.** `GetItem` on the links table uses
  `ConsistentRead`, so a read after a completed write sees that write.
  - It runs only on cache misses, and during the 10-second fence after a
    change, so the cost stays small: one read unit per miss instead of half.
  - Estimate: at the design load of 4,000 redirects/s with a 90 % cache hit
    ratio, that is about 200 extra read units per second, roughly $65 a month
    on demand. At demo traffic it is negligible.
- **Owned claims.** A claim stores the body fingerprint plus a random token.
  The request keeps the exact value it wrote. Two small Valkey scripts make
  the follow-up steps conditional:
  - **release** deletes the key only while it holds that claim;
  - **finish** stores the response only while the key holds that claim or is
    empty (the claim expired and nobody else took it). It never overwrites
    another request's record.
- **The work fits the lease.** Claimed work runs under a 10-second deadline
  (`IdemWorkDeadline`), a third of the 30-second claim. The server's write
  timeout does not cancel a handler, so this deadline is what bounds it.

## Consequences

### Positive

- After a disable returns, every request, cached or not, answers 410.
- One request can never destroy another's idempotency record; a test
  reproduces the expired-lease case.

### Negative

- Strongly consistent reads cost twice as much per cache miss.
- A create that needs more than 10 seconds fails (and releases its claim) instead of running on.

## Interview Angle

> "An invalidation is only as good as the next read. We fence the cache and
> read DynamoDB strongly consistently on a miss, because eventually consistent
> reads can return the version you just disabled. And a lease needs an owner:
> each idempotency claim carries a token, so a slow request can't clobber the
> one that replaced it."
