#!/usr/bin/env bash
# Builds each service once as a single-platform linux/arm64 OCI image layout in
# dist/images/<service>/ — the artifact every environment receives unchanged
# (ADR-0017: build once, promote the same digest). Attestations are off so each
# layout holds exactly one image manifest, whose digest ECR must report after
# every push (scripts/push-images.sh verifies it).
# Usage: scripts/build-oci.sh   (tag = 12-char git SHA; refuses a dirty tree
#        unless ALLOW_DIRTY=1, which tags "<sha>-dirty" for local testing only)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
services=(redirect api worker nightly)

tag="$(git rev-parse --short=12 HEAD)"
if [[ -n "$(git status --porcelain)" ]]; then
  [[ "${ALLOW_DIRTY:-}" == 1 ]] || { echo "refusing: working tree is dirty; images must correspond to a commit" >&2; exit 1; }
  tag="$tag-dirty"
fi
version="$(scripts/version.sh)" # ADR-0026; the image tag stays the bare SHA
out="dist/images"
rm -rf "$out" && mkdir -p "$out"

for svc in "${services[@]}"; do
  docker buildx build --quiet --platform linux/arm64 --provenance=false --sbom=false \
    --build-arg SERVICE="$svc" --build-arg VERSION="$version" --build-arg REVISION="$tag" -f build/Dockerfile \
    --output "type=oci,dest=$out/$svc,tar=false" . >/dev/null
  n="$(jq '.manifests | length' "$out/$svc/index.json")"
  [[ "$n" == 1 ]] || { echo "$svc: expected 1 manifest in the layout, found $n" >&2; exit 1; }
  digest="$(jq -r '.manifests[0].digest' "$out/$svc/index.json")"
  config="$(jq -r '.config.digest' "$out/$svc/blobs/sha256/${digest#sha256:}")"
  arch="$(jq -r '.architecture + "/" + .os + " user=" + .config.User' "$out/$svc/blobs/sha256/${config#sha256:}")"
  [[ "$arch" == "arm64/linux user=65532" || "$arch" == "arm64/linux user=65532:65532" ]] || { echo "$svc: unexpected image config: $arch" >&2; exit 1; }
  echo "$svc $digest"
done | tee "$out/digests.txt"
echo "$tag" >"$out/tag"
echo "$version" >"$out/version"
echo "built $(wc -l <"$out/digests.txt") images, tag $tag, version $version -> $out"
