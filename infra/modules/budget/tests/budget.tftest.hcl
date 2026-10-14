mock_provider "aws" {
  mock_resource "aws_ce_anomaly_monitor" {
    defaults = { arn = "arn:aws:ce::123456789012:anomalymonitor/11111111-2222-3333-4444-555555555555" }
  }
}

variables {
  name_prefix       = "shortener-test"
  cost_tag          = { key = "CostCenter", value = "shortener-test" }
  monthly_limit_usd = 120
  topic_arn         = "arn:aws:sns:us-east-1:123456789012:shortener-test-alerts"
}

run "budget_notifies_50_80_100_actual_and_100_forecast" {
  command = apply

  assert {
    condition = toset([for n in aws_budgets_budget.monthly.notification : "${n.notification_type}-${n.threshold}"]) == toset([
      "ACTUAL-50", "ACTUAL-80", "ACTUAL-100", "FORECASTED-100",
    ])
    error_message = "Budget notifications must be 50/80/100 % actual and 100 % forecast (SPEC §13)."
  }

  assert {
    condition     = alltrue([for n in aws_budgets_budget.monthly.notification : n.subscriber_sns_topic_arns == toset([var.topic_arn]) && n.threshold_type == "PERCENTAGE"])
    error_message = "Every budget notification goes to the alert topic."
  }

  assert {
    condition = (
      aws_budgets_budget.monthly.limit_amount == "120.00" &&
      one(one(aws_budgets_budget.monthly.filter_expression).tags).key == "CostCenter" &&
      one(one(aws_budgets_budget.monthly.filter_expression).tags).values == tolist(["shortener-test"])
    )
    error_message = "The budget must be scoped to the environment's cost allocation tag."
  }
}

run "anomaly_detection_scoped_and_routed_to_sns" {
  command = apply

  assert {
    condition     = jsondecode(aws_ce_anomaly_monitor.this.monitor_specification).Tags == { Key = "CostCenter", Values = ["shortener-test"] }
    error_message = "The anomaly monitor must watch the environment's cost allocation tag."
  }

  assert {
    condition = (
      aws_ce_anomaly_subscription.this.frequency == "IMMEDIATE" &&
      one(aws_ce_anomaly_subscription.this.subscriber).address == var.topic_arn &&
      contains(aws_ce_anomaly_subscription.this.monitor_arn_list, aws_ce_anomaly_monitor.this.arn)
    )
    error_message = "Anomalies must alert the topic immediately."
  }
}

run "rejects_non_positive_budget" {
  command = plan

  variables {
    monthly_limit_usd = 0
  }

  expect_failures = [var.monthly_limit_usd]
}
