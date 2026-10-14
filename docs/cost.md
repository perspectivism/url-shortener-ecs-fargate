# 💸 URL Shortener — Cost Model

> **Status:** first draft (M4). Prices verified **2026-10-14** for **us-east-1**
> with the AWS Price List API (`aws pricing get-products`, read-only) and the
> public pricing pages linked below. Service-tier numbers (M5) are estimates
> until measured in M6.

## Contents

1. [Unit prices](#1-unit-prices)
2. [Dev — foundation (phase 1)](#2-dev--foundation-phase-1)
3. [Dev — with services (phase 2, estimate)](#3-dev--with-services-phase-2-estimate)
4. [Prod — idle baseline (estimate)](#4-prod--idle-baseline-estimate)
5. [Cost drivers and levers](#5-cost-drivers-and-levers)
6. [Guardrails](#6-guardrails)

## 1. Unit prices

| Component | Price (us-east-1) | Price List usage type / source |
| --- | --- | --- |
| Interface VPC endpoint | $0.01 per endpoint per AZ-hour (+ $0.01/GB) | `USE1-VpcEndpoint-Hours` |
| NAT gateway | $0.045/hour + $0.045/GB processed | `NatGateway-Hours`, `NatGateway-Bytes` |
| Public IPv4 address (NAT EIP) | $0.005/hour | `USE1-PublicIPv4:InUseAddress` |
| Application Load Balancer | $0.0225/hour + $0.008/LCU-hour | `LoadBalancerUsage`, `LCUUsage` |
| Fargate ARM64 (Graviton) | $0.03238/vCPU-hour, $0.00356/GB-hour | `USE1-Fargate-ARM-vCPU-Hours`, `USE1-Fargate-ARM-GB-Hours` |
| Fargate x86 (comparison) | $0.04048/vCPU-hour, $0.004445/GB-hour | `USE1-Fargate-vCPU-Hours`, `USE1-Fargate-GB-Hours` |
| ElastiCache Serverless Valkey | $0.084/GB-hour stored, $0.0023 per million ECPUs | `USE1-CachedData:Valkey`, `USE1-ElastiCacheProcessingUnits:Valkey` |
| Valkey Serverless minimum | 100 MB metered per cache ([pricing](https://aws.amazon.com/elasticache/pricing/), [FAQ](https://aws.amazon.com/elasticache/faqs/)); an older [doc page](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/WhatIs.corecomponents.html) still says 1 GB | worst case shown below |
| KMS customer-managed key | $1/key-month + $0.03 per 10,000 requests | `us-east-1-KMS-Keys`, `us-east-1-KMS-Requests` |
| Secrets Manager | $0.40/secret-month + $0.05 per 10,000 API calls | `USE1-AWSSecretsManager-Secrets` |
| AWS WAF | $5/web ACL-month + $1/rule-month + $0.60 per million requests | `Global-WebACLV2`, `Rule`, `Request` |
| CloudWatch Logs | $0.50/GB ingested, $0.03/GB-month stored | `USE1-DataProcessing-Bytes`, `USE1-TimedStorage-ByteHrs` |
| DynamoDB on-demand, S3, SQS, Athena, Glue, ECR storage | pay-per-use; cents at demo scale | — |
| CloudFront | always-free tier: 1 TB and 10 M requests per month ([pricing](https://aws.amazon.com/cloudfront/pricing/)) | — |

Monthly figures use 730 hours.

## 2. Dev — foundation (phase 1)

`enable_services = false`: network, data, cache, secrets, ECR. Idle (no traffic).

| Item | Quantity | $/month |
| --- | --- | --- |
| Interface endpoints | 5 (ecr.api, ecr.dkr, logs, sqs, secretsmanager) × 1 AZ; `ssmmessages` only with break-glass ECS Exec (+$7.30) | 36.50 |
| Valkey Serverless | 100 MB minimum (1 GB worst case: $61.32) | 6.13 |
| KMS keys | env CMK + state CMK (bootstrap) | 2.00 |
| Secrets Manager | 2 secrets | 0.80 |
| NAT gateway | none (`url_scanning_enabled = false`) | 0.00 |
| DynamoDB, S3, SQS, ECR, flow logs | idle | < 1.00 |
| **Total** | | **≈ $46** (≈ $101 if the 1 GB Valkey minimum applies) |

## 3. Dev — with services (phase 2, estimate)

Adds to §2 (task sizes from SPEC §12.4; Spot assumed ~70 % below on-demand).

| Item | Quantity | $/month |
| --- | --- | --- |
| Fargate `api` | 1 × 0.25 vCPU / 0.5 GB on-demand ($0.009875/hour) | 7.21 |
| Fargate `redirect` | 1 × 0.25 vCPU / 0.5 GB Spot | ≈ 2.20 |
| Fargate `worker` | 0–1 × Spot (scales to zero when the queue is empty) | ≈ 0–2.20 |
| Fargate `nightly` | 0.5 vCPU / 1 GB Spot, a few minutes per day | < 0.10 |
| Internal ALB | 1 + minimal LCUs | ≈ 17–20 |
| WAF | 1 web ACL + 4 managed rule groups + 2 rate rules | ≈ 11 |
| CloudFront | within the free tier | 0.00 |
| CloudWatch | dashboard, ~12 alarms, logs | ≈ 5 |
| **Phase 2 total (dev)** | | **≈ $88–95** |

## 4. Prod — idle baseline (estimate)

| Item | Quantity | $/month |
| --- | --- | --- |
| Interface endpoints | 5 × 3 AZs | 109.50 |
| NAT gateway + EIP | 1 (scanning enabled) | 36.50 |
| Fargate | redirect 2 + api 2 on-demand, worker 1 Spot | ≈ 31 |
| ALB, WAF, CloudWatch | as dev, larger alarm set | ≈ 35 |
| Valkey, KMS, Secrets, data | | ≈ 9 |
| **Total** | | **≈ $220** before traffic |

## 5. Cost drivers and levers

| Driver | Lever |
| --- | --- |
| Interface endpoints (fixed, per AZ) | `interface_endpoint_az_count = 1` in dev; `ssmmessages` only when break-glass ECS Exec is on |
| NAT gateway | only when `url_scanning_enabled`; future: IPv6 egress-only gateway |
| ALB + WAF fixed fees | unavoidable with the CloudFront VPC-origin design (ADR-0012) |
| Fargate | ARM64 (−20 % vs x86), Spot for worker/nightly (and dev redirect), worker scales to zero |
| Valkey | Serverless Valkey 100 MB minimum (vs 1 GB for Redis OSS); storage/ECPU caps |
| DynamoDB | on-demand with maximum-throughput caps per table |
| Athena | per-query scan cap (1 GB) and partition projection |
| Logs | 14-day retention in dev, 90 days in prod; S3 lifecycle expiry |

## 6. Guardrails

- AWS Budgets (50/80/100 % actual, 100 % forecast) and Cost Anomaly Detection per
  environment (M6, `infra/modules/budget`).
- `make teardown` removes everything, including the state bucket (M9); KMS keys
  remain "pending deletion" for 7 days and are not billed in that state.
