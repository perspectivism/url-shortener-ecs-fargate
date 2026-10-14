# Scheduled task module

EventBridge Scheduler runs a hardened Fargate (Spot) task on a cron schedule (SPEC §8.3). The scheduler role may only run this task family on this cluster and pass only its two roles to ECS tasks.

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
| [aws_cloudwatch_log_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_ecs_task_definition.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_task_definition) | resource |
| [aws_iam_role.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role.scheduler](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role_policy.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.scheduler](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_scheduler_schedule.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/scheduler_schedule) | resource |
| [aws_security_group.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_vpc_security_group_egress_rule.gateway_endpoints](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_egress_rule.internet_https](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.ecs_tasks_trust](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.scheduler](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.scheduler_trust](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| cluster\_arn | ECS cluster ARN. | `string` | n/a | yes |
| ecr\_repository\_arn | Repository the execution role may pull from. | `string` | n/a | yes |
| egress | Egress allowances beyond the attached client security groups. | ```object({ s3_prefix_list_id = string dynamodb_prefix_list_id = string internet_https = bool })``` | n/a | yes |
| image | Full image reference: REPOSITORY\_URL:GIT\_SHA (never latest). | `string` | n/a | yes |
| kms\_key\_arn | Environment CMK (logs, secrets). | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| subnet\_ids | Subnets for the task. | `list(string)` | n/a | yes |
| task | Task name (e.g. nightly). | `string` | n/a | yes |
| task\_policy\_json | IAM policy (JSON) for the task role. | `string` | n/a | yes |
| vpc\_id | VPC ID. | `string` | n/a | yes |
| capacity\_provider | FARGATE\_SPOT (default; the job is idempotent and re-runnable) or FARGATE. | `string` | `"FARGATE_SPOT"` | no |
| cpu | Task CPU units. | `number` | `512` | no |
| environment | Plain environment variables. | `map(string)` | `{}` | no |
| extra\_security\_group\_ids | Additional security groups (endpoint clients, cache clients). | `list(string)` | `[]` | no |
| log\_retention\_days | Log retention. | `number` | `14` | no |
| memory | Task memory (MiB). | `number` | `1024` | no |
| permissions\_boundary\_arn | Permissions boundary for roles created here. | `string` | `null` | no |
| schedule\_expression | EventBridge Scheduler expression (UTC). | `string` | `"cron(15 3 * * ? *)"` | no |
| secrets | Secret environment variables: name => secret ARN. | `map(string)` | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| execution\_role\_arn | Execution role ARN (secret reader). |
| log\_group\_name | Task log group. |
| schedule\_name | EventBridge Scheduler schedule name. |
| security\_group\_id | Task security group ID (for manual run-task). |
| task\_definition\_arn | Task definition ARN (for manual runs: aws ecs run-task). |
| task\_family | Task definition family. |
| task\_role\_arn | Task role ARN. |
<!-- END_TF_DOCS -->