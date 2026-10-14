# S3 bucket module

Hardened bucket used by every bucket in the project: Block Public Access, BucketOwnerEnforced, versioning, TLS-only policy, SSE-KMS with a bucket key (or SSE-S3 for log buckets), lifecycle cleanup and optional server access logging.

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
| [aws_s3_bucket.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket) | resource |
| [aws_s3_bucket_lifecycle_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_lifecycle_configuration) | resource |
| [aws_s3_bucket_logging.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_logging) | resource |
| [aws_s3_bucket_ownership_controls.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_ownership_controls) | resource |
| [aws_s3_bucket_policy.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_policy) | resource |
| [aws_s3_bucket_public_access_block.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_public_access_block) | resource |
| [aws_s3_bucket_server_side_encryption_configuration.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_server_side_encryption_configuration) | resource |
| [aws_s3_bucket_versioning.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_versioning) | resource |
| [aws_iam_policy_document.policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Delete all objects and versions on destroy (true in dev for one-command teardown, false in prod). | `bool` | n/a | yes |
| name | Globally unique bucket name. | `string` | n/a | yes |
| access\_log\_bucket | Bucket that receives S3 server access logs for this bucket (null disables, used only by the log bucket itself). | `string` | `null` | no |
| expiration\_days | Expire current objects after this many days (null keeps them). | `number` | `null` | no |
| extra\_policy\_json | Additional bucket policy statements (aws\_iam\_policy\_document JSON) merged with the TLS-only policy. | `string` | `null` | no |
| kms\_key\_arn | CMK for SSE-KMS. Null selects SSE-S3, which only log buckets use (ALB and S3 server access log delivery do not support SSE-KMS). | `string` | `null` | no |
| noncurrent\_version\_expiration\_days | Delete noncurrent object versions after this many days. | `number` | `30` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| arn | Bucket ARN. |
| id | Bucket name. |
<!-- END_TF_DOCS -->
