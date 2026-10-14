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
  mock_resource "aws_iam_role" {
    defaults = { arn = "arn:aws:iam::123456789012:role/shortener-test-nightly-x" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/shortener/test/nightly" }
  }
  mock_resource "aws_ecs_task_definition" {
    defaults = { arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/shortener-test-nightly:1" }
  }
}

variables {
  name_prefix        = "shortener-test"
  task               = "nightly"
  cluster_arn        = "arn:aws:ecs:us-east-1:123456789012:cluster/shortener-test"
  image              = "123456789012.dkr.ecr.us-east-1.amazonaws.com/shortener-test-nightly:0123456789ab"
  ecr_repository_arn = "arn:aws:ecr:us-east-1:123456789012:repository/shortener-test-nightly"
  vpc_id             = "vpc-0123456789abcdef0"
  subnet_ids         = ["subnet-0aaaaaaaaaaaaaaaa"]
  kms_key_arn        = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  task_policy_json   = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
  egress = {
    s3_prefix_list_id       = "pl-63a5400a"
    dynamodb_prefix_list_id = "pl-02cd2c6b"
    internet_https          = true
  }
}

run "nightly_runs_hardened_on_spot_at_0315_utc" {
  command = apply

  assert {
    condition     = aws_scheduler_schedule.this.schedule_expression == "cron(15 3 * * ? *)" && aws_scheduler_schedule.this.schedule_expression_timezone == "UTC"
    error_message = "Nightly runs at 03:15 UTC (SPEC §8.3)."
  }

  assert {
    condition     = one(one(one(aws_scheduler_schedule.this.target).ecs_parameters).capacity_provider_strategy).capacity_provider == "FARGATE_SPOT"
    error_message = "The idempotent nightly job runs on Fargate Spot."
  }

  assert {
    condition     = one(one(one(aws_scheduler_schedule.this.target).ecs_parameters).network_configuration).assign_public_ip == false
    error_message = "No public IP."
  }

  assert {
    condition = (
      jsondecode(aws_ecs_task_definition.this.container_definitions)[0].readonlyRootFilesystem &&
      jsondecode(aws_ecs_task_definition.this.container_definitions)[0].user == "65532" &&
      one(aws_ecs_task_definition.this.runtime_platform).cpu_architecture == "ARM64"
    )
    error_message = "Read-only root FS, UID 65532, ARM64."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.scheduler.statement :
      s.sid == "RunThisTaskOnThisCluster" && anytrue([for c in s.condition : c.variable == "ecs:cluster"])
    ])
    error_message = "The scheduler may only run this task on this cluster."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.scheduler.statement :
      s.sid == "PassOnlyThisTasksRoles" && anytrue([for c in s.condition : c.variable == "iam:PassedToService" && contains(c.values, "ecs-tasks.amazonaws.com")])
    ])
    error_message = "iam:PassRole is limited to this task's roles and to ECS tasks."
  }

  assert {
    condition     = length(aws_vpc_security_group_egress_rule.internet_https) == 1
    error_message = "With URL scanning the nightly task needs HTTPS egress via NAT."
  }
}
