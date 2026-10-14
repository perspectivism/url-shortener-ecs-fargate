variable "name_path" {
  description = "Secret name path prefix, e.g. \"shortener/dev\" (secrets are named PATH/NAME)."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]+/[a-z0-9-]+$", var.name_path))
    error_message = "name_path must look like project/env."
  }
}

variable "kms_key_arn" {
  description = "Environment CMK for the secrets."
  type        = string
}

variable "force_destroy" {
  description = "Delete immediately on destroy (recovery window 0) instead of after 30 days."
  type        = bool
}

variable "readers" {
  description = "IAM role ARNs allowed to read each secret (task execution roles, added in M5). Empty = nobody can read."
  type = object({
    safe_browsing_api_key = list(string)
    visitor_hash_salt     = list(string)
  })
  default = {
    safe_browsing_api_key = []
    visitor_hash_salt     = []
  }
}
