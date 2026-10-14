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
    defaults = { arn = "arn:aws:iam::123456789012:role/shortener-test-redirect-x" }
  }
  mock_resource "aws_cloudwatch_log_group" {
    defaults = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/shortener/test/redirect" }
  }
  mock_resource "aws_ecs_task_definition" {
    defaults = { arn = "arn:aws:ecs:us-east-1:123456789012:task-definition/shortener-test-redirect:1" }
  }
  mock_resource "aws_appautoscaling_policy" {
    defaults = { arn = "arn:aws:autoscaling:us-east-1:123456789012:scalingPolicy:00000000-0000-0000-0000-000000000000:resource/ecs/service/x/y:policyName/p" }
  }
}

variables {
  name_prefix        = "shortener-test"
  service            = "redirect"
  cluster_arn        = "arn:aws:ecs:us-east-1:123456789012:cluster/shortener-test"
  cluster_name       = "shortener-test"
  image              = "123456789012.dkr.ecr.us-east-1.amazonaws.com/shortener-test-redirect:0123456789ab"
  ecr_repository_arn = "arn:aws:ecr:us-east-1:123456789012:repository/shortener-test-redirect"
  vpc_id             = "vpc-0123456789abcdef0"
  subnet_ids         = ["subnet-0aaaaaaaaaaaaaaaa", "subnet-0bbbbbbbbbbbbbbbb"]
  kms_key_arn        = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
  task_policy_json   = "{\"Version\":\"2012-10-17\",\"Statement\":[]}"
  environment        = { APP_ENV = "test", LINKS_TABLE = "shortener-test-links" }
  secrets            = { VISITOR_HASH_SALT = "arn:aws:secretsmanager:us-east-1:123456789012:secret:shortener/test/visitor-hash-salt-AbCdEf" }
  egress = {
    s3_prefix_list_id       = "pl-63a5400a"
    dynamodb_prefix_list_id = "pl-02cd2c6b"
    internet_https          = false
  }
  load_balancer = {
    target_group_arn      = "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/shortener-test-redirect/0123456789abcdef"
    alb_security_group_id = "sg-0a1b2c3d4e5f60718"
    alb_resource_label    = "app/shortener-test/0123456789abcdef/targetgroup/shortener-test-redirect/0123456789abcdef"
    routing_ready         = "arn:aws:elasticloadbalancing:us-east-1:123456789012:listener/app/shortener-test/0123456789abcdef/0123456789abcdef"
  }
  alb_requests_per_target = 500
}

run "task_definition_is_hardened" {
  command = plan

  assert {
    condition = (
      one(aws_ecs_task_definition.this.runtime_platform).cpu_architecture == "ARM64" &&
      one(aws_ecs_task_definition.this.runtime_platform).operating_system_family == "LINUX" &&
      aws_ecs_task_definition.this.network_mode == "awsvpc"
    )
    error_message = "Tasks must be ARM64 Linux with awsvpc networking."
  }

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].readonlyRootFilesystem == true
    error_message = "Root filesystem must be read-only."
  }

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].user == "65532"
    error_message = "Containers must run as the non-root UID 65532."
  }

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].linuxParameters.capabilities.drop == ["ALL"] && jsondecode(aws_ecs_task_definition.this.container_definitions)[0].privileged == false
    error_message = "All capabilities dropped; never privileged."
  }

  assert {
    condition = (
      jsondecode(aws_ecs_task_definition.this.container_definitions)[0].healthCheck.command == ["CMD", "/app", "healthcheck"] &&
      jsondecode(aws_ecs_task_definition.this.container_definitions)[0].stopTimeout == 30 &&
      jsondecode(aws_ecs_task_definition.this.container_definitions)[0].logConfiguration.options.mode == "non-blocking"
    )
    error_message = "Binary health check, 30 s stopTimeout and non-blocking awslogs are required."
  }

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].secrets == [{ name = "VISITOR_HASH_SALT", valueFrom = "arn:aws:secretsmanager:us-east-1:123456789012:secret:shortener/test/visitor-hash-salt-AbCdEf" }]
    error_message = "Secrets must be injected by ARN (valueFrom), never as plain environment values."
  }

  assert {
    condition     = !contains([for e in jsondecode(aws_ecs_task_definition.this.container_definitions)[0].environment : e.name], "VISITOR_HASH_SALT")
    error_message = "A secret must never appear in the plain environment."
  }
}

