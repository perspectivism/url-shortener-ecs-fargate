# DynamoDB module

The `links` and `analytics` tables (ADR-0004, ADR-0005): on-demand with maximum-throughput caps, PITR, CMK encryption, TTL on `ttl`, deletion protection driven by `!force_destroy`. Must match `internal/store/schema.go`.

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
| [aws_dynamodb_table.analytics](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table) | resource |
| [aws_dynamodb_table.links](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| deletion\_protection | Enable DynamoDB deletion protection (true in prod; derived from !force\_destroy). | `bool` | n/a | yes |
| kms\_key\_arn | Environment CMK for server-side encryption. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| analytics\_max\_read\_units | On-demand maximum read request units for the analytics table (cost cap). | `number` | `500` | no |
| analytics\_max\_write\_units | On-demand maximum write request units for the analytics table (cost cap). | `number` | `1000` | no |
| links\_max\_read\_units | On-demand maximum read request units for the links table (cost cap). | `number` | `1000` | no |
| links\_max\_write\_units | On-demand maximum write request units for the links table (cost cap). | `number` | `200` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| analytics\_table\_arn | Analytics table ARN. |
| analytics\_table\_name | Analytics table name. |
| links\_table\_arn | Links table ARN. |
| links\_table\_name | Links table name. |
<!-- END_TF_DOCS -->
