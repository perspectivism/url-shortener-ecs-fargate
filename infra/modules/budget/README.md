# Budget module

Cost guardrails for one environment (SPEC §13, ADR-0022):

- An AWS Budget with notifications at 50 %, 80 % and 100 % of actual spend and at 100 % of forecast.
- A customer-managed Cost Anomaly Detection monitor with immediate alerts at or above a USD impact threshold.

Both are scoped to the environment's `CostCenter` cost allocation tag and deliver to the alert topic. The tag must be activated in Billing (`infra/bootstrap`, `activate_cost_allocation_tag`); tag monitors can only be created in a management or standalone account.

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
| [aws_budgets_budget.monthly](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/budgets_budget) | resource |
| [aws_ce_anomaly_monitor.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ce_anomaly_monitor) | resource |
| [aws_ce_anomaly_subscription.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ce_anomaly_subscription) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| cost\_tag | Cost allocation tag that identifies this environment's spend (must be activated in Billing, see infra/bootstrap). | ```object({ key = string value = string })``` | n/a | yes |
| monthly\_limit\_usd | Monthly budget in USD; notifications at 50/80/100 % actual and 100 % forecast. | `number` | n/a | yes |
| name\_prefix | Resource name prefix, e.g. "shortener-dev". | `string` | n/a | yes |
| topic\_arn | SNS topic for budget and anomaly notifications (its policy must admit budgets.amazonaws.com and costalerts.amazonaws.com). | `string` | n/a | yes |
| anomaly\_threshold\_usd | Notify on cost anomalies whose total impact is at least this many USD. | `number` | `10` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| anomaly\_monitor\_arn | Cost Anomaly Detection monitor ARN. |
| budget\_name | AWS Budgets budget name. |
<!-- END_TF_DOCS -->
