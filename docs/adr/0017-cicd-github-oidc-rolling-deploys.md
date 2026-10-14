# ADR-0017: CI/CD with GitHub Actions OIDC; rolling deploys with circuit breaker (vs ECS blue/green)

- **Status:** Proposed (deployment strategy decided in M5; CI/CD workflows completed in M8)
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

Deploys must be reproducible, auditable and safe to roll back, without
long-lived AWS keys in CI (SPEC §14). Terraform owns the task definitions
(SPEC §12.4): a deploy is a `terraform apply` with a new `image_tag`.

## Decision Drivers

- Same artifact in every environment (no rebuild for prod)
- Automatic rollback of bad deploys
- Minimal moving parts; no extra deployment service
- No static credentials anywhere

## Considered Options

1. Rolling deploys with the ECS deployment circuit breaker
2. ECS blue/green (CodeDeploy, or ECS-native blue/green)
3. Canary/linear traffic shifting

## Decision (deployment strategy — M5)

Chosen option: **1 — rolling deploys with the circuit breaker and rollback.**

- `deployment_minimum_healthy_percent = 100`, `deployment_maximum_percent = 200`:
  new tasks start before old ones stop; capacity never drops.
- `deployment_circuit_breaker { enable = true, rollback = true }`: if new tasks
  keep failing health checks (container `healthcheck`, ALB `/healthz`), ECS stops
  the deployment and restores the last healthy task definition.
- `wait_for_steady_state = true`: `terraform apply` waits for the deployment and
  fails when the circuit breaker rolls back, so CI goes red.
- **Images are immutable and tagged with the git SHA**; `make push-images`
  refuses dirty trees. Prod receives the *same digest* that passed dev (M8
  promotes it, no rebuild).
- **Rollback procedure**: re-apply the previous `image_tag` (runbook).
- Graceful shutdown makes rolling deploys lossless: the ALB deregistration
  delay is 15 s, then SIGTERM; redirect drains HTTP and flushes buffered clicks
  within 20 s (`stopTimeout` 30 s).

**Why not blue/green:** it adds a second target group, test listeners and
traffic-shifting configuration for little gain at this scale; the circuit
breaker already provides automatic rollback. ECS-native blue/green is listed as
future work (SPEC §20).

## Decision (CI/CD — completed in M8)

GitHub Actions with OIDC roles from `infra/bootstrap` (plan role for pull
requests, one apply role per GitHub environment, permissions boundary), pinned
action SHAs, and a manual approval for prod. Details are added when M8 lands.

## Consequences

### Positive

- One mechanism for deploy and rollback (`image_tag`), visible in Terraform state.
- Bad deploys roll back automatically and fail the pipeline.

### Negative

- No traffic shifting: a bad version that passes health checks serves 100 %
  of traffic until detected by alarms (mitigated by smoke tests after deploy).
- Rolling deploys briefly run two versions side by side (both must be compatible).

## Interview Angle

> "Every deploy is an apply with a new git-SHA image tag; ECS rolls tasks with
> full capacity, and the circuit breaker automatically rolls back a version
> that can't get healthy — Terraform fails, so the pipeline fails."
