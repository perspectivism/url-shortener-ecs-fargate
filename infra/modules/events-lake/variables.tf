variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "kms_key_arn" {
  description = "Environment CMK (S3 SSE-KMS and Athena results)."
  type        = string
}

variable "force_destroy" {
  description = "Allow destroying non-empty buckets and the Athena workgroup."
  type        = bool
}

variable "log_bucket_id" {
  description = "Bucket receiving S3 server access logs."
  type        = string
}

variable "events_retention_days" {
  description = "Days to keep raw click events (null keeps them forever)."
  type        = number
  default     = null
}

variable "bytes_scanned_cutoff_per_query" {
  description = "Athena per-query scan limit in bytes (cost guard; minimum 10 MB)."
  type        = number
  default     = 1073741824

  validation {
    condition     = var.bytes_scanned_cutoff_per_query >= 10485760
    error_message = "bytes_scanned_cutoff_per_query must be at least 10485760 (10 MB)."
  }
}

variable "partition_start_date" {
  description = "First date (yyyy-MM-dd) covered by partition projection."
  type        = string
  default     = "2026-01-01"

  validation {
    condition     = can(regex("^\\d{4}-\\d{2}-\\d{2}$", var.partition_start_date))
    error_message = "partition_start_date must be yyyy-MM-dd."
  }
}
