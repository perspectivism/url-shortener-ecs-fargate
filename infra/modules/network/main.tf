# VPC with three subnet tiers (ADR-0013):
#   public   - NAT gateway only (when enabled); the IGW is also required by
#              CloudFront VPC origins, which do not route through it.
#   egress   - api / nightly tasks; default route to NAT only when enabled.
#   isolated - ALB, redirect, worker, Valkey, interface endpoints; no internet route.

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

data "aws_availability_zones" "available" {
  #checkov:skip=CKV_AWS_394:Subnets are keyed by AZ ID and use the first az_count AZs, so a newly added AZ never moves existing subnets.
  state            = "available"
  exclude_zone_ids = var.excluded_az_ids

  filter {
    name   = "opt-in-status"
    values = ["opt-in-not-required"]
  }
}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region

  # AZs keyed by AZ ID (stable across accounts), in name order.
  azs = {
    for i in range(var.az_count) : data.aws_availability_zones.available.zone_ids[i] => {
      index = i
      name  = data.aws_availability_zones.available.names[i]
    }
  }
  az_ids          = [for i in range(var.az_count) : data.aws_availability_zones.available.zone_ids[i]]
  endpoint_az_ids = slice(local.az_ids, 0, min(var.interface_endpoint_az_count, var.az_count))
  nat_az_id       = local.az_ids[0]

  interface_endpoints = toset(concat(
    ["ecr.api", "ecr.dkr", "logs", "sqs", "secretsmanager"],
    var.ecs_exec_enabled ? ["ssmmessages"] : [],
  ))
}

resource "aws_vpc" "this" {
  cidr_block           = var.cidr_block
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = var.name_prefix }
}

# No rules: nothing may use the default security group.
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.this.id
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.name_prefix }
}

resource "aws_subnet" "public" {
  for_each = local.azs

  vpc_id                  = aws_vpc.this.id
  availability_zone_id    = each.key
  cidr_block              = cidrsubnet(var.cidr_block, 8, each.value.index)
  map_public_ip_on_launch = false

  tags = { Name = "${var.name_prefix}-public-${each.value.name}", Tier = "public" }
}

resource "aws_subnet" "egress" {
  for_each = local.azs

  vpc_id               = aws_vpc.this.id
  availability_zone_id = each.key
  cidr_block           = cidrsubnet(var.cidr_block, 4, 1 + each.value.index)

  tags = { Name = "${var.name_prefix}-egress-${each.value.name}", Tier = "egress" }
}

resource "aws_subnet" "isolated" {
  for_each = local.azs

  vpc_id               = aws_vpc.this.id
  availability_zone_id = each.key
  cidr_block           = cidrsubnet(var.cidr_block, 4, 8 + each.value.index)

  tags = { Name = "${var.name_prefix}-isolated-${each.value.name}", Tier = "isolated" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-public" }
}

resource "aws_route" "public_internet" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.this.id
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  subnet_id      = each.value.id
  route_table_id = aws_route_table.public.id
}

resource "aws_eip" "nat" {
  #checkov:skip=CKV2_AWS_19:The EIP is attached to the NAT gateway, not to an EC2 instance.
  count = var.nat_enabled ? 1 : 0

  domain = "vpc"
  tags   = { Name = "${var.name_prefix}-nat" }
}

resource "aws_nat_gateway" "this" {
  count = var.nat_enabled ? 1 : 0

  allocation_id = aws_eip.nat[0].id
  subnet_id     = aws_subnet.public[local.nat_az_id].id
  tags          = { Name = var.name_prefix }

  depends_on = [aws_internet_gateway.this]
}

resource "aws_route_table" "egress" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-egress" }
}

resource "aws_route" "egress_nat" {
  count = var.nat_enabled ? 1 : 0

  route_table_id         = aws_route_table.egress.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.this[0].id
}

resource "aws_route_table_association" "egress" {
  for_each = aws_subnet.egress

  subnet_id      = each.value.id
  route_table_id = aws_route_table.egress.id
}

# Isolated tier: intentionally no aws_route (local VPC routing and gateway endpoints only).
resource "aws_route_table" "isolated" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name_prefix}-isolated" }
}

resource "aws_route_table_association" "isolated" {
  for_each = aws_subnet.isolated

  subnet_id      = each.value.id
  route_table_id = aws_route_table.isolated.id
}

# --- Gateway endpoints (free) ------------------------------------------------

data "aws_iam_policy_document" "s3_endpoint" {
  # Data perimeter (threat model, exfiltration): tasks reach S3 only through
  # this endpoint, so it admits only this environment's buckets and the object
  # actions the services use. Endpoint policies need principal "*"; the task
  # roles' IAM policies still apply on top.
  statement {
    sid       = "ProjectBucketObjects"
    effect    = "Allow"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:AbortMultipartUpload"]
    resources = ["arn:${local.partition}:s3:::${var.s3_bucket_name_prefix}*/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceAccount"
      values   = [local.account_id]
    }
  }

  # ECR stores image layers in this AWS-owned bucket; pulls fail without it.
  statement {
    sid       = "ECRImageLayers"
    effect    = "Allow"
    actions   = ["s3:GetObject"]
    resources = ["arn:${local.partition}:s3:::prod-${local.region}-starport-layer-bucket/*"]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}

data "aws_iam_policy_document" "dynamodb_endpoint" {
  # Same perimeter for DynamoDB: only this environment's tables (and indexes).
  # Endpoint policies need principal "*"; "dynamodb:*" is bounded by the
  # table ARNs, and the task roles grant the specific actions.
  statement {
    sid     = "ProjectTables"
    effect  = "Allow"
    actions = ["dynamodb:*"]
    resources = [
      "arn:${local.partition}:dynamodb:${local.region}:${local.account_id}:table/${var.dynamodb_table_name_prefix}*",
    ]

    principals {
      type        = "*"
      identifiers = ["*"]
    }
  }
}

resource "aws_vpc_endpoint" "s3" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${local.region}.s3"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.egress.id, aws_route_table.isolated.id]
  policy            = data.aws_iam_policy_document.s3_endpoint.json
  tags              = { Name = "${var.name_prefix}-s3" }
}

