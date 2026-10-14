variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "kms_key_arn" {
  description = "CMK for ECS Exec session encryption and the exec log group."
  type        = string
}

variable "log_retention_days" {
  description = "Retention of the ECS Exec audit log group."
  type        = number
  default     = 14
}

variable "container_insights" {
  description = "Container Insights mode: \"enhanced\", \"enabled\" or \"disabled\"."
  type        = string
  default     = "enabled"

  validation {
    condition     = contains(["enhanced", "enabled", "disabled"], var.container_insights)
    error_message = "container_insights must be enhanced, enabled or disabled."
  }
}
