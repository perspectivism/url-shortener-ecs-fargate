# Reusable ECS service (SPEC §12.4): hardened Fargate task definition, one task
# role and one execution role, its own security group, rolling deployment with
# the circuit breaker, and autoscaling.

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

locals {
  name       = "${var.name_prefix}-${var.service}"
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region

  container_definition = {
    name      = var.service
    image     = var.image
    essential = true
    user      = "65532"
    # AWS: ECS Exec requires a writable root filesystem (break-glass only).
    readonlyRootFilesystem = !var.enable_execute_command
    privileged             = false
    stopTimeout            = 30
    portMappings           = [{ containerPort = var.container_port, protocol = "tcp" }]
    environment            = [for k in sort(keys(var.environment)) : { name = k, value = var.environment[k] }]
    secrets                = [for k in sort(keys(var.secrets)) : { name = k, valueFrom = var.secrets[k] }]
    linuxParameters = {
      capabilities       = { add = [], drop = ["ALL"] }
      initProcessEnabled = var.enable_execute_command
    }
    healthCheck = {
      command     = ["CMD", "/app", "healthcheck"]
      interval    = 15
      timeout     = 5
      retries     = 3
      startPeriod = 10
    }
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.this.name
        awslogs-region        = local.region
        awslogs-stream-prefix = var.service
        mode                  = "non-blocking"
        max-buffer-size       = "25m"
      }
    }
  }
}

resource "aws_cloudwatch_log_group" "this" {
  #checkov:skip=CKV_AWS_338:Retention is 14 days (dev) / 90 days (prod) per SPEC §12.6.
  name              = "/${replace(var.name_prefix, "-", "/")}/${var.service}"
  retention_in_days = var.log_retention_days
  kms_key_id        = var.kms_key_arn
}

# --- IAM ---------------------------------------------------------------------------

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

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${local.partition}:ecs:${local.region}:${local.account_id}:*"]
    }
  }
}

