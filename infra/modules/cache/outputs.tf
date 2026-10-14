output "endpoint" {
  description = "Primary endpoint host:port for CACHE_ADDR."
  value       = "${aws_elasticache_serverless_cache.this.endpoint[0].address}:${aws_elasticache_serverless_cache.this.endpoint[0].port}"
}

output "cache_name" {
  description = "Serverless cache name (CACHE_NAME; used in the IAM auth token)."
  value       = aws_elasticache_serverless_cache.this.name
}

output "cache_arn" {
  description = "Serverless cache ARN (elasticache:Connect resource)."
  value       = aws_elasticache_serverless_cache.this.arn
}

output "app_user_id" {
  description = "IAM-authenticated user ID (CACHE_USER)."
  value       = aws_elasticache_user.app.user_id
}

output "app_user_arn" {
  description = "IAM-authenticated user ARN (elasticache:Connect resource)."
  value       = aws_elasticache_user.app.arn
}

output "client_security_group_id" {
  description = "Attach to tasks that use Valkey."
  value       = aws_security_group.clients.id
}
