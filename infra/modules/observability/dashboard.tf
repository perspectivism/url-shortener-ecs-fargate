# One dashboard per environment (SPEC §13): golden signals per service, cache
# hit ratio, click pipeline, queue/DLQ, DynamoDB throttles, WAF and cost.
# Every SPEC §13 EMF metric is on it (scripts/check-metrics.sh enforces this).

locals {
  alb = { LoadBalancer = local.svc.alb_arn_suffix }
  tg  = { for k, v in local.svc.target_group_arn_suffixes : k => merge(local.alb, { TargetGroup = v }) }

  widgets = [
    {
      type = "text", width = 24, height = 2
      properties = {
        markdown = "## ${var.name_prefix} · golden signals\nAlarms notify SNS topic `${aws_sns_topic.alerts.name}`. SLOs and alarm procedures: `docs/runbook.md`. Redirect SLO: p99 < 100 ms at the ALB, 99.9 % non-5xx."
      }
    },
    {
      type = "alarm", width = 24, height = 3
      properties = {
        title  = "Alarms"
        alarms = [for a in aws_cloudwatch_metric_alarm.this : a.arn]
      }
    },
    # Redirect
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Redirect · requests and 5xx (ALB)", region = local.region, view = "timeSeries", period = 60
        metrics = [
          ["AWS/ApplicationELB", "RequestCount", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["redirect"].TargetGroup, { stat = "Sum", label = "requests" }],
          ["AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["redirect"].TargetGroup, { stat = "Sum", label = "target 5xx", yAxis = "right" }],
          ["AWS/ApplicationELB", "HTTPCode_Target_4XX_Count", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["redirect"].TargetGroup, { stat = "Sum", label = "target 4xx", yAxis = "right" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Redirect · latency", region = local.region, view = "timeSeries", period = 60
        metrics = [
          ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["redirect"].TargetGroup, { stat = "p50", label = "ALB p50 (s)" }],
          ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["redirect"].TargetGroup, { stat = "p99", label = "ALB p99 (s)" }],
          [local.ns, "RedirectLatencyMs", "Service", "redirect", { stat = "p99", label = "handler p99 (ms)", yAxis = "right" }],
        ]
        annotations = { horizontal = [{ value = 0.1, label = "p99 SLO 100 ms" }] }
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Redirect · cache", region = local.region, view = "timeSeries", period = 300
        metrics = [
          [{ expression = "100 * FILL(hit, 0) / IF(FILL(hit, 0) + FILL(miss, 0) > 0, FILL(hit, 0) + FILL(miss, 0), 1)", label = "hit ratio %", id = "ratio" }],
          [local.ns, "CacheHit", "Service", "redirect", { stat = "Sum", id = "hit", label = "hits", yAxis = "right" }],
          [local.ns, "CacheMiss", "Service", "redirect", { stat = "Sum", id = "miss", label = "misses", yAxis = "right" }],
          [local.ns, "NegativeCacheHit", "Service", "redirect", { stat = "Sum", label = "negative hits", yAxis = "right" }],
        ]
      }
    },
    # API
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "API · requests and errors (ALB)", region = local.region, view = "timeSeries", period = 60
        metrics = [
          ["AWS/ApplicationELB", "RequestCount", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["api"].TargetGroup, { stat = "Sum", label = "requests" }],
          ["AWS/ApplicationELB", "HTTPCode_Target_5XX_Count", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["api"].TargetGroup, { stat = "Sum", label = "target 5xx", yAxis = "right" }],
          ["AWS/ApplicationELB", "HTTPCode_Target_4XX_Count", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["api"].TargetGroup, { stat = "Sum", label = "target 4xx", yAxis = "right" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "API · latency", region = local.region, view = "timeSeries", period = 60
        metrics = [
          ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["api"].TargetGroup, { stat = "p50", label = "p50 (s)" }],
          ["AWS/ApplicationELB", "TargetResponseTime", "LoadBalancer", local.alb.LoadBalancer, "TargetGroup", local.tg["api"].TargetGroup, { stat = "p99", label = "p99 (s)" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Links and URL scanning", region = local.region, view = "timeSeries", period = 300, stat = "Sum"
        metrics = [
          [local.ns, "LinksCreated", "Service", "api", { label = "links created" }],
          [local.ns, "KeyBlocksAllocated", "Service", "api", { label = "key blocks allocated" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"URLScanFlagged\"', 'Sum', 300)", id = "flagged", label = "scan flagged" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"URLScanUnavailable\"', 'Sum', 300)", id = "unavailable", label = "scan unavailable" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"URLScanSkipped\"', 'Sum', 300)", id = "skipped", label = "scan skipped" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"LinksDisabled\"', 'Sum', 300)", id = "disabled", label = "links disabled" }],
        ]
      }
    },
    # Tasks
    {
      type = "metric", width = 12, height = 6
      properties = {
        title = "ECS · CPU and memory utilization %", region = local.region, view = "timeSeries", period = 60, stat = "Average"
        metrics = concat(
          [for s in ["redirect", "api", "worker"] : ["AWS/ECS", "CPUUtilization", "ClusterName", local.svc.cluster_name, "ServiceName", local.svc.service_names[s], { label = "${s} CPU" }]],
          [for s in ["redirect", "api", "worker"] : ["AWS/ECS", "MemoryUtilization", "ClusterName", local.svc.cluster_name, "ServiceName", local.svc.service_names[s], { label = "${s} memory" }]],
        )
      }
    },
    {
      type = "metric", width = 12, height = 6
      properties = {
        title   = "ECS · running tasks", region = local.region, view = "timeSeries", period = 60, stat = "Average"
        metrics = [for s in ["redirect", "api", "worker"] : ["ECS/ContainerInsights", "RunningTaskCount", "ClusterName", local.svc.cluster_name, "ServiceName", local.svc.service_names[s], { label = s }]]
      }
    },
    # Click pipeline
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Click pipeline", region = local.region, view = "timeSeries", period = 60, stat = "Sum"
        metrics = [
          [local.ns, "ClickEventsEnqueued", "Service", "redirect", { label = "enqueued" }],
          [local.ns, "ClickEventsDropped", "Service", "redirect", { label = "dropped" }],
          [local.ns, "WorkerEventsProcessed", "Service", "worker", { label = "processed" }],
          [local.ns, "WorkerDuplicateEvents", "Service", "worker", { label = "duplicates" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Queue depth and age", region = local.region, view = "timeSeries", period = 60, stat = "Maximum"
        metrics = [
          ["AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", var.queue_name, { label = "visible" }],
          ["AWS/SQS", "ApproximateAgeOfOldestMessage", "QueueName", var.queue_name, { label = "oldest (s)", yAxis = "right" }],
          ["AWS/SQS", "ApproximateNumberOfMessagesVisible", "QueueName", var.dlq_name, { label = "DLQ visible" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "Cache and rate limiting", region = local.region, view = "timeSeries", period = 300, stat = "Sum"
        metrics = [
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"CacheErrors\"', 'Sum', 300)", id = "cacheerrors", label = "cache errors" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"RateLimited\"', 'Sum', 300)", id = "ratelimited", label = "rate limited" }],
          [{ expression = "SEARCH('{${local.ns},Service} MetricName=\"CacheInvalidationFailed\"', 'Sum', 300)", id = "invalidation", label = "invalidation failed" }],
        ]
      }
    },
    # Data, edge, cost
    {
      type = "metric", width = 8, height = 6
      properties = {
        title = "DynamoDB · throttles and consumed capacity", region = local.region, view = "timeSeries", period = 60, stat = "Sum"
        metrics = concat(
          flatten([for k, t in var.table_names : [
            ["AWS/DynamoDB", "ReadThrottleEvents", "TableName", t, { label = "${k} read throttles" }],
            ["AWS/DynamoDB", "WriteThrottleEvents", "TableName", t, { label = "${k} write throttles" }],
          ]]),
          flatten([for k, t in var.table_names : [
            ["AWS/DynamoDB", "ConsumedReadCapacityUnits", "TableName", t, { label = "${k} RCU", yAxis = "right" }],
            ["AWS/DynamoDB", "ConsumedWriteCapacityUnits", "TableName", t, { label = "${k} WCU", yAxis = "right" }],
          ]]),
        )
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        # CLOUDFRONT-scope web ACL metrics live in us-east-1.
        title = "WAF · allowed, blocked, counted", region = "us-east-1", view = "timeSeries", period = 300
        metrics = [
          [{ expression = "SEARCH('Namespace=\"AWS/WAFV2\" WebACL=\"${local.svc.web_acl_name}\" Rule=\"ALL\"', 'Sum', 300)", id = "waf", label = "" }],
        ]
      }
    },
    {
      type = "metric", width = 8, height = 6
      properties = {
        # Account-wide; requires "Receive Billing Alerts" in Billing preferences.
        title   = "Cost · estimated charges this month (account, USD)", region = "us-east-1", view = "singleValue", period = 21600, stat = "Maximum"
        metrics = [["AWS/Billing", "EstimatedCharges", "Currency", "USD", { label = "estimated charges" }]]
      }
    },
  ]
}

resource "aws_cloudwatch_dashboard" "this" {
  count = local.services ? 1 : 0

  dashboard_name = var.name_prefix
  dashboard_body = jsonencode({ widgets = local.widgets })
}
