# ECS service module

Reusable Fargate service (SPEC §12.4, ADR-0003): hardened task definition (ARM64, read-only root FS, UID 65532, all capabilities dropped, non-blocking awslogs, binary health check), dedicated task and execution roles (execution role limited to its own repository, log group and secrets), its own security group (ingress from the ALB only), rolling deploys with the circuit breaker and rollback (ADR-0017), and target-tracking or queue-backlog autoscaling.

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
| terraform | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_appautoscaling_policy.alb_requests](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy) | resource |
| [aws_appautoscaling_policy.cpu](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy) | resource |
| [aws_appautoscaling_policy.queue_in](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy) | resource |
| [aws_appautoscaling_policy.queue_out](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_policy) | resource |
| [aws_appautoscaling_target.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appautoscaling_target) | resource |
| [aws_cloudwatch_log_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_cloudwatch_metric_alarm.queue_in](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm) | resource |
| [aws_cloudwatch_metric_alarm.queue_out](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm) | resource |
| [aws_ecs_service.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_service) | resource |
| [aws_ecs_task_definition.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_task_definition) | resource |
| [aws_iam_role.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role_policy.exec](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_security_group.task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_vpc_security_group_egress_rule.gateway_endpoints](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_egress_rule.internet_https](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_ingress_rule.from_alb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule) | resource |
| [terraform_data.routing_ready](https://registry.terraform.io/providers/hashicorp/terraform/latest/docs/resources/data) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.ecs_tasks_trust](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.exec](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.execution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| cluster\_arn | ECS cluster ARN. | `string` | n/a | yes |
| cluster\_name | ECS cluster name (autoscaling resource ID). | `string` | n/a | yes |
| ecr\_repository\_arn | Repository the execution role may pull from. | `string` | n/a | yes |
| egress | Egress allowances beyond the attached client security groups. | ```object({ s3_prefix_list_id = string dynamodb_prefix_list_id = string internet_https = bool # only for tasks in NAT-routed subnets (URL scanning) })``` | n/a | yes |
| image | Full image reference: REPOSITORY\_URL:GIT\_SHA (never latest). | `string` | n/a | yes |
| kms\_key\_arn | Environment CMK (log group encryption, secret decryption). | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| service | Service name (redirect, api, worker). | `string` | n/a | yes |
| subnet\_ids | Subnets for the tasks (no public IPs). | `list(string)` | n/a | yes |
| task\_policy\_json | IAM policy (JSON) for the task role: exactly what the application needs. | `string` | n/a | yes |
| vpc\_id | VPC ID. | `string` | n/a | yes |
| alb\_requests\_per\_target | Target-tracking ALBRequestCountPerTarget (null disables; needs load\_balancer). | `number` | `null` | no |
| capacity\_provider\_strategy | Fargate capacity provider strategy. | ```list(object({ capacity_provider = string base = number weight = number }))``` | ```[ { "base": 1, "capacity_provider": "FARGATE", "weight": 1 } ]``` | no |
| container\_port | Port the container listens on (HTTP\_ADDR). | `number` | `8080` | no |
| cpu | Task CPU units. | `number` | `256` | no |
| cpu\_target\_percent | Target-tracking CPU utilisation (null disables). | `number` | `60` | no |
| desired\_count | Initial desired count (autoscaling owns it afterwards). | `number` | `1` | no |
| enable\_execute\_command | Break-glass ECS Exec. AWS does not support it with a read-only root filesystem, so enabling it also relaxes that (never in prod; ADR-0003). | `bool` | `false` | no |
| environment | Plain (non-secret) environment variables. | `map(string)` | `{}` | no |
| extra\_security\_group\_ids | Additional security groups (VPC endpoint clients, cache clients). | `list(string)` | `[]` | no |
| health\_check\_grace\_period\_seconds | Grace period before ALB health checks count (services with a load balancer). | `number` | `30` | no |
| load\_balancer | ALB integration (null for services without a load balancer, e.g. worker). | ```object({ target_group_arn = string alb_security_group_id = string alb_resource_label = string # "<alb arn_suffix>/<target group arn_suffix>" for ALBRequestCountPerTarget routing_ready = string # listener or rule ARN attaching the target group (creation ordering) })``` | `null` | no |
| log\_retention\_days | Application log retention. | `number` | `14` | no |
| max\_count | Autoscaling maximum. | `number` | `2` | no |
| memory | Task memory (MiB). | `number` | `512` | no |
| min\_count | Autoscaling minimum. | `number` | `1` | no |
| permissions\_boundary\_arn | Permissions boundary for the task and execution roles. | `string` | `null` | no |
| secrets | Secret environment variables: name => Secrets Manager secret ARN (injected via valueFrom). | `map(string)` | `{}` | no |
| sqs\_queue\_name | Scale on this queue's backlog (step scaling, supports scale to zero; null disables). | `string` | `null` | no |
| wait\_for\_steady\_state | Make terraform apply wait for the deployment (and fail when the circuit breaker rolls back). | `bool` | `true` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| execution\_role\_arn | Execution role ARN (the only principal allowed to read this service's secrets). |
| log\_group\_name | Application log group. |
| security\_group\_id | Task security group ID. |
| service\_name | ECS service name. |
| task\_definition\_arn | Current task definition revision ARN. |
| task\_role\_arn | Task role ARN. |
<!-- END_TF_DOCS -->