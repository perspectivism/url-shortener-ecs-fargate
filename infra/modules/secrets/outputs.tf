output "safe_browsing_api_key_arn" {
  description = "ARN of the Safe Browsing API key secret."
  value       = aws_secretsmanager_secret.this["safe_browsing_api_key"].arn
}

output "visitor_hash_salt_arn" {
  description = "ARN of the visitor hash salt secret."
  value       = aws_secretsmanager_secret.this["visitor_hash_salt"].arn
}

output "names" {
  description = "Secret names (for make seed-secrets)."
  value       = { for k, s in aws_secretsmanager_secret.this : k => s.name }
}
