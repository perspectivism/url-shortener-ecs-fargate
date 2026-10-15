# Alerting and dashboards (SPEC §13, ADR-0022): one CMK-encrypted SNS topic,
# every §13 alarm wired to it, a log metric filter and an EventBridge rule for
# nightly-job failures, and one dashboard per environment.
#
# Queue, DLQ and DynamoDB alarms exist from phase 1; service alarms and the
# dashboard are added when var.services is set (enable_services = true).

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = data.aws_region.current.region
  ns         = var.metrics_namespace
  services   = var.services != null
  # Placeholder values keep the phase-2 definitions well-typed while
  # var.services is null; nothing is created from them.
  svc = local.services ? var.services : {
    cluster_arn               = ""
    cluster_name              = ""
    service_names             = { redirect = "", api = "", worker = "" }
    alb_arn_suffix            = ""
    target_group_arn_suffixes = { redirect = "", api = "" }
    web_acl_name              = ""
    nightly                   = { schedule_group_name = "", log_group_name = "", task_family = "" }
  }
  emitters = ["redirect", "api", "worker", "nightly"] # EMF dimension Service
}

# --- Alert topic -----------------------------------------------------------------

resource "aws_sns_topic" "alerts" {
  name              = "${var.name_prefix}-alerts"
  kms_master_key_id = var.kms_key_arn
}

data "aws_iam_policy_document" "alerts" {
  statement {
    sid       = "AccountOwnerManagesTopic"
    effect    = "Allow"
    actions   = ["SNS:GetTopicAttributes", "SNS:SetTopicAttributes", "SNS:Subscribe", "SNS:ListSubscriptionsByTopic", "SNS:Publish"]
    resources = [aws_sns_topic.alerts.arn]

    principals {
      type        = "AWS"
      identifiers = ["arn:${local.partition}:iam::${local.account_id}:root"]
    }
  }

  statement {
    sid       = "CloudWatchAlarmsPublish"
    effect    = "Allow"
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.alerts.arn]

    principals {
      type        = "Service"
      identifiers = ["cloudwatch.amazonaws.com"]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${local.partition}:cloudwatch:${local.region}:${local.account_id}:alarm:${var.name_prefix}-*"]
    }
  }

  statement {
    sid       = "BudgetsAndCostAnomalyPublish"
    effect    = "Allow"
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.alerts.arn]

    principals {
      type        = "Service"
      identifiers = ["budgets.amazonaws.com", "costalerts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account_id]
    }
  }

  statement {
    sid       = "EventBridgeRulesPublish"
    effect    = "Allow"
    actions   = ["SNS:Publish"]
    resources = [aws_sns_topic.alerts.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values   = ["arn:${local.partition}:events:${local.region}:${local.account_id}:rule/${var.name_prefix}-*"]
    }
  }

  statement {
    sid       = "DenyInsecureTransport"
    effect    = "Deny"
    actions   = ["SNS:Publish", "SNS:Subscribe"]
    resources = [aws_sns_topic.alerts.arn]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_sns_topic_policy" "alerts" {
  arn    = aws_sns_topic.alerts.arn
  policy = data.aws_iam_policy_document.alerts.json
}

