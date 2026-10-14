variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "subnet_ids" {
  description = "Isolated subnets for the internal ALB (AZs supported by CloudFront VPC origins)."
  type        = list(string)

  validation {
    condition     = length(var.subnet_ids) >= 2
    error_message = "The ALB needs at least two subnets."
  }
}

variable "container_port" {
  description = "Target port of the services."
  type        = number
  default     = 8080
}

variable "target_security_group_ids" {
  description = "Task security groups the ALB may reach, keyed by service (redirect, api)."
  type        = map(string)
}

variable "log_bucket_id" {
  description = "Log bucket for ALB access logs (prefix alb/)."
  type        = string
}

variable "us_east_1_kms_key_arn" {
  description = "CMK in us-east-1 for the CloudFront and WAF log groups."
  type        = string
}

variable "log_retention_days" {
  description = "Retention of the CloudFront and WAF log groups."
  type        = number
  default     = 14
}

variable "force_destroy" {
  description = "Disable ALB deletion protection (dev teardown)."
  type        = bool
}

variable "domain_name" {
  description = "Optional custom domain (e.g. sho.rt). Enables ACM certificates, HTTPS to the origin and TLSv1.2_2021 for viewers."
  type        = string
  default     = null
}

variable "hosted_zone_id" {
  description = "Route 53 public hosted zone for domain_name (required with domain_name)."
  type        = string
  default     = null
}

variable "api_rate_limit_per_minute" {
  description = "WAF per-IP limit for /api/* requests per minute."
  type        = number
  default     = 300

  validation {
    condition     = var.api_rate_limit_per_minute >= 10
    error_message = "WAF rate limits must be at least 10 per evaluation window."
  }
}

variable "redirect_rate_limit_per_minute" {
  description = "WAF per-IP limit for redirect requests per minute."
  type        = number
  default     = 1200

  validation {
    condition     = var.redirect_rate_limit_per_minute >= 10
    error_message = "WAF rate limits must be at least 10 per evaluation window."
  }
}

variable "anonymous_ip_action" {
  description = "AWSManagedRulesAnonymousIpList: \"count\" (initial) or \"block\" (after review)."
  type        = string
  default     = "count"

  validation {
    condition     = contains(["count", "block"], var.anonymous_ip_action)
    error_message = "anonymous_ip_action must be count or block."
  }
}
