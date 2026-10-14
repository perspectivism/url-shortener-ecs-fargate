variable "name" {
  description = "Globally unique bucket name."
  type        = string

  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$", var.name))
    error_message = "name must be a valid S3 bucket name."
  }
}

variable "kms_key_arn" {
  description = "CMK for SSE-KMS. Null selects SSE-S3, which only log buckets use (ALB and S3 server access log delivery do not support SSE-KMS)."
  type        = string
  default     = null
}

variable "force_destroy" {
  description = "Delete all objects and versions on destroy (true in dev for one-command teardown, false in prod)."
  type        = bool
}

variable "access_log_bucket" {
  description = "Bucket that receives S3 server access logs for this bucket (null disables, used only by the log bucket itself)."
  type        = string
  default     = null
}

variable "expiration_days" {
  description = "Expire current objects after this many days (null keeps them)."
  type        = number
  default     = null

  validation {
    condition     = var.expiration_days == null || try(var.expiration_days >= 1, false)
    error_message = "expiration_days must be null or at least 1."
  }
}

variable "noncurrent_version_expiration_days" {
  description = "Delete noncurrent object versions after this many days."
  type        = number
  default     = 30

  validation {
    condition     = var.noncurrent_version_expiration_days >= 1
    error_message = "noncurrent_version_expiration_days must be at least 1."
  }
}

variable "extra_policy_json" {
  description = "Additional bucket policy statements (aws_iam_policy_document JSON) merged with the TLS-only policy."
  type        = string
  default     = null
}
