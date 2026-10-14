variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "kms_key_arn" {
  description = "Environment CMK for server-side encryption."
  type        = string
}

variable "deletion_protection" {
  description = "Enable DynamoDB deletion protection (true in prod; derived from !force_destroy)."
  type        = bool
}

variable "links_max_read_units" {
  description = "On-demand maximum read request units for the links table (cost cap)."
  type        = number
  default     = 1000

  validation {
    condition     = var.links_max_read_units >= 1
    error_message = "links_max_read_units must be at least 1."
  }
}

variable "links_max_write_units" {
  description = "On-demand maximum write request units for the links table (cost cap)."
  type        = number
  default     = 200

  validation {
    condition     = var.links_max_write_units >= 1
    error_message = "links_max_write_units must be at least 1."
  }
}

variable "analytics_max_read_units" {
  description = "On-demand maximum read request units for the analytics table (cost cap)."
  type        = number
  default     = 500

  validation {
    condition     = var.analytics_max_read_units >= 1
    error_message = "analytics_max_read_units must be at least 1."
  }
}

variable "analytics_max_write_units" {
  description = "On-demand maximum write request units for the analytics table (cost cap)."
  type        = number
  default     = 1000

  validation {
    condition     = var.analytics_max_write_units >= 1
    error_message = "analytics_max_write_units must be at least 1."
  }
}
