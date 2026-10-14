#!/usr/bin/env bash
# Waits until the local emulators accept TCP connections (max ~60 s).
set -euo pipefail

endpoints=("${AWS_ENDPOINT_URL_DYNAMODB:-http://localhost:8000}" "${AWS_ENDPOINT_URL_SQS:-http://localhost:9324}" "${AWS_ENDPOINT_URL_S3:-http://localhost:9090}" "tcp://${CACHE_ADDR:-localhost:6379}")
for ep in "${endpoints[@]}"; do
  hostport="${ep#*://}"
  host="${hostport%%:*}"
  port="${hostport##*:}"
  for i in $(seq 1 60); do
    if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
      break
    fi
    if [[ "$i" -eq 60 ]]; then
      echo "emulator $host:$port is not reachable — run 'make up' first" >&2
      exit 1
    fi
    sleep 1
  done
done
echo "emulators reachable"
