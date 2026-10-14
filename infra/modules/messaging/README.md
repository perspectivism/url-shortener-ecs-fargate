# Messaging module

Click-event SQS queue and DLQ (ADR-0010): SSE-KMS, 120 s visibility timeout, redrive after 5 receives, 14-day DLQ retention, DLQ restricted to its source queue, TLS-only queue policies.

Tests: `tests/*.tftest.hcl` (`terraform test`, mock provider).

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| terraform | >= 1.16.5, < 2.0.0 |
| aws | >= 6.68.0, < 7.0.0 |

## Providers

| Name | Version |
| ---- | ------- |
| aws | >= 6.68.0, < 7.0.0 |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_sqs_queue.clicks](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue) | resource |
| [aws_sqs_queue.dlq](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue) | resource |
| [aws_sqs_queue_policy.clicks](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_policy) | resource |
| [aws_sqs_queue_policy.dlq](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_policy) | resource |
| [aws_sqs_queue_redrive_allow_policy.dlq](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_redrive_allow_policy) | resource |
| [aws_iam_policy_document.tls_only](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| kms\_key\_arn | Environment CMK for SQS server-side encryption. | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| max\_receive\_count | Receives before a message moves to the DLQ. | `number` | `5` | no |
| visibility\_timeout\_seconds | Click queue visibility timeout; must exceed the worker's flush interval plus long-poll wait (SPEC §8.2). | `number` | `120` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| dlq\_arn | Dead-letter queue ARN. |
| dlq\_name | Dead-letter queue name. |
| dlq\_url | Dead-letter queue URL. |
| queue\_arn | Click-event queue ARN. |
| queue\_name | Click-event queue name. |
| queue\_url | Click-event queue URL. |
<!-- END_TF_DOCS -->
