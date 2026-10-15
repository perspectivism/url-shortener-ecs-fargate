# Secrets module

Secret containers only ([ADR-0014](../../../docs/adr/0014-secrets-management.md)) — never values. CMK encryption, recovery window 0 (dev) / 30 days (prod), and a resource policy that denies `GetSecretValue` to everyone except the listed reader roles.

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
| [aws_secretsmanager_secret.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret) | resource |
| [aws_secretsmanager_secret_policy.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret_policy) | resource |
| [aws_iam_policy_document.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Delete immediately on destroy (recovery window 0) instead of after 30 days. | `bool` | n/a | yes |
| kms\_key\_arn | Environment CMK for the secrets. | `string` | n/a | yes |
| name\_path | Secret name path prefix, e.g. "shortener/dev" (secrets are named PATH/NAME). | `string` | n/a | yes |
| readers | IAM role ARNs allowed to read each secret (task execution roles, added in M5). Empty = nobody can read. | ```object({ safe_browsing_api_key = list(string) visitor_hash_salt = list(string) })``` | ```{ "safe_browsing_api_key": [], "visitor_hash_salt": [] }``` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| names | Secret names (for make seed-secrets). |
| safe\_browsing\_api\_key\_arn | ARN of the Safe Browsing API key secret. |
| visitor\_hash\_salt\_arn | ARN of the visitor hash salt secret. |
<!-- END_TF_DOCS -->
