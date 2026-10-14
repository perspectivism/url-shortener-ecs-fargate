# Cache module

ElastiCache Serverless for Valkey (ADR-0008, ADR-0009) with IAM authentication: an IAM app user (`user_id == user_name`, `-@dangerous`), a disabled mandatory `default` user, CMK encryption, storage/ECPU caps, no snapshots, and ingress only from a client security group.

Tests: `tests/*.tftest.hcl` (`terraform test`, mock provider).

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| terraform | >= 1.16.5, < 2.0.0 |
| aws | >= 6.68.0, < 7.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| aws | >= 6.68.0, < 7.0.0 |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_elasticache_serverless_cache.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_serverless_cache) | resource |
| [aws_elasticache_user.app](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_user) | resource |
| [aws_elasticache_user.default](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_user) | resource |
| [aws_elasticache_user_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/elasticache_user_group) | resource |
| [aws_security_group.cache](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_security_group.clients](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_vpc_security_group_egress_rule.clients_to_cache](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_ingress_rule.cache_from_clients](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| kms\_key\_arn | Environment CMK for data at rest. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev" (also the cache name). | `string` | n/a | yes |
| subnet\_ids | Isolated subnets for the serverless cache endpoint. | `list(string)` | n/a | yes |
| vpc\_id | VPC for the cache security groups. | `string` | n/a | yes |
| app\_access\_string | ACL for the application user: all keys and commands except the @dangerous category (FLUSHALL, KEYS, CONFIG, DEBUG, ...). | `string` | `"on ~* &* +@all -@dangerous"` | no |
| major\_engine\_version | Valkey major version (ElastiCache supports 7.2-9.1 as of 2026-10-14). | `string` | `"9"` | no |
| max\_data\_storage\_gb | Upper bound on cached data (GB) — cost cap. | `number` | `1` | no |
| max\_ecpu\_per\_second | Upper bound on ElastiCache Processing Units per second — cost cap. | `number` | `5000` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| app\_user\_arn | IAM-authenticated user ARN (elasticache:Connect resource). |
| app\_user\_id | IAM-authenticated user ID (CACHE\_USER). |
| cache\_arn | Serverless cache ARN (elasticache:Connect resource). |
| cache\_name | Serverless cache name (CACHE\_NAME; used in the IAM auth token). |
| client\_security\_group\_id | Attach to tasks that use Valkey. |
| endpoint | Primary endpoint host:port for CACHE\_ADDR. |
<!-- END_TF_DOCS -->
