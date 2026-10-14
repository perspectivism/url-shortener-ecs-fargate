# ADR-0016: Terraform layout, environments, S3 native state locking

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

Everything is Terraform (SPEC §12). Two environments must be identical in
shape but differ in size and safety settings. State must be remote, encrypted
and locked. CI needs scoped roles. Terraform ≥ 1.11 supports locking with an S3
lock file (`use_lockfile`), removing the need for a DynamoDB lock table.

## Decision Drivers

- dev and prod cannot drift structurally
- Reviewable, testable modules
- No long-lived credentials; no secrets in state
- Clean bootstrap and complete teardown

## Considered Options

1. Single root with workspaces per environment
2. Terragrunt
3. Plain Terraform: reusable modules + one root per environment + a bootstrap root

## Decision

Chosen option: **3**.

```text
infra/
├── bootstrap/        state bucket + CMK, GitHub OIDC, CI boundary, plan/apply roles
├── modules/          kms, network, dynamodb, messaging, events-lake, cache, secrets,
│                     ecr, s3-bucket, log-bucket (+ ecs/edge/observability in M5–M6)
└── envs/{dev,prod}/  identical main.tf/outputs.tf/versions.tf; only defaults differ
```

- **Versions:** Terraform `~> 1.16.5` (`.terraform-version` = 1.16.5); AWS
  provider `~> 6.68` with `.terraform.lock.hcl` committed for the three roots,
  locked to **6.68.0** with hashes for linux/darwin × amd64/arm64. Child modules
  declare `>= 6.68.0, < 7.0.0` and carry no lock file.
- **Parity:** `scripts/check-env-parity.sh` fails if `envs/dev` and `envs/prod`
  differ in anything but variable defaults.
- **State:** bucket `shortener-tfstate-{account}` with SSE-KMS (dedicated CMK),
  versioning, Block Public Access, TLS-only policy, access logging, and
  **S3 native locking** (`use_lockfile = true`). Keys: `bootstrap/`, `envs/dev/`, `envs/prod/`.
- **Bootstrap chicken-and-egg:** `scripts/bootstrap.sh` applies once with a
  temporary, gitignored `backend_override.tf` (local backend), writes
  `backend.hcl` for all three roots from the outputs, then runs
  `terraform init -migrate-state`. Teardown reverses it (`unmigrate`).
- **Backend config:** partial `backend "s3" {}` in code; `backend.hcl` is
  generated and gitignored (`backend.hcl.example` committed). `terraform.tfvars`
  is gitignored; `example.tfvars` committed; tfvars never contain secrets.
- **Two-phase deploy without `-target`:** `enable_services = false` builds the
  foundation; phase 2 (M5) adds services after secrets are seeded and images pushed.
- **Saved plans:** `make plan` writes `tfplan` (+ JSON and a summary);
  `make apply` applies exactly that file and refuses without `APPROVED=yes`
  (prod additionally needs `PROD_APPLY_CONFIRMED=yes`). Without a backend
  (nothing bootstrapped yet), `make plan` runs in a scratch copy with a
  temporary local backend — read-only.
- **Every role carries the CI permissions boundary** (`shortener-ci-boundary`),
  because CI may only create roles that carry it (ADR-0017).
- **Quality gates:** `make tf-check` = fmt, validate, tflint (AWS ruleset + all
  core rules), checkov, Trivy config (MEDIUM+), `terraform test` with
  `mock_provider` for every module and the bootstrap, parity, and README
  freshness (terraform-docs).

## Consequences

### Positive

- One code path for both environments; drift is a CI failure.
- No DynamoDB lock table to create, pay for or tear down.
- Plans are reviewable artifacts; applies are exactly what was reviewed.

### Negative

- Bootstrap needs a one-time local-state step (scripted).
- Root-module duplication across `envs/*` (deliberate, guarded by the parity check).

## Interview Angle

> "Modules are tested with mock providers, each environment is a thin root
> that's provably identical to the other except for sizes, state lives in S3
> with native lock files, and what gets applied is the exact saved plan someone approved."
