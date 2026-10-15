# Events lake module

Cold analytics path ([ADR-0011](../../../docs/adr/0011-analytics-hot-and-cold-paths.md)): raw-event and Athena-results buckets, Glue table with partition projection over `events/dt=/hour=/`, Athena workgroup with enforced encrypted results and a per-query scan cap, and three saved queries that deduplicate by `event_id`.

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
| athena\_results | ../s3-bucket | n/a |
| events\_bucket | ../s3-bucket | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_athena_named_query.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/athena_named_query) | resource |
| [aws_athena_workgroup.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/athena_workgroup) | resource |
| [aws_glue_catalog_database.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_catalog_database) | resource |
| [aws_glue_catalog_table.click_events](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/glue_catalog_table) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| force\_destroy | Allow destroying non-empty buckets and the Athena workgroup. | `bool` | n/a | yes |
| kms\_key\_arn | Environment CMK (S3 SSE-KMS and Athena results). | `string` | n/a | yes |
| log\_bucket\_id | Bucket receiving S3 server access logs. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| bytes\_scanned\_cutoff\_per\_query | Athena per-query scan limit in bytes (cost guard; minimum 10 MB). | `number` | `1073741824` | no |
| events\_retention\_days | Days to keep raw click events (null keeps them forever). | `number` | `null` | no |
| partition\_start\_date | First date (yyyy-MM-dd) covered by partition projection. | `string` | `"2026-01-01"` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| athena\_results\_bucket\_id | Athena query results bucket name. |
| athena\_workgroup | Athena workgroup name. |
| events\_bucket\_arn | Raw click-event bucket ARN. |
| events\_bucket\_id | Raw click-event bucket name. |
| glue\_database | Glue database holding the click\_events table. |
<!-- END_TF_DOCS -->
