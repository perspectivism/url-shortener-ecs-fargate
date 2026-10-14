mock_provider "aws" {}

variables {
  name_prefix = "shortener-test"
  kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/11111111-2222-3333-4444-555555555555"
}

run "cluster_has_spot_insights_and_encrypted_exec_audit" {
  command = plan

  assert {
    condition     = toset(aws_ecs_cluster_capacity_providers.this.capacity_providers) == toset(["FARGATE", "FARGATE_SPOT"])
    error_message = "Both Fargate and Fargate Spot capacity providers are required."
  }

  assert {
    condition     = one(aws_ecs_cluster.this.setting).value == "enabled"
    error_message = "Container Insights must be on."
  }

  assert {
    condition = (
      one(one(aws_ecs_cluster.this.configuration).execute_command_configuration).kms_key_id == var.kms_key_arn &&
      one(one(one(aws_ecs_cluster.this.configuration).execute_command_configuration).log_configuration).cloud_watch_encryption_enabled
    )
    error_message = "ECS Exec sessions must be KMS-encrypted and logged to an encrypted log group."
  }

  assert {
    condition     = aws_cloudwatch_log_group.exec.kms_key_id == var.kms_key_arn && aws_cloudwatch_log_group.exec.retention_in_days == 14
    error_message = "The exec audit log group needs CMK encryption and retention."
  }
}

run "rejects_unknown_insights_mode" {
  command = plan

  variables {
    container_insights = "maybe"
  }

  expect_failures = [var.container_insights]
}
