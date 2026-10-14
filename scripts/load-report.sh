#!/usr/bin/env bash
# Server-side numbers for a k6 run (read-only CloudWatch queries), for the M6
# baseline in GATES.md: ALB request rate, 5xx, TargetResponseTime p50/p95/p99
# for the redirect target group, and the redirect cache hit ratio (EMF).
# Usage: scripts/load-report.sh <env> <start ISO-8601> <end ISO-8601>
#   e.g. scripts/load-report.sh dev 2026-10-14T18:00:00Z 2026-10-14T18:05:00Z
set -euo pipefail
env="${1:?usage: load-report.sh <env> <start> <end>}"
start="${2:?start time required}"
end="${3:?end time required}"
project="${PROJECT:-shortener}"
prefix="$project-$env"
ns="${project^}/$env"

tg_arn="$(aws elbv2 describe-target-groups --names "$prefix-redirect" --query 'TargetGroups[0].TargetGroupArn' --output text)"
lb_arn="$(aws elbv2 describe-target-groups --names "$prefix-redirect" --query 'TargetGroups[0].LoadBalancerArns[0]' --output text)"
tg="${tg_arn##*:}"                # targetgroup/<name>/<id>
lb="${lb_arn#*:loadbalancer/}"    # app/<name>/<id>
secs=$(($(date -d "$end" +%s) - $(date -d "$start" +%s)))
((secs > 0)) || { echo "end must be after start" >&2; exit 2; }
period=$(((secs + 59) / 60 * 60))

alb() { printf '{"Namespace":"AWS/ApplicationELB","MetricName":"%s","Dimensions":[{"Name":"LoadBalancer","Value":"%s"},{"Name":"TargetGroup","Value":"%s"}]}' "$1" "$lb" "$tg"; }
emf() { printf '{"Namespace":"%s","MetricName":"%s","Dimensions":[{"Name":"Service","Value":"redirect"}]}' "$ns" "$1"; }
q() { printf '{"Id":"%s","MetricStat":{"Metric":%s,"Period":%d,"Stat":"%s"}}' "$1" "$2" "$period" "$3"; }

queries="[$(q req "$(alb RequestCount)" Sum),$(q e5 "$(alb HTTPCode_Target_5XX_Count)" Sum),\
$(q p50 "$(alb TargetResponseTime)" p50),$(q p95 "$(alb TargetResponseTime)" p95),$(q p99 "$(alb TargetResponseTime)" p99),\
$(q hit "$(emf CacheHit)" Sum),$(q miss "$(emf CacheMiss)" Sum),$(q h99 "$(emf RedirectLatencyMs)" p99)]"

aws cloudwatch get-metric-data --start-time "$start" --end-time "$end" --metric-data-queries "$queries" --output json |
  jq -r --argjson secs "$secs" '
    (.MetricDataResults | map({(.Id): ((.Values | add) // 0)}) | add) as $v |
    "window:            \($secs) s",
    "ALB requests:      \($v.req) (\(($v.req / $secs * 10 | round) / 10) req/s)",
    "ALB target 5xx:    \($v.e5) (\(if $v.req > 0 then ($v.e5 / $v.req * 10000 | round) / 100 else 0 end) %)",
    "ALB p50/p95/p99:   \($v.p50 * 1000 | round) / \($v.p95 * 1000 | round) / \($v.p99 * 1000 | round) ms",
    "handler p99 (EMF): \($v.h99 | round) ms",
    "cache hit ratio:   \(if ($v.hit + $v.miss) > 0 then ($v.hit / ($v.hit + $v.miss) * 1000 | round) / 10 else "n/a" end) % (\($v.hit) hits, \($v.miss) misses)"'
