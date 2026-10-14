variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "cidr_block" {
  description = "VPC IPv4 CIDR (a /16 is split into /24 public and /20 private subnets)."
  type        = string
  default     = "10.20.0.0/16"

  validation {
    condition     = can(cidrhost(var.cidr_block, 0)) && endswith(var.cidr_block, "/16")
    error_message = "cidr_block must be a valid /16."
  }
}

variable "az_count" {
  description = "Number of Availability Zones (2 in dev, 3 in prod)."
  type        = number
  default     = 2

  validation {
    condition     = var.az_count >= 2 && var.az_count <= 3
    error_message = "az_count must be 2 or 3."
  }
}

variable "excluded_az_ids" {
  description = "AZ IDs to avoid. use1-az3 does not support CloudFront VPC origins (verified 2026-10-14)."
  type        = list(string)
  default     = ["use1-az3"]
}

variable "nat_enabled" {
  description = "Create a NAT gateway for the egress tier (only needed when URL scanning calls Google Safe Browsing)."
  type        = bool
  default     = false
}

variable "interface_endpoint_az_count" {
  description = "Number of AZs that get interface endpoint ENIs (1 in dev to save cost, all AZs in prod)."
  type        = number
  default     = 1

  validation {
    condition     = var.interface_endpoint_az_count >= 1 && var.interface_endpoint_az_count <= 3
    error_message = "interface_endpoint_az_count must be between 1 and 3."
  }
}

variable "ecs_exec_enabled" {
  description = "Add the ssmmessages interface endpoint required by ECS Exec."
  type        = bool
  default     = false
}

variable "kms_key_arn" {
  description = "CMK for the flow-log log group."
  type        = string
}

variable "flow_log_retention_days" {
  description = "Retention of the VPC flow-log log group."
  type        = number
  default     = 14

  validation {
    condition     = contains([1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653], var.flow_log_retention_days)
    error_message = "flow_log_retention_days must be a CloudWatch Logs retention value."
  }
}

variable "dynamodb_table_name_prefix" {
  description = "DynamoDB tables reachable through the gateway endpoint (name prefix)."
  type        = string
}

variable "s3_bucket_name_prefix" {
  description = "S3 buckets reachable through the gateway endpoint (name prefix, e.g. \"shortener-dev-\")."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{2,40}-$", var.s3_bucket_name_prefix))
    error_message = "s3_bucket_name_prefix must be a lowercase bucket-name prefix ending in a hyphen."
  }
}

variable "permissions_boundary_arn" {
  description = "Permissions boundary for IAM roles this module creates (the CI boundary from infra/bootstrap; null for none)."
  type        = string
  default     = null
}
