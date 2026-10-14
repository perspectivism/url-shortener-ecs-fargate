#!/usr/bin/env bash
# Invariants over a saved plan (terraform show -json), checked for every
# resource of a type in every module — what per-module terraform tests cannot
# enumerate (SPEC §13, M6 gate):
#   - every CloudWatch log group has a retention period and a KMS key
#   - every CloudWatch alarm notifies at least one action (the SNS topic)
#   - every SNS topic is KMS-encrypted
# Usage: scripts/check-plan-invariants.sh <tfplan.json>
set -euo pipefail
plan="${1:?usage: check-plan-invariants.sh <tfplan.json>}"

# Resources this plan creates or keeps (anything but a pure delete), with
# "set" meaning known-and-non-empty or known-after-apply.
violations="$(jq -r '
  def live: .change.actions != ["delete"];
  def set(f): ((.change.after | f) // null) as $v | ((.change.after_unknown | f) // false) as $u
    | ($u != false) or ($v != null and $v != "" and $v != [] and $v != 0);
  [ .resource_changes[] | select(.mode == "managed") | select(live)
    | if .type == "aws_cloudwatch_log_group" then
        (if set(.retention_in_days) | not then "\(.address): log group without retention" else empty end),
        (if set(.kms_key_id) | not then "\(.address): log group without KMS key" else empty end)
      elif .type == "aws_cloudwatch_metric_alarm" then
        (if set(.alarm_actions) | not then "\(.address): alarm without an action" else empty end)
      elif .type == "aws_sns_topic" then
        (if set(.kms_master_key_id) | not then "\(.address): SNS topic without KMS" else empty end)
      else empty end
  ] | .[]
' "$plan")"

counts="$(jq -r '
  [.resource_changes[] | select(.mode == "managed" and .change.actions != ["delete"]) | .type] as $t |
  "log groups=\([$t[] | select(. == "aws_cloudwatch_log_group")] | length) alarms=\([$t[] | select(. == "aws_cloudwatch_metric_alarm")] | length) topics=\([$t[] | select(. == "aws_sns_topic")] | length)"
' "$plan")"

if [[ -n "$violations" ]]; then
  echo "$violations" >&2
  echo "FAIL: plan invariants ($counts)" >&2
  exit 1
fi
echo "plan invariants OK ($counts)"
