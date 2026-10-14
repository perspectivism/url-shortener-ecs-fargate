# ADR-0003: ECS on Fargate (vs Lambda, EKS, EC2)

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

Four workloads with different shapes (SPEC §3.2): a latency-critical HTTP
service (redirect), a low-volume HTTP API, a queue consumer that should scale to
zero, and a daily batch job. The team is one person; there should be no servers
to patch and the demo must be cheap when idle.

## Decision Drivers

- p99 < 50 ms on the redirect path, warm connection pools to DynamoDB and Valkey
- No nodes or control planes to operate
- Same container runs locally and in AWS
- Hardened runtime (read-only root FS, non-root, no capabilities)
- Interview value: classic "stateless service behind a load balancer"

## Considered Options

1. AWS Lambda (+ API Gateway / Function URLs)
2. Amazon EKS
3. ECS on EC2
4. ECS on Fargate

## Decision

Chosen option: **4 — ECS on Fargate**, ARM64, with Spot where safe (ADR-0020).

| | Lambda | EKS | ECS on EC2 | **ECS Fargate** |
| --- | --- | --- | --- | --- |
| Hot-path latency | cold starts; new connections per instance | good | good | **good, long-lived pools** |
| Operations | none | control plane + nodes | nodes, AMIs, patching | **none** |
| Idle cost | ~0 | ~$73/mo control plane + nodes | instances | **per task** |
| Local parity | emulation | containers | containers | **same image** |
| Fits a queue consumer / cron | yes | yes | yes | **yes (services + Scheduler)** |

Module layout: `infra/modules/ecs-cluster` (Fargate + Fargate Spot capacity
providers, Container Insights), `ecs-service` (reusable service: hardened task
definition, task + execution role, own security group, rolling deploy with
circuit breaker, autoscaling) and `scheduled-task` (EventBridge Scheduler →
Fargate Spot `RunTask`).

Task definitions (SPEC §12.4): `cpuArchitecture = ARM64`,
`readonlyRootFilesystem = true`, `user = "65532"`, `capabilities.drop = ["ALL"]`,
`awslogs` in `non-blocking` mode, `stopTimeout = 30`, and a container health
check that runs the binary's own `healthcheck` subcommand (distroless has no shell).

**ECS Exec (deviation from SPEC §11.3/§12.6):** AWS documents that ECS Exec is
not supported with a read-only root filesystem (the SSM agent must write to the
container filesystem), and command logging needs `script` and `cat` in the
image; distroless has neither, nor a shell. Keeping the hardened runtime wins:
`ecs_exec_enabled` defaults to **false in dev and prod**. It remains a
break-glass toggle that also relaxes `readonlyRootFilesystem` for that
environment and adds the `ssmmessages` endpoint; a validation forbids it in
prod. Diagnosis relies on structured logs, EMF metrics and `/healthz`.

## Consequences

### Positive

- Long-lived processes keep warm DynamoDB/Valkey connections and in-process caches.
- No servers, AMIs or Kubernetes upgrades; per-task IAM roles and security groups.
- The worker scales to zero on an empty queue; the nightly job only exists while running.

### Negative

- Always-on tasks cost money even at zero traffic (unlike Lambda).
- Fargate cold-start (task launch) is ~30–60 s, so scaling must start early
  (target tracking on requests per target, not only CPU).
- No interactive shell for debugging (by design).

## Interview Angle

> "Lambda is great until your hot path needs warm connections and single-digit
> millisecond overhead; EKS is a lot of platform for four services. Fargate
> gives me long-lived containers with per-task IAM and security groups, and no
> nodes to patch."
