output "task_definition_arn" {
  description = "Task definition ARN (for manual runs: aws ecs run-task)."
  value       = aws_ecs_task_definition.this.arn
}

output "task_family" {
  description = "Task definition family."
  value       = aws_ecs_task_definition.this.family
}

output "execution_role_arn" {
  description = "Execution role ARN (secret reader)."
  value       = aws_iam_role.execution.arn
}

output "task_role_arn" {
  description = "Task role ARN."
  value       = aws_iam_role.task.arn
}

output "security_group_id" {
  description = "Task security group ID (for manual run-task)."
  value       = aws_security_group.task.id
}

output "schedule_name" {
  description = "EventBridge Scheduler schedule name."
  value       = aws_scheduler_schedule.this.name
}

output "log_group_name" {
  description = "Task log group."
  value       = aws_cloudwatch_log_group.this.name
}

output "schedule_group_name" {
  description = "EventBridge Scheduler schedule group (CloudWatch dimension ScheduleGroup)."
  value       = aws_scheduler_schedule_group.this.name
}
