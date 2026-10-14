# 🚦 Gate Log

Results of every milestone gate (format: AGENTS.md §5). Gates with cloud checks
are closed as 🟡 **closed-local** in this session (owner instruction: local mode,
no AWS deployments); their pending cloud checks are listed per gate and in the
[summary](#-pending-cloud-checks-summary) at the end.

## Contents

- [M0 🧱 Foundations](#m0--foundations---closed)
- [M1 🔗 Core links (local)](#m1--core-links-local---closed)
- [M2 📊 Click pipeline (local)](#m2--click-pipeline-local---closed)
- [M3 🐳 Containers](#m3--containers---closed)
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

## M2 📊 Click pipeline (local) — ✅ closed

- **Date:** 2026-10-14T14:35Z
- **Commit:** `143d2b4` (code), `f73974d` (docs) (tag `gate-m2`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Exact counts | `TestPipelineExactCounts` | ✅ | 500 clicks over 3 links (460 human, 40 bot): `clicks`/`bot_clicks` exact per link; every dimension sums to the human count |
| Redelivery | same test | ✅ | all 500 events re-sent to SQS → counts unchanged; worker logged ≥ 500 `WorkerDuplicateEvents` |
| Bots excluded from uniques | same test | ✅ | uniques = 12 / 7 / 1 distinct human visitors (bots present on every link) |
| S3 completeness | same test | ✅ | gzip NDJSON under `events/dt=…/hour=…/`; dedupe by `event_id` = exactly 200 / 150 / 150 |
| SIGTERM flush | `TestSIGTERMFlushesBufferedClicks` | ✅ | 30 sent in full batches, 7 still buffered (stable 3 s); SIGTERM → clean exit → 37 counted |
| Queue down | `TestQueueDownStillRedirects` | ✅ | SQS endpoint unreachable: 10/10 redirects 302; `ClickEventsDropped` = 10 (EMF) |
| Valkey down | `TestValkeyDownStillRedirects` | ✅ | cache unreachable: 302 via DynamoDB, 404 still correct; `CacheErrors` ≥ 3 (EMF) |
| Poison → DLQ | `TestPoisonMessageGoesToDLQ` | ✅ | unparseable message in DLQ after 5 receives (< 2 s); worker alive and still counting |
| Nightly | `TestNightly` | ✅ | uniques persisted (4, bot excluded); fake-flagged pending link disabled (`unsafe_url`), `l:{code}` removed, redirect 410; re-run: 0 scanned / 0 flagged / 0 disabled, uniques unchanged |
| Full integration suite | `make test-integration` (×2) | ✅ | 14/14 tests, 67.6 s and 72.4 s |
| Unit tests + coverage | `make test` | ✅ | `internal/...` 97.7% |
| Lint / vuln / docs | `make lint`, `make vuln`, `make docs-check` | ✅ | 0 issues; no vulnerabilities; 19/19 diagrams, 103/103 links |

- **Decisions / ADRs:** ADR-0010, ADR-0011. Threat model v0.2 (pipeline STRIDE P1–P8).
- **Deviations / assumptions:**
  - `clicks` (humans) and `bot_clicks` are disjoint; dimensions and uniques count humans only.
  - The nightly job finds "links with yesterday's activity" via a Valkey set `active:{date}` (8-day TTL) written by the worker, instead of scanning DynamoDB.
  - Valkey failures in the worker degrade uniques but never block counting or push events to the DLQ.
  - The worker makes poison messages visible immediately (visibility 0) so they reach the DLQ in seconds.
  - `TransactWriteItems` does not use `ClientRequestToken` (it would mask replays from the duplicate metric); exactly-once relies on the `EVENT#` condition.
  - Stats default range: last 7 days including today; `uniques` for recent, not-yet-persisted days are read live from the HLL.
  - `NIGHTLY_DATE` override (default yesterday UTC) for backfills and tests.
- **Pending (if closed-local):** none (no cloud checks in M2)

## M3 🐳 Containers — ✅ closed

- **Date:** 2026-10-14T16:20Z
- **Commit:** `16ead6d` (build), `5b4d824` (test isolation fix; gate run) (tag `gate-m3`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Build arm64 | `make images` | ✅ | `shortener/{redirect,api,worker,nightly}:5b4d82448194`, `linux/arm64`, cross-compiled (no emulation) |
| Size < 30 MB | `make check-images` | ✅ | uncompressed 16.3 / 16.5 / 17.2 / 15.6 MB (compressed 5.4–5.9 MB) |
| Non-root | `make check-images` | ✅ | `User=65532:65532` for all; entrypoint `["/app"]` (distroless static-debian13, no shell) |
| Trivy vulnerabilities | `make scan-images` | ✅ | 0 fixable HIGH/CRITICAL; 0 vulnerabilities of any severity, fixed or not |
| Secrets in layers | `make scan-images` + `make test-image-secret-scan` | ✅ | Trivy secret scan clean on all images; self-test: planted fake key in a throwaway image detected (2 findings) |
| Build args / history | `make scan-images` | ✅ | our layers expose only `SERVICE` and `VERSION` (git SHA); no secret-looking content |
| Read-only, cap-drop, non-root containers pass integration tests | `make test-integration-containers` | ✅ | all 14 integration tests with every service instance run via `docker compose run` with `read_only: true`, `cap_drop: [ALL]`, `no-new-privileges`, UID 65532 (63.7 s) |
| Process-mode integration (regression) | `make test-integration` | ✅ | 14/14 (80.1 s), run after the container suite against the same Valkey |
| Unit / lint / vuln / docs | `make test lint vuln docs-check` | ✅ | coverage 97.7%; 0 lint issues; no vulnerabilities; 19/19 diagrams, 105/105 links |

- **Decisions / ADRs:** ADR-0020 (Graviton ARM64 + Fargate Spot; Spot ARM64 support verified against the AWS announcement).
- **Deviations / assumptions:**
  - Size gate measured on the **uncompressed** filesystem (`docker export`): with the containerd image store, `docker image inspect .Size` is the compressed size and would understate the image.
  - `make up` now builds native-platform images (`images-local`, tag `local`) and starts the full containerized stack; `make up-deps` starts only the emulators. A one-shot `init` container runs `local-init`, so the quickstart needs only Docker + make.
  - The build-history review excludes the distroless base image's own history (it contains `bazel build //common:passwd`, a false positive).
  - The ALB access-log bucket will use SSE-S3 (the only encryption ALB log delivery supports) — to be suppressed with justification in M5.
- **Regression fixed:** integration runs shared the local Valkey and regenerated the same Sqids codes (fresh tables restart the ID counter), doubling uniques when a container-mode run preceded a process-mode run. Each run now uses a freshly shuffled alphabet and run-scoped owner IDs. An `httpx` shutdown test flake (speculative `StateNew` connection under load) was fixed in the test.
- **Pending (if closed-local):** none (no cloud checks in M3)

## 📋 Pending cloud checks summary

Filled in as gates close.
