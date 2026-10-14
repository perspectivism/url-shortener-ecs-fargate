output "links_table_name" {
  description = "Links table name."
  value       = aws_dynamodb_table.links.name
}

output "links_table_arn" {
  description = "Links table ARN."
  value       = aws_dynamodb_table.links.arn
}

output "analytics_table_name" {
  description = "Analytics table name."
  value       = aws_dynamodb_table.analytics.name
}

output "analytics_table_arn" {
  description = "Analytics table ARN."
  value       = aws_dynamodb_table.analytics.arn
}
