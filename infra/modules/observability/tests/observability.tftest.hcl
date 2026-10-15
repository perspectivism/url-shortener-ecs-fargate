mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "us-east-1" }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_resource "aws_sns_topic" {
    defaults = { arn = "arn:aws:sns:us-east-1:123456789012:shortener-test-alerts" }
  }
  mock_resource "aws_cloudwatch_metric_alarm" {
    defaults = { arn = "arn:aws:cloudwatch:us-east-1:123456789012:alarm:shortener-test-mock" }
  }
}

variables {
  name_prefix       = "shortener-test"
  metrics_namespace = "Shortener/test"
  kms_key_arn       = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  queue_name        = "shortener-test-clicks"
  dlq_name          = "shortener-test-clicks-dlq"
  table_names       = { links = "shortener-test-links", analytics = "shortener-test-analytics" }
  alert_email       = "alerts@example.com"
}

run "phase1_data_alarms_only" {
  command = apply

  assert {
    condition     = toset(keys(aws_cloudwatch_metric_alarm.this)) == toset(["queue-age", "dlq", "dynamodb-throttles-links", "dynamodb-throttles-analytics"])
    error_message = "Without services only the queue, DLQ and DynamoDB alarms exist."
  }

  assert {
    condition     = length(aws_cloudwatch_dashboard.this) == 0 && length(aws_cloudwatch_event_rule.nightly_exit) == 0
    error_message = "The dashboard and nightly rule need services."
  }

  assert {
    condition     = aws_sns_topic.alerts.kms_master_key_id == var.kms_key_arn
    error_message = "The alert topic must be CMK-encrypted."
  }

  assert {
    condition     = aws_sns_topic_subscription.email[0].protocol == "email" && nonsensitive(aws_sns_topic_subscription.email[0].endpoint) == "alerts@example.com"
    error_message = "alert_email must be subscribed."
  }
}

run "phase2_every_spec_alarm_notifies_sns" {
  command = apply

  variables {
    services = {
      cluster_arn               = "arn:aws:ecs:us-east-1:123456789012:cluster/shortener-test"
      cluster_name              = "shortener-test"
      service_names             = { redirect = "shortener-test-redirect", api = "shortener-test-api", worker = "shortener-test-worker" }
      alb_arn_suffix            = "app/shortener-test/0123456789abcdef"
      target_group_arn_suffixes = { redirect = "targetgroup/shortener-test-redirect/0123456789abcdef", api = "targetgroup/shortener-test-api/0123456789abcdef" }
      web_acl_name              = "shortener-test"
      nightly = {
        schedule_group_name = "shortener-test-nightly"
        log_group_name      = "/shortener/test/nightly"
        task_family         = "shortener-test-nightly"
      }
    }
  }

  # SPEC §13 alarm table: 5xx, latency, running tasks (per service), queue age,
  # DLQ, dropped clicks, DynamoDB, cache, nightly. Budgets live in the budget module.
  assert {
    condition = length(setsubtract([
      "redirect-5xx", "redirect-latency-p99",
      "running-tasks-redirect", "running-tasks-api", "running-tasks-worker",
      "queue-age", "dlq", "dropped-clicks",
      "dynamodb-throttles-links", "dynamodb-throttles-analytics",
      "cache-errors", "cache-invalidation-failed", "nightly-schedule-failures", "nightly-failed",
    ], keys(aws_cloudwatch_metric_alarm.this))) == 0
    error_message = "Every SPEC §13 alarm must exist."
  }

  assert {
    condition = alltrue([
      for a in aws_cloudwatch_metric_alarm.this :
      a.alarm_actions == toset([aws_sns_topic.alerts.arn]) && a.ok_actions == toset([aws_sns_topic.alerts.arn])
    ])
    error_message = "Every alarm must notify the SNS topic on ALARM and OK."
  }

  assert {
    condition     = alltrue([for a in aws_cloudwatch_metric_alarm.this : a.treat_missing_data == "notBreaching" && a.datapoints_to_alarm == a.evaluation_periods])
    error_message = "Alarms fire only when every datapoint in the window breaches, and missing data is not an outage signal."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.this["redirect-latency-p99"].extended_statistic == "p99" &&
      aws_cloudwatch_metric_alarm.this["redirect-latency-p99"].threshold == 0.1 &&
      aws_cloudwatch_metric_alarm.this["redirect-latency-p99"].evaluation_periods == 10 &&
      aws_cloudwatch_metric_alarm.this["redirect-latency-p99"].period == 60
    )
    error_message = "Redirect latency: p99 > 100 ms for 10 minutes."
  }

  assert {
    condition = (
      aws_cloudwatch_metric_alarm.this["redirect-5xx"].threshold == 1 &&
      length(aws_cloudwatch_metric_alarm.this["redirect-5xx"].metric_query) == 3
    )
    error_message = "Redirect 5xx is a rate (metric math) above 1 %."
  }

  assert {
    condition     = aws_cloudwatch_metric_alarm.this["dropped-clicks"].period * aws_cloudwatch_metric_alarm.this["dropped-clicks"].evaluation_periods == 900
    error_message = "Dropped clicks must breach for 15 minutes."
  }

  assert {
    condition     = aws_cloudwatch_log_metric_filter.nightly_failed[0].pattern == "{ $.msg = \"nightly failed\" }"
    error_message = "The nightly log metric filter must match the job's failure log line."
  }

  assert {
    condition = (
      aws_cloudwatch_event_target.nightly_exit[0].arn == aws_sns_topic.alerts.arn &&
      jsondecode(aws_cloudwatch_event_rule.nightly_exit[0].event_pattern).detail.group == ["family:shortener-test-nightly"]
    )
    error_message = "Non-zero nightly exits must notify SNS."
  }

  assert {
    condition = alltrue([
      for m in ["RedirectLatencyMs", "CacheHit", "CacheMiss", "NegativeCacheHit", "CacheErrors", "ClickEventsEnqueued", "ClickEventsDropped", "LinksCreated", "URLScanFlagged", "URLScanUnavailable", "URLScanSkipped", "LinksDisabled", "WorkerEventsProcessed", "WorkerDuplicateEvents", "KeyBlocksAllocated", "CacheInvalidationFailed", "RateLimited"] :
      strcontains(aws_cloudwatch_dashboard.this[0].dashboard_body, m)
    ])
    error_message = "Every SPEC §13 EMF metric must be on the dashboard."
  }

  assert {
    condition = alltrue([
      for w in ["DynamoDB", "WAF", "Cost", "Queue", "cache", "running tasks", "latency"] :
      strcontains(aws_cloudwatch_dashboard.this[0].dashboard_body, w)
    ])
    error_message = "The dashboard needs golden signals, queue, DynamoDB, WAF and cost widgets."
  }
}

run "topic_policy_is_scoped_and_tls_only" {
  command = plan

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.alerts.statement :
      s.effect == "Deny" && anytrue([for c in s.condition : c.variable == "aws:SecureTransport" && contains(c.values, "false")])
    ])
    error_message = "The topic must deny non-TLS publishing."
  }

  assert {
    condition = alltrue([
      for s in data.aws_iam_policy_document.alerts.statement :
      length(s.condition) > 0 if s.effect == "Allow" && anytrue([for p in s.principals : p.type == "Service"])
    ])
    error_message = "Service principals must be scoped by source account or ARN."
  }
}

run "rejects_bad_email" {
  command = plan

  variables {
    alert_email = "not-an-email"
  }

  expect_failures = [var.alert_email]
}
