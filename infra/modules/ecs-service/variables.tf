variable "name_prefix" {
  description = "Resource name prefix, e.g. \"shortener-dev\"."
  type        = string
}

variable "service" {
  description = "Service name (redirect, api, worker)."
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,20}$", var.service))
    error_message = "service must be a short lowercase name."
  }
}

variable "cluster_arn" {
  description = "ECS cluster ARN."
  type        = string
}

variable "cluster_name" {
  description = "ECS cluster name (autoscaling resource ID)."
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
  default     = 256
}

variable "memory" {
  description = "Task memory (MiB)."
  type        = number
  default     = 512
}

variable "container_port" {
  description = "Port the container listens on (HTTP_ADDR)."
  type        = number
  default     = 8080
}

variable "environment" {
  description = "Plain (non-secret) environment variables."
  type        = map(string)
  default     = {}
}

variable "secrets" {
  description = "Secret environment variables: name => Secrets Manager secret ARN (injected via valueFrom)."
  type        = map(string)
  default     = {}
}

variable "vpc_id" {
  description = "VPC ID."
  type        = string
}

variable "subnet_ids" {
  description = "Subnets for the tasks (no public IPs)."
  type        = list(string)
}

variable "extra_security_group_ids" {
  description = "Additional security groups (VPC endpoint clients, cache clients)."
  type        = list(string)
  default     = []
}

variable "load_balancer" {
  description = "ALB integration (null for services without a load balancer, e.g. worker)."
  type = object({
    target_group_arn      = string
    alb_security_group_id = string
    alb_resource_label    = string # "<alb arn_suffix>/<target group arn_suffix>" for ALBRequestCountPerTarget
    routing_ready         = string # listener or rule ARN attaching the target group (creation ordering)
  })
  default = null
}

variable "egress" {
  description = "Egress allowances beyond the attached client security groups."
  type = object({
    s3_prefix_list_id       = string
    dynamodb_prefix_list_id = string
    internet_https          = bool # only for tasks in NAT-routed subnets (URL scanning)
  })
}

variable "capacity_provider_strategy" {
  description = "Fargate capacity provider strategy."
  type = list(object({
    capacity_provider = string
    base              = number
    weight            = number
  }))
  default = [{ capacity_provider = "FARGATE", base = 1, weight = 1 }]

  validation {
    condition     = alltrue([for s in var.capacity_provider_strategy : contains(["FARGATE", "FARGATE_SPOT"], s.capacity_provider)])
    error_message = "capacity providers must be FARGATE or FARGATE_SPOT."
  }
}

variable "desired_count" {
  description = "Initial desired count (autoscaling owns it afterwards)."
  type        = number
  default     = 1
}

variable "min_count" {
  description = "Autoscaling minimum."
  type        = number
  default     = 1
}

variable "max_count" {
  description = "Autoscaling maximum."
  type        = number
  default     = 2

  validation {
    condition     = var.max_count >= 1
    error_message = "max_count must be at least 1."
  }
}

variable "cpu_target_percent" {
  description = "Target-tracking CPU utilisation (null disables)."
  type        = number
  default     = 60
}

variable "alb_requests_per_target" {
  description = "Target-tracking ALBRequestCountPerTarget (null disables; needs load_balancer)."
  type        = number
  default     = null
}

variable "sqs_queue_name" {
  description = "Scale on this queue's backlog (step scaling, supports scale to zero; null disables)."
  type        = string
  default     = null
}

variable "task_policy_json" {
  description = "IAM policy (JSON) for the task role: exactly what the application needs."
  type        = string
}

variable "kms_key_arn" {
  description = "Environment CMK (log group encryption, secret decryption)."
  type        = string
}

variable "log_retention_days" {
  description = "Application log retention."
  type        = number
  default     = 14
}

variable "permissions_boundary_arn" {
  description = "Permissions boundary for the task and execution roles."
  type        = string
  default     = null
}

variable "enable_execute_command" {
  description = "Break-glass ECS Exec. AWS does not support it with a read-only root filesystem, so enabling it also relaxes that (never in prod; ADR-0003)."
  type        = bool
  default     = false
}

variable "health_check_grace_period_seconds" {
  description = "Grace period before ALB health checks count (services with a load balancer)."
  type        = number
  default     = 30
}

variable "wait_for_steady_state" {
  description = "Make terraform apply wait for the deployment (and fail when the circuit breaker rolls back)."
  type        = bool
  default     = true
}
