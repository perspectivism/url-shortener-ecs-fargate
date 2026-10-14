output "repository_urls" {
  description = "Repository URL per service."
  value       = { for k, r in aws_ecr_repository.this : k => r.repository_url }
}

output "repository_arns" {
  description = "Repository ARN per service."
  value       = { for k, r in aws_ecr_repository.this : k => r.arn }
}
