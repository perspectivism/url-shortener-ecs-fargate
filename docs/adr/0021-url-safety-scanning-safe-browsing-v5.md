# ADR-0021: URL safety scanning with Safe Browsing v5 `urls:search`

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

SPEC §10 requires checking destinations against Google Safe Browsing at create
and update (300 ms timeout, fail to `pending`) and nightly, using "the Lookup
API". Checked on 2026-10-14: the Safe Browsing **v4** APIs are deprecated and
Google ends support on **2027-03-31**, recommending v5 or Web Risk
([v4 overview](https://developers.google.com/safe-browsing/v4),
[v5 `urls.search`](https://developers.google.com/safe-browsing/reference/rest/v5/urls/search)).

## Decision Drivers

- Must keep working past the v4 support end date
- Real-time lookup semantics (the intent of SPEC's "Lookup API")
- Never block link creation on a scanner outage
- Never leak the API key into URLs or logs

## Considered Options

1. v4 Lookup API (`threatMatches:find`)
2. v4/v5 Update API (local hash-prefix database)
3. **v5 `urls:search`** (real-time URL lookup)
4. Google Web Risk (commercial)

## Decision

Chosen option: **3 — Safe Browsing v5 `GET /v5/urls:search`** behind the
`links.Scanner` interface (`internal/scanner`), with a `Fake` (flags URLs
containing `malware.testing.google.test`) and a `Noop` (scanning disabled — no NAT).

- Up to 50 URLs per request (nightly rescans batch); a single-URL request
  attributes any returned threat to that URL, since v5 can match host-suffix or
  path-prefix expressions of the URL. If a batch returns a threat that cannot
  be attributed, each URL is re-checked individually (conservative).
- The key goes in the `X-Goog-Api-Key` header, never the `?key=` query
  parameter; transport errors are stripped of the request URL before logging.
- Outcomes: flagged → 422 `unsafe_url` (`URLScanFlagged`); timeout/error →
  `scan_status=pending` (`URLScanUnavailable`), rescanned nightly; disabled →
  `pending` (`URLScanSkipped`).
- Safe Browsing is licensed for non-commercial use; a commercial deployment
  should switch the `Scanner` implementation to Web Risk (same interface).

**Deviation from SPEC:** SPEC names the v4 "Lookup API"; v5 `urls:search` is
its direct successor. Recorded in `GATES.md` (M1).

## Consequences

### Positive

- Survives the v4 shutdown; same real-time semantics.
- Scanner outages degrade to "pending + nightly rescan" instead of failing creates.

### Negative

- Lookups send full URLs to Google (the hash-prefix Update API would be more
  private but needs a local database — not worth it at this scale).
- Zero-day gap: a URL that becomes malicious after creation is caught only by
  the next nightly rescan (accepted risk in the threat model).

## Interview Angle

> "Every destination is checked against Safe Browsing, but a scanner outage
> never blocks a create — the link goes in as 'pending' and the nightly job
> rescans it. I used v5 because v4 is being shut down."
