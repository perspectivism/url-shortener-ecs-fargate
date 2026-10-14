#!/usr/bin/env bash
# Read-only check that a full teardown left nothing behind (SPEC §19.1).
# Exits non-zero if any project resource remains. KMS keys pending deletion
# (AWS minimum 7-day window, not billed) and unconfirmed SNS email
# subscriptions (removed by AWS after 3 days) are reported but expected.
#
# Usage: scripts/verify-teardown.sh [--dry-run]
#   PROJECT (default shortener), AWS_REGION (default us-east-1); CloudFront,
#   WAF and their log groups are checked in us-east-1 as well.
#   --dry-run lists the checks without calling AWS.
set -euo pipefail
project="${PROJECT:-shortener}"
region="${AWS_REGION:-us-east-1}"
global=us-east-1
dry_run=false
[[ "${1:-}" == --dry-run ]] && dry_run=true

leftovers=0 checks=0
indent() { while IFS= read -r l; do echo "           $l"; done; }
report() { # name, newline-separated findings
  local name="$1" found="$2"
  checks=$((checks + 1))
  if [[ -n "$found" ]]; then
    leftovers=$((leftovers + $(wc -l <<<"$found")))
    echo "LEFT     $name"
    indent <<<"$found"
  else
    echo "ok       $name"
  fi
}
check() { # name, command producing one leftover per line (empty = clean)
  local name="$1"
  shift
  if $dry_run; then
    checks=$((checks + 1))
    echo "would check  $name"
    return
  fi
  report "$name" "$("$@" | sed '/^None$/d; /^$/d')"
}
aws_r() { aws --region "$region" --output text "$@"; }
aws_g() { aws --region "$global" --output text "$@"; }

# Tagged resources (both regions); KMS keys pending deletion are expected.
tagged() {
  local r arn
  for r in $(printf '%s\n' "$region" "$global" | sort -u); do
    aws --region "$r" resourcegroupstaggingapi get-resources --tag-filters "Key=Project,Values=$project" \
      --query 'ResourceTagMappingList[].ResourceARN' --output text | tr '\t' '\n' | sed '/^$/d' |
      while read -r arn; do
        if [[ "$arn" == arn:*:kms:* ]] && [[ "$(aws --region "$r" kms describe-key --key-id "$arn" --query KeyMetadata.KeyState --output text 2>/dev/null)" == PendingDeletion ]]; then
          continue
        fi
        echo "$arn"
      done
  done
}
pending_keys() {
  local r arn
  for r in $(printf '%s\n' "$region" "$global" | sort -u); do
    aws --region "$r" resourcegroupstaggingapi get-resources --tag-filters "Key=Project,Values=$project" --resource-type-filters kms:key \
      --query 'ResourceTagMappingList[].ResourceARN' --output text | tr '\t' '\n' | sed '/^$/d' |
      while read -r arn; do
        state="$(aws --region "$r" kms describe-key --key-id "$arn" --query 'KeyMetadata.[KeyState,DeletionDate]' --output text 2>/dev/null || true)"
        [[ "$state" == PendingDeletion* ]] && echo "$arn (deletes ${state#PendingDeletion})"
      done
  done
}

