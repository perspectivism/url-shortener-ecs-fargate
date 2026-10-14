# KMS module

One customer-managed key per environment (rotation on, 7-day deletion window) for DynamoDB, SQS, S3, CloudWatch Logs, Secrets Manager, ECR and ElastiCache. Workloads get access through IAM; the key policy only adds statements for CloudWatch Logs (scoped to this environment's log groups) and CloudWatch alarms → SNS.

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
| [aws_kms_alias.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_alias) | resource |
| [aws_kms_key.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| log\_group\_name\_prefix | CloudWatch Logs log groups whose names start with this prefix may use the key (encryption context condition). | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| deletion\_window\_in\_days | Waiting period before a scheduled key deletion completes (AWS minimum is 7). | `number` | `7` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| alias\_name | Alias of the environment CMK. |
| key\_arn | ARN of the environment CMK. |
| key\_id | ID of the environment CMK. |
<!-- END_TF_DOCS -->
