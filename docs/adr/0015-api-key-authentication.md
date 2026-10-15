# ADR-0015: API key authentication (vs Cognito)

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

The management API is used by programs (scripts, CI, integrations), not by
humans in a browser — there is no web UI (SPEC non-goals). Callers need
per-owner isolation and coarse scopes (`links:read`, `links:write`, `admin`).

## Decision Drivers

- Simple machine-to-machine auth with revocation
- No secret material stored in a recoverable form
- Low per-request overhead on 0.25 vCPU tasks
- Least privilege via scopes

## Considered Options

1. Amazon Cognito user pools + OAuth2 client credentials
2. API Gateway usage-plan API keys
3. Self-managed bearer API keys, stored as hashes
4. mTLS client certificates

## Decision

Chosen option: **3 — self-managed API keys** (`internal/auth`).

- Format: `shortener_{env}_` + base64url(32 random bytes) (43 chars). The prefix
  makes keys recognisable to secret scanners (a custom gitleaks rule is in
  `.gitleaks.toml`) and stops a dev key from working in prod.
- Shown **once** by `shortenerctl apikey create`; stored only as SHA-256 hex in
  `APIKEY#{hash}` with owner, name, scopes (string set), status and `last_used_day`.
- **Why SHA-256 and not bcrypt/argon2:** slow hashes protect *low-entropy*
  secrets (passwords) from offline guessing. A 256-bit random key cannot be
  brute-forced through any hash, so a fast hash loses nothing and keeps every
  request cheap.
- Malformed keys (wrong prefix/env/length) are rejected before any lookup.
- An in-process cache (60 s TTL, bounded size) avoids a DynamoDB read per
  request; **revocation therefore takes effect within 60 s** (documented).
- `last_used_day` is updated at most once per key per day per task with a
  conditional write.
- Authorization order: 401 (no/invalid key) → 403 (missing scope) → 429 (rate limit).
- Owner isolation: every read/update/delete checks `owner_id`; other owners'
  links return **404**, never 403, so existence is not revealed.

## Consequences

### Positive

- No external identity provider to run or pay for; trivial for scripts.
- A database leak exposes hashes only, which are useless as credentials.

### Negative

- No user login, MFA or federation (a web dashboard would need Cognito/OIDC — SPEC §20).
- Key distribution is manual (operator CLI).
- Up to 60 s revocation delay per task.

## Summary

Clients are programs, so bearer API keys are the right tool: 256 random
bits, shown once, stored as a SHA-256 — slow hashing only matters for
guessable secrets — scoped, revocable, and checked per owner so a caller can't
even learn that another owner's link exists.
