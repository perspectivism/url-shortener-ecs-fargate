output "key_arn" {
  description = "ARN of the environment CMK."
  value       = aws_kms_key.this.arn
}

output "key_id" {
  description = "ID of the environment CMK."
  value       = aws_kms_key.this.key_id
}

output "alias_name" {
  description = "Alias of the environment CMK."
  value       = aws_kms_alias.this.name
}
