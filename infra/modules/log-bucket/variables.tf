variable "name" {
  description = "Globally unique name of the log bucket."
  type        = string
}

variable "force_destroy" {
  description = "Delete all objects and versions on destroy."
  type        = bool
}

variable "retention_days" {
  description = "Days to keep log objects."
  type        = number

  validation {
    condition     = var.retention_days >= 1 && var.retention_days <= 3650
    error_message = "retention_days must be between 1 and 3650."
  }
}