run "service_deploys_safely_and_privately" {
  command = plan

  assert {
    condition     = one(aws_ecs_service.this.deployment_circuit_breaker).enable && one(aws_ecs_service.this.deployment_circuit_breaker).rollback
    error_message = "Deployment circuit breaker with rollback is required."
  }

  assert {
    condition     = aws_ecs_service.this.deployment_minimum_healthy_percent == 100 && aws_ecs_service.this.deployment_maximum_percent == 200
    error_message = "Rolling deploys keep full capacity (100/200)."
  }

  assert {
    condition     = one(aws_ecs_service.this.network_configuration).assign_public_ip == false
    error_message = "Tasks never get public IPs."
  }

  assert {
    condition     = !aws_ecs_service.this.enable_execute_command
    error_message = "ECS Exec is off by default."
  }

  assert {
    condition     = length(aws_vpc_security_group_ingress_rule.from_alb) == 1 && aws_vpc_security_group_ingress_rule.from_alb[0].referenced_security_group_id == "sg-0a1b2c3d4e5f60718" && aws_vpc_security_group_ingress_rule.from_alb[0].cidr_ipv4 == null
    error_message = "Task ingress must come only from the ALB security group."
  }

  assert {
    condition     = length(aws_vpc_security_group_egress_rule.internet_https) == 0
    error_message = "No internet egress unless explicitly needed for URL scanning."
  }

  assert {
    condition     = length(aws_appautoscaling_policy.alb_requests) == 1 && length(aws_appautoscaling_policy.cpu) == 1
    error_message = "Redirect scales on ALB requests per target and CPU."
  }
}

run "execution_role_reads_only_its_secrets" {
  command = plan

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.execution.statement :
      s.sid == "ReadOwnSecrets" && s.resources == toset(["arn:aws:secretsmanager:us-east-1:123456789012:secret:shortener/test/visitor-hash-salt-AbCdEf"])
    ])
    error_message = "The execution role may read exactly this service's secrets."
  }

  assert {
    condition = anytrue([
      for s in data.aws_iam_policy_document.execution.statement :
      s.sid == "DecryptSecretsViaSecretsManager" && anytrue([for c in s.condition : c.variable == "kms:ViaService" && contains(c.values, "secretsmanager.us-east-1.amazonaws.com")])
    ])
    error_message = "Secret decryption must be limited to Secrets Manager (kms:ViaService)."
  }
}

run "worker_scales_on_queue_without_load_balancer" {
  command = plan

  variables {
    service                 = "worker"
    load_balancer           = null
    alb_requests_per_target = null
    cpu_target_percent      = null
    secrets                 = {}
    min_count               = 0
    sqs_queue_name          = "shortener-test-click-events"
    capacity_provider_strategy = [
      { capacity_provider = "FARGATE_SPOT", base = 0, weight = 4 },
      { capacity_provider = "FARGATE", base = 0, weight = 1 },
    ]
  }

  assert {
    condition     = length(aws_vpc_security_group_ingress_rule.from_alb) == 0 && length(aws_ecs_service.this.load_balancer) == 0
    error_message = "The worker has no ingress at all."
  }

  assert {
    condition     = length(aws_appautoscaling_policy.queue_out) == 1 && length(aws_appautoscaling_policy.queue_in) == 1 && aws_appautoscaling_target.this.min_capacity == 0
    error_message = "The worker scales on queue backlog, down to zero."
  }

  assert {
    condition     = length(data.aws_iam_policy_document.execution.statement) == 3
    error_message = "Without secrets the execution role has no Secrets Manager access."
  }
}

run "break_glass_exec_relaxes_read_only_fs" {
  command = plan

  variables {
    enable_execute_command = true
  }

  assert {
    condition     = jsondecode(aws_ecs_task_definition.this.container_definitions)[0].readonlyRootFilesystem == false && length(aws_iam_role_policy.exec) == 1
    error_message = "ECS Exec needs a writable root filesystem and the ssmmessages policy."
  }
}

run "rejects_latest_tag" {
  command = plan

  variables {
    image = "123456789012.dkr.ecr.us-east-1.amazonaws.com/shortener-test-redirect:latest"
  }

  expect_failures = [var.image]
}
