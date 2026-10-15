# Changelog

Releases are created automatically when a commit reaches prod
([ADR-0026](docs/adr/0026-release-versioning-with-tags.md)): each GitHub
Release lists its commits and the exact image digests deployed. This file
summarizes the notable changes per release.

## v0.1.0 — 2026-10-15

First complete build: milestones M0–M9 closed (locally; the AWS deployment
checks are pending, see [GATES.md](GATES.md)).

- **Services:** redirect, api, worker and nightly in Go; `shortenerctl` CLI.
- **Analytics:** exactly-once click counting with batched transactions
  ([ADR-0024](docs/adr/0024-batched-exactly-once-click-counting.md)) and
  13-month retention ([ADR-0025](docs/adr/0025-analytics-retention-13-months.md)).
- **Infrastructure:** Terraform for bootstrap, dev and prod (CloudFront + WAF →
  internal ALB → ECS Fargate ARM64, DynamoDB, Valkey Serverless, SQS, S3/Athena).
- **Operations:** 14 alarms, a dashboard, budgets and cost-anomaly alerts;
  a runbook section per alarm.
- **Delivery:** GitHub Actions with OIDC; build once and promote verified digests;
  release tagging; deploys only by hand, with approval per environment
  ([ADR-0027](docs/adr/0027-manual-approval-for-every-deploy.md)).
- **Docs:** README interview walkthrough, design doc, 27 ADRs, threat model, cost model.
- **License:** MIT.
