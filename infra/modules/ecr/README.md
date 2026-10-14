# ECR module

One repository per service with immutable tags, scan on push, CMK encryption, `force_delete` for teardown, and a lifecycle policy (untagged after 7 days, keep the latest 30).

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
| [aws_ecr_lifecycle_policy.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecr_lifecycle_policy) | resource |
| [aws_ecr_repository.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecr_repository) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Delete repositories that still contain images. | `bool` | n/a | yes |
| kms\_key\_arn | Environment CMK for image encryption. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| keep\_images | Number of most recent images kept per repository. | `number` | `30` | no |
| services | Services that get a repository. | `set(string)` | ```[ "redirect", "api", "worker", "nightly" ]``` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| repository\_arns | Repository ARN per service. |
| repository\_urls | Repository URL per service. |
<!-- END_TF_DOCS -->
