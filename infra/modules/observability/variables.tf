variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,30}$", var.name_prefix))
    error_message = "name_prefix must be 3-31 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "metrics_namespace" {
  description = "CloudWatch namespace of the services' EMF metrics, e.g. \"Shortener/dev\"."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9]+/[a-z0-9]+$", var.metrics_namespace))
    error_message = "metrics_namespace must look like Project/env."
  }
}

variable "kms_key_arn" {
  description = "CMK that encrypts the alert topic (its key policy must admit the publishing services)."
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:kms:", var.kms_key_arn))
    error_message = "kms_key_arn must be a KMS key ARN."
  }
}

variable "alert_email" {
  description = "Email address subscribed to the alert topic (the recipient must confirm). Null creates no subscription."
  type        = string
  sensitive   = true # personal data: kept out of plan output and CI logs
  default     = null

  validation {
    condition     = var.alert_email == null || can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", var.alert_email))
    error_message = "alert_email must be an email address."
  }
}

variable "queue_name" {
  description = "Click-event queue name (age alarm)."
  type        = string
}

variable "dlq_name" {
  description = "Click-event dead-letter queue name (any message alarms)."
  type        = string
}

variable "table_names" {
  description = "DynamoDB tables to watch for throttling, keyed by a short label (e.g. links, analytics)."
  type        = map(string)

  validation {
    condition     = length(var.table_names) > 0
    error_message = "At least one table is required."
  }
}

variable "services" {
  description = "Phase-2 resources (null while enable_services is false): service alarms and the dashboard are created only when set."
  type = object({
    cluster_arn               = string
    cluster_name              = string
    service_names             = map(string) # redirect, api, worker -> ECS service name
    alb_arn_suffix            = string
    target_group_arn_suffixes = map(string) # redirect, api
    web_acl_name              = string
    nightly = object({
      schedule_group_name = string
      log_group_name      = string
      task_family         = string
    })
  })
  default = null

  validation {
    condition = var.services == null || (
      length(setsubtract(["redirect", "api", "worker"], keys(try(var.services.service_names, {})))) == 0 &&
      length(setsubtract(["redirect", "api"], keys(try(var.services.target_group_arn_suffixes, {})))) == 0
    )
    error_message = "services needs service_names for redirect, api and worker, and target groups for redirect and api."
  }
}
