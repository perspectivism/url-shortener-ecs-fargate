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
- [M4 🏗️ Bootstrap, network & data](#m4-️-bootstrap-network--data---closed-local)
- [M5 🚀 ECS services & edge](#m5--ecs-services--edge---closed-local)
- [M6 📈 Observability & cost](#m6--observability--cost---closed-local)
- [M7 🛡️ Security hardening](#m7-️-security-hardening---closed-local)
- [M8 🔁 CI/CD](#m8--cicd---closed-local)
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

## M4 🏗️ Bootstrap, network & data — 🟡 closed-local

- **Date:** 2026-10-14T18:10Z
- **Commit:** `1de7ae6` (tag `gate-m4-local`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| fmt | `terraform fmt -check -recursive` (in `make tf-check`) | ✅ | clean |
| validate | `make tf-check` | ✅ | 3 roots (lock files readonly, AWS provider **6.68.0**) + 10 modules |
| tflint | `make tf-check` | ✅ | 0 issues (AWS ruleset 0.49.0 + all core rules; negative test confirmed findings are reported) |
| checkov | `make tf-check` | ✅ | 401 passed, 0 failed, 29 skipped — every skip justified inline and listed in the threat model |
| Trivy config | `make tf-check` | ✅ | 0 MEDIUM/HIGH/CRITICAL (3 HIGH `s3:*` findings fixed by scoping CI S3 actions to `shortener-*` buckets) |
| terraform test | `make tf-check` | ✅ | 29 runs in 11 suites: CMK encryption (DynamoDB, SQS, S3, Logs, ECR, Valkey, Secrets), PITR on, BPA on, TLS-only bucket/queue policies, no CIDR ingress (SG-referenced only), isolated subnets without a default route, NAT only with scanning, Valkey IAM auth (`user_id == user_name`, disabled default user), secrets readable by nobody until readers are wired, OIDC `aud`/`sub` scoping, CI boundary anti-escalation, per-env apply isolation |
| dev/prod parity | `scripts/check-env-parity.sh` | ✅ | roots identical except variable defaults |
| Plan bootstrap | `make plan ENV=bootstrap` | ✅ | read-only, local mode: +26 ~0 −0 |
| Plan dev | `make plan ENV=dev` | ✅ | read-only, local mode: +93 ~0 −0; isolated subnets in `use1-az2`, `use1-az4` (`use1-az3` excluded); Valkey major 9 |
| Plan prod | `make plan ENV=prod` | ✅ | read-only, local mode: +101 ~0 −0 (3 AZs, NAT, endpoints in all AZs) |
| Regression | `make lint test docs-check` | ✅ | lint 0 issues; coverage 97.7%; 21/21 diagrams, 115/115 links |

- **Decisions / ADRs:** ADR-0013, ADR-0014, ADR-0016, ADR-0019. `docs/cost.md` first draft (Price List API, 2026-10-14): dev foundation ≈ $53/month idle (≈ $108 if the 1 GB Valkey minimum applied).
- **Deviations / assumptions:**
  - AWS provider pinned `~> 6.68` and locked to **6.68.0** (owner request; newer than the 7-day tool cooldown).
  - Added two helper modules beyond SPEC §12.2: `s3-bucket` (shared hardened bucket) and `log-bucket` (SSE-S3 log archive: S3 server access logs and ALB logs cannot target SSE-KMS buckets).
  - Plans ran in **local mode**: no state bucket exists yet, so `make plan` copies the root to a scratch directory with a temporary local backend (empty state) — read-only, never applied.
  - Security groups for endpoints and Valkey admit *client security groups* that tasks attach (M5), rather than per-service task SGs that do not exist yet; still SG-to-SG, no CIDRs.
  - Valkey app user ACL `on ~* &* +@all -@dangerous` (to be verified against the real cache).
  - CI apply roles: broad service actions (single account, ADR-0019) bounded by the permissions boundary, cross-environment tag denies and state-file isolation.
  - ⚠️ The AWS credentials available in this session are **root-user access keys**; only read-only calls were made. Recommend replacing them with an IAM Identity Center/admin role and deleting the root keys before any apply.
- **Pending (closed-local):**
  1. ☁️ Create `infra/bootstrap/terraform.tfvars` (repository, owner), then `make bootstrap` → 👤 approve → `APPROVED=yes make bootstrap-apply` (applies, writes `backend.hcl` ×3, migrates bootstrap state into S3).
  2. ☁️ `make plan ENV=dev` (S3 backend) → 👤 approve → `APPROVED=yes make apply ENV=dev` (foundation, `enable_services=false`).
  3. ☁️ Post-apply verification (AWS CLI, read-only): DynamoDB PITR + SSE-KMS on both tables; S3 Block Public Access on all buckets; SQS SSE-KMS + redrive policy; Valkey Serverless `available` with the IAM user group; secrets exist with **no** versions yet (metadata only). Record IDs in GATES.md.
  4. 👤 `make seed-secrets ENV=dev` (human, own terminal) → ☁️ agent verifies versions exist via `list-secret-version-ids` (metadata only).

## M5 🚀 ECS services & edge — 🟡 closed-local

- **Date:** 2026-10-14T21:40Z
- **Commit:** `3b2d556` (tag `gate-m5-local`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Static checks (M4 set on new code) | `make tf-check` | ✅ | fmt, validate (3 roots, 14 modules), tflint 0 issues, checkov 859 passed / 0 failed / 73 justified skips, Trivy MEDIUM+ 0 (2 inline justified ignores), parity (5 files identical), READMEs fresh |
| terraform test | `make tf-check` | ✅ | 43 runs in 15 suites; new assertions: ALB `internal = true` + `drop_invalid_header_fields`; ALB SG ingress = CloudFront origin-facing prefix list only, egress only to task SGs; task SG ingress from the ALB SG only; read-only root FS, UID 65532, `drop ALL`, ARM64, binary health check, non-blocking awslogs; circuit breaker with rollback (100/200); `CachingDisabled` on both behaviors; WAF associated (managed rules, AnonymousIpList in count, 429 rate rules, Authorization redacted); VPC origin; secrets via `valueFrom` only; execution role reads only its own secrets; scheduler may only run its task on its cluster; custom domain → HTTPS listener, https-only origin, TLSv1.2_2021 |
| Plans (phase 1) | `make plan ENV=dev/prod` | ✅ | read-only, local mode: dev +92, prod +101 |
| Plans (phase 2) | `TF_VAR_enable_services=true TF_VAR_image_tag=3b2d556f3a67 make plan` | ✅ | read-only, local mode: dev +171, prod +182, 0 destroy; WAF `CLOUDFRONT` in `us-east-1`; dev redirect on Spot, prod redirect/api on-demand base 2 |
| Smoke suite | `make smoke` against the local containerized stack | ✅ | create → 302 (`no-store`) → click counted after 5 s → 404 → delete → 410 → 401 ×2 → Google test URL 422 `unsafe_url` (fake scanner); oversize 413; cross-owner 404 (GET/DELETE/stats) |
| Regression | `make lint test test-integration docs-check` | ✅ | lint 0 issues (smoke tag linted); coverage 97.7%; integration 14/14 (83.7 s); 23/23 diagrams, 119/119 links |

- **Decisions / ADRs:** ADR-0003, ADR-0012, ADR-0017 (Proposed: deploy strategy decided; CI/CD completed in M8). Design doc deployment section.
- **Deviations / assumptions:**
  - **ECS Exec off by default in dev too:** AWS does not support ECS Exec with a read-only root filesystem, and distroless images lack a shell and the `script`/`cat` needed for session logging. SPEC's hardened runtime wins; `ecs_exec_enabled` is a break-glass toggle (relaxes the read-only FS for that environment; forbidden in prod by validation). Dev no longer needs the `ssmmessages` endpoint (−$7.30/month).
  - CloudFront-scope WAF, the viewer ACM certificate and CloudFront/WAF log groups use the AWS provider 6.x per-resource `region = "us-east-1"` instead of a provider alias (SPEC §12.1); same result, modules stay independently validatable.
  - CloudFront access logs use standard logging v2 into a CMK-encrypted CloudWatch log group (us-east-1) with retention.
  - WAF `SizeRestrictions_BODY` is counted, not blocked, so the API's own 8 KiB limit answers 413 (SPEC M7 smoke expects 413).
  - Worker scales on queue backlog with step scaling (target tracking on backlog-per-task cannot scale from zero).
  - Services wait for routing via `routing_ready` (listener/rule ARN) to avoid a module-level dependency cycle.
  - Without a custom domain, the viewer TLS policy is fixed by AWS for `*.cloudfront.net`; the CloudFront→ALB and ALB→task hops are HTTP inside the AWS network (accepted risks in the threat model).
  - Trivy scans of the source tree skip saved `tfplan` files (they embed stale source snapshots).
- **Pending (closed-local):**
  1. 👤/☁️ Build and push images: `make images` then `APPROVED=yes make push-images ENV=dev` (approval).
  2. ☁️ Phase 2: set `enable_services = true` and `image_tag` in `infra/envs/dev/terraform.tfvars`; `make plan ENV=dev` → approve → `APPROVED=yes make apply ENV=dev`.
  3. ☁️ Verify all services steady with healthy targets (`aws ecs describe-services`, `aws elbv2 describe-target-health`).
  4. ☁️ Create an API key against dev (`shortenerctl apikey create` with dev `LINKS_TABLE`), export `SMOKE_API_KEY`, then `make smoke BASE_URL=$(terraform output -raw base_url)`.
  5. ☁️ ALB DNS **not reachable** from the public internet: `curl --max-time 10 http://<alb_dns_name>/healthz` must fail/time out (record).
  6. ☁️ Nightly runs successfully via a manual `aws ecs run-task` (parameters from output `nightly_run_task`); check exit code 0 and the summary log line.
  7. ☁️ Forced bad deploy: throwaway apply with a non-existent `image_tag` (or an image whose health check fails) → circuit breaker rolls back; record the rollback event, then re-apply the good tag.

## M6 📈 Observability & cost — 🟡 closed-local

- **Date:** 2026-10-14T23:30Z
- **Commit:** see tag `gate-m6-local`
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Every §13 alarm exists with an SNS action | `make tf-check` (`observability`, `envs/{dev,prod}` tests) | ✅ | phase 1: 4 data-tier alarms; phase 2: 14 alarms (SPEC set + `cache-invalidation-failed`), each with ALARM **and** OK → CMK-encrypted topic; 5xx as a rate (metric math), p99 > 100 ms × 10 min, dropped clicks 3 × 5 min; nightly log filter + Scheduler errors + EventBridge non-zero exit → SNS |
| Every log group has retention + KMS | `make tf-check` (module tests + `check-plan-invariants.sh` self-test) | ✅ | Container Insights log group now Terraform-managed (retention + CMK; prefix added to the key policy); invariant script rejects all 4 seeded violations and runs on every `make plan` |
| Budget + anomaly detection | `make tf-check` (`budget` tests) | ✅ | 50/80/100 % actual + 100 % forecast → SNS; tag monitor + IMMEDIATE subscription; scoped to `CostCenter = shortener-{env}` |
| EMF metrics verified | `scripts/check-metrics.sh` (in `make tf-check`) | ✅ | 17/17 SPEC §13 metrics defined, emitted by non-test code and on the dashboard |
| Static checks | `make tf-check` | ✅ | validate 3 roots + 16 modules; tflint 0; checkov 874 passed / 0 failed / 75 justified skips; Trivy 0; terraform test 57 runs in 19 suites; parity incl. env tests |
| k6 script | `make load` against the local compose stack (20 s, 15 req/s) | ✅ | 0 % failed; redirect p99 9.7 ms (local, not a cloud baseline); setup/teardown honour 429 `Retry-After`; all links deleted |
| Regression | `make lint test docs-check test-secret-scan secret-scan check-ignore vuln` | ✅ | lint 0 issues; coverage 97.7 %; 25/25 diagrams, 147+ links; gitleaks clean; govulncheck: no vulnerabilities |
| Plans (phase 1/2, dev/prod) | `make plan ENV=…` | ⏸️ | **not run**: the AWS CLI session expired (`aws login` required) — read-only plans moved to pending; offline equivalent: `envs/{dev,prod}` mock-provider tests plan both phases |

- **Decisions / ADRs:** ADR-0022 (observability, alerting and cost guardrails). Runbook v0.2 (SLIs/SLOs, alarm responses), cost model v0.3, design §4, threat model v0.3.
- **Deviations / assumptions:**
  - `CostCenter = <project>-<env>` added to `default_tags` (SPEC §12.1 lists five tags): customer-managed anomaly monitors filter on a single tag key, and `Environment` is not project-unique in a shared account. Activation is a second bootstrap apply (`activate_cost_allocation_tag = true`) once Billing has seen the tag (≤ 24 h).
  - Tag-based anomaly monitors only work in a management/standalone account (AWS docs); in an Organizations member account that resource fails to create (future work: account-level monitor).
  - Extra alarm `cache-invalidation-failed` (the threat model's accepted stale-redirect risk depends on it).
  - The nightly schedule now lives in its own schedule group so `AWS/Scheduler` metrics (dimension `ScheduleGroup`) are environment-scoped.
  - Container Insights stays `enabled` (standard): the running-tasks alarms need `DesiredTaskCount`/`RunningTaskCount`; it adds ≈ $15–30/month per environment (cost.md).
  - Budget defaults: dev $150, prod $400/month; cost model revised to ≈ $110–135 (dev phase 2) and ≈ $255–270 (prod idle) after adding CloudWatch line items.
- **Pending (closed-local):**
  1. ☁️ Re-authenticate the AWS CLI (`aws login`), then the read-only plans: `make plan ENV=dev` (phase 1 and 2) — plan invariants must print OK.
  2. ☁️ Apply the M6 changes with the dev phase-2 apply (`alert_email` set in `terraform.tfvars`) → approve → `APPROVED=yes make apply ENV=dev`.
  3. 👤 Confirm the SNS subscription email; verify with `aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw alert_topic_arn)` (no `PendingConfirmation`).
  4. ☁️ `aws cloudwatch set-alarm-state --alarm-name shortener-dev-dlq --state-value ALARM --state-reason "alert path test"` → 👤 confirm the email arrived (and the OK email after re-evaluation).
  5. ☁️ k6 baseline: dev API key in `LOAD_API_KEY`; `make load BASE_URL=<cloudfront>` (default 15 req/s × 2 min), then `scripts/load-report.sh dev <start> <end>`; record RPS, p50/p95/p99 (client and ALB), error rate, cache hit ratio here.
  6. ☁️ Dashboard `shortener-dev` shows data in every widget (Cost widget needs "Receive Billing Alerts").
  7. ☁️ After ≥ 24 h: `activate_cost_allocation_tag = true` in bootstrap → plan → approve → apply; after ≥ 3 days record measured spend in cost.md §7.

## M7 🛡️ Security hardening — 🟡 closed-local

- **Date:** 2026-10-15T00:40Z
- **Commit:** see tag `gate-m7-local`
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| IAM wildcards justified (offline) | `scripts/check_iam_wildcards.py` (in `make tf-check`) | ✅ | 84 policy statements; every Allow with `*`, `service:*`, resource `*` or principal `*` carries an inline justification; self-test fixture: all 4 seeded violations reported |
| Access Analyzer `validate-policy` | `make validate-iam ENV=dev` | ⏸️ | script built and fails closed (expired AWS session → exit 255; unknown policies → fail); extraction self-test in `tf-check` (3 policies, 1 unknown); dry run over the M5 local plan found 35 policies (20 rendered only at apply) → **pending: run against state after the first apply** |
| checkov / Trivy / tflint, zero unsuppressed MEDIUM+ | `make tf-check` | ✅ | checkov 884 passed / 0 failed / 75 skipped; Trivy 0; tflint 0 |
| Suppressions listed in threat model | `scripts/check_suppressions.py` (in `make tf-check`) | ✅ | 41 suppressions, each with a row naming rule and file; negative probe detected |
| `gitleaks` over full history | `make secret-scan` | ✅ | working tree + 17 commits: no leaks; self-test detects the planted key |
| STRIDE rows mitigated/accepted | `scripts/check_threat_model.py` (in `make docs-check`) | ✅ | 51 rows (R1–R10, A1–A12, P1–P8, I1–I12, C1–C9), none open; §17.3 coverage matrix |
| terraform test | `make tf-check` | ✅ | 57 runs in 19 suites; new: S3 endpoint names buckets/actions, interface endpoints per-service + `aws:PrincipalAccount`/`aws:ResourceAccount`, WAF rate rules before managed groups, 429 carries `Retry-After` |
| Smoke security negatives (local) | `make smoke` against the local stack | ✅ | 422 unsafe URL (fake scanner), 413, cross-owner 404 ×3 (two keys), invalid key 401, `TestALBNotPublic` fails to connect (unroutable probe address); `TestWAFRateLimit` skipped locally (no WAF) |
| Regression | `make lint test docs-check vuln` | ✅ | lint 0 issues; coverage 97.7 %; 25/25 diagrams, 164/164 links; govulncheck: no vulnerabilities |

- **Decisions / ADRs:** ADR-0023 (IAM review, VPC endpoint data perimeter, WAF tuning). Threat model v1.0.
- **Deviations / assumptions:**
  - Interface endpoints now carry per-service data-perimeter policies (`aws:PrincipalAccount` + `aws:ResourceAccount`); the S3 gateway endpoint is limited to `<project>-<env>-*` buckets and object actions. Not testable offline — the first deploy verifies them (tasks must pull images, write logs and read secrets).
  - WAF rate rules moved ahead of the managed groups (priorities 1–2) so they count every request; `GenericRFI_BODY` stays blocking (accepted trade-off, threat model I9).
  - Access Analyzer validation requires AWS credentials and rendered policies; against an empty-state plan most policies are unknown, so it is a post-apply check.
- **Pending (closed-local):**
  1. ☁️ After the first dev apply: `make plan ENV=dev` then `make validate-iam ENV=dev` — 0 ERROR / SECURITY_WARNING, 0 unknown (also `ENV=bootstrap`).
  2. ☁️ Smoke security negatives through CloudFront: `SMOKE_ALB_DNS=$(terraform output -raw alb_dns_name) SMOKE_API_KEY_2=<second owner> make smoke BASE_URL=<cloudfront>`; then `SMOKE_WAF_BURST=true` for the 429 burst (last; rate-limits the runner IP ≈ 1 min). With scanning enabled (prod-like) also `SMOKE_EXPECT_SCANNING=true` → 422.
  3. ☁️ Endpoint data perimeter works: all services steady (ECR pull, logs, secrets through the endpoints); clicks reach the worker (SQS endpoint).

## M8 🔁 CI/CD — 🟡 closed-local

- **Date:** 2026-10-15T01:30Z
- **Commit:** see tag `gate-m8-local`
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| actionlint | `make check-workflows` | ✅ | `ci.yml`, `deploy.yml`: 0 issues |
| Every `uses:` pinned to a 40-char SHA | `scripts/check-pins.sh` (in `make check-workflows`) | ✅ | 23 action references (full SHAs + version comments), 6 container images by digest, exact tool versions, no `latest`; negative probe (`@v4`, missing comment) rejected |
| zizmor medium+ | `zizmor --offline --min-severity medium .github/workflows` | ✅ | no findings (also none with `--persona auditor`) |
| CI steps locally | `make ci` | ✅ | lint 0 issues; coverage 97.7 %; govulncheck clean; gitleaks 19 commits clean; integration 72 s green; images arm64, UID 65532, 15.6–17.2 MB, Trivy 0 fixable HIGH/CRITICAL; tf-check (checkov 884/0, 57 test runs); docs-check; check-workflows |
| OIDC trust policies | `terraform test` (`infra/bootstrap`) | ✅ | `aud = sts.amazonaws.com`; exact `sub` (StringEquals, one value, no wildcards): plan → `repo:<repo>:pull_request`, apply → `repo:<repo>:environment:<env>`; only these three roles trust GitHub; all carry the boundary |
| Build once, promote digests | `make images-oci` + `scripts/push-images.sh` against a local `crane registry serve` | ✅ | 4 single-manifest arm64 layouts (UID 65532); dev and prod pushes report identical digests; re-push idempotent; dirty tag and missing approval refused |

- **Decisions / ADRs:** ADR-0017 completed (Accepted): workflows, approvals, digest promotion, hardening. Runbook §5.1 CI/CD setup, §5.2 deploy, §5.3 rollback. Threat model C5/C6/C9 updated.
- **Deviations / assumptions:**
  - Images are promoted as OCI layouts (built once in `deploy.yml`, pushed with pinned `crane`, digest verified per environment) because each environment has its own ECR repositories and the apply roles cannot cross environments (ADR-0019 guardrail).
  - Provenance/SBOM attestations are disabled to keep one manifest digest per image (signing is future work).
  - `make push-images` now pushes the OCI layouts from `make images-oci` (was: docker push of `make images`), so manual and CI deploys share one path.
  - The `deploy` pipeline assumes phase 1 is applied and secrets are seeded; it always deploys with `enable_services = true`.
  - `.github/CODEOWNERS` contains the placeholder `@OWNER` for the human to replace.
  - **Flake observed once:** in the first `make ci` run `TestKeyRangeAllocationNoDuplicates` reported 136 of 10,000 creates failing (0 duplicates). Not reproducible in 7 further runs (3 alone, 3 under parallel `go test -race` load, 2 full suites) nor in the full `make ci` reruns. The test now records failure reasons (status/problem code or transport error) so a recurrence is diagnosable; suspected cause: DynamoDB Local latency spikes beyond the 200 ms deadline (→ 503). No assertion was loosened.
- **Pending (closed-local):**
  1. 👤 Push the repository to GitHub; replace `@OWNER` in `.github/CODEOWNERS`.
  2. 👤 Configure repository variables, environments `dev`/`prod` (deployment branch `main`; `prod` required reviewers + prevent self-review), environment variables and the `SMOKE_API_KEY` secrets — exact commands in [runbook §5.1](docs/runbook.md#51-cicd-setup-one-time--repository-admin).
  3. 👤 Branch ruleset on `main`: PR + 1 approval + code owners; required checks (the five `ci` jobs and `terraform plan (dev)`); block force pushes.
  4. 👤 Open a PR: `ci` green, plan comment posted by `terraform plan (dev)`.
  5. ☁️ Merge: `deploy` builds once, dev pushes with verified digests, applies, smoke passes; the `prod` job **waits for approval** (the agent does not approve).

## 📋 Pending cloud checks summary

Cumulative, in execution order. Hand this list back when ready to deploy.

1. **M4** — Replace root access keys with a role session (recommended). Create `infra/bootstrap/terraform.tfvars`; `make bootstrap` → approve → `APPROVED=yes make bootstrap-apply`.
2. **M4** — `make plan ENV=dev` → approve → `APPROVED=yes make apply ENV=dev` (foundation).
3. **M4** — Post-apply verification of the dev foundation (DynamoDB PITR/SSE, S3 BPA, SQS SSE/redrive, Valkey status/auth, secrets exist without values).
4. **M4** — 👤 `make seed-secrets ENV=dev`; verify secret versions (metadata only).
5. **M5** — `make images`; approve → `APPROVED=yes make push-images ENV=dev`.
6. **M5** — Phase 2: `enable_services = true`, `image_tag = <sha>`; `make plan ENV=dev` → approve → `APPROVED=yes make apply ENV=dev`.
7. **M5** — Services steady, targets healthy; API key for dev; `make smoke BASE_URL=<cloudfront>`.
8. **M5** — ALB DNS unreachable from the internet; manual nightly run succeeds; forced bad deploy triggers circuit-breaker rollback (evidence recorded).
9. **M6** — `aws login`; read-only `make plan ENV=dev` (both phases, plan invariants OK); dev apply with `alert_email` (approval); 👤 confirm the SNS subscription.
10. **M6** — `set-alarm-state` on `shortener-dev-dlq` → 👤 email received; k6 baseline (`make load` + `scripts/load-report.sh`) recorded; dashboard widgets show data.
11. **M6** — ≥ 24 h later: bootstrap `activate_cost_allocation_tag = true` (approval); ≥ 3 days later: measured spend into cost.md §7.
12. **M7** — Endpoint data perimeter verified by steady services; `make plan ENV=dev` + `make validate-iam ENV=dev` (and `ENV=bootstrap`) clean.
13. **M7** — Smoke security negatives through CloudFront (`SMOKE_ALB_DNS`, `SMOKE_API_KEY_2`), then `SMOKE_WAF_BURST=true` → 429 with `Retry-After`.
14. **M8** — 👤 Push to GitHub, fix `CODEOWNERS`; configure variables, environments (prod reviewers), secrets and the `main` ruleset (runbook §5.1); open a PR → `ci` green + plan comment.
15. **M8** — Merge → `deploy`: dev push (verified digests), apply, smoke green; prod job waits for approval.
