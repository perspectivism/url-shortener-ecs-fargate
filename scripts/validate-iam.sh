#!/usr/bin/env bash
# IAM Access Analyzer validate-policy over every policy document in a plan or
# state (`terraform show -json`), the M7 gate. Read-only: ValidatePolicy does
# not create or change anything. Fails on any ERROR or SECURITY_WARNING finding,
# and on any policy that could not be validated (value unknown until apply).
#
# Usage: scripts/validate-iam.sh <tfplan.json | state.json> [--dry-run] [--allow-unknown]
#   --dry-run        list the policies that would be validated (no AWS calls)
#   --allow-unknown  report, but do not fail on, policies only known after apply
#                    (a plan against empty state, i.e. local mode before the
#                    first apply; after apply, every policy is known)
set -euo pipefail
json="${1:?usage: validate-iam.sh <tfplan.json|state.json> [--dry-run] [--allow-unknown]}"
shift
dry_run=false allow_unknown=false
for a in "$@"; do
  case "$a" in
    --dry-run) dry_run=true ;;
    --allow-unknown) allow_unknown=true ;;
    *) echo "unknown flag: $a" >&2; exit 2 ;;
  esac
done

# One line per policy (scripts/validate-iam.jq): address, policy type,
# resource type, document or "UNKNOWN" (only known after apply).
extracted="$(jq -r -f "$(dirname "${BASH_SOURCE[0]}")/validate-iam.jq" "$json")" # fails the script on bad input
rows=()
[[ -z "$extracted" ]] || mapfile -t rows <<<"$extracted"
total=${#rows[@]} unknown=0 failed=0 validated=0

for row in "${rows[@]}"; do
  IFS=$'\x1f' read -r addr ptype rtype doc <<<"$row"
  if [[ "$doc" == "UNKNOWN" ]]; then
    unknown=$((unknown + 1))
    echo "UNKNOWN  $addr (rendered at apply time)"
    continue
  fi
  if $dry_run; then
    echo "would validate  $ptype ${rtype:+($rtype) }$addr"
    continue
  fi
  args=(--policy-type "$ptype" --policy-document "$doc" --output json)
  [[ -n "$rtype" ]] && args+=(--validate-policy-resource-type "$rtype")
  findings="$(aws accessanalyzer validate-policy "${args[@]}")"
  validated=$((validated + 1))
  bad="$(jq -r '.findings[] | select(.findingType == "ERROR" or .findingType == "SECURITY_WARNING") | "  \(.findingType) \(.issueCode): \(.findingDetails)"' <<<"$findings")"
  notes="$(jq -r '[.findings[] | select(.findingType == "WARNING" or .findingType == "SUGGESTION") | .issueCode] | unique | join(", ")' <<<"$findings")"
  if [[ -n "$bad" ]]; then
    failed=$((failed + 1))
    echo "FAIL     $addr"
    echo "$bad"
  else
    echo "ok       $addr${notes:+  (notes: $notes)}"
  fi
done

if ((total == 0)); then
  echo "FAIL: no policy documents found in $json (not a terraform show -json plan/state?)" >&2
  exit 1
fi
echo "policies: $total, validated: $validated, failed: $failed, unknown until apply: $unknown$($dry_run && echo ' (dry run)')"
((failed == 0)) || exit 1
if ((unknown > 0)) && ! $allow_unknown; then
  echo "FAIL: $unknown policies could not be validated (re-run against a plan with state, or the state after apply)" >&2
  exit 1
fi
