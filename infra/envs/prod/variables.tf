variable "project" {
  description = "Project name: resource prefix and Project tag."
  type        = string
  default     = "shortener"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{2,15}$", var.project))
    error_message = "project must be 3-16 lowercase letters/digits."
  }
}

variable "environment" {
  description = "Environment name."
  type        = string
  default     = "prod"

  validation {
    condition     = contains(["dev", "prod"], var.environment)
    error_message = "environment must be dev or prod."
  }
}

variable "region" {
  description = "AWS Region for regional resources."
  type        = string
  default     = "us-east-1"

  validation {
    condition     = can(regex("^[a-z]{2}(-[a-z]+)+-\\d$", var.region))
    error_message = "region must be an AWS Region name such as us-east-1."
  }
}

variable "repository" {
  description = "GitHub repository (owner/name), recorded in the Repository tag."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.repository))
    error_message = "repository must be owner/name."
  }
}

variable "owner" {
  description = "Owner tag value (team or person responsible)."
  type        = string

  validation {
    condition     = length(var.owner) > 0
    error_message = "owner must not be empty."
  }
}

variable "az_count" {
  description = "Availability Zones (2 dev / 3 prod)."
  type        = number
  default     = 3
}

variable "interface_endpoint_az_count" {
  description = "AZs with interface endpoint ENIs (1 dev / all prod)."
  type        = number
  default     = 3
}

variable "url_scanning_enabled" {
  description = "Scan destinations with Google Safe Browsing; creates the NAT gateway (false in dev, true in prod)."
  type        = bool
  default     = true
}

variable "ecs_exec_enabled" {
  description = "Break-glass ECS Exec (ADR-0003): off by default because AWS does not support it with read-only root filesystems; enabling relaxes that and adds the ssmmessages endpoint. Never in prod."
  type        = bool
  default     = false

  validation {
    condition     = !(var.ecs_exec_enabled && var.environment == "prod")
    error_message = "ECS Exec must not be enabled in prod."
  }
}

variable "log_retention_days" {
  description = "CloudWatch Logs and log-bucket retention (14 dev / 90 prod)."
  type        = number
  default     = 90
}

variable "events_retention_days" {
  description = "Raw click-event retention in S3 (null keeps forever)."
  type        = number
  default     = 365
}

variable "force_destroy" {
  description = "Allow one-command teardown: empty buckets/repos on destroy, no deletion protection, no secret recovery window (true dev / false prod)."
  type        = bool
  default     = false
}

variable "cache_max_data_storage_gb" {
  description = "Valkey Serverless data storage cap (GB)."
  type        = number
  default     = 5
}

variable "cache_max_ecpu_per_second" {
  description = "Valkey Serverless ECPU/s cap."
  type        = number
  default     = 50000
}

variable "enable_services" {
  description = "Phase 2: deploy ECS services and the edge (M5). Requires seeded secrets and pushed images."
  type        = bool
  default     = false
}

variable "image_tag" {
  description = "Image tag (git SHA) for every service; required when enable_services is true. Never \"latest\"."
  type        = string
  default     = null

  validation {
    condition     = !var.enable_services || (var.image_tag != null && can(regex("^[0-9a-f]{7,40}$", coalesce(var.image_tag, "x"))))
    error_message = "image_tag must be a git SHA (7-40 hex characters) when enable_services is true."
  }
}

variable "sqids_alphabet" {
  description = "Non-secret shuffled Sqids alphabet for short codes (changing it changes future codes)."
  type        = string
  default     = "O7mH0bSxUPJnNZefjW25TzIALs3iX1kcRdMa8QD9hgKY4rEqlVyv6wGFoutBpC"

  validation {
    condition     = can(regex("^[A-Za-z0-9]{10,62}$", var.sqids_alphabet))
    error_message = "sqids_alphabet must be 10-62 ASCII letters and digits."
  }
}

variable "domain_name" {
  description = "Optional custom domain (enables ACM certificates, HTTPS to the origin and TLSv1.2_2021)."
  type        = string
  default     = null
}

variable "hosted_zone_id" {
  description = "Route 53 hosted zone for domain_name."
  type        = string
  default     = null
}

variable "redirect_min_count" {
  description = "Minimum (and initial) redirect tasks."
  type        = number
  default     = 2
}

variable "redirect_max_count" {
  description = "Maximum redirect tasks."
  type        = number
  default     = 10
}

variable "redirect_on_spot" {
  description = "Run redirect on Fargate Spot (dev only; prod uses on-demand, ADR-0020)."
  type        = bool
  default     = false

  validation {
    condition     = !(var.redirect_on_spot && var.environment == "prod")
    error_message = "redirect must not run on Spot in prod."
  }
}

variable "api_min_count" {
  description = "Minimum (and initial) api tasks."
  type        = number
  default     = 2
}

variable "api_max_count" {
  description = "Maximum api tasks."
  type        = number
  default     = 4
}

variable "worker_min_count" {
  description = "Minimum worker tasks (0 lets the worker scale to zero on an empty queue)."
  type        = number
  default     = 1
}

variable "worker_max_count" {
  description = "Maximum worker tasks."
  type        = number
  default     = 5
}

variable "waf_api_rate_limit_per_minute" {
  description = "WAF per-IP rate limit for /api/* (requests per minute)."
  type        = number
  default     = 300
}

variable "waf_redirect_rate_limit_per_minute" {
  description = "WAF per-IP rate limit for redirects (requests per minute)."
  type        = number
  default     = 1200
}

variable "waf_anonymous_ip_action" {
  description = "AnonymousIpList action: count (initial) or block (after review)."
  type        = string
  default     = "count"
}

variable "alert_email" {
  description = "Email subscribed to the alert topic (alarms, budgets, cost anomalies); the recipient must confirm. Null: no subscription."
  type        = string
  default     = null
}

variable "monthly_budget_usd" {
  description = "Monthly AWS Budget for this environment in USD (docs/cost.md)."
  type        = number
  default     = 400
}

variable "anomaly_threshold_usd" {
  description = "Cost Anomaly Detection alert threshold (total impact, USD)."
  type        = number
  default     = 25
}
