# Alarms, dashboard and cost guardrails (SPEC §13, ADR-0022). Data-tier
# alarms and the budget exist from phase 1; service alarms and the dashboard
# follow enable_services. Identical in envs/dev and envs/prod.

module "observability" {
  source = "../../modules/observability"

  name_prefix       = local.name_prefix
  metrics_namespace = "${title(var.project)}/${var.environment}"
  kms_key_arn       = module.kms.key_arn
  alert_email       = var.alert_email
  queue_name        = module.messaging.queue_name
  dlq_name          = module.messaging.dlq_name
  table_names = {
    links     = module.dynamodb.links_table_name
    analytics = module.dynamodb.analytics_table_name
  }

  services = local.services_on ? {
    cluster_arn  = module.ecs_cluster[0].cluster_arn
    cluster_name = module.ecs_cluster[0].cluster_name
    service_names = {
      redirect = module.redirect[0].service_name
      api      = module.api[0].service_name
      worker   = module.worker[0].service_name
    }
    alb_arn_suffix            = module.edge[0].alb_arn_suffix
    target_group_arn_suffixes = module.edge[0].target_group_arn_suffixes
    web_acl_name              = module.edge[0].web_acl_name
    nightly = {
      schedule_group_name = module.nightly[0].schedule_group_name
      log_group_name      = module.nightly[0].log_group_name
      task_family         = module.nightly[0].task_family
    }
  } : null
}

module "budget" {
  source = "../../modules/budget"

  name_prefix             = local.name_prefix
  cost_tag                = { key = "CostCenter", value = local.name_prefix }
  monthly_limit_usd       = var.monthly_budget_usd
  anomaly_threshold_usd   = var.anomaly_threshold_usd
  anomaly_monitor_enabled = var.cost_anomaly_monitor_enabled
  topic_arn               = module.observability.topic_arn
}
