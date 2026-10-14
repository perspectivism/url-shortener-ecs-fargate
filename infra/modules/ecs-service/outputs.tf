output "service_name" {
  description = "ECS service name."
  value       = aws_ecs_service.this.name
}

output "task_definition_arn" {
  description = "Current task definition revision ARN."
  value       = aws_ecs_task_definition.this.arn
}

output "task_role_arn" {
  description = "Task role ARN."
  value       = aws_iam_role.task.arn
}

output "execution_role_arn" {
  description = "Execution role ARN (the only principal allowed to read this service's secrets)."
  value       = aws_iam_role.execution.arn
}

output "security_group_id" {
  description = "Task security group ID."
  value       = aws_security_group.task.id
}

output "log_group_name" {
  description = "Application log group."
  value       = aws_cloudwatch_log_group.this.name
}
