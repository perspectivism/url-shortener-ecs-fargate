variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "task" {
  description = "Task name (e.g. nightly)."
  type        = string
}

variable "cluster_arn" {
  description = "ECS cluster ARN."
  type        = string
}

variable "image" {
  description = "Full image reference: REPOSITORY_URL:GIT_SHA (never latest)."
  type        = string

  validation {
    condition     = can(regex(":[A-Za-z0-9._-]+$", var.image)) && !endswith(var.image, ":latest")
    error_message = "image must carry an explicit, non-latest tag."
  }
}

variable "ecr_repository_arn" {
  description = "Repository the execution role may pull from."
  type        = string
}

variable "cpu" {
  description = "Task CPU units."
  type        = number
  default     = 512
}

variable "memory" {
  description = "Task memory (MiB)."
  type        = number
  default     = 1024
}

variable "environment" {
  description = "Plain environment variables."
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Secret environment variables: name => secret ARN."
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "subnet_ids" {
  description = "Subnets for the task."
  type        = list(string)
}

variable "extra_security_group_ids" {
  description = "Additional security groups (endpoint clients, cache clients)."
  type        = list(string)
  default     = []
}

variable "egress" {
  description = "Egress allowances beyond the attached client security groups."
  type = object({
    s3_prefix_list_id       = string
    dynamodb_prefix_list_id = string
    internet_https          = bool
  })
}

variable "schedule_expression" {
  description = "EventBridge Scheduler expression (UTC)."
  type        = string
  default     = "cron(15 3 * * ? *)"
}

variable "capacity_provider" {
  description = "FARGATE_SPOT (default; the job is idempotent and re-runnable) or FARGATE."
  type        = string
  default     = "FARGATE_SPOT"

  validation {
    condition     = contains(["FARGATE", "FARGATE_SPOT"], var.capacity_provider)
    error_message = "capacity_provider must be FARGATE or FARGATE_SPOT."
  }
}

variable "task_policy_json" {
  description = "IAM policy (JSON) for the task role."
  type        = string
}

variable "kms_key_arn" {
  description = "Environment CMK (logs, secrets)."
  type        = string
}

variable "log_retention_days" {
  description = "Log retention."
  type        = number
  default     = 14
}

variable "permissions_boundary_arn" {
  description = "Permissions boundary for roles created here."
  type        = string
  default     = null
}
