variable "project" {
  description = "Project name: resource prefix and Project tag."
  type        = string
  default     = "shortener"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{2,15}$", var.project))
    error_message = "project must be 3-16 lowercase letters/digits."
  }
}

variable "environment" {
  description = "Environment name."
  type        = string
  default     = "prod"

  validation {
    condition     = contains(["dev", "prod"], var.environment)
    error_message = "environment must be dev or prod."
  }
}

variable "region" {
  description = "AWS Region for regional resources."
  type        = string
  default     = "us-east-1"

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-\\d$", var.region))
    error_message = "region must be an AWS Region name such as us-east-1."
  }
}

variable "repository" {
  description = "GitHub repository (owner/name), recorded in the Repository tag."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.repository))
    error_message = "repository must be owner/name."
  }
}

variable "owner" {
  description = "Owner tag value (team or person responsible)."
  type        = string

  validation {
    condition     = length(var.owner) > 0
    error_message = "owner must not be empty."
  }
}

variable "az_count" {
  description = "Availability Zones (2 dev / 3 prod)."
  type        = number
  default     = 3
}

variable "interface_endpoint_az_count" {
  description = "AZs with interface endpoint ENIs (1 dev / all prod)."
  type        = number
  default     = 3
}

variable "url_scanning_enabled" {
  description = "Scan destinations with Google Safe Browsing; creates the NAT gateway (false in dev, true in prod)."
  type        = bool
  default     = true
}

variable "ecs_exec_enabled" {
  description = "ECS Exec for debugging (dev only); adds the ssmmessages endpoint."
  type        = bool
  default     = false
}

variable "log_retention_days" {
  description = "CloudWatch Logs and log-bucket retention (14 dev / 90 prod)."
  type        = number
  default     = 90
}

variable "events_retention_days" {
  description = "Raw click-event retention in S3 (null keeps forever)."
  type        = number
  default     = 365
}

variable "force_destroy" {
  description = "Allow one-command teardown: empty buckets/repos on destroy, no deletion protection, no secret recovery window (true dev / false prod)."
  type        = bool
  default     = false
}

variable "cache_max_data_storage_gb" {
  description = "Valkey Serverless data storage cap (GB)."
  type        = number
  default     = 5
}

variable "cache_max_ecpu_per_second" {
  description = "Valkey Serverless ECPU/s cap."
  type        = number
  default     = 50000
}

variable "enable_services" {
  description = "Phase 2: deploy ECS services and the edge (M5). Requires seeded secrets and pushed images."
  type        = bool
  default     = false
}
