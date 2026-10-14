# ECS cluster with Fargate and Fargate Spot capacity providers (ADR-0003,
# ADR-0020). ECS Exec sessions (break-glass only, ADR-0003) are encrypted with
# the environment CMK and audited to CloudWatch Logs.

resource "aws_cloudwatch_log_group" "exec" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6.
  name              = "/${replace(var.name_prefix, "-", "/")}/ecs-exec"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

resource "aws_ecs_cluster" "this" {
  name = var.name_prefix

  setting {
    name  = "containerInsights"
    value = var.container_insights
  }

  configuration {
    execute_command_configuration {
      kms_key_id = var.kms_key_arn
      logging    = "OVERRIDE"

      log_configuration {
        cloud_watch_encryption_enabled = true
        cloud_watch_log_group_name     = aws_cloudwatch_log_group.exec.name
      }
    }
  }
}

resource "aws_ecs_cluster_capacity_providers" "this" {
  cluster_name       = aws_ecs_cluster.this.name
  capacity_providers = ["FARGATE", "FARGATE_SPOT"]

  default_capacity_provider_strategy {
    capacity_provider = "FARGATE"
    base              = 0
    weight            = 1
  }
}
