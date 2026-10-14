# 📚 Architecture Decision Records

Decisions use the [MADR](https://adr.github.io/madr/) format with an extra
**Interview Angle** section. Start new records from [`template.md`](template.md);
see [ADR-0001](0001-record-architecture-decisions.md) for the rules.

| # | Title | Status |
| --- | --- | --- |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-go-for-all-services.md) | Go for all services | Accepted |
| [0004](0004-dynamodb-for-the-link-store.md) | DynamoDB for the link store (vs Postgres/Aurora) | Accepted |
| [0005](0005-separate-links-and-analytics-tables.md) | Separate links and analytics tables | Accepted |
| [0006](0006-short-code-generation-range-allocation-and-sqids.md) | Short code generation: range allocation + Sqids | Accepted |
| [0007](0007-302-redirects-and-no-edge-caching.md) | 302 redirects and no edge caching of redirects | Accepted |
| [0008](0008-elasticache-serverless-valkey.md) | ElastiCache Serverless Valkey (vs DAX, vs no cache) | Accepted |
| [0009](0009-valkey-iam-authentication.md) | Valkey IAM authentication (no passwords) | Accepted |
| [0010](0010-async-click-pipeline.md) | Async click pipeline: SQS, best-effort enqueue, idempotent worker | Accepted |
| [0011](0011-analytics-hot-and-cold-paths.md) | Analytics hot/cold paths: DynamoDB counters + S3/Athena | Accepted |
| [0015](0015-api-key-authentication.md) | API key authentication (vs Cognito) | Accepted |
| [0018](0018-privacy-hmac-visitor-ids-referrer-host-only.md) | Privacy: HMAC visitor IDs, referrer host only | Accepted |
| [0020](0020-graviton-arm64-and-fargate-spot.md) | Graviton (ARM64) and Fargate Spot usage | Accepted |
| [0021](0021-url-safety-scanning-safe-browsing-v5.md) | URL safety scanning with Safe Browsing v5 `urls:search` | Accepted |
