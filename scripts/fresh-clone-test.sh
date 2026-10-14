#!/usr/bin/env bash
# Fresh-clone test (SPEC §18 M9): clone the committed HEAD into a temporary
# directory, run the README "Local in 5 minutes" quickstart VERBATIM (the bash
# blocks between the quickstart-local markers), require that the click was
# counted, then run the integration tests from the clone.
#
# Usage: scripts/fresh-clone-test.sh
#   FRESH_TOOLS=1  install the toolchain from scratch in the clone (slow);
#                  default links this checkout's verified ./.tools (the
#                  quickstart itself needs only Docker, make, curl and jq).
# The local stack must not be running elsewhere (ports 8080/8081).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

for port in 8080 8081; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "port $port is in use: run 'make down' in the other checkout first" >&2
    exit 1
  fi
done

work="$(mktemp -d)"
clone="$work/url-shortener-ecs-fargate"
cleanup() {
  (cd "$clone" 2>/dev/null && make -s down >/dev/null 2>&1) || true
  rm -rf "$work"
}
trap cleanup EXIT

git clone --quiet "$ROOT" "$clone"
echo "cloned $(git -C "$clone" rev-parse --short=12 HEAD) into $clone"

# Extract the quickstart exactly as written.
awk '/<!-- quickstart-local:start -->/ {on=1; next}
     /<!-- quickstart-local:end -->/ {on=0}
     on && /^```bash$/ {code=1; next}
     on && code && /^```$/ {code=0; next}
     on && code {print}' "$clone/README.md" >"$work/quickstart.sh"
[[ -s "$work/quickstart.sh" ]] || { echo "no quickstart blocks found between the README markers" >&2; exit 1; }
cat >>"$work/quickstart.sh" <<'CHECK'
[ "$clicks" -ge 1 ] || { echo "FAIL: the click was not counted" >&2; exit 1; }
CHECK

echo "== README quickstart ($(grep -c . "$work/quickstart.sh") lines)"
(cd "$clone" && bash -euo pipefail "$work/quickstart.sh") 2>&1 | grep -v 'shortener_[a-z]*_' # never show a key

if [[ "${FRESH_TOOLS:-}" == 1 ]]; then
  (cd "$clone" && make tools)
else
  ln -s "$ROOT/.tools" "$clone/.tools"
fi

echo "== integration tests from the clone"
(cd "$clone" && make test-integration)
echo "fresh-clone test passed"
