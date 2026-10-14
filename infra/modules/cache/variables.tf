variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\" (also the cache name)."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,30}$", var.name_prefix)) && !strcontains(var.name_prefix, "--")
    error_message = "name_prefix must be lowercase letters, digits and single hyphens (ElastiCache naming)."
  }
}

variable "kms_key_arn" {
  description = "Environment CMK for data at rest."
  type        = string
}

variable "vpc_id" {
  description = "VPC for the cache security groups."
  type        = string
}

variable "subnet_ids" {
  description = "Isolated subnets for the serverless cache endpoint."
  type        = list(string)

  validation {
    condition     = length(var.subnet_ids) >= 2
    error_message = "At least two subnets (AZs) are required."
  }
}

variable "major_engine_version" {
  description = "Valkey major version (ElastiCache supports 7.2-9.1 as of 2026-10-14)."
  type        = string
  default     = "9"
}

variable "max_data_storage_gb" {
  description = "Upper bound on cached data (GB) — cost cap."
  type        = number
  default     = 1

  validation {
    condition     = var.max_data_storage_gb >= 1 && var.max_data_storage_gb <= 5000
    error_message = "max_data_storage_gb must be between 1 and 5000."
  }
}

variable "max_ecpu_per_second" {
  description = "Upper bound on ElastiCache Processing Units per second — cost cap."
  type        = number
  default     = 5000

  validation {
    condition     = var.max_ecpu_per_second >= 1000 && var.max_ecpu_per_second <= 15000000
    error_message = "max_ecpu_per_second must be between 1000 and 15000000."
  }
}

variable "app_access_string" {
  description = "ACL for the application user: all keys and commands except the @dangerous category (FLUSHALL, KEYS, CONFIG, DEBUG, ...)."
  type        = string
  default     = "on ~* &* +@all -@dangerous"
}
