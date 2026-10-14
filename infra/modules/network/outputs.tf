output "vpc_id" {
  description = "VPC ID."
  value       = aws_vpc.this.id
}

output "vpc_cidr" {
  description = "VPC IPv4 CIDR."
  value       = aws_vpc.this.cidr_block
}

output "az_ids" {
  description = "AZ IDs in use, in subnet order."
  value       = local.az_ids
}

output "public_subnet_ids" {
  description = "Public subnet IDs (NAT only)."
  value       = [for id in local.az_ids : aws_subnet.public[id].id]
}

output "egress_subnet_ids" {
  description = "Private subnets with NAT egress (api, nightly)."
  value       = [for id in local.az_ids : aws_subnet.egress[id].id]
}

output "isolated_subnet_ids" {
  description = "Private subnets without an internet route (ALB, redirect, worker, Valkey, endpoints)."
  value       = [for id in local.az_ids : aws_subnet.isolated[id].id]
}

output "endpoint_client_security_group_id" {
  description = "Attach to tasks that call AWS APIs through interface endpoints."
  value       = aws_security_group.endpoint_clients.id
}

output "s3_prefix_list_id" {
  description = "Prefix list of the S3 gateway endpoint (for task security group egress rules)."
  value       = aws_vpc_endpoint.s3.prefix_list_id
}

output "dynamodb_prefix_list_id" {
  description = "Prefix list of the DynamoDB gateway endpoint (for task security group egress rules)."
  value       = aws_vpc_endpoint.dynamodb.prefix_list_id
}

output "nat_enabled" {
  description = "Whether the egress tier has a NAT gateway."
  value       = var.nat_enabled
}

output "flow_log_group_name" {
  description = "VPC flow-log log group."
  value       = aws_cloudwatch_log_group.flow_logs.name
}
