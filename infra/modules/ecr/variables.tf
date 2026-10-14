variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "services" {
  description = "Services that get a repository."
  type        = set(string)
  default     = ["redirect", "api", "worker", "nightly"]
}

variable "kms_key_arn" {
  description = "Environment CMK for image encryption."
  type        = string
}

variable "force_destroy" {
  description = "Delete repositories that still contain images."
  type        = bool
}

variable "keep_images" {
  description = "Number of most recent images kept per repository."
  type        = number
  default     = 30

  validation {
    condition     = var.keep_images >= 5
    error_message = "keep_images must be at least 5 (rollback needs history)."
  }
}
