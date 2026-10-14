variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,30}$", var.name_prefix))
    error_message = "name_prefix must be 3-31 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "deletion_window_in_days" {
  description = "Waiting period before a scheduled key deletion completes (AWS minimum is 7)."
  type        = number
  default     = 7

  validation {
    condition     = var.deletion_window_in_days >= 7 && var.deletion_window_in_days <= 30
    error_message = "deletion_window_in_days must be between 7 and 30."
  }
}

variable "log_group_name_prefixes" {
  description = "CloudWatch Logs log groups whose names start with one of these prefixes may use the key (encryption context condition)."
  type        = list(string)

  validation {
    condition     = length(var.log_group_name_prefixes) > 0
    error_message = "At least one log group prefix is required."
  }
}
