# ECS cluster module

ECS cluster with Fargate and Fargate Spot capacity providers and Container Insights (ADR-0003, ADR-0020). Break-glass ECS Exec sessions are KMS-encrypted and audited to a CMK-encrypted log group.

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
| [aws_cloudwatch_log_group.exec](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_ecs_cluster.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_cluster) | resource |
| [aws_ecs_cluster_capacity_providers.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_cluster_capacity_providers) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| kms\_key\_arn | CMK for ECS Exec session encryption and the exec log group. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| container\_insights | Container Insights mode: "enhanced", "enabled" or "disabled". | `string` | `"enabled"` | no |
| log\_retention\_days | Retention of the ECS Exec audit log group. | `number` | `14` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| cluster\_arn | ECS cluster ARN. |
| cluster\_name | ECS cluster name. |
<!-- END_TF_DOCS -->