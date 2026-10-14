# Log bucket module

Per-environment log archive for S3 server access logs (`s3/`), ALB access logs (`alb/`) and CloudFront standard logs (`cloudfront/`). SSE-S3 because those delivery services do not support SSE-KMS targets; every delivery grant is limited to this account.

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

## Modules

| Name | Source | Version |
| ---- | ------ | ------- |
| bucket | ../s3-bucket | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.delivery](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Delete all objects and versions on destroy. | `bool` | n/a | yes |
| name | Globally unique name of the log bucket. | `string` | n/a | yes |
| retention\_days | Days to keep log objects. | `number` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| arn | Log bucket ARN. |
| id | Log bucket name. |
<!-- END_TF_DOCS -->