data "aws_iam_policy_document" "execution" {
  statement {
    sid       = "ECRAuth"
    effect    = "Allow"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"] # AWS: GetAuthorizationToken has no resource-level permissions.
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

data "aws_iam_policy_document" "exec" {
  count = var.enable_execute_command ? 1 : 0

  statement {
    sid       = "ECSExecChannels"
    effect    = "Allow"
    actions   = ["ssmmessages:CreateControlChannel", "ssmmessages:CreateDataChannel", "ssmmessages:OpenControlChannel", "ssmmessages:OpenDataChannel"]
    resources = ["*"] # AWS: ssmmessages actions have no resource-level permissions.
  }

  statement {
    sid       = "ECSExecSessionKey"
    effect    = "Allow"
    actions   = ["kms:Decrypt"]
    resources = [var.kms_key_arn]
  }
}

resource "aws_iam_role_policy" "exec" {
  count = var.enable_execute_command ? 1 : 0

  name   = "ecs-exec"
  role   = aws_iam_role.task.id
  policy = data.aws_iam_policy_document.exec[0].json
}

# --- Network -------------------------------------------------------------------------

resource "aws_security_group" "task" {
  name        = "${local.name}-task"
  description = "${var.service} tasks"
  vpc_id      = var.vpc_id
  tags        = { Name = "${local.name}-task" }
}

resource "aws_vpc_security_group_ingress_rule" "from_alb" {
  count = var.load_balancer == null ? 0 : 1

  security_group_id            = aws_security_group.task.id
  description                  = "Container port from the ALB only"
  ip_protocol                  = "tcp"
  from_port                    = var.container_port
  to_port                      = var.container_port
  referenced_security_group_id = var.load_balancer.alb_security_group_id
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

# Trivy AWS-0104 suppressed: HTTPS-only egress to Google Safe Browsing (no fixed IP range) via NAT; only api with url_scanning_enabled (ADR-0013).
#trivy:ignore:AWS-0104
resource "aws_vpc_security_group_egress_rule" "internet_https" {
  #checkov:skip=CKV_AWS_382:HTTPS-only egress for Google Safe Browsing via NAT; only api/nightly with url_scanning_enabled (ADR-0013).
  count = var.egress.internet_https ? 1 : 0

  security_group_id = aws_security_group.task.id
  description       = "HTTPS to the internet via NAT (Google Safe Browsing)"
  ip_protocol       = "tcp"
  from_port         = 443
  to_port           = 443
  cidr_ipv4         = "0.0.0.0/0"
}

# --- Task definition and service ------------------------------------------------------

resource "aws_ecs_task_definition" "this" {
  family                   = local.name
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = var.cpu
  memory                   = var.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = aws_iam_role.task.arn
  container_definitions    = jsonencode([local.container_definition])

  runtime_platform {
    cpu_architecture        = "ARM64"
    operating_system_family = "LINUX"
  }
}

# ECS rejects a service whose target group is not yet attached to a listener;
# this node orders service creation after that attachment.
resource "terraform_data" "routing_ready" {
  input = var.load_balancer == null ? null : var.load_balancer.routing_ready
}

resource "aws_ecs_service" "this" {
  name                               = var.service
  cluster                            = var.cluster_arn
  task_definition                    = aws_ecs_task_definition.this.arn
  desired_count                      = var.desired_count
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200
  enable_execute_command             = var.enable_execute_command
  propagate_tags                     = "SERVICE"
  enable_ecs_managed_tags            = true
  wait_for_steady_state              = var.wait_for_steady_state
  health_check_grace_period_seconds  = var.load_balancer == null ? null : var.health_check_grace_period_seconds
  availability_zone_rebalancing      = "ENABLED"

  dynamic "capacity_provider_strategy" {
    for_each = var.capacity_provider_strategy
    content {
      capacity_provider = capacity_provider_strategy.value.capacity_provider
      base              = capacity_provider_strategy.value.base
      weight            = capacity_provider_strategy.value.weight
    }
  }

  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    subnets          = var.subnet_ids
    security_groups  = concat([aws_security_group.task.id], var.extra_security_group_ids)
    assign_public_ip = false
  }

  dynamic "load_balancer" {
    for_each = var.load_balancer == null ? [] : [var.load_balancer]
    content {
      target_group_arn = load_balancer.value.target_group_arn
      container_name   = var.service
      container_port   = var.container_port
    }
  }

  lifecycle {
    ignore_changes = [desired_count] # owned by Application Auto Scaling
  }

  depends_on = [terraform_data.routing_ready]
}

# --- Autoscaling -------------------------------------------------------------------------

resource "aws_appautoscaling_target" "this" {
  service_namespace  = "ecs"
  resource_id        = "service/${var.cluster_name}/${aws_ecs_service.this.name}"
  scalable_dimension = "ecs:service:DesiredCount"
  min_capacity       = var.min_count
  max_capacity       = var.max_count
}

resource "aws_appautoscaling_policy" "cpu" {
  count = var.cpu_target_percent == null ? 0 : 1

  name               = "${local.name}-cpu"
  policy_type        = "TargetTrackingScaling"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension

  target_tracking_scaling_policy_configuration {
    target_value       = var.cpu_target_percent
    scale_in_cooldown  = 120
    scale_out_cooldown = 60

    predefined_metric_specification {
      predefined_metric_type = "ECSServiceAverageCPUUtilization"
    }
  }
}

resource "aws_appautoscaling_policy" "alb_requests" {
  count = var.alb_requests_per_target == null || var.load_balancer == null ? 0 : 1

  name               = "${local.name}-alb-requests"
  policy_type        = "TargetTrackingScaling"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension

  target_tracking_scaling_policy_configuration {
    target_value       = var.alb_requests_per_target
    scale_in_cooldown  = 120
    scale_out_cooldown = 60

    predefined_metric_specification {
      predefined_metric_type = "ALBRequestCountPerTarget"
      resource_label         = var.load_balancer.alb_resource_label
    }
  }
}

# Queue-backlog step scaling: works from zero tasks (target tracking on
# backlog-per-task cannot, because it divides by the running task count).
resource "aws_appautoscaling_policy" "queue_out" {
  count = var.sqs_queue_name == null ? 0 : 1

  name               = "${local.name}-queue-out"
  policy_type        = "StepScaling"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension

  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    cooldown                = 60
    metric_aggregation_type = "Maximum"

    step_adjustment {
      metric_interval_lower_bound = 0
      metric_interval_upper_bound = 1000
      scaling_adjustment          = 1
    }

    step_adjustment {
      metric_interval_lower_bound = 1000
      metric_interval_upper_bound = 10000
      scaling_adjustment          = 2
    }

    step_adjustment {
      metric_interval_lower_bound = 10000
      scaling_adjustment          = 4
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "queue_out" {
  count = var.sqs_queue_name == null ? 0 : 1

  alarm_name          = "${local.name}-queue-backlog-scale-out"
  alarm_description   = "Scaling trigger: click events are waiting"
  namespace           = "AWS/SQS"
  metric_name         = "ApproximateNumberOfMessagesVisible"
  dimensions          = { QueueName = var.sqs_queue_name }
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_appautoscaling_policy.queue_out[0].arn]
}

resource "aws_appautoscaling_policy" "queue_in" {
  count = var.sqs_queue_name == null ? 0 : 1

  name               = "${local.name}-queue-in"
  policy_type        = "StepScaling"
  service_namespace  = aws_appautoscaling_target.this.service_namespace
  resource_id        = aws_appautoscaling_target.this.resource_id
  scalable_dimension = aws_appautoscaling_target.this.scalable_dimension

  step_scaling_policy_configuration {
    adjustment_type         = "ChangeInCapacity"
    cooldown                = 300
    metric_aggregation_type = "Maximum"

    step_adjustment {
      metric_interval_upper_bound = 0
      scaling_adjustment          = -1
    }
  }
}

resource "aws_cloudwatch_metric_alarm" "queue_in" {
  count = var.sqs_queue_name == null ? 0 : 1

  alarm_name          = "${local.name}-queue-idle-scale-in"
  alarm_description   = "Scaling trigger: queue fully drained for 10 minutes"
  comparison_operator = "LessThanOrEqualToThreshold"
  threshold           = 0
  evaluation_periods  = 10
  treat_missing_data  = "breaching"
  alarm_actions       = [aws_appautoscaling_policy.queue_in[0].arn]

  metric_query {
    id          = "backlog"
    expression  = "visible + inflight"
    label       = "Messages visible + in flight"
    return_data = true
  }

  metric_query {
    id = "visible"
    metric {
      namespace   = "AWS/SQS"
      metric_name = "ApproximateNumberOfMessagesVisible"
      dimensions  = { QueueName = var.sqs_queue_name }
      period      = 60
      stat        = "Maximum"
    }
  }

  metric_query {
    id = "inflight"
    metric {
      namespace   = "AWS/SQS"
      metric_name = "ApproximateNumberOfMessagesNotVisible"
      dimensions  = { QueueName = var.sqs_queue_name }
      period      = 60
      stat        = "Maximum"
    }
  }
}
