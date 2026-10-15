# 🚦 Gate Log

Results of every milestone gate (format: AGENTS.md §5). Gates with cloud checks
are closed as 🟡 **closed-local** in this session (owner instruction: local mode,
no AWS deployments); their pending cloud checks are listed per gate and in the
[summary](#-pending-cloud-checks-summary) at the end.

## ⚠️ Assumptions needing owner confirmation

Choices the agent made that only the owner can confirm. Each is a variable or
one-line change; nothing below has been deployed.

| Assumption | Current value | Where | Status |
| --- | --- | --- | --- |
| GitHub repository / Go module path | `perspectivism/url-shortener-ecs-fargate` | `go.mod`, CODEOWNERS, example tfvars | ✅ confirmed 2026-10-15 |
| Alert email | set in gitignored `terraform.tfvars` only | `alert_email` | ✅ confirmed 2026-10-15 |
| AWS account type | standalone (anomaly monitor on) | `cost_anomaly_monitor_enabled` | ✅ confirmed 2026-10-15 |
| Deploys | manual only: run the workflow, approve dev, approve prod | [ADR-0027](docs/adr/0027-manual-approval-for-every-deploy.md) | ✅ confirmed 2026-10-15 |
| License | MIT, © 2026 Pero Matic | `LICENSE` | ✅ confirmed 2026-10-15 |
| Release numbering | `v0.1.0` now; minor bump per prod release; majors tagged by hand | [ADR-0026](docs/adr/0026-release-versioning-with-tags.md) | ✅ confirmed 2026-10-15 |
| Analytics retention | 13 months; stats survive link deletion | [ADR-0025](docs/adr/0025-analytics-retention-13-months.md) | ✅ confirmed 2026-10-15 |
| Region | us-east-1 only (enforced by validation) | `region` | ❓ confirm |
| Monthly budgets | dev $150, prod $400 | `monthly_budget_usd` | ❓ confirm |
| Cost anomaly alert threshold | dev $10, prod $25 impact | `anomaly_threshold_usd` | ❓ confirm |
| WAF per-IP limits | API 300/min, redirects 1,200/min | `waf_*_rate_limit_per_minute` | ❓ confirm |
| CloudFront edge locations | `PriceClass_100` (North America + Europe); Asia-Pacific users get higher latency (design §6) | `infra/modules/edge` | ❓ confirm |
| Custom domain | none (`*.cloudfront.net`, HTTP on the internal VPC-origin hop) | `domain_name` | ❓ confirm |
| URL scanning in prod | on: needs a Google Safe Browsing API key at `make seed-secrets ENV=prod` | `url_scanning_enabled` | ❓ confirm you have or will create a key |
| Owner tag | `perspectivism` | `owner` | ❓ confirm (team name?) |

## Contents

- [Assumptions needing owner confirmation](#️-assumptions-needing-owner-confirmation)
- [M0 🧱 Foundations](#m0--foundations---closed)
- [M1 🔗 Core links (local)](#m1--core-links-local---closed)
- [M2 📊 Click pipeline (local)](#m2--click-pipeline-local---closed)
- [M3 🐳 Containers](#m3--containers---closed)
- [M4 🏗️ Bootstrap, network & data](#m4-️-bootstrap-network--data---closed-local)
- [M5 🚀 ECS services & edge](#m5--ecs-services--edge---closed-local)
- [M6 📈 Observability & cost](#m6--observability--cost---closed-local)
- [M7 🛡️ Security hardening](#m7-️-security-hardening---closed-local)
- [M8 🔁 CI/CD](#m8--cicd---closed-local)
- [M9 📚 Docs & final review](#m9--docs--final-review---closed-local)
- [Post-M9: batched click counting](#post-m9-change-batched-exactly-once-click-counting---closed-local)
- [Post-M9 review](#post-m9-review-module-path-retention-docs-accuracy---closed-local)
- [Post-M9: release versioning](#post-m9-release-versioning-v010---closed-local)
- [Pending cloud checks summary](#-pending-cloud-checks-summary)

## M0 🧱 Foundations — ✅ closed

- **Date:** 2026-10-14T03:55Z
- **Commit:** `7d49d8e` (tag `gate-m0`)
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

- **Decisions / ADRs:** [ADR-0001](docs/adr/0001-record-architecture-decisions.md). Toolchain pinned with a 7-day release cooldown (supply-chain hygiene): Go 1.27.2, Terraform 1.16.5, golangci-lint 2.14.0, gitleaks 8.30.1, tflint 0.64.0 (+ AWS ruleset 0.49.0), Trivy 0.75.0, actionlint 1.7.12, shellcheck 0.11.0, terraform-docs 0.24.0, k6 2.3.0, govulncheck 1.8.0, checkov 3.3.26, zizmor 1.30.1, pre-commit 4.6.2, markdownlint-cli2 0.23.3, mermaid-cli 12.0.0.
- **Deviations / assumptions:**
  - SPEC.md TOC anchor for §12 was broken; fixed with owner approval (only change to SPEC.md).
  - SPEC.md and AGENTS.md are excluded from markdownlint (style-only findings: MD032/MD060; formatting edits not authorized). Both are still covered by the link check and Mermaid render check.
  - gitleaks path allowlist for the gitignored `./.tools/` toolchain directory (listed in the threat model suppressions table).
  - Go module path `github.com/shortener/url-shortener-ecs-fargate` (GitHub org unknown; path is not resolved remotely). **Changed 2026-10-15** to `github.com/perspectivism/url-shortener-ecs-fargate`, the owner's account; the placeholder named an organization the owner does not control.
  - Headless Chrome for Mermaid runs with `--no-sandbox` (trusted repo input; Ubuntu 24.04 blocks the sandbox's user namespaces). On Linux hosts missing NSS/ALSA libraries, `make tools` extracts them into `.tools/lib` without root.
- **Pending (if closed-local):** none (no cloud checks in M0)

## M1 🔗 Core links (local) — ✅ closed

- **Date:** 2026-10-14T13:40Z
- **Commit:** `89456a0` (code), `031e282` (docs) (tag `gate-m1`)
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

- **Decisions / ADRs:** [ADR-0002](docs/adr/0002-go-for-all-services.md), 0004, 0005, 0006, 0007, 0008, 0009, 0015, 0018, **0021** (new: Safe Browsing v5). Threat model v0 (application DFD + STRIDE for redirect and API).
- **Deviations / assumptions:**
  - Naming overrides applied: CLI is `cmd/shortenerctl` (SPEC: `snipctl`), tables `shortener-{env}-*`, API key prefix `shortener_{env}_`, metrics namespace `Shortener/{env}`.
  - Safe Browsing **v5 `urls:search`** instead of the deprecated v4 Lookup API (v4 support ends 2027-03-31) — [ADR-0021](docs/adr/0021-url-safety-scanning-safe-browsing-v5.md).
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
- **Commit:** `7433e43` (code), `939f4c3` (docs) (tag `gate-m2`)
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

- **Decisions / ADRs:** [ADR-0010](docs/adr/0010-async-click-pipeline.md), [ADR-0011](docs/adr/0011-analytics-hot-and-cold-paths.md). Threat model v0.2 (pipeline STRIDE P1–P8).
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
- **Commit:** `1db7b24` (build), `7e28f71` (test isolation fix; gate run) (tag `gate-m3`)
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

- **Decisions / ADRs:** [ADR-0020](docs/adr/0020-graviton-arm64-and-fargate-spot.md) (Graviton ARM64 + Fargate Spot; Spot ARM64 support verified against the AWS announcement).
- **Deviations / assumptions:**
  - Size gate measured on the **uncompressed** filesystem (`docker export`): with the containerd image store, `docker image inspect .Size` is the compressed size and would understate the image.
  - `make up` now builds native-platform images (`images-local`, tag `local`) and starts the full containerized stack; `make up-deps` starts only the emulators. A one-shot `init` container runs `local-init`, so the quickstart needs only Docker + make.
  - The build-history review excludes the distroless base image's own history (it contains `bazel build //common:passwd`, a false positive).
  - The ALB access-log bucket will use SSE-S3 (the only encryption ALB log delivery supports) — to be suppressed with justification in M5.
- **Regression fixed:** integration runs shared the local Valkey and regenerated the same Sqids codes (fresh tables restart the ID counter), doubling uniques when a container-mode run preceded a process-mode run. Each run now uses a freshly shuffled alphabet and run-scoped owner IDs. An `httpx` shutdown test flake (speculative `StateNew` connection under load) was fixed in the test.
- **Pending (if closed-local):** none (no cloud checks in M3)

## M4 🏗️ Bootstrap, network & data — 🟡 closed-local

- **Date:** 2026-10-14T18:10Z
- **Commit:** `808714d` (tag `gate-m4-local`)
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

- **Decisions / ADRs:** [ADR-0013](docs/adr/0013-subnet-tiers-vpc-endpoints-nat-only-for-scanning.md), [ADR-0014](docs/adr/0014-secrets-management.md), [ADR-0016](docs/adr/0016-terraform-layout-environments-s3-native-locking.md), [ADR-0019](docs/adr/0019-single-aws-account-for-dev-and-prod.md). `docs/cost.md` first draft (Price List API, 2026-10-14): dev foundation ≈ $53/month idle (≈ $108 if the 1 GB Valkey minimum applied).
- **Deviations / assumptions:**
  - AWS provider pinned `~> 6.68` and locked to **6.68.0** (owner request; newer than the 7-day tool cooldown).
  - Added two helper modules beyond SPEC §12.2: `s3-bucket` (shared hardened bucket) and `log-bucket` (SSE-S3 log archive: S3 server access logs and ALB logs cannot target SSE-KMS buckets).
  - Plans ran in **local mode**: no state bucket exists yet, so `make plan` copies the root to a scratch directory with a temporary local backend (empty state) — read-only, never applied.
  - Security groups for endpoints and Valkey admit *client security groups* that tasks attach (M5), rather than per-service task SGs that do not exist yet; still SG-to-SG, no CIDRs.
  - Valkey app user ACL `on ~* &* +@all -@dangerous` (to be verified against the real cache).
  - CI apply roles: broad service actions (single account, [ADR-0019](docs/adr/0019-single-aws-account-for-dev-and-prod.md)) bounded by the permissions boundary, cross-environment tag denies and state-file isolation.
  - ⚠️ The AWS credentials available in this session are **root-user access keys**; only read-only calls were made. Recommend replacing them with an IAM Identity Center/admin role and deleting the root keys before any apply.
- **Pending (closed-local):**
  1. ☁️ Create `infra/bootstrap/terraform.tfvars` (repository, owner), then `make bootstrap` → 👤 approve → `APPROVED=yes make bootstrap-apply` (applies, writes `backend.hcl` ×3, migrates bootstrap state into S3).
  2. ☁️ `make plan ENV=dev` (S3 backend) → 👤 approve → `APPROVED=yes make apply ENV=dev` (foundation, `enable_services=false`).
  3. ☁️ Post-apply verification (AWS CLI, read-only): DynamoDB PITR + SSE-KMS on both tables; S3 Block Public Access on all buckets; SQS SSE-KMS + redrive policy; Valkey Serverless `available` with the IAM user group; secrets exist with **no** versions yet (metadata only). Record IDs in GATES.md.
  4. 👤 `make seed-secrets ENV=dev` (human, own terminal) → ☁️ agent verifies versions exist via `list-secret-version-ids` (metadata only).

## M5 🚀 ECS services & edge — 🟡 closed-local

- **Date:** 2026-10-14T21:40Z
- **Commit:** `2357c7b` (tag `gate-m5-local`)
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Static checks (M4 set on new code) | `make tf-check` | ✅ | fmt, validate (3 roots, 14 modules), tflint 0 issues, checkov 859 passed / 0 failed / 73 justified skips, Trivy MEDIUM+ 0 (2 inline justified ignores), parity (5 files identical), READMEs fresh |
| terraform test | `make tf-check` | ✅ | 43 runs in 15 suites; new assertions: ALB `internal = true` + `drop_invalid_header_fields`; ALB SG ingress = CloudFront origin-facing prefix list only, egress only to task SGs; task SG ingress from the ALB SG only; read-only root FS, UID 65532, `drop ALL`, ARM64, binary health check, non-blocking awslogs; circuit breaker with rollback (100/200); `CachingDisabled` on both behaviors; WAF associated (managed rules, AnonymousIpList in count, 429 rate rules, Authorization redacted); VPC origin; secrets via `valueFrom` only; execution role reads only its own secrets; scheduler may only run its task on its cluster; custom domain → HTTPS listener, https-only origin, TLSv1.2_2021 |
| Plans (phase 1) | `make plan ENV=dev/prod` | ✅ | read-only, local mode: dev +92, prod +101 |
| Plans (phase 2) | `TF_VAR_enable_services=true TF_VAR_image_tag=3b2d556f3a67 make plan` | ✅ | read-only, local mode: dev +171, prod +182, 0 destroy; WAF `CLOUDFRONT` in `us-east-1`; dev redirect on Spot, prod redirect/api on-demand base 2 |
| Smoke suite | `make smoke` against the local containerized stack | ✅ | create → 302 (`no-store`) → click counted after 5 s → 404 → delete → 410 → 401 ×2 → Google test URL 422 `unsafe_url` (fake scanner); oversize 413; cross-owner 404 (GET/DELETE/stats) |
| Regression | `make lint test test-integration docs-check` | ✅ | lint 0 issues (smoke tag linted); coverage 97.7%; integration 14/14 (83.7 s); 23/23 diagrams, 119/119 links |

- **Decisions / ADRs:** [ADR-0003](docs/adr/0003-ecs-on-fargate.md), [ADR-0012](docs/adr/0012-cloudfront-vpc-origin-internal-alb-waf.md), [ADR-0017](docs/adr/0017-cicd-github-oidc-rolling-deploys.md) (Proposed: deploy strategy decided; CI/CD completed in M8). Design doc deployment section.
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

- **Decisions / ADRs:** [ADR-0022](docs/adr/0022-observability-alerting-and-cost-guardrails.md) (observability, alerting and cost guardrails). Runbook v0.2 (SLIs/SLOs, alarm responses), cost model v0.3, design §4, threat model v0.3.
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

- **Decisions / ADRs:** [ADR-0023](docs/adr/0023-iam-review-data-perimeter-and-waf-tuning.md) (IAM review, VPC endpoint data perimeter, WAF tuning). Threat model v1.0.
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

- **Decisions / ADRs:** [ADR-0017](docs/adr/0017-cicd-github-oidc-rolling-deploys.md) completed (Accepted): workflows, approvals, digest promotion, hardening. Runbook §5.1 CI/CD setup, §5.2 deploy, §5.3 rollback. Threat model C5/C6/C9 updated.
- **Deviations / assumptions:**
  - Images are promoted as OCI layouts (built once in `deploy.yml`, pushed with pinned `crane`, digest verified per environment) because each environment has its own ECR repositories and the apply roles cannot cross environments ([ADR-0019](docs/adr/0019-single-aws-account-for-dev-and-prod.md) guardrail).
  - Provenance/SBOM attestations are disabled to keep one manifest digest per image (signing is future work).
  - `make push-images` now pushes the OCI layouts from `make images-oci` (was: docker push of `make images`), so manual and CI deploys share one path.
  - The `deploy` pipeline assumes phase 1 is applied and secrets are seeded; it always deploys with `enable_services = true`.
  - `.github/CODEOWNERS` assigns every path to `@perspectivism` (set 2026-10-15).
  - **Flake observed once:** in the first `make ci` run `TestKeyRangeAllocationNoDuplicates` reported 136 of 10,000 creates failing (0 duplicates). Not reproducible in 7 further runs (3 alone, 3 under parallel `go test -race` load, 2 full suites) nor in the full `make ci` reruns. The test now records failure reasons (status/problem code or transport error) so a recurrence is diagnosable; suspected cause: DynamoDB Local latency spikes beyond the 200 ms deadline (→ 503). No assertion was loosened.
- **Pending (closed-local):**
  1. 👤 Push the repository to GitHub (`perspectivism/url-shortener-ecs-fargate`).
  2. 👤 Configure repository variables, environments `dev`/`prod` (deployment branch `main`; required reviewers on **both**; prevent self-review off while you are the only reviewer, see [ADR-0027](docs/adr/0027-manual-approval-for-every-deploy.md)), environment variables and the `SMOKE_API_KEY` secrets — exact commands in [runbook §5.1](docs/runbook.md#51-cicd-setup-one-time--repository-admin).
  3. 👤 Branch ruleset on `main`: PR + 1 approval + code owners; required checks (the five `ci` jobs and `terraform plan (dev)`); block force pushes.
  4. 👤 Open a PR: `ci` green, plan comment posted by `terraform plan (dev)`.
  5. ☁️ Merge: `deploy` builds once, dev pushes with verified digests, applies, smoke passes; the `prod` job **waits for approval** (the agent does not approve).

## M9 📚 Docs & final review — 🟡 closed-local

- **Date:** 2026-10-15T02:30Z
- **Commit:** see tag `gate-m9-local`
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Docs render and link | `make docs-check` | ✅ | markdownlint 0 issues (50 files); 44/44 Mermaid diagrams; 274/274 relative links |
| Fresh-clone test | `make fresh-clone-test` | ✅ | clone of `ccc8c12`; README local quickstart run verbatim: `make up`, key, create, `302 -> https://example.com/hello`, `total_clicks: 1`; integration tests from the clone green (86 s) |
| ADRs complete | `scripts/check_adrs.py` (in `make docs-check`) | ✅ | 23 ADRs: all 20 required + 0021–0023; all Accepted; MADR sections + Interview Angle; all indexed |
| Runbook ↔ alarms | `terraform test` (`infra/envs/{dev,prod}`) | ✅ | runbook `### 4.n` sections name exactly the 14 Terraform alarms; negative probe (renamed heading) fails |
| Teardown tooling | `shellcheck`, `scripts/verify-teardown.sh --dry-run` | ✅ | shellcheck clean (teardown, bootstrap, verify-teardown); 34 read-only checks |
| `terraform plan -destroy` per root | `make plan-destroy ENV=dev` / `prod`; bootstrap | ⏸️ | needs AWS credentials (session expired) and deployed state → pending |
| Final full run | `make lint test test-integration tf-check docs-check` (+ `check-workflows`, secret scans, `vuln`) | ✅ | lint 0; coverage 97.7 %; integration 80 s; checkov 884/0/75; tflint 0; Trivy 0; 57 `terraform test` runs; pins OK; gitleaks 22 commits clean; govulncheck clean |

- **Decisions / ADRs:** none new; README, design v1.0, runbook v1.0, cost v1.0.
- **Deviations / assumptions:**
  - Runbook alarm coverage is enforced by a Terraform test in the env roots (reads `docs/runbook.md`) rather than a separate script.
  - The link checker now keeps inline code when slugging headings (GitHub behaviour); before, headings with code produced wrong anchors.
  - The fresh-clone test links this checkout's verified `./.tools` by default (`FRESH_TOOLS=1` installs from scratch); the quickstart itself needs no tools beyond Docker, make, curl and jq.
- **Pending (closed-local):** the consolidated list below (items 18–19 are M9's).

## Post-M9 change: batched exactly-once click counting — 🟡 closed-local

- **Date:** 2026-10-15
- **Commits:** `0af8040`, `f1527c4` (tag `post-m9-batched-counting-local`)
- **Why:** answers "what happens when a link goes viral?". With one transaction per click (12 WRU), a hot link's `DAY#` counter item hits DynamoDB's ~1,000 writes/s per-item limit at a few hundred clicks/s. Approved by the owner on 2026-10-15.
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Unit (store, worker, config) | `make test` | ✅ | coverage 97.8 %; new tests: hot-link aggregation, ≤ 100-item packing, duplicates rebuilt from cancellation reasons, conflict/throttle retries, human + bot clicks sharing the `DAY#` item, visitors added before the counting commit |
| Hot-link integration test | `make test-integration` (`TestHotLinkBatchedCounting`) | ✅ | 1,500 clicks on one link, each delivered twice: exact totals, dimensions and uniques; 1,500 duplicates detected; **17 transactions (88 clicks per counting write)** |
| Full regression | `make ci` | ✅ | on `f1527c4`: lint 0; coverage 97.8 %; integration (15 tests) 87 s; images 15.6–17.2 MB, UID 65532; checkov 884/0/75; tflint 0; 57 `terraform test` runs; 45/45 diagrams, 287/287 links; 24 ADRs; pins OK; zizmor clean; gitleaks 26 commits clean; govulncheck clean |
| Fresh-clone test | `make fresh-clone-test` | ✅ | clone of `f1527c4`: quickstart `302`, `total_clicks: 1`; integration green (95 s) |

- **Second bug caught by the integration tests:** the first batched worker added visitors to the HyperLogLog *after* committing counters, so stats briefly showed clicks with `uniques = 0`. Visitors are now added before the commit (PFADD is idempotent on redelivery); covered by `TestUniquesAddedBeforeCounting`.
- **Bug caught by the new integration test:** the first version sent two operations on the same `DAY#` item (human `clicks` and `bot_clicks`) in one transaction, which DynamoDB rejects. Fixed with one update per item carrying several `ADD` clauses; the unit-test fake now rejects duplicate items like DynamoDB does.
- **Decisions / ADRs:** [ADR-0024](docs/adr/0024-batched-exactly-once-click-counting.md) (supersedes [ADR-0010](docs/adr/0010-async-click-pipeline.md)'s per-event transaction). Updated: design §2/§4/§8/§12, README walkthrough steps 2, 8 and 12 plus the testing table, threat model P1, runbook 4.4/4.7, cost levers, dashboard (`WorkerCountTransactions`), `.env.example` (worker settings, including the new `WORKER_COUNT_INTERVAL` and `WORKER_COUNT_MAX_EVENTS`).
- **Deviations from SPEC:** §8.2 ("for each message: TransactWriteItems") → batched transactions per link with per-click markers; stats freshness gains at most 2 s (SPEC N4 allows 2 min).
- **Pending (closed-local):** no new cloud checks. Existing item 9 (services healthy) and item 10 (smoke: stats within 2 minutes) cover it in AWS. Optional: watch `WorkerCountTransactions` against `WorkerEventsProcessed` on the dashboard during the k6 baseline (item 14).

## Post-M9 review: module path, retention, docs accuracy — 🟡 closed-local

- **Date:** 2026-10-15
- **Commit:** see tag `post-m9-review-local`
- **What the owner found or asked for, and what changed:**
  - **Go module path** was a placeholder naming a GitHub org the owner doesn't control (`github.com/shortener/...`) → `github.com/perspectivism/url-shortener-ecs-fargate` in `go.mod`, every import, golangci, the Dockerfile ldflag and image source label.
  - **Analytics retention** (owner decision): counters expire 13 months after their day, including after link deletion; prod raw events 365 → 395 days ([ADR-0025](docs/adr/0025-analytics-retention-13-months.md)).
  - **Interview gaps written up:** same-URL policy, retention, user-perceived latency, cache eviction/sizing/hot keys, availability and DR (RPO/RTO) in design §3/§4/§6/§7/§11, README follow-ups, runbook §5.14 region outage.
  - **Severity review (agent):** anomaly monitor made optional (`cost_anomaly_monitor_enabled`, on: standalone account confirmed); `region` restricted to us-east-1 (the edge reuses the regional CMK); APAC latency claim corrected (`PriceClass_100` has no Asia-Pacific edges); threat-model row C10 (PR plan runs PR code with read-only credentials).
  - **Source-control hygiene:** history clean of emails and credentials (commit metadata uses GitHub noreply addresses); `.gitignore` now covers all saved plans, plan logs and summaries (they contain variable values, including the alert email) and `.docker/`; `check-ignore.sh` asserts 35 paths.
  - **Docs accuracy:**
    - 72 bare ADR mentions linked; `check_adr_links.py` fails on new ones;
    - `check_doc_facts.py` checks every quoted count of ADRs, integration tests, alarms and STRIDE rows against the source of truth;
    - an audit of every backticked reference found no dangling make target, path, env var, metric or Terraform variable;
    - config values quoted in the docs were checked against code;
    - two stale counts were fixed (integration tests 16; coverage now "≈ 98 %").
  - **Process fixes:**
    - pre-commit `terraform-docs` hook: a stale module README now blocks the commit (this mistake happened three times);
    - an "Assumptions needing owner confirmation" table at the top of this log.
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Full regression | `make ci` | ✅ | on `6e00f0c`: lint 0; coverage 97.8 %; integration (16 tests) 91 s; checkov 884/0/75; 60 `terraform test` runs; 45/45 diagrams, 381/381 links; ADR links and doc facts OK; check-ignore OK; gitleaks 30 commits clean |
| Fresh-clone test | `make fresh-clone-test` | ✅ | clone of `6e00f0c`: quickstart `302`, `total_clicks: 1`; integration green |

- **Pending (closed-local):** unchanged list below; first `make plan` after `aws login` is the first real-provider plan of everything added since M5 (observability, budget, endpoint policies, schedule group, Container Insights log group, retention).

## Post-M9: release versioning (v0.1.0) — 🟡 closed-local

- **Date:** 2026-10-15
- **Commit:** tag `v0.1.0`
- **Why:** owner asked for a package version; Go versions modules by git tag, and nothing named what reached prod.
- **Change** ([ADR-0026](docs/adr/0026-release-versioning-with-tags.md)):
  - `scripts/version.sh` stamps binaries with `v0.1.0+<sha>` / `v0.1.0-N-g<sha>` (logs' `version` field; OCI `version` + `revision` labels); image tags stay the bare SHA.
  - After prod smoke tests pass, `deploy.yml` runs `scripts/release.sh`, which creates the next GitHub Release (minor bump; a manual tag such as `v1.0.0` is respected) with the commits and image digests (prod job: `contents: write`).
  - `v0.1.0` tags the first complete build; [CHANGELOG.md](CHANGELOG.md); runbook deploy/rollback name releases.
- **Also in v0.1.0 (owner requests, 2026-10-15):**
  - MIT [LICENSE](LICENSE) (© 2026 Pero Matic) and a CI status badge in the README.
  - **No automatic deploys** ([ADR-0027](docs/adr/0027-manual-approval-for-every-deploy.md)):
    - `deploy.yml` runs only on `workflow_dispatch` from `main`;
    - `dev` and `prod` both require a reviewer;
    - `scripts/check-deploy-gate.sh` (in `make check-workflows`) fails on any other trigger, or on an AWS role outside an approval-gated environment;
    - deviation from SPEC §14 recorded.
  - Runbook fix: "Prevent self-review" stays off while the owner is the only reviewer (it would block every deploy).
  - **Regression fixed:** `scan-images.sh` rejected the new `REVISION` build arg (its build-arg allowlist predates it); added it to the allowlist.
- **Walkthrough accuracy review (2026-10-15):** every README walkthrough claim checked against code, Terraform and tests. Fixed:
  - the testing table said 57 `terraform test` runs (actual 60); `check_doc_facts.py` now derives this count too;
  - design §1 lists F1–F9 with IDs, as the walkthrough cites them;
  - ALB + WAF fixed cost ~$30/month (was ~$28; [cost.md](docs/cost.md) has $28–31) in README and [ADR-0012](docs/adr/0012-cloudfront-vpc-origin-internal-alb-waf.md).
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Version and release logic | `make test-scripts` (also in `make ci` and the CI `go` job) | ✅ | version: no tag / on tag / after tag / dirty / non-release tags ignored; release: first v0.1.0, minor bump, manual major kept, bump after major |
| Docs facts | `make docs-check` | ✅ | 9 quoted counts match incl. 60 `terraform test` runs; negative probe (README says 59) fails |
| Workflows | `make check-workflows` + `zizmor --persona auditor` | ✅ | actionlint clean; zizmor no findings (auditor); pins OK; deploy gate OK (negative probes: push trigger and environment-less job both rejected) |
| Full regression | `make ci` | ✅ | on `9d9cb27`: lint 0; coverage 97.8 %; test-scripts OK; govulncheck clean; gitleaks 32 commits clean; integration (16 tests) 90 s; images arm64, UID 65532, Trivy 0 fixable HIGH/CRITICAL; checkov 884/0/75; 60 `terraform test` runs; 46/46 diagrams, 414/414 links; pins OK; deploy gate OK |
| Fresh-clone test | `make fresh-clone-test` | ✅ | on `9d9cb27`: Quickstart from a clean clone passed; integration 96 s |

- **Pending:** the first release beyond v0.1.0 is created by the first prod deploy (pending cloud check 17 onward). Push tags with `git push origin v0.1.0` (gate tags `gate-*` are milestone markers; push them only if wanted).

## Post-M9: commit trailers removed from history — ✅ closed

- **Date:** 2026-10-15
- **Why:** owner request: no `Co-Authored-By` trailer in any commit, now or later.
- **Change:** `git filter-branch --msg-filter` removed the trailer line from all 32 commits before anything was pushed (no remote existed, so no published history changed). Tags moved to the rewritten commits; commit SHAs quoted in this file were updated to the new ones. Image tags recorded as evidence (M3 `5b4d82448194`, M5 `3b2d556f3a67`) stay as built: they name the pre-rewrite commits.
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Content unchanged | compare tree, author, committer and dates per commit, before vs after | ✅ | 33/33 commits identical except message |
| No trailers | `git log main --tags --format=%B \| grep -ci co-authored` | ✅ | 0 |
| Tags | each tag's old target mapped to its new target | ✅ | 11/11 |

## Post-M9: external review fixes — 🟡 closed-local

- **Date:** 2026-10-15
- **Why:** an external code review reported 8 findings and 4 doc errors; all were verified against the code and fixed, each with a regression test that fails without the fix (negative probe recorded below).
- **Fixes:**
  - **WAF sampling exposed API keys** (sampling ignores log redaction): sampling off on the web ACL and every rule ([ADR-0029](docs/adr/0029-edge-log-privacy.md)).
  - **Disabled link could be re-cached** by a racing fill: invalidation leaves a 10 s fence, fills are `SET NX`, fills slower than 5 s skip the write ([ADR-0030](docs/adr/0030-consistency-under-concurrency.md)).
  - **In-batch repeat of a failed event was acknowledged:** repeats take the first copy's result (no click was lost; the outcome was wrong).
  - **Concurrent idempotent creates made two links:** atomic claim (`SET NX`, 30 s) before the create; a repeat in flight gets 409 `request_in_progress`; failures release the claim.
  - **Stale scan verdict could disable a replaced URL:** `ApplyScan` takes the scanned URL and checks it inside the conditional update.
  - **Approval preceded the plan:** per environment, a read-only `plan-<env>` job publishes the saved plan and its SHA-256; the approval-gated job applies that plan only if the hash matches ([ADR-0028](docs/adr/0028-approve-the-saved-plan-versioned-environment-settings.md)). New bootstrap roles `shortener-ci-plan-{dev,prod}`.
  - **CI fell back to defaults:** committed `infra/envs/<env>/deploy.tfvars` (replaces `example.tfvars`) used by every plan; the overlay holds only `alert_email` (now `sensitive`) and the image tag.
  - **Dedupe marker expired before queue retention:** TTL 4 days from when it is written; a Terraform test keeps queue retention ≤ 4 days.
  - **Docs:** privacy claim corrected; CloudFront logs now use a field allowlist (no IPs, referrers, user agents, query strings, cookies) and WAF logs keep only blocked/counted requests; runbook disable command (JSON `reason`, content type); stats access pattern; [ADR-0012](docs/adr/0012-cloudfront-vpc-origin-internal-alb-waf.md) link in this file.
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Full regression | `make ci` | ✅ | lint 0; coverage 97.6 %; test-scripts OK (incl. saved-plan: altered, missing, other-environment and malformed plans refused); govulncheck clean; gitleaks 35 commits clean; integration 17 tests 79 s; images arm64, UID 65532, 15.6–17.2 MB; checkov 911/0/75; 61 `terraform test` runs; 47/47 diagrams, 466/466 links; 30 ADRs; doc facts OK; pins OK; zizmor clean; deploy gate OK |
| Regression tests fail without the fix | negative probes | ✅ | fence-blind fill → `TestResolveFillRacingDisable` caches the phishing URL; plain `SET` claim → 50/50 concurrent claims win; no URL check → stale verdict disables the new URL; repeat not propagated → `[Failed Duplicate]`; click-time TTL → marker TTL check fails; sampling on, `c-ip` logged → edge tests fail |
| Deploy gate | `scripts/check-deploy-gate.sh` + 5 negative probes | ✅ | rejects: apply job not depending on its plan job, apply without the saved-plan check, apply role in a plan job, AWS role outside the four environments, a push trigger |
| Workflows | `zizmor --persona auditor` | ✅ | no findings |
| Fresh-clone test | `make fresh-clone-test` | ✅ | on `ab0ab3d`: Quickstart from a clean clone passed; integration 97 s |
| Docs facts also cover CHANGELOG | `make docs-check` | ✅ | CHANGELOG said 27 ADRs (actual 30); `check_doc_facts.py` now scans it; negative probe (29) fails |

- **Decisions / ADRs:** [ADR-0028](docs/adr/0028-approve-the-saved-plan-versioned-environment-settings.md), [ADR-0029](docs/adr/0029-edge-log-privacy.md), [ADR-0030](docs/adr/0030-consistency-under-concurrency.md); status notes on [ADR-0008](docs/adr/0008-elasticache-serverless-valkey.md)/0010/0012/0016/0018/0027.
- **Deviations from SPEC:** N6: WAF logs of blocked/counted requests keep the client IP (WAF cannot redact it; 14/90 days). §5.2: dedupe marker TTL is write time + 4 days (SPEC: now + 3 days). §14: per-environment plan job before the approval-gated apply.
- **Pending (closed-local):** the new bootstrap roles, edge logging settings and plan hand-over are verified only in the pending cloud checks below (items 2, 16, 17, 19).

## Post-M9: second review fixes and two environments — 🟡 closed-local

- **Date:** 2026-10-15
- **Why:** a second external review found three remaining issues and one stale doc; separately, the owner asked for two GitHub environments instead of four.
- **Fixes:**
  - **Immediate disable relied on an eventually consistent read:** link reads use `ConsistentRead` ([ADR-0031](docs/adr/0031-consistent-link-reads-and-owned-idempotency-claims.md)).
  - **Idempotency claims had no owner:** each claim carries a random token; release and finish are Valkey scripts conditional on it; claimed work has a 10 s deadline inside the 30 s lease.
  - **Var-file errors were swallowed** (`mapfile < <(…)` ignores the helper's exit status): `tf-plan.sh` and `teardown.sh` capture the output in an assignment and resolve settings before anything else. The guard now also rejects sensitive variables, email addresses and `enable_services` in `deploy.tfvars`.
  - **Env READMEs** described `example.tfvars`; they now describe `deploy.tfvars` and the overlay.
  - **Two environments** ([ADR-0032](docs/adr/0032-two-github-environments-deploy-plans-trust-main.md)): plan jobs run without an environment under one read-only role `shortener-ci-deploy-plan` that trusts only `repo:<repo>:ref:refs/heads/main`; only `dev` and `prod` exist (required reviewers). Replaces the `dev-plan`/`prod-plan` environments and `shortener-ci-plan-{dev,prod}` roles of the previous entry.
- **Checks:**

| Check | Command | Result | Evidence |
| --- | --- | --- | --- |
| Lint, unit, scripts, vuln, secrets | `make lint test test-scripts vuln test-secret-scan secret-scan check-ignore` | ✅ | lint 0; coverage 97.5 %; test-scripts OK incl. new `test-varfiles.sh` (9 cases); govulncheck clean; gitleaks 37 commits clean |
| Integration | `make test-integration` | ✅ | 17 tests, 81 s and 79 s on two runs. The first run inside `make ci` failed once: a DynamoDB Local `PutItem` took 222 ms, past the 200 ms deadline (503), in `TestValidationAndAuthErrors`, a path this change does not touch. Emulator latency, not a regression; the deadline was not loosened |
| Images, Terraform, docs, workflows | `make images check-images scan-images test-image-secret-scan tf-check docs-check check-workflows` | ✅ | arm64, UID 65532, 15.6–17.2 MB; checkov 903/0/75; 61 `terraform test` runs; 47/47 diagrams, 504/504 links; 32 ADRs; doc facts OK; pins OK; zizmor clean; deploy gate OK |
| Regression tests fail without the fix | negative probes | ✅ | eventually consistent read → `TestGetLinkSeesCompletedDisable` gets `active`; unconditional release/put → `TestIdempotencyClaimOwnership` overwrites B's record; previous `tf-plan.sh` / `teardown.sh` → `test-varfiles.sh` fails |
| Deploy gate | `scripts/check-deploy-gate.sh` + 6 negative probes | ✅ | rejects: apply job without its plan dependency, apply without the saved-plan check, apply role in a plan job, push/apply in a job without an environment, any environment other than dev/prod, a push trigger |

- **Decisions / ADRs:** [ADR-0031](docs/adr/0031-consistent-link-reads-and-owned-idempotency-claims.md), [ADR-0032](docs/adr/0032-two-github-environments-deploy-plans-trust-main.md); status notes on [ADR-0004](docs/adr/0004-dynamodb-for-the-link-store.md)/0028/0030.
- **Deviations from SPEC:** SPEC §12 and §17 and AGENTS.md §6 name `example.tfvars` for the environment roots; they now use the committed `deploy.tfvars` ([ADR-0028](docs/adr/0028-approve-the-saved-plan-versioned-environment-settings.md)). The bootstrap root keeps `example.tfvars`.
- **Pending (closed-local):** pending items 2 (new role), 16 (variables, two environments) and 17 (deploy).

## 📋 Pending cloud checks summary

Complete and ordered; every ☁️ mutating step is plan → 👤 approve → apply of
that saved plan. Prod is planned only, never applied, unless the owner
instructs it.

1. 👤 **Credentials:** `aws login` (the CLI session expired during M6–M9) with an admin **role session**, not root access keys.
2. **Bootstrap:** create `infra/bootstrap/terraform.tfvars`; `make bootstrap` → approve → `APPROVED=yes make bootstrap-apply`.
3. **Dev phase 1:** settings are in the committed `infra/envs/dev/deploy.tfvars`; the local, gitignored `terraform.tfvars` holds `alert_email` (it may still repeat repository and owner); `make plan ENV=dev` (plan invariants must print OK; **first real-provider plan of everything added since M5**, so expect to fix issues here) → approve → `APPROVED=yes make apply ENV=dev`.
4. **Verify foundation:** DynamoDB PITR/SSE, S3 BPA, SQS SSE/redrive, Valkey available with IAM auth, secrets exist without values.
5. 👤 Confirm the SNS subscription email (`aws sns list-subscriptions-by-topic` shows no `PendingConfirmation`).
6. 👤 `make seed-secrets ENV=dev`; verify secret versions (metadata only).
7. **Images:** `make images-oci` (clean commit) → approve → `APPROVED=yes make push-images ENV=dev` (digests verified).
8. **Dev phase 2:** add `enable_services = true` and `image_tag = <printed tag>` to the local `terraform.tfvars`; `make plan ENV=dev` → approve → `APPROVED=yes make apply ENV=dev`.
9. **Services healthy:** all services steady, targets healthy. This also proves the endpoint data perimeter (ECR pull, Logs, Secrets, SQS through endpoints).
10. **Smoke:** create dev API keys straight into env vars (README step 6); `SMOKE_API_KEY_2=… make smoke-env ENV=dev` (includes ALB-unreachable); then `SMOKE_WAF_BURST=true make smoke-env ENV=dev` last (429 + `Retry-After`).
11. **Resilience:** manual nightly run succeeds (runbook §5.12); forced bad deploy (non-existent `image_tag`, throwaway apply) → circuit-breaker rollback recorded; re-apply the good tag.
12. **IAM:** `make plan ENV=dev && make validate-iam ENV=dev` and the same for `ENV=bootstrap` — 0 ERROR/SECURITY_WARNING, 0 unknown.
13. **Alerts:** `aws cloudwatch set-alarm-state --alarm-name shortener-dev-dlq --state-value ALARM --state-reason test` → 👤 email received; dashboard `shortener-dev` shows data in every widget.
14. **Load baseline:** `LOAD_API_KEY=… make load BASE_URL=<cloudfront>`; `scripts/load-report.sh dev <start> <end>`; record RPS, p50/p95/p99, error rate, cache hit ratio in GATES (M6).
15. **Cost:** ≥ 24 h after step 3: bootstrap `activate_cost_allocation_tag = true` → plan → approve → apply; ≥ 3 days: measured spend into cost.md §7.
16. 👤 **CI/CD setup:** push to GitHub (`perspectivism/url-shortener-ecs-fargate`), configure variables, the `dev` and `prod` environments (required reviewers), secrets and the `main` ruleset (runbook §5.1); open a PR → `ci` green + plan comment.
17. **CD:** 👤 start **Actions → deploy → Run workflow** on `main` → `plan-dev` prints the plan and stores it under `plans/dev/` in the state bucket → 👤 read it and approve `dev` → hash verified, push (verified digests), apply, smoke green → `plan-prod` prints the prod plan → the prod job **waits for approval** (not approved by the agent). Merges alone never deploy.
18. **Destroy plans (read-only):** `make plan-destroy ENV=dev` (and `ENV=prod` if deployed); bootstrap: `terraform -chdir=infra/bootstrap plan -destroy -var-file=terraform.tfvars` against the S3 backend — all succeed.
19. **Edge privacy and sampling (after phase 2):** `aws wafv2 get-web-acl` shows `SampledRequestsEnabled: false` everywhere; a CloudFront log record has no `c-ip`, `x-forwarded-for`, referrer, user agent, query or cookie fields; the WAF log group receives only BLOCK/COUNT records (e.g. after the WAF burst smoke test).
20. **Only if requested:** `make teardown` (typed confirmation per step), then `make verify-teardown` → nothing left except KMS keys pending deletion.
