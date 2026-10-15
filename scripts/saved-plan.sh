#!/usr/bin/env bash
# Hands a saved plan from the deploy workflow's plan job to its apply job
# (ADR-0028), so reviewers approve the exact plan that is applied.
#
#   saved-plan.sh put <env>         upload infra/envs/<env>/tfplan (+ plan-summary.txt)
#                                   to s3://$TF_STATE_BUCKET/plans/<env>/<run>/<sha256>/
#                                   and print the SHA-256 (the plan job's output)
#   saved-plan.sh get <env> <sha>   download it into infra/envs/<env>/ and refuse
#                                   unless its SHA-256 equals <sha>
#   saved-plan.sh rm <env> <sha>    delete it after the apply
#
# The state bucket is private and KMS-encrypted (plans hold resource attributes);
# only the deploy plan role may write plans/<env>/ and the hash comes from the
# plan job's output within the same run, so a planted or altered plan is refused.
# Tests: S3_URL_PREFIX=file://<dir> replaces S3, PLAN_DIR the plan directory.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cmd="${1:?usage: saved-plan.sh put|get|rm <env> [sha256]}"
env_name="${2:?usage: saved-plan.sh put|get|rm <env> [sha256]}"
[[ "$env_name" == dev || "$env_name" == prod ]] || { echo "unknown environment: $env_name" >&2; exit 2; }
run="${GITHUB_RUN_ID:?GITHUB_RUN_ID}"
[[ "$run" =~ ^[0-9]+$ ]] || { echo "invalid GITHUB_RUN_ID" >&2; exit 2; }
dir="${PLAN_DIR:-$ROOT/infra/envs/$env_name}" # PLAN_DIR: tests
prefix="${S3_URL_PREFIX:-s3://${TF_STATE_BUCKET:?TF_STATE_BUCKET}}/plans/$env_name/$run"

sha_of() { sha256sum "$1" | cut -d' ' -f1; }

copy() { # copy <src> <dst>: S3 via the AWS CLI, file:// locally (tests)
  if [[ "$1" == file://* || "$2" == file://* ]]; then
    mkdir -p "$(dirname "${2#file://}")"
    cp "${1#file://}" "${2#file://}"
  else
    aws s3 cp --only-show-errors "$1" "$2"
  fi
}

case "$cmd" in
  put)
    [[ -f "$dir/tfplan" ]] || { echo "no saved plan: run make plan ENV=$env_name first" >&2; exit 1; }
    sha="$(sha_of "$dir/tfplan")"
    copy "$dir/tfplan" "$prefix/$sha/tfplan"
    copy "$dir/plan-summary.txt" "$prefix/$sha/plan-summary.txt"
    echo "$sha"
    ;;
  get)
    want="${3:?usage: saved-plan.sh get <env> <sha256>}"
    [[ "$want" =~ ^[0-9a-f]{64}$ ]] || { echo "invalid sha256" >&2; exit 2; }
    rm -f "$dir/tfplan" "$dir/plan-summary.txt"
    copy "$prefix/$want/tfplan" "$dir/tfplan"
    copy "$prefix/$want/plan-summary.txt" "$dir/plan-summary.txt"
    got="$(sha_of "$dir/tfplan")"
    if [[ "$got" != "$want" ]]; then
      rm -f "$dir/tfplan" "$dir/plan-summary.txt"
      echo "refusing: saved plan SHA-256 $got does not match the approved plan $want" >&2
      exit 1
    fi
    echo "saved plan verified: $want"
    ;;
  rm)
    want="${3:?usage: saved-plan.sh rm <env> <sha256>}"
    [[ "$want" =~ ^[0-9a-f]{64}$ ]] || { echo "invalid sha256" >&2; exit 2; }
    if [[ "$prefix" == file://* ]]; then
      local_dir="${prefix#file://}"
      rm -rf "${local_dir:?}/$want"
    else
      aws s3 rm --only-show-errors --recursive "$prefix/$want/"
    fi
    ;;
  *) echo "unknown command: $cmd" >&2; exit 2 ;;
esac