resource "aws_vpc_endpoint" "dynamodb" {
  vpc_id            = aws_vpc.this.id
  service_name      = "com.amazonaws.${local.region}.dynamodb"
  vpc_endpoint_type = "Gateway"
  route_table_ids   = [aws_route_table.egress.id, aws_route_table.isolated.id]
  policy            = data.aws_iam_policy_document.dynamodb_endpoint.json
  tags              = { Name = "${var.name_prefix}-dynamodb" }
}

# --- Interface endpoints -----------------------------------------------------

# Tasks attach this group; the endpoint group admits only its members.
resource "aws_security_group" "endpoint_clients" {
  #checkov:skip=CKV2_AWS_5:Attached to ECS tasks by the ecs-service module (M5).
  name        = "${var.name_prefix}-vpce-clients"
  description = "Members may reach the interface VPC endpoints on 443"
  vpc_id      = aws_vpc.this.id
  tags        = { Name = "${var.name_prefix}-vpce-clients" }
}

resource "aws_security_group" "endpoints" {
  name        = "${var.name_prefix}-vpce"
  description = "Interface VPC endpoints: HTTPS from endpoint clients only"
  vpc_id      = aws_vpc.this.id
  tags        = { Name = "${var.name_prefix}-vpce" }
}

resource "aws_vpc_security_group_ingress_rule" "endpoints_from_clients" {
  security_group_id            = aws_security_group.endpoints.id
  description                  = "HTTPS from endpoint clients"
  ip_protocol                  = "tcp"
  from_port                    = 443
  to_port                      = 443
  referenced_security_group_id = aws_security_group.endpoint_clients.id
}

resource "aws_vpc_security_group_egress_rule" "clients_to_endpoints" {
  security_group_id            = aws_security_group.endpoint_clients.id
  description                  = "HTTPS to interface endpoints"
  ip_protocol                  = "tcp"
  from_port                    = 443
  to_port                      = 443
  referenced_security_group_id = aws_security_group.endpoints.id
}

# Data perimeter for the interface endpoints (M7, threat model "exfiltration"):
# only this account's principals, and only this account's resources. Code
# running in a task cannot use attacker credentials, or reach another account's
# queue, log group, secret or registry, through the endpoints. ssmmessages
# (break-glass ECS Exec only) has no resource ARNs and keeps the default policy.
data "aws_iam_policy_document" "interface_endpoint" {
  for_each = { for k in local.interface_endpoints : k => split(".", k)[0] if k != "ssmmessages" }

  statement {
    sid       = "OwnAccountPrincipalsAndResources"
    effect    = "Allow"
    actions   = ["${each.value}:*"] # Endpoint policy: narrows to this endpoint's service, never grants; task roles name the actions.
    resources = ["*"]               # Scoped by the aws:ResourceAccount condition below.

    # Endpoint policies require principal "*"; the conditions scope it.
    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:PrincipalAccount"
      values   = [local.account_id]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:ResourceAccount"
      values   = [local.account_id]
    }
  }
}

resource "aws_vpc_endpoint" "interface" {
  for_each = local.interface_endpoints

  vpc_id              = aws_vpc.this.id
  service_name        = "com.amazonaws.${local.region}.${each.key}"
  vpc_endpoint_type   = "Interface"
  private_dns_enabled = true
  subnet_ids          = [for id in local.endpoint_az_ids : aws_subnet.isolated[id].id]
  security_group_ids  = [aws_security_group.endpoints.id]
  policy              = each.key == "ssmmessages" ? null : data.aws_iam_policy_document.interface_endpoint[each.key].json
  tags                = { Name = "${var.name_prefix}-${replace(each.key, ".", "-")}" }
}

# --- Flow logs -----------------------------------------------------------------

resource "aws_cloudwatch_log_group" "flow_logs" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6 cost guardrails.
  name              = "/${replace(var.name_prefix, "-", "/")}/vpc-flow-logs"
  retention_in_days = var.flow_log_retention_days
  kms_key_id        = var.kms_key_arn
}

data "aws_iam_policy_document" "flow_logs_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["vpc-flow-logs.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${local.partition}:ec2:${local.region}:${local.account_id}:vpc-flow-log/*"]
    }
  }
}

data "aws_iam_policy_document" "flow_logs" {
  statement {
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents", "logs:DescribeLogStreams"]
    resources = ["${aws_cloudwatch_log_group.flow_logs.arn}:*"]
  }
}

resource "aws_iam_role" "flow_logs" {
  name                 = "${var.name_prefix}-vpc-flow-logs"
  assume_role_policy   = data.aws_iam_policy_document.flow_logs_trust.json
  permissions_boundary = var.permissions_boundary_arn
}

resource "aws_iam_role_policy" "flow_logs" {
  name   = "write-flow-logs"
  role   = aws_iam_role.flow_logs.id
  policy = data.aws_iam_policy_document.flow_logs.json
}

resource "aws_flow_log" "this" {
  vpc_id                   = aws_vpc.this.id
  traffic_type             = "ALL"
  log_destination_type     = "cloud-watch-logs"
  log_destination          = aws_cloudwatch_log_group.flow_logs.arn
  iam_role_arn             = aws_iam_role.flow_logs.arn
  max_aggregation_interval = 600
}
