# ADR-0020: Graviton (ARM64) and Fargate Spot usage

- **Status:** Accepted
- **Date:** 2026-10-14
- **Deciders:** project owner, coding agent

## Context

Every service runs on ECS Fargate ([ADR-0003](0003-ecs-on-fargate.md)). Fargate offers x86_64 and ARM64
(Graviton) and two capacity types: on-demand and Spot (up to ~70 % cheaper,
interruptible with a two-minute warning). The services are static Go binaries
([ADR-0002](0002-go-for-all-services.md)), so the CPU architecture is a build flag, not a porting effort.
Checked on 2026-10-14: ECS supports Graviton on Fargate Spot since September
2024 ([announcement](https://aws.amazon.com/about-aws/whats-new/2024/09/amazon-ecs-graviton-based-spot-compute-fargate/)),
platform version 1.4.0+.

## Decision Drivers

- Lowest cost per request at equal or better performance
- Interruptions must not lose data or fail user requests
- One image per service, built once and promoted dev → prod ([ADR-0017](0017-cicd-github-oidc-rolling-deploys.md))
- Fast, reproducible builds on x86 developer machines and CI runners

## Considered Options

1. x86_64, on-demand everywhere
2. ARM64, on-demand everywhere
3. ARM64 everywhere; Spot for interruption-tolerant workloads only
4. ARM64 + Spot everywhere

## Decision

Chosen option: **3 — ARM64 for every task; Fargate Spot where interruption is harmless.**

| Workload | Dev | Prod | Why it tolerates (or not) Spot |
| --- | --- | --- | --- |
| `redirect` | Spot | on-demand base 2, scale-out on-demand | User-facing; in prod a Spot reclaim must never shrink capacity below the SLO. Dev has no SLO. |
| `api` | on-demand | on-demand | Low volume; interruption mid-request would surface as client errors. |
| `worker` | Spot (on-demand fallback weight) | Spot (on-demand fallback weight) | SQS redelivers anything not deleted; the dedupe marker makes replays harmless ([ADR-0010](0010-async-click-pipeline.md)). |
| `nightly` | Spot | Spot | Idempotent batch job; a reclaimed run is simply re-run (alarm on failure). |

**Images (`build/Dockerfile`):** the build stage runs on the build host's
platform (`--platform=$BUILDPLATFORM`) and cross-compiles with
`GOARCH=$TARGETARCH`, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`. No
QEMU emulation is involved, so arm64 images build at native speed on x86 CI
runners. The runtime base is `gcr.io/distroless/static-debian13:nonroot`
(UID 65532, no shell). Measured: 15.6–17.2 MB uncompressed, 5.4–5.9 MB
compressed; Trivy reports zero vulnerabilities. Local runs may use native
amd64 images (`make images-local`); deployments always use arm64.

**Spot interruption handling** reuses the normal graceful-shutdown path: ECS
sends SIGTERM (two-minute warning). The redirect service drains HTTP and
flushes buffered click events. The worker stops receiving, flushes its S3
batch and deletes only what was flushed. Everything finishes within the 20 s
shutdown budget (`stopTimeout` 30 s).

## Consequences

### Positive

- Graviton Fargate is priced ~20 % below x86 per vCPU/GB-hour; Spot cuts the
  worker and nightly costs by up to ~70 % more.
- Small, static, distroless images: fast pulls on scale-out and Spot replacement.
- No architecture-specific code; tests run natively on any developer machine.

### Negative

- Images must be built for arm64 explicitly (buildx); running them on an x86
  laptop requires emulation — hence the separate native `images-local` target.
- Spot capacity can be unavailable; the worker's on-demand fallback weight
  keeps the queue draining, and redirect in prod never depends on Spot.
- Dev redirect on Spot can briefly drop to zero tasks after a reclaim
  (accepted for dev; no SLO).

## Interview Angle

> "Everything is a static Go binary, so Graviton is just a GOARCH flag — about
> 20 % cheaper for the same work. Spot only goes where an interruption is
> harmless: the worker, because SQS redelivers and the consumer is idempotent,
> and the nightly job, because it's re-runnable. The user-facing redirect path
> stays on on-demand in prod."