check "tagged resources (Project=$project)" tagged
check "S3 buckets $project-*" aws_r s3api list-buckets --query "Buckets[?starts_with(Name, '$project-')].Name"
check "DynamoDB tables" aws_r dynamodb list-tables --query "TableNames[?starts_with(@, '$project-')]"
check "SQS queues" aws_r sqs list-queues --queue-name-prefix "$project-" --query 'QueueUrls[]'
check "ECR repositories" aws_r ecr describe-repositories --query "repositories[?starts_with(repositoryName, '$project-')].repositoryName"
check "ECS clusters" aws_r ecs list-clusters --query "clusterArns[?contains(@, ':cluster/$project-')]"
check "ElastiCache serverless caches" aws_r elasticache describe-serverless-caches --query "ServerlessCaches[?starts_with(ServerlessCacheName, '$project-')].ServerlessCacheName"
check "ElastiCache users" aws_r elasticache describe-users --query "Users[?starts_with(UserId, '$project-')].UserId"
check "secrets (incl. scheduled for deletion)" aws_r secretsmanager list-secrets --include-planned-deletion --filters "Key=name,Values=$project/" --query 'SecretList[].Name'
check "log groups /$project/ (regional)" aws_r logs describe-log-groups --log-group-name-prefix "/$project/" --query 'logGroups[].logGroupName'
check "log groups Container Insights" aws_r logs describe-log-groups --log-group-name-prefix "/aws/ecs/containerinsights/$project-" --query 'logGroups[].logGroupName'
check "log groups /$project/ (us-east-1)" aws_g logs describe-log-groups --log-group-name-prefix "/$project/" --query 'logGroups[].logGroupName'
check "log groups aws-waf-logs-$project (us-east-1)" aws_g logs describe-log-groups --log-group-name-prefix "aws-waf-logs-$project" --query 'logGroups[].logGroupName'
check "CloudFront distributions" aws_g cloudfront list-distributions --query "DistributionList.Items[?starts_with(Comment, '$project-')].Id"
check "CloudFront VPC origins" aws_g cloudfront list-vpc-origins --query "VpcOriginList.Items[?starts_with(Name, '$project-')].Id"
check "WAF web ACLs (CLOUDFRONT)" aws_g wafv2 list-web-acls --scope CLOUDFRONT --query "WebACLs[?starts_with(Name, '$project-')].Name"
check "load balancers" aws_r elbv2 describe-load-balancers --query "LoadBalancers[?starts_with(LoadBalancerName, '$project-')].LoadBalancerName"
check "target groups" aws_r elbv2 describe-target-groups --query "TargetGroups[?starts_with(TargetGroupName, '$project-')].TargetGroupName"
check "VPCs tagged Project" aws_r ec2 describe-vpcs --filters "Name=tag:Project,Values=$project" --query 'Vpcs[].VpcId'
check "ENIs in project VPCs or tagged" aws_r ec2 describe-network-interfaces --filters "Name=tag:Project,Values=$project" --query 'NetworkInterfaces[].NetworkInterfaceId'
check "Elastic IPs tagged Project" aws_r ec2 describe-addresses --filters "Name=tag:Project,Values=$project" --query 'Addresses[].AllocationId'
check "IAM roles $project-*" aws_r iam list-roles --query "Roles[?starts_with(RoleName, '$project-')].RoleName"
check "IAM policies $project-*" aws_r iam list-policies --scope Local --query "Policies[?starts_with(PolicyName, '$project-')].PolicyName"
oidc() {
  local arn
  for arn in $(aws iam list-open-id-connect-providers --query 'OpenIDConnectProviderList[].Arn' --output text); do
    [[ "$arn" == *token.actions.githubusercontent.com ]] || continue
    aws iam list-open-id-connect-provider-tags --open-id-connect-provider-arn "$arn" --query "Tags[?Key=='Project' && Value=='$project']" --output text | grep -q . && echo "$arn"
  done
  true
}
check "GitHub OIDC provider created by bootstrap" oidc
check "SNS topics" aws_r sns list-topics --query "Topics[?contains(TopicArn, ':$project-')].TopicArn"
check "CloudWatch alarms" aws_r cloudwatch describe-alarms --alarm-name-prefix "$project-" --query 'MetricAlarms[].AlarmName'
check "CloudWatch dashboards" aws_r cloudwatch list-dashboards --dashboard-name-prefix "$project-" --query 'DashboardEntries[].DashboardName'
check "EventBridge rules" aws_r events list-rules --name-prefix "$project-" --query 'Rules[].Name'
check "Scheduler schedule groups" aws_r scheduler list-schedule-groups --name-prefix "$project-" --query 'ScheduleGroups[].Name'
check "Athena workgroups" aws_r athena list-work-groups --query "WorkGroups[?starts_with(Name, '$project-')].Name"
check "Glue databases" aws_r glue get-databases --query "DatabaseList[?starts_with(Name, '${project}_')].Name"
budgets() { aws budgets describe-budgets --account-id "$(aws sts get-caller-identity --query Account --output text)" --query "Budgets[?starts_with(BudgetName, '$project-')].BudgetName" --output text; }
check "AWS Budgets" budgets
check "Cost Anomaly monitors" aws_g ce get-anomaly-monitors --query "AnomalyMonitors[?starts_with(MonitorName, '$project-')].MonitorName"
check "Cost Anomaly subscriptions" aws_g ce get-anomaly-subscriptions --query "AnomalySubscriptions[?starts_with(SubscriptionName, '$project-')].SubscriptionName"

if $dry_run; then
  echo "dry run: $checks checks"
  exit 0
fi

pending="$(pending_keys)"
if [[ -n "$pending" ]]; then
  echo "expected KMS keys pending deletion (AWS minimum 7 days; not billed):"
  indent <<<"$pending"
fi
echo "note: unconfirmed SNS email subscriptions cannot be deleted; AWS removes them after 3 days (not a leftover)"
echo "checks: $checks, leftovers: $leftovers"
((leftovers == 0))
