# ADR-0030: Consistency under concurrency — invalidation fences, idempotency claims, dedupe window

- **Status:** Accepted (supersedes delete-on-write invalidation in [ADR-0008](0008-elasticache-serverless-valkey.md) and the 3-day dedupe marker TTL in [ADR-0010](0010-async-click-pipeline.md))
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

An external review reproduced four race conditions:

1. **A disabled link could come back into the cache.** A cache fill read the
   active link, the admin disabled it and deleted the cache key, and then the
   fill wrote the old entry back for up to an hour. The runbook's "410 at
   once" was false in that case.
2. **Idempotency was check-then-act.** Look up the key, create the link, store
   the response: two concurrent requests with the same key both created a link.
3. **A scan verdict could hit the wrong destination.** The nightly job applied
   a verdict for a code, not for a URL. If the owner changed the URL during
   the scan, an old unsafe verdict disabled the new URL.
4. **Dedupe markers expired too early.** Their TTL was the click time plus 3
   days, but the queue keeps messages for 4 days, so a backlog could outlive
   the marker. Separately, a repeat of an event in the same batch was marked
   `Duplicate` before the first copy was counted. If that copy failed, the
   repeat was acknowledged anyway. No click was lost, since the failed copy
   stays in SQS, but the outcome was wrong.

## Decision Drivers

- A disable must take effect at once and stay in effect (phishing response)
- Correct under concurrency across tasks, not only within one process
- No new infrastructure; small, testable changes

## Considered Options

- For 1: short positive TTLs (smaller window, still wrong); versioned entries
  with compare-and-set (a Lua script per fill); **invalidation fences**.
- For 2: a DynamoDB idempotency table with conditional writes (durable, an
  extra write per create); **an atomic claim in Valkey** (shared by all API tasks).

## Decision

- **Invalidation fences.** A mutation replaces the cache entry with a fence
  marker for 10 s (`links.FenceTTL`) instead of deleting it. Reads treat a
  fence as a miss. Fills use `SET NX`, so they never overwrite a fence or an
  entry. A fill that takes longer than 5 s writes nothing, so no fill can
  outlive the fence of a mutation that happened after its read. The
  `cache-invalidation-failed` alarm and the 1-hour bound on staleness still
  apply if Valkey is unreachable.
- **Idempotency claims.** Before creating, the API claims the key with
  `SET NX` (30 s TTL, holding the body fingerprint):
  - a concurrent repeat gets **409 `request_in_progress`** (or 400 if the body differs);
  - success stores the response for 24 h;
  - failure releases the claim, so the client can retry;
  - a crashed request's claim expires.

  Valkey errors still fail open, as before.
- **Verdicts are bound to the scanned URL.** `ApplyScan(code, scannedURL,
  verdict)` changes the link only if it still points at `scannedURL`; the
  check runs inside the conditional update. Otherwise the verdict is
  dropped (`ErrStaleScan`), and the new URL was already scanned on update
  and is scanned again next night.
- **Dedupe window.** The marker TTL is now 4 days from when the marker is
  written (`store.DedupeTTL`). A Terraform test keeps the click queue's
  retention within that window. An in-batch repeat takes its first copy's
  final result.

**Deviation recorded:** SPEC §5.2 sets the marker TTL to "now + 3 days";
it is now write time + 4 days. Redriving DLQ messages is safe within 4 days.

## Consequences

### Positive

- An admin disable is immediate and stays in effect; a test reproduces the race.
- One link per idempotency key, even across API tasks; tests at the unit and
  integration level.
- Scan verdicts and dedupe markers match what they describe.

### Negative

- For 10 s after a change, reads go to DynamoDB (no cache fill), which
  matters only for a link edited and read heavily at the same moment.
- Clients may see 409 for a concurrent repeat and must retry, as the
  IETF idempotency-key draft recommends.
- Markers live one day longer, which costs a few more storage bytes per click.

## Interview Angle

> "Cache-aside has a classic race: a slow reader re-caches stale data after
> the writer invalidates. We fence instead of delete and make fills
> SET-if-absent. Idempotency got the same treatment: claim the key
> atomically, then act."
