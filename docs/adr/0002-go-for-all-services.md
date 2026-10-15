# ADR-0002: Go for all services

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

The system has four runtime components (redirect, api, worker, nightly) and an
operator CLI (SPEC §3.2). The redirect path has a p99 < 50 ms target at the
origin (SPEC N1) and runs on small Fargate tasks (0.25 vCPU / 512 MiB). Images
should be tiny and distroless; containers have no shell, so health checks must
be built into the binary (SPEC §12.4).

## Decision Drivers

- Low, predictable latency and memory on 0.25 vCPU tasks
- Static binaries for distroless images (fast pulls, minimal attack surface)
- Fast cold starts for scale-out and Spot replacement
- A strong standard library (HTTP routing, structured logging, context) so the
  dependency tree stays small and auditable
- First-class AWS SDK and Valkey client support

## Considered Options

1. Go for everything
2. TypeScript (Node.js) for the API, Go for the redirect path
3. Java / Kotlin (Spring Boot or Quarkus)
4. Rust

## Decision

Chosen option: **1 — Go for all services** (Go 1.27, pinned in `go.mod`
`toolchain` and `.tool-versions`).

- Standard library first: `net/http` method/path patterns for routing,
  `log/slog` for JSON logs, `context` for deadlines on every outbound call.
- Approved dependencies only (AGENTS.md §8): `aws-sdk-go-v2`, `go-redis/v9`,
  `sqids-go`, `google/uuid`, `golang.org/x/sync`. Two additions with
  justification:
  - `golang.org/x/net/idna` — IDN → punycode normalization required by SPEC §10.
  - Test-only: `alicebob/miniredis` (Valkey fake for unit tests) and
    `pb33f/libopenapi-validator` (OpenAPI 3.1 contract tests; SPEC §9.4).
- One module, `cmd/*` for wiring only, logic in `internal/*`, interfaces
  defined where consumed.

## Consequences

### Positive

- One toolchain, one linter config, one test runner for every service.
- Static ~10–15 MB binaries; `CGO_ENABLED=0` makes distroless `static` images possible.
- Each binary ships its own `healthcheck` subcommand (no curl in the image).
- `go test -race` and `govulncheck` run across the whole codebase.

### Negative

- More hand-written plumbing than a framework would provide (problem+json,
  middleware) — kept small and fully tested in `internal/httpx`.
- Generics/ergonomics are less expressive than Rust/Kotlin for some domain code.

## Summary

Go produces a static binary that starts in milliseconds and serves the hot
path from a quarter of a vCPU, with almost no dependencies to audit — the
standard library already does routing, structured logs and deadlines.
