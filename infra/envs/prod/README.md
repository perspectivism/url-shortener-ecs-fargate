# Environment: prod

Environment root wiring the modules. `main.tf`, `outputs.tf` and `versions.tf` are identical in `dev` and `prod` (checked by `scripts/check-env-parity.sh`); only variable defaults differ. Copy `example.tfvars` to `terraform.tfvars` and `backend.hcl.example` to `backend.hcl` (both gitignored; `make bootstrap` generates `backend.hcl`).

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| terraform | ~> 1.16.5 |
| aws | ~> 6.68 |

## Providers

| Name | Version |
| ---- | ------- |
| aws | ~> 6.68 |

## Modules

| Name | Source | Version |
| ---- | ------ | ------- |
| api | ../../modules/ecs-service | n/a |
| budget | ../../modules/budget | n/a |
| cache | ../../modules/cache | n/a |
| dynamodb | ../../modules/dynamodb | n/a |
| ecr | ../../modules/ecr | n/a |
| ecs\_cluster | ../../modules/ecs-cluster | n/a |
| edge | ../../modules/edge | n/a |
| events\_lake | ../../modules/events-lake | n/a |
| kms | ../../modules/kms | n/a |
| logs | ../../modules/log-bucket | n/a |
| messaging | ../../modules/messaging | n/a |
| network | ../../modules/network | n/a |
| nightly | ../../modules/scheduled-task | n/a |
| observability | ../../modules/observability | n/a |
| redirect | ../../modules/ecs-service | n/a |
| secrets | ../../modules/secrets | n/a |
| worker | ../../modules/ecs-service | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.api](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.cache_connect](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.nightly](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.redirect](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.worker](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| owner | Owner tag value (team or person responsible). | `string` | n/a | yes |
| repository | GitHub repository (owner/name), recorded in the Repository tag. | `string` | n/a | yes |
| alert\_email | Email subscribed to the alert topic (alarms, budgets, cost anomalies); the recipient must confirm. Null: no subscription. | `string` | `null` | no |
| anomaly\_threshold\_usd | Cost Anomaly Detection alert threshold (total impact, USD). | `number` | `25` | no |
| api\_max\_count | Maximum api tasks. | `number` | `4` | no |
| api\_min\_count | Minimum (and initial) api tasks. | `number` | `2` | no |
| az\_count | Availability Zones (2 dev / 3 prod). | `number` | `3` | no |
| cache\_max\_data\_storage\_gb | Valkey Serverless data storage cap (GB). | `number` | `5` | no |
| cache\_max\_ecpu\_per\_second | Valkey Serverless ECPU/s cap. | `number` | `50000` | no |
| domain\_name | Optional custom domain (enables ACM certificates, HTTPS to the origin and TLSv1.2\_2021). | `string` | `null` | no |
| ecs\_exec\_enabled | Break-glass ECS Exec (ADR-0003): off by default because AWS does not support it with read-only root filesystems; enabling relaxes that and adds the ssmmessages endpoint. Never in prod. | `bool` | `false` | no |
| enable\_services | Phase 2: deploy ECS services and the edge (M5). Requires seeded secrets and pushed images. | `bool` | `false` | no |
| environment | Environment name. | `string` | `"prod"` | no |
| events\_retention\_days | Raw click-event retention in S3 (null keeps forever). | `number` | `365` | no |
| force\_destroy | Allow one-command teardown: empty buckets/repos on destroy, no deletion protection, no secret recovery window (true dev / false prod). | `bool` | `false` | no |
| hosted\_zone\_id | Route 53 hosted zone for domain\_name. | `string` | `null` | no |
| image\_tag | Image tag (git SHA) for every service; required when enable\_services is true. Never "latest". | `string` | `null` | no |
| interface\_endpoint\_az\_count | AZs with interface endpoint ENIs (1 dev / all prod). | `number` | `3` | no |
| log\_retention\_days | CloudWatch Logs and log-bucket retention (14 dev / 90 prod). | `number` | `90` | no |
| monthly\_budget\_usd | Monthly AWS Budget for this environment in USD (docs/cost.md). | `number` | `400` | no |
| project | Project name: resource prefix and Project tag. | `string` | `"shortener"` | no |
| redirect\_max\_count | Maximum redirect tasks. | `number` | `10` | no |
| redirect\_min\_count | Minimum (and initial) redirect tasks. | `number` | `2` | no |
| redirect\_on\_spot | Run redirect on Fargate Spot (dev only; prod uses on-demand, ADR-0020). | `bool` | `false` | no |
| region | AWS Region for regional resources. | `string` | `"us-east-1"` | no |
| sqids\_alphabet | Non-secret shuffled Sqids alphabet for short codes (changing it changes future codes). | `string` | `"O7mH0bSxUPJnNZefjW25TzIALs3iX1kcRdMa8QD9hgKY4rEqlVyv6wGFoutBpC"` | no |
| url\_scanning\_enabled | Scan destinations with Google Safe Browsing; creates the NAT gateway (false in dev, true in prod). | `bool` | `true` | no |
| waf\_anonymous\_ip\_action | AnonymousIpList action: count (initial) or block (after review). | `string` | `"count"` | no |
| waf\_api\_rate\_limit\_per\_minute | WAF per-IP rate limit for /api/* (requests per minute). | `number` | `300` | no |
| waf\_redirect\_rate\_limit\_per\_minute | WAF per-IP rate limit for redirects (requests per minute). | `number` | `1200` | no |
| worker\_max\_count | Maximum worker tasks. | `number` | `5` | no |
| worker\_min\_count | Minimum worker tasks (0 lets the worker scale to zero on an empty queue). | `number` | `1` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| alarm\_names | CloudWatch alarm names keyed by alarm. |
| alb\_dns\_name | Internal ALB DNS name (phase 2) — must NOT be reachable from the internet. |
| alert\_topic\_arn | SNS topic for alarms, budgets and cost anomalies (aws cloudwatch set-alarm-state test target). |
| analytics\_table | Analytics table name (ANALYTICS\_TABLE). |
| athena\_workgroup | Athena workgroup for click analytics. |
| base\_url | Public base URL (phase 2): smoke tests and short links. |
| cache\_endpoint | Valkey endpoint host:port (CACHE\_ADDR). |
| cache\_name | Valkey Serverless cache name (CACHE\_NAME). |
| cache\_user | IAM-authenticated cache user (CACHE\_USER). |
| click\_dlq\_url | Click-event DLQ URL. |
| click\_queue\_url | Click-event queue URL (CLICK\_QUEUE\_URL). |
| cloudfront\_distribution\_id | CloudFront distribution ID (phase 2). |
| dashboard\_name | CloudWatch dashboard (phase 2). |
| ecr\_repository\_urls | ECR repository URL per service. |
| ecs\_cluster\_name | ECS cluster name (phase 2). |
| events\_bucket | Raw click-event bucket (EVENTS\_BUCKET). |
| kms\_key\_arn | Environment CMK. |
| links\_table | Links table name (LINKS\_TABLE). |
| name\_prefix | Resource name prefix of this environment. |
| nightly\_run\_task | Parameters for a manual nightly run (aws ecs run-task), phase 2. |
| secret\_names | Secret containers to seed with make seed-secrets. |
| services\_enabled | Whether phase 2 (ECS services and edge) is enabled. |
| url\_scanning\_enabled | Whether destinations are scanned (smoke tests then expect 422 for the Safe Browsing test URL). |
| vpc\_id | VPC ID. |
<!-- END_TF_DOCS -->
