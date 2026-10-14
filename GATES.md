# 🚦 Gate Log

Results of every milestone gate (format: AGENTS.md §5). Gates with cloud checks
are closed as 🟡 **closed-local** in this session (owner instruction: local mode,
no AWS deployments); their pending cloud checks are listed per gate and in the
[summary](#-pending-cloud-checks-summary) at the end.

## Contents

- [M0 🧱 Foundations](#m0--foundations---closed)
- [M1 🔗 Core links (local)](#m1--core-links-local---closed)
- [Pending cloud checks summary](#-pending-cloud-checks-summary)

## M0 🧱 Foundations — ✅ closed

- **Date:** 2026-10-14T03:55Z
- **Commit:** `a3373ef` (tag `gate-m0`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Lint | `make lint` | ✅ | golangci-lint 0 issues; gofmt/goimports clean; markdownlint 9 files 0 issues; shellcheck clean; tflint/actionlint: no inputs yet |
| Secret scan (tree) | `gitleaks detect --no-git` | ✅ | no leaks |
| Secret scan (staged) | `gitleaks protect --staged` | ✅ | no leaks |
| Secret scan (history) | `gitleaks git .` | ✅ | no leaks (1 commit) |
| Scanner self-test | `make test-secret-scan` | ✅ | clean control passes; planted runtime-generated AWS key flagged in tree, staged and history (asserted on JSON report rule `aws-access-token`) |
| Ignore rules | `make check-ignore` | ✅ | 13 secret-bearing paths ignored (`.env`, `infra/envs/dev/terraform.tfvars`, `backend.hcl`, `x.tfstate`, …); 5 examples trackable (`.env.example`, `example.tfvars`, …) |
| Docs | `make docs-check` | ✅ | 12/12 Mermaid diagrams rendered; 55/55 relative links resolved |
| Unit tests | `make test` | ✅ | internal/... coverage 100.0% (min 80%) |
| Stubs fail loudly | `make up/images/tf-check/ci/teardown` | ✅ | each exits 1 with "not yet implemented (Mn)" |

- **Decisions / ADRs:** ADR-0001. Toolchain pinned with a 7-day release cooldown (supply-chain hygiene): Go 1.27.2, Terraform 1.16.5, golangci-lint 2.14.0, gitleaks 8.30.1, tflint 0.64.0 (+ AWS ruleset 0.49.0), Trivy 0.75.0, actionlint 1.7.12, shellcheck 0.11.0, terraform-docs 0.24.0, k6 2.3.0, govulncheck 1.8.0, checkov 3.3.26, zizmor 1.30.1, pre-commit 4.6.2, markdownlint-cli2 0.23.3, mermaid-cli 12.0.0.
- **Deviations / assumptions:**
  - SPEC.md TOC anchor for §12 was broken; fixed with owner approval (only change to SPEC.md).
  - SPEC.md and AGENTS.md are excluded from markdownlint (style-only findings: MD032/MD060; formatting edits not authorized). Both are still covered by the link check and Mermaid render check.
  - gitleaks path allowlist for the gitignored `./.tools/` toolchain directory (listed in the threat model suppressions table).
  - Go module path `github.com/shortener/url-shortener-ecs-fargate` (GitHub org unknown; path is not resolved remotely).
  - Headless Chrome for Mermaid runs with `--no-sandbox` (trusted repo input; Ubuntu 24.04 blocks the sandbox's user namespaces). On Linux hosts missing NSS/ALSA libraries, `make tools` extracts them into `.tools/lib` without root.
- **Pending (if closed-local):** none (no cloud checks in M0)

## M1 🔗 Core links (local) — ✅ closed

- **Date:** 2026-10-14T13:40Z
- **Commit:** `c469109` (code), `4da5f6b` (docs) (tag `gate-m1`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Unit tests (race) + coverage | `make test` | ✅ | 20 packages green; `internal/...` coverage 97.3% (min 80%) |
| Integration (compose) | `make up && make test-integration` | ✅ | 8 tests: create → 302 + `Cache-Control`/`X-Robots-Tag`/`Referrer-Policy`; HEAD emits no event (1 event for GET+HEAD); 404 + negative cache entry `{"nf":true}` (TTL ≤ 60 s) and repeat hit; 410 expired / disabled / deleted; alias 409; reserved alias 400; 7 invalid URLs 422; unsafe URL 422 (fake scanner); 401 / 403; cross-owner 404 (GET/PATCH/DELETE); idempotent replay byte-identical; 429 + `Retry-After`; foreign cursor 400 |
| Key range allocation | `TestKeyRangeAllocationNoDuplicates` | ✅ | 10 000 concurrent creates via 2 API instances (block size 100), 32 workers: 10 000 unique codes, 0 duplicates, 0 failures, 8.5 s |
| Contract tests | `internal/api` + integration | ✅ | Every handler response (all endpoints, all error shapes) and every live integration response validated against `api/openapi.yaml` (OpenAPI 3.1, document itself validated); validator negative tests detect 10/10 planted violations |
| Lint | `make lint` | ✅ | golangci-lint 0 issues, gofmt clean, markdownlint, shellcheck |
| Vulnerabilities | `make vuln` | ✅ | govulncheck: no vulnerabilities found |
| Docs | `make docs-check` | ✅ | 17/17 Mermaid diagrams render; 94/94 relative links resolve |
| Secret scan | `gitleaks git --staged` (pre-commit) | ✅ | no leaks |

- **Decisions / ADRs:** ADR-0002, 0004, 0005, 0006, 0007, 0008, 0009, 0015, 0018, **0021** (new: Safe Browsing v5). Threat model v0 (application DFD + STRIDE for redirect and API).
- **Deviations / assumptions:**
  - Naming overrides applied: CLI is `cmd/shortenerctl` (SPEC: `snipctl`), tables `shortener-{env}-*`, API key prefix `shortener_{env}_`, metrics namespace `Shortener/{env}`.
  - Safe Browsing **v5 `urls:search`** instead of the deprecated v4 Lookup API (v4 support ends 2027-03-31) — ADR-0021.
  - Rate-limit buckets are `write` (create, update, delete, admin; 60/min) and `read` (300/min), configurable via env.
  - Added problem code `method_not_allowed` (405) for wrong methods on known API paths (SPEC §9.3 lists no 405 code).
  - `expires_at` accepts any RFC 3339 offset and is normalized to UTC (second precision); `PATCH` with `"expires_at": null` removes the expiry.
  - Idempotency-Key reuse with a different body returns 400 `invalid_request`. Two *concurrent* first requests with the same key are not serialized (both may create); documented limitation.
  - List pages filter deleted links server-side, so a page may contain fewer than `limit` items.
  - Valkey runs standalone (no TLS/IAM) locally; AWS uses cluster mode + TLS + IAM (enforced by config validation in dev/prod).
  - Lint config: removed `contextcheck` (false positives on idiomatic middleware, before any gate depended on it); test files excluded from gosec/forcetypeassert/unparam/noctx/bodyclose (listed in the threat model suppressions table).
- **Pending (if closed-local):** none (no cloud checks in M1)

## 📋 Pending cloud checks summary

Filled in as gates close.
