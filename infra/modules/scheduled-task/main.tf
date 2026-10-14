# Scheduled ECS task (SPEC §8.3): EventBridge Scheduler runs a hardened Fargate
# (Spot) task on a cron schedule. Failures surface as a non-zero exit code
# (alarmed in M6 via a log metric filter and Scheduler target errors).

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

locals {
  name       = "${var.name_prefix}-${var.task}"
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region
}

resource "aws_cloudwatch_log_group" "this" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6.
  name              = "/${replace(var.name_prefix, "-", "/")}/${var.task}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

data "aws_iam_policy_document" "ecs_tasks_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

data "aws_iam_policy_document" "execution" {
  statement {
    sid       = "ECRAuth"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"] # AWS: no resource-level permissions.
  }

  statement {
    sid       = "PullOwnImage"
    effect    = "Allow"
    actions   = ["ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer"]
    resources = [var.ecr_repository_arn]
  }

  statement {
    sid       = "WriteOwnLogs"
    effect    = "Allow"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.this.arn}:*"]
  }

  dynamic "statement" {
    for_each = length(var.secrets) > 0 ? [1] : []
    content {
      sid       = "ReadOwnSecrets"
      effect    = "Allow"
      actions   = ["secretsmanager:GetSecretValue"]
      resources = sort(distinct(values(var.secrets)))
    }
  }

  dynamic "statement" {
    for_each = length(var.secrets) > 0 ? [1] : []
    content {
      sid       = "DecryptSecretsViaSecretsManager"
      effect    = "Allow"
      actions   = ["kms:Decrypt"]
      resources = [var.kms_key_arn]

      condition {
        test     = "StringEquals"
        variable = "kms:ViaService"
        values   = ["secretsmanager.${local.region}.amazonaws.com"]
      }
    }
  }
}

resource "aws_iam_role" "execution" {
  name                 = "${local.name}-exec"
  assume_role_policy   = data.aws_iam_policy_document.ecs_tasks_trust.json
  permissions_boundary = var.permissions_boundary_arn
}

resource "aws_iam_role_policy" "execution" {
  name   = "execution"
  role   = aws_iam_role.execution.id
  policy = data.aws_iam_policy_document.execution.json
}

resource "aws_iam_role" "task" {
  name                 = "${local.name}-task"
  assume_role_policy   = data.aws_iam_policy_document.ecs_tasks_trust.json
  permissions_boundary = var.permissions_boundary_arn
}

resource "aws_iam_role_policy" "task" {
  name   = "application"
  role   = aws_iam_role.task.id
  policy = var.task_policy_json
}

resource "aws_security_group" "task" {
  #checkov:skip=CKV2_AWS_5:Attached to each run through the EventBridge Scheduler ECS target (network_configuration).
  name        = "${local.name}-task"
  description = "${var.task} scheduled task (no ingress)"
  vpc_id      = var.vpc_id
  tags        = { Name = "${local.name}-task" }
}

resource "aws_vpc_security_group_egress_rule" "gateway_endpoints" {
  for_each = { s3 = var.egress.s3_prefix_list_id, dynamodb = var.egress.dynamodb_prefix_list_id }

  security_group_id = aws_security_group.task.id
  description       = "HTTPS to the ${each.key} gateway endpoint"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  prefix_list_id    = each.value
}

# Trivy AWS-0104 suppressed: HTTPS-only egress to Google Safe Browsing (no fixed IP range) via NAT; only nightly with url_scanning_enabled (ADR-0013).
#trivy:ignore:AWS-0104
resource "aws_vpc_security_group_egress_rule" "internet_https" {
  #checkov:skip=CKV_AWS_382:HTTPS-only egress for Google Safe Browsing via NAT, only with url_scanning_enabled (ADR-0013).
  count = var.egress.internet_https ? 1 : 0

  security_group_id = aws_security_group.task.id
  description       = "HTTPS to the internet via NAT (Google Safe Browsing)"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

resource "aws_ecs_task_definition" "this" {
  family                   = local.name
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn

  container_definitions = jsonencode([{
    name                   = var.task
    image                  = var.image
    essential              = true
    user                   = "65532"
    readonlyRootFilesystem = true
    privileged             = false
    stopTimeout            = 30
    environment            = [for k in sort(keys(var.environment)) : { name = k, value = var.environment[k] }]
    secrets                = [for k in sort(keys(var.secrets)) : { name = k, valueFrom = var.secrets[k] }]
    linuxParameters        = { capabilities = { add = [], drop = ["ALL"] } }
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.this.name
        awslogs-region        = local.region
        awslogs-stream-prefix = var.task
        mode                  = "non-blocking"
        max-buffer-size       = "25m"
      }
    }
  }])

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
}

# --- EventBridge Scheduler ---------------------------------------------------------------

data "aws_iam_policy_document" "scheduler_trust" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["scheduler.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }
}

data "aws_iam_policy_document" "scheduler" {
  statement {
    sid       = "RunThisTaskOnThisCluster"
    effect    = "Allow"
    actions   = ["ecs:RunTask"]
    resources = ["arn:${local.partition}:ecs:${local.region}:${local.account_id}:task-definition/${local.name}:*"]

    condition {
      test     = "ArnEquals"
      variable = "ecs:cluster"
      values   = [var.cluster_arn]
    }
  }

  statement {
    sid       = "TagTasksAtLaunch"
    effect    = "Allow"
    actions   = ["ecs:TagResource"]
    resources = ["arn:${local.partition}:ecs:${local.region}:${local.account_id}:task/*"]

    condition {
      test     = "StringEquals"
      variable = "ecs:CreateAction"
      values   = ["RunTask"]
    }
  }

  statement {
    sid       = "PassOnlyThisTasksRoles"
    effect    = "Allow"
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.task.arn, aws_iam_role.execution.arn]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["ecs-tasks.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "scheduler" {
  name                 = "${local.name}-scheduler"
  assume_role_policy   = data.aws_iam_policy_document.scheduler_trust.json
  permissions_boundary = var.permissions_boundary_arn
}

resource "aws_iam_role_policy" "scheduler" {
  name   = "run-task"
  role   = aws_iam_role.scheduler.id
  policy = data.aws_iam_policy_document.scheduler.json
}

resource "aws_scheduler_schedule" "this" {
  name                         = local.name
  description                  = "${var.task} (${var.name_prefix})"
  schedule_expression          = var.schedule_expression
  schedule_expression_timezone = "UTC"
  kms_key_arn                  = var.kms_key_arn

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = var.cluster_arn
    role_arn = aws_iam_role.scheduler.arn

    ecs_parameters {
      task_definition_arn     = aws_ecs_task_definition.this.arn
      task_count              = 1
      platform_version        = "LATEST"
      enable_ecs_managed_tags = true
      propagate_tags          = "TASK_DEFINITION"

      capacity_provider_strategy {
        capacity_provider = var.capacity_provider
        base              = 0
        weight            = 1
      }

      network_configuration {
        subnets          = var.subnet_ids
        security_groups  = concat([aws_security_group.task.id], var.extra_security_group_ids)
        assign_public_ip = false
      }
    }

    retry_policy {
      maximum_event_age_in_seconds = 3600
      maximum_retry_attempts       = 2
    }
  }
}
