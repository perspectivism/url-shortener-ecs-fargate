# Bootstrap

One-time, per-account root ([ADR-0016](../../docs/adr/0016-terraform-layout-environments-s3-native-locking.md), [ADR-0017](../../docs/adr/0017-cicd-github-oidc-rolling-deploys.md)): the Terraform state bucket (SSE-KMS, versioning, S3 native locking) and its access-log bucket, the state CMK, the GitHub OIDC provider, the CI permissions boundary, the pull-request plan role and one apply role per GitHub environment. Run through `make bootstrap` (approval required); the first apply uses a temporary local backend and then migrates its own state into the bucket it created.

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
| state\_bucket | ../modules/s3-bucket | n/a |
| state\_logs | ../modules/log-bucket | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_ce_cost_allocation_tag.cost_center](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ce_cost_allocation_tag) | resource |
| [aws_iam_openid_connect_provider.github](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_openid_connect_provider) | resource |
| [aws_iam_policy.boundary](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_policy) | resource |
| [aws_iam_role.apply](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role.deploy_plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role.plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role_policy.apply](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.deploy_plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy.plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_iam_role_policy_attachment.deploy_plan_read_only](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy_attachment) | resource |
| [aws_iam_role_policy_attachment.plan_read_only](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy_attachment) | resource |
| [aws_kms_alias.state](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_alias) | resource |
| [aws_kms_key.state](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_openid_connect_provider.github](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_openid_connect_provider) | data source |
| [aws_iam_policy.read_only](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy) | data source |
| [aws_iam_policy_document.apply](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.boundary](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.deploy_plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.plan](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.state_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.trust](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| owner | Owner tag value. | `string` | n/a | yes |
| repository | GitHub repository (owner/name) whose workflows may assume the CI roles. | `string` | n/a | yes |
| activate\_cost\_allocation\_tag | Activate the CostCenter cost allocation tag. Set to true once Billing has seen the tag (up to 24 hours after the first tagged resources exist). | `bool` | `false` | no |
| create\_oidc\_provider | Create the GitHub OIDC provider (false if the account already has one for token.actions.githubusercontent.com). | `bool` | `true` | no |
| environments | Environments that get an apply role (GitHub environment names). | `set(string)` | ```[ "dev", "prod" ]``` | no |
| force\_destroy | Allow destroying the state bucket with its object versions (set true only for full teardown). | `bool` | `false` | no |
| project | Project name: resource prefix and Project tag. | `string` | `"shortener"` | no |
| region | Region of the state bucket and keys. | `string` | `"us-east-1"` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| apply\_role\_arns | Apply role per GitHub environment (environment variable AWS\_APPLY\_ROLE\_ARN). |
| deploy\_plan\_role\_arns | Deploy plan role per environment, for the GitHub environments dev-plan and prod-plan (their variable AWS\_DEPLOY\_PLAN\_ROLE\_ARN). |
| permissions\_boundary\_arn | Boundary attached to every CI role and every role CI creates. |
| plan\_role\_arn | Role for pull-request plans (GitHub variable AWS\_PLAN\_ROLE\_ARN). |
| state\_bucket | Terraform state bucket (backend.hcl bucket). |
| state\_kms\_key\_arn | CMK encrypting Terraform state (backend.hcl kms\_key\_id). |
<!-- END_TF_DOCS -->
