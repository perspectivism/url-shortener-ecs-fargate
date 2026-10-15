# 💸 URL Shortener — Cost Model

> **Status:** v1.0 (M9). Prices verified **2026-10-14** for **us-east-1**
> with the AWS Price List API (`aws pricing get-products`, read-only) and the
> public pricing pages linked below. All figures are **estimates** until dev
> has run for a few days. The measured month-to-date spend (Cost Explorer,
> `CostCenter = shortener-dev`) is recorded in §7 after the first deployment
> (pending cloud check, GATES.md).

## Contents

1. [Unit prices](#1-unit-prices)
2. [Dev — foundation (phase 1)](#2-dev--foundation-phase-1)
3. [Dev — with services (phase 2, estimate)](#3-dev--with-services-phase-2-estimate)
4. [Prod — idle baseline (estimate)](#4-prod--idle-baseline-estimate)
5. [Cost drivers and levers](#5-cost-drivers-and-levers)
6. [Guardrails](#6-guardrails)
7. [Measured (dev)](#7-measured-dev)

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
| CloudWatch custom metrics (EMF, Container Insights) | $0.30/metric-month (first 10,000), prorated hourly; 10 free per account | [pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch alarms (standard) | $0.10 per alarm metric-month; metric-math alarms are billed per metric in the expression; 10 free per account | [pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| CloudWatch dashboards | 3 free per account (up to 50 metrics each), then a fixed monthly fee per dashboard | [pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| Container Insights (standard, Fargate) | 13 metrics per cluster + 15 per service + 10 per task family, charged as custom metrics | [pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| SNS email, AWS Budgets, Cost Anomaly Detection | first 1,000 emails free; budgets and anomaly detection free at this scale | — |
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
| Container Insights | 1 cluster + 2–3 services + 2–4 task families (worker and nightly publish only while running) | ≈ 15–30 |
| CloudWatch EMF metrics | ~25 metric × service series, published only when non-empty | ≈ 5–8 |
| CloudWatch alarms | 14 alarms (27 alarm metrics, incl. metric math) + autoscaling alarms | ≈ 3 |
| CloudWatch Logs, dashboard | app, ALB/CloudFront/WAF logs at demo volume; 1 dashboard (free tier) | ≈ 2 |
| **Phase 2 total (dev)** | | **≈ $110–135** |

## 4. Prod — idle baseline (estimate)

| Item | Quantity | $/month |
| --- | --- | --- |
| Interface endpoints | 5 × 3 AZs | 109.50 |
| NAT gateway + EIP | 1 (scanning enabled) | 36.50 |
| Fargate | redirect 2 + api 2 on-demand, worker 1 Spot | ≈ 31 |
| ALB, WAF | as dev | ≈ 30 |
| CloudWatch | Container Insights (more tasks), EMF metrics, alarms, logs (90-day retention) | ≈ 40–55 |
| Valkey, KMS, Secrets, data | | ≈ 9 |
| **Total** | | **≈ $255–270** before traffic |

## 5. Cost drivers and levers

| Driver | Lever |
| --- | --- |
| Interface endpoints (fixed, per AZ) | `interface_endpoint_az_count = 1` in dev; `ssmmessages` only when break-glass ECS Exec is on |
| NAT gateway | only when `url_scanning_enabled`; future: IPv6 egress-only gateway |
| ALB + WAF fixed fees | unavoidable with the CloudFront VPC-origin design (ADR-0012) |
| Fargate | ARM64 (−20 % vs x86), Spot for worker/nightly (and dev redirect), worker scales to zero |
| Valkey | Serverless Valkey 100 MB minimum (vs 1 GB for Redis OSS); storage/ECPU caps |
| DynamoDB | on-demand with maximum-throughput caps per table; click counters written in batches per link (≈ 2.2 instead of 12 write units per click for hot links, [ADR-0024](adr/0024-batched-exactly-once-click-counting.md)) |
| Athena | per-query scan cap (1 GB) and partition projection |
| Logs | 14-day retention in dev, 90 days in prod; S3 lifecycle expiry |
| Analytics storage | counters expire 13 months after their day (DynamoDB TTL deletes are free); raw events 30 days dev, 13 months prod ([ADR-0025](adr/0025-analytics-retention-13-months.md)) |
| Container Insights | `container_insights` on the cluster module: the running-tasks alarms need it (`DesiredTaskCount`/`RunningTaskCount`); turning it off saves ≈ $15–30/month per environment but loses those alarms |
| EMF metrics | metrics are emitted only when non-empty, so idle series are not billed; keep dimensions to `Service` (no per-link dimensions) |

## 6. Guardrails

- AWS Budgets per environment (`monthly_budget_usd`: dev $150, prod $400) with
  notifications at 50/80/100 % actual and 100 % forecast, plus a Cost Anomaly
  Detection monitor (alerts at ≥ $10 dev / $25 prod impact) — `infra/modules/budget`,
  [ADR-0022](adr/0022-observability-alerting-and-cost-guardrails.md).
- Both filter on the `CostCenter = shortener-{env}` cost allocation tag. Activate
  it once it appears in Billing (`activate_cost_allocation_tag = true` in
  `infra/bootstrap`); until then they report $0.
- The dashboard's "Cost" widget shows account-wide `EstimatedCharges` (requires
  "Receive Billing Alerts" in Billing preferences).
- `make teardown` removes everything, including the state bucket, from saved and
  confirmed plans; `make verify-teardown` checks 34 resource categories for
  leftovers. KMS keys remain "pending deletion" for 7 days and are not billed
  in that state ([runbook §5.15](runbook.md#515-full-teardown)).

## 7. Measured (dev)

Pending the first deployment (GATES.md, pending cloud checks). After dev has
run for at least 3 full days, record here the month-to-date spend by service
from Cost Explorer (filter `CostCenter = shortener-dev`, group by service),
and the k6 baseline cost (Valkey ECPU, ALB LCU, CloudFront requests).
