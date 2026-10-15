# Cost guardrails per environment (SPEC §13, ADR-0022): an AWS Budget with
# 50/80/100 % actual and 100 % forecast notifications, and a Cost Anomaly
# Detection monitor with immediate alerts, both scoped to this environment's
# cost allocation tag and delivered to the alert topic.

locals {
  budget_notifications = {
    actual-50    = { type = "ACTUAL", threshold = 50 }
    actual-80    = { type = "ACTUAL", threshold = 80 }
    actual-100   = { type = "ACTUAL", threshold = 100 }
    forecast-100 = { type = "FORECASTED", threshold = 100 }
  }
}

resource "aws_budgets_budget" "monthly" {
  name         = "${var.name_prefix}-monthly"
  budget_type  = "COST"
  limit_amount = format("%.2f", var.monthly_limit_usd)
  limit_unit   = "USD"
  time_unit    = "MONTHLY"
  metrics      = ["UnblendedCost"]

  filter_expression {
    tags {
      key    = var.cost_tag.key
      values = [var.cost_tag.value]
    }
  }

  dynamic "notification" {
    for_each = local.budget_notifications

    content {
      notification_type         = notification.value.type
      comparison_operator       = "GREATER_THAN"
      threshold                 = notification.value.threshold
      threshold_type            = "PERCENTAGE"
      subscriber_sns_topic_arns = [var.topic_arn]
    }
  }
}

# Customer-managed tag monitor: one tag key, specific values (AWS limit: 10).
# Tag monitors can only be created in a management (or standalone) account;
# set anomaly_monitor_enabled = false in an AWS Organizations member account.
resource "aws_ce_anomaly_monitor" "this" {
  count = var.anomaly_monitor_enabled ? 1 : 0

  name         = "${var.name_prefix}-spend"
  monitor_type = "CUSTOM"

  monitor_specification = jsonencode({
    Tags = {
      Key    = var.cost_tag.key
      Values = [var.cost_tag.value]
    }
  })
}

resource "aws_ce_anomaly_subscription" "this" {
  count = var.anomaly_monitor_enabled ? 1 : 0

  name             = "${var.name_prefix}-spend-alerts"
  frequency        = "IMMEDIATE" # individual alerts; requires SNS
  monitor_arn_list = [aws_ce_anomaly_monitor.this[0].arn]

  subscriber {
    type    = "SNS"
    address = var.topic_arn
  }

  threshold_expression {
    dimension {
      key           = "ANOMALY_TOTAL_IMPACT_ABSOLUTE"
      match_options = ["GREATER_THAN_OR_EQUAL"]
      values        = [format("%.2f", var.anomaly_threshold_usd)]
    }
  }
}
