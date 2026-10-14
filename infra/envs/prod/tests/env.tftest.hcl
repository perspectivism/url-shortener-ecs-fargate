# Offline wiring check for the environment root (mock provider, no AWS calls):
# both deploy phases must plan, and the M6 alerting must be complete.
# Identical in envs/dev and envs/prod; values come from each root's defaults.

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
  mock_data "aws_availability_zones" {
    defaults = {
      names    = ["us-east-1a", "us-east-1b", "us-east-1d"]
      zone_ids = ["use1-az1", "use1-az2", "use1-az4"]
    }
  }
  mock_data "aws_iam_policy_document" {
    defaults = { json = "{\"Version\":\"2012-10-17\",\"Statement\":[]}" }
  }
  mock_data "aws_ec2_managed_prefix_list" {
    defaults = { id = "pl-3b927c52" }
  }
  mock_data "aws_cloudfront_cache_policy" {
    defaults = { id = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad" }
  }
  mock_data "aws_cloudfront_response_headers_policy" {
    defaults = { id = "67f7725c-6f97-4210-82d7-5512b31e9d03" }
  }
}

variables {
  repository = "example/url-shortener-ecs-fargate"
  owner      = "platform-team"
}

run "phase1_foundation_with_data_alarms_and_budget" {
  command = plan

  assert {
    condition     = toset(keys(module.observability.alarm_names)) == toset(["queue-age", "dlq", "dynamodb-throttles-links", "dynamodb-throttles-analytics"])
    error_message = "Phase 1 has the data-tier alarms only."
  }

  assert {
    condition     = module.observability.dashboard_name == null
    error_message = "The dashboard follows enable_services."
  }

  assert {
    condition     = module.budget.budget_name == "shortener-${var.environment}-monthly"
    error_message = "Every environment has a budget."
  }
}

run "phase2_services_with_every_spec_alarm" {
  command = plan

  variables {
    enable_services = true
    image_tag       = "3b2d556f3a67"
  }

  assert {
    condition = length(setsubtract([
      "redirect-5xx", "redirect-latency-p99",
      "running-tasks-redirect", "running-tasks-api", "running-tasks-worker",
      "queue-age", "dlq", "dropped-clicks",
      "dynamodb-throttles-links", "dynamodb-throttles-analytics",
      "cache-errors", "cache-invalidation-failed", "nightly-schedule-failures", "nightly-failed",
    ], keys(module.observability.alarm_names))) == 0
    error_message = "Every SPEC §13 alarm must exist in phase 2."
  }

  assert {
    condition     = module.observability.dashboard_name == "shortener-${var.environment}"
    error_message = "Phase 2 has the dashboard."
  }
}
