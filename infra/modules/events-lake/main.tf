# Cold analytics path (ADR-0011): raw click events in S3 as gzip NDJSON under
# events/dt=YYYY-MM-DD/hour=HH/, queried by Athena via partition projection.
# Objects can contain duplicates (redelivery after a failed flush), so every
# query deduplicates by event_id.

data "aws_caller_identity" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  db_name    = replace(var.name_prefix, "-", "_")
}

module "events_bucket" {
  source = "../s3-bucket"

  name              = "${var.name_prefix}-events-${local.account_id}"
  kms_key_arn       = var.kms_key_arn
  force_destroy     = var.force_destroy
  access_log_bucket = var.log_bucket_id
  expiration_days   = var.events_retention_days
}

module "athena_results" {
  source = "../s3-bucket"

  name                               = "${var.name_prefix}-athena-${local.account_id}"
  kms_key_arn                        = var.kms_key_arn
  force_destroy                      = var.force_destroy
  access_log_bucket                  = var.log_bucket_id
  expiration_days                    = 7
  noncurrent_version_expiration_days = 1
}

resource "aws_athena_workgroup" "this" {
  name          = var.name_prefix
  force_destroy = var.force_destroy

  configuration {
    enforce_workgroup_configuration    = true
    publish_cloudwatch_metrics_enabled = true
    bytes_scanned_cutoff_per_query     = var.bytes_scanned_cutoff_per_query

    result_configuration {
      output_location = "s3://${module.athena_results.id}/results/"

      encryption_configuration {
        encryption_option = "SSE_KMS"
        kms_key_arn       = var.kms_key_arn
      }
    }
  }
}

resource "aws_glue_catalog_database" "this" {
  name        = local.db_name
  description = "URL Shortener click analytics (${var.name_prefix})"
}

resource "aws_glue_catalog_table" "click_events" {
  name          = "click_events"
  database_name = aws_glue_catalog_database.this.name
  table_type    = "EXTERNAL_TABLE"

  parameters = {
    EXTERNAL                      = "TRUE"
    classification                = "json"
    "compressionType"             = "gzip"
    "projection.enabled"          = "true"
    "projection.dt.type"          = "date"
    "projection.dt.format"        = "yyyy-MM-dd"
    "projection.dt.range"         = "${var.partition_start_date},NOW"
    "projection.dt.interval"      = "1"
    "projection.dt.interval.unit" = "DAYS"
    "projection.hour.type"        = "integer"
    "projection.hour.range"       = "0,23"
    "projection.hour.digits"      = "2"
    "storage.location.template"   = "s3://${module.events_bucket.id}/events/dt=$${dt}/hour=$${hour}/"
  }

  partition_keys {
    name = "dt"
    type = "string"
  }

  partition_keys {
    name = "hour"
    type = "string"
  }

  storage_descriptor {
    location      = "s3://${module.events_bucket.id}/events/"
    input_format  = "org.apache.hadoop.mapred.TextInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"

    ser_de_info {
      serialization_library = "org.openx.data.jsonserde.JsonSerDe"
      parameters            = { "ignore.malformed.json" = "true" }
    }

    columns {
      name = "v"
      type = "int"
    }
    columns {
      name = "event_id"
      type = "string"
    }
    columns {
      name = "code"
      type = "string"
    }
    columns {
      name    = "ts"
      type    = "string"
      comment = "RFC 3339 UTC; use from_iso8601_timestamp(ts)"
    }
    columns {
      name = "visitor"
      type = "string"
    }
    columns {
      name = "referrer_host"
      type = "string"
    }
    columns {
      name = "country"
      type = "string"
    }
    columns {
      name = "user_agent"
      type = "string"
    }
    columns {
      name = "is_bot"
      type = "boolean"
    }
  }
}

locals {
  table = "${aws_glue_catalog_database.this.name}.${aws_glue_catalog_table.click_events.name}"
  since = "dt >= date_format(current_date - interval '7' day, '%Y-%m-%d')"
  queries = {
    "top-links-7d" = {
      description = "Top 20 links by human clicks over the last 7 days (deduplicated by event_id)."
      query       = "SELECT code, count(DISTINCT event_id) AS clicks FROM ${local.table} WHERE ${local.since} AND NOT is_bot GROUP BY code ORDER BY clicks DESC LIMIT 20"
    }
    "clicks-by-hour-7d" = {
      description = "Human clicks per hour over the last 7 days (deduplicated by event_id)."
      query       = "SELECT dt, hour, count(DISTINCT event_id) AS clicks FROM ${local.table} WHERE ${local.since} AND NOT is_bot GROUP BY dt, hour ORDER BY dt, hour"
    }
    "referrers-7d" = {
      description = "Referrer host breakdown over the last 7 days (deduplicated by event_id)."
      query       = "SELECT coalesce(nullif(referrer_host, ''), 'direct') AS referrer, count(DISTINCT event_id) AS clicks FROM ${local.table} WHERE ${local.since} AND NOT is_bot GROUP BY 1 ORDER BY clicks DESC LIMIT 50"
    }
  }
}

resource "aws_athena_named_query" "this" {
  for_each = local.queries

  name        = "${var.name_prefix}-${each.key}"
  description = each.value.description
  workgroup   = aws_athena_workgroup.this.name
  database    = aws_glue_catalog_database.this.name
  query       = each.value.query
}
