output "queue_url" {
  description = "Click-event queue URL."
  value       = aws_sqs_queue.clicks.id
}

output "queue_arn" {
  description = "Click-event queue ARN."
  value       = aws_sqs_queue.clicks.arn
}

output "queue_name" {
  description = "Click-event queue name."
  value       = aws_sqs_queue.clicks.name
}

output "dlq_url" {
  description = "Dead-letter queue URL."
  value       = aws_sqs_queue.dlq.id
}

output "dlq_arn" {
  description = "Dead-letter queue ARN."
  value       = aws_sqs_queue.dlq.arn
}

output "dlq_name" {
  description = "Dead-letter queue name."
  value       = aws_sqs_queue.dlq.name
}