resource "aws_sns_topic_subscription" "email" {
  # count, not for_each: the address is sensitive (it must stay out of plan
  # output in CI logs), and sensitive values cannot be for_each keys.
  count = var.alert_email == null ? 0 : 1

  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

# --- Alarms ------------------------------------------------------------------------
#
# Each alarm is either a single metric (namespace/metric/dimensions/stat) or a
# metric-math expression over `metrics` (ids are map keys). "For N minutes"
# in SPEC §13 means every datapoint in that window breaches.

locals {
  queue_alarms = {
    queue-age = {
      description = "Oldest click event waited more than 5 minutes: the worker is not keeping up or is down (analytics freshness, SPEC N4)."
      namespace   = "AWS/SQS", metric = "ApproximateAgeOfOldestMessage", stat = "Maximum"
      dimensions  = { QueueName = var.queue_name }
      period      = 300, evaluation_periods = 1, threshold = 300
    }
    dlq = {
      description = "Click events reached the dead-letter queue after 5 failed receives; inspect and redrive (runbook)."
      namespace   = "AWS/SQS", metric = "ApproximateNumberOfMessagesVisible", stat = "Maximum"
      dimensions  = { QueueName = var.dlq_name }
      period      = 300, evaluation_periods = 1, threshold = 0
    }
  }

  dynamodb_alarms = {
    for k, table in var.table_names : "dynamodb-throttles-${k}" => {
      description = "DynamoDB table ${table} throttled requests for 5 minutes (maximum-throughput cap or hot partition)."
      expression  = "FILL(r, 0) + FILL(w, 0)"
      metrics = {
        r = { namespace = "AWS/DynamoDB", metric = "ReadThrottleEvents", stat = "Sum", dimensions = { TableName = table } }
        w = { namespace = "AWS/DynamoDB", metric = "WriteThrottleEvents", stat = "Sum", dimensions = { TableName = table } }
      }
      period = 60, evaluation_periods = 5, threshold = 0
    }
  }

  service_alarm_defs = merge(
    {
      redirect-5xx = {
        description = "Redirect target 5xx rate above 1% for 5 minutes (availability SLO, SPEC N2)."
        expression  = "IF(req > 0, 100 * FILL(err, 0) / req, 0)"
        metrics = {
          err = { namespace = "AWS/ApplicationELB", metric = "HTTPCode_Target_5XX_Count", stat = "Sum", dimensions = { LoadBalancer = local.svc.alb_arn_suffix, TargetGroup = local.svc.target_group_arn_suffixes["redirect"] } }
          req = { namespace = "AWS/ApplicationELB", metric = "RequestCount", stat = "Sum", dimensions = { LoadBalancer = local.svc.alb_arn_suffix, TargetGroup = local.svc.target_group_arn_suffixes["redirect"] } }
        }
        period = 60, evaluation_periods = 5, threshold = 1
      }
      redirect-latency-p99 = {
        description = "Redirect p99 TargetResponseTime above 100 ms for 10 minutes (latency SLO, SPEC N1)."
        namespace   = "AWS/ApplicationELB", metric = "TargetResponseTime", stat = "p99"
        dimensions  = { LoadBalancer = local.svc.alb_arn_suffix, TargetGroup = local.svc.target_group_arn_suffixes["redirect"] }
        period      = 60, evaluation_periods = 10, threshold = 0.1
      }
      dropped-clicks = {
        description = "Redirect dropped click events (queue unavailable or buffer full) in every 5-minute period for 15 minutes; redirects still succeed (SPEC N3)."
        namespace   = local.ns, metric = "ClickEventsDropped", stat = "Sum"
        dimensions  = { Service = "redirect" }
        period      = 300, evaluation_periods = 3, threshold = 0
      }
      cache-errors = {
        description = "More than 50 Valkey errors in 5 minutes across services; the services degrade to DynamoDB (runbook: Valkey failure mode)."
        expression  = join(" + ", [for s in local.emitters : "FILL(${s}, 0)"])
        metrics = {
          for s in local.emitters : s => { namespace = local.ns, metric = "CacheErrors", stat = "Sum", dimensions = { Service = s } }
        }
        period = 300, evaluation_periods = 1, threshold = 50
      }
      # Not in the SPEC §13 table: the threat model relies on it (a disabled
      # link may keep redirecting from cache for up to the positive TTL).
      cache-invalidation-failed = {
        description = "A link update or disable could not invalidate Valkey (both attempts failed): the old target may be served until the TTL expires; purge the key (runbook)."
        expression  = join(" + ", [for s in local.emitters : "FILL(${s}, 0)"])
        metrics = {
          for s in local.emitters : s => { namespace = local.ns, metric = "CacheInvalidationFailed", stat = "Sum", dimensions = { Service = s } }
        }
        period = 300, evaluation_periods = 1, threshold = 0
      }
      nightly-schedule-failures = {
        description = "EventBridge Scheduler could not start the nightly task (target error or retries exhausted)."
        expression  = "FILL(err, 0) + FILL(dropped, 0)"
        metrics = {
          err     = { namespace = "AWS/Scheduler", metric = "TargetErrorCount", stat = "Sum", dimensions = { ScheduleGroup = local.svc.nightly.schedule_group_name } }
          dropped = { namespace = "AWS/Scheduler", metric = "InvocationDroppedCount", stat = "Sum", dimensions = { ScheduleGroup = local.svc.nightly.schedule_group_name } }
        }
        period = 300, evaluation_periods = 1, threshold = 0
      }
      nightly-failed = {
        description = "The nightly job logged \"nightly failed\" (log metric filter); see its log group."
        namespace   = local.ns, metric = "NightlyFailures", stat = "Sum", dimensions = {}
        period      = 300, evaluation_periods = 1, threshold = 0
      }
    },
    {
      for s in ["redirect", "api", "worker"] : "running-tasks-${s}" => {
        description = "${s}: fewer running tasks than desired for 5 minutes (crash loop, failed deployment or capacity)."
        expression  = "FILL(desired, 0) - FILL(running, 0)"
        metrics = {
          desired = { namespace = "ECS/ContainerInsights", metric = "DesiredTaskCount", stat = "Maximum", dimensions = { ClusterName = local.svc.cluster_name, ServiceName = local.svc.service_names[s] } }
          running = { namespace = "ECS/ContainerInsights", metric = "RunningTaskCount", stat = "Minimum", dimensions = { ClusterName = local.svc.cluster_name, ServiceName = local.svc.service_names[s] } }
        }
        period = 60, evaluation_periods = 5, threshold = 0
      }
    },
  )

  service_alarms = { for k, v in local.service_alarm_defs : k => v if local.services }

  alarms = merge(local.queue_alarms, local.dynamodb_alarms, local.service_alarms)
}

resource "aws_cloudwatch_metric_alarm" "this" {
  for_each = local.alarms

  alarm_name          = "${var.name_prefix}-${each.key}"
  alarm_description   = each.value.description
  comparison_operator = "GreaterThanThreshold"
  threshold           = each.value.threshold
  period              = try(each.value.expression, null) == null ? each.value.period : null
  evaluation_periods  = each.value.evaluation_periods
  datapoints_to_alarm = each.value.evaluation_periods
  treat_missing_data  = "notBreaching"
  alarm_actions       = [aws_sns_topic.alerts.arn]
  ok_actions          = [aws_sns_topic.alerts.arn]

  # Single-metric alarms.
  namespace          = try(each.value.expression, null) == null ? each.value.namespace : null
  metric_name        = try(each.value.expression, null) == null ? each.value.metric : null
  dimensions         = try(each.value.expression, null) == null ? each.value.dimensions : null
  statistic          = try(each.value.expression, null) == null && !startswith(try(each.value.stat, ""), "p") ? each.value.stat : null
  extended_statistic = try(each.value.expression, null) == null && startswith(try(each.value.stat, ""), "p") ? each.value.stat : null

  # Metric-math alarms.
  dynamic "metric_query" {
    for_each = try(each.value.expression, null) == null ? {} : { e = each.value.expression }

    content {
      id          = "e"
      expression  = metric_query.value
      label       = each.key
      return_data = true
    }
  }

  dynamic "metric_query" {
    for_each = try(each.value.metrics, {})

    content {
      id = metric_query.key

      metric {
        namespace   = metric_query.value.namespace
        metric_name = metric_query.value.metric
        dimensions  = metric_query.value.dimensions
        stat        = metric_query.value.stat
        period      = each.value.period
      }
    }
  }

  depends_on = [aws_sns_topic_policy.alerts]
}

# --- Nightly job failures ----------------------------------------------------------

# The job logs {"msg":"nightly failed",...} before exiting non-zero.
resource "aws_cloudwatch_log_metric_filter" "nightly_failed" {
  count = local.services ? 1 : 0

  name           = "${var.name_prefix}-nightly-failed"
  log_group_name = local.svc.nightly.log_group_name
  pattern        = "{ $.msg = \"nightly failed\" }"

  metric_transformation {
    name      = "NightlyFailures"
    namespace = local.ns
    value     = "1"
    unit      = "Count"
  }
}

# Catches what the log filter cannot: crashes, OOM kills and image pull
# failures that stop the task with a non-zero exit code.
resource "aws_cloudwatch_event_rule" "nightly_exit" {
  count = local.services ? 1 : 0

  name        = "${var.name_prefix}-nightly-nonzero-exit"
  description = "Nightly task stopped with a non-zero exit code."
  event_pattern = jsonencode({
    source        = ["aws.ecs"]
    "detail-type" = ["ECS Task State Change"]
    detail = {
      clusterArn = [local.svc.cluster_arn]
      lastStatus = ["STOPPED"]
      group      = ["family:${local.svc.nightly.task_family}"]
      containers = { exitCode = [{ "anything-but" = 0 }] }
    }
  })
}

resource "aws_cloudwatch_event_target" "nightly_exit" {
  count = local.services ? 1 : 0

  rule = aws_cloudwatch_event_rule.nightly_exit[0].name
  arn  = aws_sns_topic.alerts.arn

  input_transformer {
    input_paths = {
      task   = "$.detail.taskArn"
      reason = "$.detail.stoppedReason"
    }
    input_template = "\"${var.name_prefix}: nightly task <task> stopped with a non-zero exit code (<reason>). Runbook: nightly job failed.\""
  }

  depends_on = [aws_sns_topic_policy.alerts]
}
