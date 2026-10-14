variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "kms_key_arn" {
  description = "Environment CMK for SQS server-side encryption."
  type        = string
}

variable "visibility_timeout_seconds" {
  description = "Click queue visibility timeout; must exceed the worker's flush interval plus long-poll wait (SPEC §8.2)."
  type        = number
  default     = 120

  validation {
    condition     = var.visibility_timeout_seconds >= 90 && var.visibility_timeout_seconds <= 43200
    error_message = "visibility_timeout_seconds must be between 90 and 43200."
  }
}

variable "max_receive_count" {
  description = "Receives before a message moves to the DLQ."
  type        = number
  default     = 5

  validation {
    condition     = var.max_receive_count >= 1 && var.max_receive_count <= 1000
    error_message = "max_receive_count must be between 1 and 1000."
  }
}
