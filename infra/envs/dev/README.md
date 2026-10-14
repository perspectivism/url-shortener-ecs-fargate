# Environment: dev

Environment root wiring the modules. `main.tf`, `outputs.tf` and `versions.tf` are identical in `dev` and `prod` (checked by `scripts/check-env-parity.sh`); only variable defaults differ. Copy `example.tfvars` to `terraform.tfvars` and `backend.hcl.example` to `backend.hcl` (both gitignored; `make bootstrap` generates `backend.hcl`).

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
| cache | ../../modules/cache | n/a |
| dynamodb | ../../modules/dynamodb | n/a |
| ecr | ../../modules/ecr | n/a |
| events\_lake | ../../modules/events-lake | n/a |
| kms | ../../modules/kms | n/a |
| logs | ../../modules/log-bucket | n/a |
| messaging | ../../modules/messaging | n/a |
| network | ../../modules/network | n/a |
| secrets | ../../modules/secrets | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| owner | Owner tag value (team or person responsible). | `string` | n/a | yes |
| repository | GitHub repository (owner/name), recorded in the Repository tag. | `string` | n/a | yes |
| az\_count | Availability Zones (2 dev / 3 prod). | `number` | `2` | no |
| cache\_max\_data\_storage\_gb | Valkey Serverless data storage cap (GB). | `number` | `1` | no |
| cache\_max\_ecpu\_per\_second | Valkey Serverless ECPU/s cap. | `number` | `5000` | no |
| ecs\_exec\_enabled | ECS Exec for debugging (dev only); adds the ssmmessages endpoint. | `bool` | `true` | no |
| enable\_services | Phase 2: deploy ECS services and the edge (M5). Requires seeded secrets and pushed images. | `bool` | `false` | no |
| environment | Environment name. | `string` | `"dev"` | no |
| events\_retention\_days | Raw click-event retention in S3 (null keeps forever). | `number` | `30` | no |
| force\_destroy | Allow one-command teardown: empty buckets/repos on destroy, no deletion protection, no secret recovery window (true dev / false prod). | `bool` | `true` | no |
| interface\_endpoint\_az\_count | AZs with interface endpoint ENIs (1 dev / all prod). | `number` | `1` | no |
| log\_retention\_days | CloudWatch Logs and log-bucket retention (14 dev / 90 prod). | `number` | `14` | no |
| project | Project name: resource prefix and Project tag. | `string` | `"shortener"` | no |
| region | AWS Region for regional resources. | `string` | `"us-east-1"` | no |
| url\_scanning\_enabled | Scan destinations with Google Safe Browsing; creates the NAT gateway (false in dev, true in prod). | `bool` | `false` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| analytics\_table | Analytics table name (ANALYTICS\_TABLE). |
| athena\_workgroup | Athena workgroup for click analytics. |
| cache\_endpoint | Valkey endpoint host:port (CACHE\_ADDR). |
| cache\_name | Valkey Serverless cache name (CACHE\_NAME). |
| cache\_user | IAM-authenticated cache user (CACHE\_USER). |
| click\_dlq\_url | Click-event DLQ URL. |
| click\_queue\_url | Click-event queue URL (CLICK\_QUEUE\_URL). |
| ecr\_repository\_urls | ECR repository URL per service. |
| events\_bucket | Raw click-event bucket (EVENTS\_BUCKET). |
| kms\_key\_arn | Environment CMK. |
| links\_table | Links table name (LINKS\_TABLE). |
| name\_prefix | Resource name prefix of this environment. |
| secret\_names | Secret containers to seed with make seed-secrets. |
| services\_enabled | Whether phase 2 (ECS services and edge) is enabled. |
| vpc\_id | VPC ID. |
<!-- END_TF_DOCS -->
