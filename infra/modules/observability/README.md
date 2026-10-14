# Observability module

Alerting and dashboards for one environment (SPEC §13, ADR-0022):

- CMK-encrypted SNS alert topic with an optional email subscription. Its policy admits CloudWatch alarms, EventBridge rules (by source ARN), AWS Budgets and Cost Anomaly Detection (by source account) and denies non-TLS publishing.
- Every SPEC §13 alarm, wired to the topic on ALARM and OK. Queue, DLQ and DynamoDB alarms exist from phase 1. Service alarms follow `services`: redirect 5xx rate and p99 latency, running tasks below desired, dropped clicks, cache errors, failed cache invalidations (the threat model relies on this alarm), and nightly scheduler and job failures.
- Nightly failures are also caught by a log metric filter (`"nightly failed"`) and an EventBridge rule that fires on any non-zero exit code.
- One dashboard. It shows golden signals per service, cache hit ratio, the click pipeline, queue and DLQ, DynamoDB throttles, WAF and estimated charges, and it includes every EMF metric (enforced by `scripts/check-metrics.sh`).

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
| [aws_cloudwatch_dashboard.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_dashboard) | resource |
| [aws_cloudwatch_event_rule.nightly_exit](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_rule) | resource |
| [aws_cloudwatch_event_target.nightly_exit](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_target) | resource |
| [aws_cloudwatch_log_metric_filter.nightly_failed](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_metric_filter) | resource |
| [aws_cloudwatch_metric_alarm.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_metric_alarm) | resource |
| [aws_sns_topic.alerts](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic) | resource |
| [aws_sns_topic_policy.alerts](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_policy) | resource |
| [aws_sns_topic_subscription.email](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription) | resource |
| [aws_caller_identity.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/caller_identity) | data source |
| [aws_iam_policy_document.alerts](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/iam_policy_document) | data source |
| [aws_partition.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/partition) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| dlq\_name | Click-event dead-letter queue name (any message alarms). | `string` | n/a | yes |
| kms\_key\_arn | CMK that encrypts the alert topic (its key policy must admit the publishing services). | `string` | n/a | yes |
| metrics\_namespace | CloudWatch namespace of the services' EMF metrics, e.g. "Shortener/dev". | `string` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| queue\_name | Click-event queue name (age alarm). | `string` | n/a | yes |
| table\_names | DynamoDB tables to watch for throttling, keyed by a short label (e.g. links, analytics). | `map(string)` | n/a | yes |
| alert\_email | Email address subscribed to the alert topic (the recipient must confirm). Null creates no subscription. | `string` | `null` | no |
| services | Phase-2 resources (null while enable\_services is false): service alarms and the dashboard are created only when set. | ```object({ cluster_arn = string cluster_name = string service_names = map(string) # redirect, api, worker -> ECS service name alb_arn_suffix = string target_group_arn_suffixes = map(string) # redirect, api web_acl_name = string nightly = object({ schedule_group_name = string log_group_name = string task_family = string }) })``` | `null` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| alarm\_names | CloudWatch alarm names keyed by alarm (SPEC §13). |
| dashboard\_name | CloudWatch dashboard name (null until services are enabled). |
| topic\_arn | CMK-encrypted SNS topic that receives every alarm, budget and anomaly notification. |
<!-- END_TF_DOCS -->
