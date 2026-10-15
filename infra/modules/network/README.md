# Network module

VPC with three subnet tiers ([ADR-0013](../../../docs/adr/0013-subnet-tiers-vpc-endpoints-nat-only-for-scanning.md)): public (NAT only), private egress (api, nightly; NAT route only when URL scanning is enabled) and private isolated (ALB, redirect, worker, Valkey, endpoints; no internet route). S3/DynamoDB gateway endpoints with account-scoped policies, interface endpoints behind a client security group, VPC flow logs to a CMK-encrypted log group. AZs are selected by AZ ID, excluding `use1-az3` (no CloudFront VPC origin support).

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
| [aws_cloudwatch_log_group.flow_logs](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group) | resource |
| [aws_default_security_group.default](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/default_security_group) | resource |
| [aws_eip.nat](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip) | resource |
| [aws_flow_log.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/flow_log) | resource |
| [aws_iam_role.flow_logs](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role_policy.flow_logs](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy) | resource |
| [aws_internet_gateway.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/internet_gateway) | resource |
| [aws_nat_gateway.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway) | resource |
| [aws_route.egress_nat](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route) | resource |
| [aws_route.public_internet](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route) | resource |
| [aws_route_table.egress](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) | resource |
| [aws_route_table.isolated](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) | resource |
| [aws_route_table.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) | resource |
| [aws_route_table_association.egress](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_route_table_association.isolated](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_route_table_association.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_security_group.endpoint_clients](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_security_group.endpoints](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_subnet.egress](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_subnet.isolated](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_subnet.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_vpc.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc) | resource |
| [aws_vpc_endpoint.dynamodb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) | resource |
| [aws_vpc_endpoint.interface](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) | resource |
| [aws_vpc_endpoint.s3](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) | resource |
| [aws_vpc_security_group_egress_rule.clients_to_endpoints](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_egress_rule) | resource |
| [aws_vpc_security_group_ingress_rule.endpoints_from_clients](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_security_group_ingress_rule) | resource |
| [aws_availability_zones.available](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/availability_zones) | data source |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.dynamodb_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.flow_logs](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.flow_logs_trust](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.interface_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_iam_policy_document.s3_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| dynamodb\_table\_name\_prefix | DynamoDB tables reachable through the gateway endpoint (name prefix). | `string` | n/a | yes |
| kms\_key\_arn | CMK for the flow-log log group. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| s3\_bucket\_name\_prefix | S3 buckets reachable through the gateway endpoint (name prefix, e.g. "shortener-dev-"). | `string` | n/a | yes |
| az\_count | Number of Availability Zones (2 in dev, 3 in prod). | `number` | `2` | no |
| cidr\_block | VPC IPv4 CIDR (a /16 is split into /24 public and /20 private subnets). | `string` | `"10.20.0.0/16"` | no |
| ecs\_exec\_enabled | Add the ssmmessages interface endpoint required by ECS Exec. | `bool` | `false` | no |
| excluded\_az\_ids | AZ IDs to avoid. use1-az3 does not support CloudFront VPC origins (verified 2026-10-14). | `list(string)` | ```[ "use1-az3" ]``` | no |
| flow\_log\_retention\_days | Retention of the VPC flow-log log group. | `number` | `14` | no |
| interface\_endpoint\_az\_count | Number of AZs that get interface endpoint ENIs (1 in dev to save cost, all AZs in prod). | `number` | `1` | no |
| nat\_enabled | Create a NAT gateway for the egress tier (only needed when URL scanning calls Google Safe Browsing). | `bool` | `false` | no |
| permissions\_boundary\_arn | Permissions boundary for IAM roles this module creates (the CI boundary from infra/bootstrap; null for none). | `string` | `null` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| az\_ids | AZ IDs in use, in subnet order. |
| dynamodb\_prefix\_list\_id | Prefix list of the DynamoDB gateway endpoint (for task security group egress rules). |
| egress\_subnet\_ids | Private subnets with NAT egress (api, nightly). |
| endpoint\_client\_security\_group\_id | Attach to tasks that call AWS APIs through interface endpoints. |
| flow\_log\_group\_name | VPC flow-log log group. |
| isolated\_subnet\_ids | Private subnets without an internet route (ALB, redirect, worker, Valkey, endpoints). |
| nat\_enabled | Whether the egress tier has a NAT gateway. |
| public\_subnet\_ids | Public subnet IDs (NAT only). |
| s3\_prefix\_list\_id | Prefix list of the S3 gateway endpoint (for task security group egress rules). |
| vpc\_cidr | VPC IPv4 CIDR. |
| vpc\_id | VPC ID. |
<!-- END_TF_DOCS -->
