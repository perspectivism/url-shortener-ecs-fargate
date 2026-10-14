output "cluster_arn" {
  description = "ECS cluster ARN."
  value       = aws_ecs_cluster.this.arn
}

output "cluster_name" {
  description = "ECS cluster name."
  value       = aws_ecs_cluster.this.name
}

output "container_insights_log_group_name" {
  description = "Container Insights performance log group (retention + CMK)."
  value       = aws_cloudwatch_log_group.container_insights.name
}
