#!/usr/bin/env bash
# Checks built images (SPEC §18 M3): non-root user 65532, linux/arm64,
# distroless entrypoint, and size under 30 MB. The size gate uses the
# uncompressed filesystem (docker export of a created, never-started
# container): with the containerd image store, `.Size` is the compressed
# content size, which would understate the image.
# Usage: scripts/check-images.sh <repo> <tag> <service>...
set -euo pipefail

repo="$1"
tag="$2"
shift 2
max_bytes=30000000
fail=0
printf '%-40s %-12s %-6s %-9s %-9s %s\n' IMAGE USER ARCH MB_GZIP MB_RAW ENTRYPOINT
for svc in "$@"; do
  img="$repo/$svc:$tag"
  if ! docker image inspect "$img" >/dev/null 2>&1; then
    echo "FAIL: $img not found (run make images)" >&2
    fail=1
    continue
  fi
  user="$(docker image inspect --format '{{.Config.User}}' "$img")"
  arch="$(docker image inspect --format '{{.Architecture}}' "$img")"
  size="$(docker image inspect --format '{{.Size}}' "$img")"
  entry="$(docker image inspect --format '{{json .Config.Entrypoint}}' "$img")"
  cid="$(docker create --platform "linux/$arch" "$img")"
  raw="$(docker export "$cid" | wc -c)"
  docker rm -f "$cid" >/dev/null
  mb() { awk -v s="$1" 'BEGIN{printf "%.1f", s/1000000}'; }
  printf '%-40s %-12s %-6s %-9s %-9s %s\n' "$img" "$user" "$arch" "$(mb "$size")" "$(mb "$raw")" "$entry"
  [[ "$user" == "65532:65532" || "$user" == "65532" ]] || { echo "FAIL: $img runs as '$user', want 65532" >&2; fail=1; }
  [[ "$arch" == "arm64" ]] || { echo "FAIL: $img is $arch, want arm64" >&2; fail=1; }
  [[ "$raw" -lt "$max_bytes" ]] || { echo "FAIL: $img is $raw bytes uncompressed (limit $max_bytes)" >&2; fail=1; }
  [[ "$entry" == '["/app"]' ]] || { echo "FAIL: $img entrypoint $entry" >&2; fail=1; }
done
exit "$fail"
