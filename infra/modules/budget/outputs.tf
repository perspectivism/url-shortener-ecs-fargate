output "budget_name" {
  description = "AWS Budgets budget name."
  value       = aws_budgets_budget.monthly.name
}

output "anomaly_monitor_arn" {
  description = "Cost Anomaly Detection monitor ARN (null when anomaly_monitor_enabled is false)."
  value       = one(aws_ce_anomaly_monitor.this[*].arn)
}
