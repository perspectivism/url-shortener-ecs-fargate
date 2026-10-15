variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,30}$", var.name_prefix))
    error_message = "name_prefix must be 3-31 lowercase letters, digits or hyphens, starting with a letter."
  }
}

variable "cost_tag" {
  description = "Cost allocation tag that identifies this environment's spend (must be activated in Billing, see infra/bootstrap)."
  type = object({
    key   = string
    value = string
  })
}

variable "monthly_limit_usd" {
  description = "Monthly budget in USD; notifications at 50/80/100 % actual and 100 % forecast."
  type        = number

  validation {
    condition     = var.monthly_limit_usd > 0
    error_message = "monthly_limit_usd must be positive."
  }
}

variable "anomaly_threshold_usd" {
  description = "Notify on cost anomalies whose total impact is at least this many USD."
  type        = number
  default     = 10

  validation {
    condition     = var.anomaly_threshold_usd > 0
    error_message = "anomaly_threshold_usd must be positive."
  }
}

variable "topic_arn" {
  description = "SNS topic for budget and anomaly notifications (its policy must admit budgets.amazonaws.com and costalerts.amazonaws.com)."
  type        = string

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:", var.topic_arn))
    error_message = "topic_arn must be an SNS topic ARN."
  }
}

variable "anomaly_monitor_enabled" {
  description = "Create the Cost Anomaly Detection tag monitor and subscription. Tag monitors need a management or standalone account; set false in an AWS Organizations member account (the budget still works)."
  type        = bool
  default     = true
}
