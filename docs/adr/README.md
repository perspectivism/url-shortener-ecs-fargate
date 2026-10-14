# 📚 Architecture Decision Records

Decisions use the [MADR](https://adr.github.io/madr/) format with an extra
**Interview Angle** section. Start new records from [`template.md`](template.md);
see [ADR-0001](0001-record-architecture-decisions.md) for the rules.

| # | Title | Status |
| --- | --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-go-for-all-services.md) | Go for all services | Accepted |
| [0003](0003-ecs-on-fargate.md) | ECS on Fargate (vs Lambda, EKS, EC2) | Accepted |
| [0004](0004-dynamodb-for-the-link-store.md) | DynamoDB for the link store (vs Postgres/Aurora) | Accepted |
| [0005](0005-separate-links-and-analytics-tables.md) | Separate links and analytics tables | Accepted |
| [0006](0006-short-code-generation-range-allocation-and-sqids.md) | Short code generation: range allocation + Sqids | Accepted |
| [0007](0007-302-redirects-and-no-edge-caching.md) | 302 redirects and no edge caching of redirects | Accepted |
| [0008](0008-elasticache-serverless-valkey.md) | ElastiCache Serverless Valkey (vs DAX, vs no cache) | Accepted |
| [0009](0009-valkey-iam-authentication.md) | Valkey IAM authentication (no passwords) | Accepted |
| [0010](0010-async-click-pipeline.md) | Async click pipeline: SQS, best-effort enqueue, idempotent worker | Accepted |
| [0011](0011-analytics-hot-and-cold-paths.md) | Analytics hot/cold paths: DynamoDB counters + S3/Athena | Accepted |
| [0012](0012-cloudfront-vpc-origin-internal-alb-waf.md) | CloudFront VPC origin with internal ALB and WAF | Accepted |
| [0013](0013-subnet-tiers-vpc-endpoints-nat-only-for-scanning.md) | Subnet tiers, VPC endpoints, NAT only for URL scanning | Accepted |
| [0014](0014-secrets-management.md) | Secrets management: Secrets Manager, out-of-band values, nothing in state | Accepted |
| [0015](0015-api-key-authentication.md) | API key authentication (vs Cognito) | Accepted |
| [0016](0016-terraform-layout-environments-s3-native-locking.md) | Terraform layout, environments, S3 native state locking | Accepted |
| [0017](0017-cicd-github-oidc-rolling-deploys.md) | CI/CD with GitHub Actions OIDC; rolling deploys with circuit breaker | Proposed (CI part in M8) |
| [0018](0018-privacy-hmac-visitor-ids-referrer-host-only.md) | Privacy: HMAC visitor IDs, referrer host only | Accepted |
| [0019](0019-single-aws-account-for-dev-and-prod.md) | Single AWS account for dev/prod (multi-account as future) | Accepted |
| [0020](0020-graviton-arm64-and-fargate-spot.md) | Graviton (ARM64) and Fargate Spot usage | Accepted |
| [0021](0021-url-safety-scanning-safe-browsing-v5.md) | URL safety scanning with Safe Browsing v5 `urls:search` | Accepted |
