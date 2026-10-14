output "topic_arn" {
  description = "CMK-encrypted SNS topic that receives every alarm, budget and anomaly notification."
  value       = aws_sns_topic.alerts.arn
}

output "alarm_names" {
  description = "CloudWatch alarm names keyed by alarm (SPEC §13)."
  value       = { for k, a in aws_cloudwatch_metric_alarm.this : k => a.alarm_name }
}

output "dashboard_name" {
  description = "CloudWatch dashboard name (null until services are enabled)."
  value       = one(aws_cloudwatch_dashboard.this[*].dashboard_name)
}
