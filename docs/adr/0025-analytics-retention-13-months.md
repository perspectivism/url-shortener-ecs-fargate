# ADR-0025: Analytics retention of 13 months

- **Status:** Accepted
- **Date:** 2026-10-15
- **Deciders:** project owner, coding agent

## Context

Analytics items (daily totals, dimension counters, persisted uniques) had no
expiry. Only the dedupe markers expired, after 3 days. The analytics table
therefore grew with links × days forever. Raw click events in S3 were kept
30 days in dev and 365 days in prod. There was no stated retention policy for
pseudonymous visitor data, and no rule for what happens to a deleted link's
stats.

## Decision Drivers

- Bounded storage and cost
- Year-over-year comparison is the longest window owners typically need
- Data minimisation: keep pseudonymous analytics no longer than useful
- Abuse investigations need history for a while, even after a link is deleted

## Considered Options

1. Keep forever (status quo)
2. 90 days
3. **13 months**
4. Delete stats when the link is deleted

## Decision

Chosen option: **3 — 13 months**, decided by the project owner on 2026-10-15.

- Every counter write sets `ttl` = the counter's day + 13 months, in the same
  update that adds the counts (`store.RecordClicks`; `SetUniques` too).
  DynamoDB TTL removes items in the background, usually within days of expiry.
- **Deleting a link does not delete its stats:** they expire on the same
  schedule. They support abuse investigations (who clicked a malicious link,
  from which referrers), and they are aggregates with no IPs (visitor IDs are
  per-day keyed HMACs, ADR-0018). The API answers 404 for a deleted link, so
  owners no longer see them.
- **Raw events in S3:** prod retention goes from 365 to 395 days (13 months)
  to match; dev stays at 30 days for cost.
- Dedupe markers (3 days), link TTL (`expires_at` + 30 days) and Valkey key
  TTLs are unchanged.

Option 2 is too short for seasonal comparison. Option 4 would destroy evidence
exactly when a link is deleted for abuse.

## Consequences

### Positive

- Analytics storage is bounded by 13 months of activity.
- One clear, documented retention policy across DynamoDB and S3.

### Negative

- Stats older than 13 months are gone. Owners wanting longer history need an export.
- Items written before this change have no `ttl` and never expire. That is
  harmless here: there is no deployed data yet.
- A deleted link's stats are retained but not readable through the API.

## Interview Angle

> "Analytics expire after 13 months, so you can still compare this month with
> the same month last year, and the table doesn't grow forever. Stats
> outlive a deleted link because that is when you need them for abuse
> investigations, and they're pseudonymous aggregates anyway."
