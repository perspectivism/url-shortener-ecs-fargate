mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
  }
  mock_data "aws_availability_zones" {
    defaults = {
      names    = ["us-east-1a", "us-east-1b", "us-east-1d"]
      zone_ids = ["use1-az1", "use1-az2", "use1-az4"]
    }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/shortener/test/vpc-flow-logs" }
  }
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/shortener-test-vpc-flow-logs" }
  }
}

variables {
  name_prefix                = "shortener-test"
  kms_key_arn                = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  dynamodb_table_name_prefix = "shortener-test-"
}

# command = apply with mock_provider only simulates (no AWS calls) and makes
# computed IDs known, so cross-resource references can be compared.
run "dev_defaults_have_no_nat_and_isolated_tier_has_no_default_route" {
  command = apply

  assert {
    condition     = length(aws_nat_gateway.this) == 0 && length(aws_route.egress_nat) == 0 && length(aws_eip.nat) == 0
    error_message = "No NAT gateway (or route to one) unless URL scanning is enabled."
  }

  assert {
    condition     = length(aws_subnet.public) == 2 && length(aws_subnet.egress) == 2 && length(aws_subnet.isolated) == 2
    error_message = "Two AZs with three tiers each in dev."
  }

  assert {
    condition     = aws_route.public_internet.route_table_id != aws_route_table.isolated.id
    error_message = "The only internet route must not belong to the isolated tier."
  }

  assert {
    condition     = alltrue([for s in aws_subnet.public : !s.map_public_ip_on_launch])
    error_message = "Public subnets must not auto-assign public IPs (they host only the NAT)."
  }

  assert {
    condition     = toset([for s in aws_subnet.isolated : s.availability_zone_id]) == toset(["use1-az1", "use1-az2"])
    error_message = "Subnets must be placed by AZ ID."
  }

  assert {
    condition     = toset([for s in aws_subnet.isolated : s.cidr_block]) == toset(["10.20.128.0/20", "10.20.144.0/20"])
    error_message = "Unexpected isolated CIDRs."
  }

  assert {
    condition     = length(aws_vpc_endpoint.interface) == 5 && alltrue([for e in aws_vpc_endpoint.interface : length(e.subnet_ids) == 1 && e.private_dns_enabled])
    error_message = "Five interface endpoints in one AZ with private DNS in dev."
  }

  assert {
    condition     = aws_vpc_security_group_ingress_rule.endpoints_from_clients.cidr_ipv4 == null && aws_vpc_security_group_ingress_rule.endpoints_from_clients.from_port == 443
    error_message = "Endpoint ingress must reference the client security group, never a CIDR."
  }

  assert {
    condition     = aws_flow_log.this.traffic_type == "ALL" && aws_cloudwatch_log_group.flow_logs.kms_key_id == var.kms_key_arn
    error_message = "Flow logs must capture all traffic into a CMK-encrypted log group."
  }

  assert {
    condition     = length(aws_default_security_group.default.ingress) == 0 && length(aws_default_security_group.default.egress) == 0
    error_message = "The default security group must have no rules."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.s3_endpoint.statement :
      anytrue([for c in s.condition : c.variable == "aws:ResourceAccount" && contains(c.values, "123456789012")])
    ])
    error_message = "The S3 gateway endpoint must be limited to this account's buckets."
  }

  assert {
    condition     = contains(flatten([for s in data.aws_iam_policy_document.dynamodb_endpoint.statement : s.resources]), "arn:aws:dynamodb:us-east-1:123456789012:table/shortener-test-*")
    error_message = "The DynamoDB gateway endpoint must be limited to this environment's tables."
  }
}

run "prod_with_scanning_has_nat_and_endpoints_in_all_azs" {
  command = apply

  variables {
    az_count                    = 3
    nat_enabled                 = true
    interface_endpoint_az_count = 3
    ecs_exec_enabled            = false
  }

  assert {
    condition     = length(aws_nat_gateway.this) == 1 && length(aws_route.egress_nat) == 1
    error_message = "NAT gateway and egress default route when scanning is enabled."
  }

  assert {
    condition     = aws_route.egress_nat[0].route_table_id == aws_route_table.egress.id
    error_message = "The NAT route belongs to the egress tier only."
  }

  assert {
    condition     = length(aws_subnet.isolated) == 3 && alltrue([for e in aws_vpc_endpoint.interface : length(e.subnet_ids) == 3])
    error_message = "Three AZs with endpoint ENIs in each."
  }
}

run "ecs_exec_adds_ssmmessages" {
  command = plan

  variables {
    ecs_exec_enabled = true
  }

  assert {
    condition     = contains(keys(aws_vpc_endpoint.interface), "ssmmessages")
    error_message = "ECS Exec needs the ssmmessages endpoint."
  }
}

run "rejects_bad_inputs" {
  command = plan

  variables {
    az_count                = 4
    cidr_block              = "10.20.0.0/24"
    flow_log_retention_days = 10
  }

  expect_failures = [var.az_count, var.cidr_block, var.flow_log_retention_days]
}
