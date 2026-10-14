#!/usr/bin/env bash
# Installs the pinned toolchain from .tool-versions (plus tools/requirements.txt
# and tools/package.json) into ./.tools. Every binary download is verified
# against a SHA-256 checksum before it is unpacked.
#
# Usage: scripts/install-tools.sh            install missing / outdated tools
#        scripts/install-tools.sh --check    verify only; non-zero if anything is missing
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLS="${TOOLS_DIR:-$ROOT/.tools}"
BIN="$TOOLS/bin"
STAMPS="$TOOLS/stamps"
CHECK_ONLY=false
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=true

mkdir -p "$BIN" "$STAMPS"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
case "$os" in linux | darwin) ;; *) echo "unsupported OS: $os" >&2; exit 1 ;; esac

version_of() {
  awk -v t="$1" '$1 == t { print $2 }' "$ROOT/.tool-versions"
}

sha256() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# fetch URL DEST EXPECTED_SHA256
fetch() {
  local url="$1" dest="$2" want="$3" got
  curl -fsSL --retry 3 -o "$dest" "$url"
  got="$(sha256 "$dest")"
  if [[ "$got" != "$want" ]]; then
    echo "checksum mismatch for $url: want $want got $got" >&2
    exit 1
  fi
}

# checksum_from URL FILENAME — look up FILENAME in a published checksums file.
checksum_from() {
  local sum
  sum="$(curl -fsSL --retry 3 "$1" | awk -v f="$2" '{ n=$2; sub(/^\*/, "", n); if (n == f) print $1 }')"
  if [[ -z "$sum" ]]; then
    echo "no checksum for $2 in $1" >&2
    exit 1
  fi
  echo "$sum"
}

stamp_ok() { [[ -f "$STAMPS/$1" && "$(cat "$STAMPS/$1")" == "$2" ]]; }

missing=()
need() {
  local tool="$1" ver="$2"
  if stamp_ok "$tool" "$ver"; then return 1; fi
  if $CHECK_ONLY; then missing+=("$tool@$ver"); return 1; fi
  echo "==> installing $tool $ver"
  return 0
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# --- Go -----------------------------------------------------------------------
v="$(version_of golang)"
if need go "$v"; then
  f="go${v}.${os}-${arch}.tar.gz"
  sum="$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' | jq -r --arg f "$f" '.[].files[] | select(.filename == $f) | .sha256')"
  fetch "https://go.dev/dl/$f" "$tmp/$f" "$sum"
  rm -rf "$TOOLS/go" && tar -C "$TOOLS" -xzf "$tmp/$f"
  ln -sf "$TOOLS/go/bin/go" "$BIN/go" && ln -sf "$TOOLS/go/bin/gofmt" "$BIN/gofmt"
  echo "$v" >"$STAMPS/go"
fi

# --- Terraform ----------------------------------------------------------------
v="$(cat "$ROOT/.terraform-version")"
if need terraform "$v"; then
  f="terraform_${v}_${os}_${arch}.zip"
  base="https://releases.hashicorp.com/terraform/$v"
  fetch "$base/$f" "$tmp/$f" "$(checksum_from "$base/terraform_${v}_SHA256SUMS" "$f")"
  unzip -oq "$tmp/$f" terraform -d "$BIN"
  echo "$v" >"$STAMPS/terraform"
fi

# --- GitHub-released single binaries (archive with published checksums) -------
# gh_tool NAME REPO TAG ASSET CHECKSUM_ASSET BINARY_PATH_IN_ARCHIVE
gh_tool() {
  local name="$1" repo="$2" tag="$3" asset="$4" sums="$5" inner="$6" ver="$7"
  need "$name" "$ver" || return 0
  local base="https://github.com/$repo/releases/download/$tag"
  fetch "$base/$asset" "$tmp/$asset" "$(checksum_from "$base/$sums" "$asset")"
  mkdir -p "$tmp/x-$name"
  case "$asset" in
    *.zip) unzip -oq "$tmp/$asset" -d "$tmp/x-$name" ;;
    *.tar.gz) tar -C "$tmp/x-$name" -xzf "$tmp/$asset" ;;
  esac
  install -m 0755 "$tmp/x-$name/$inner" "$BIN/$name"
  echo "$ver" >"$STAMPS/$name"
}

v="$(version_of gitleaks)"
ga="$arch"; [[ "$arch" == amd64 ]] && ga=x64
gh_tool gitleaks gitleaks/gitleaks "v$v" "gitleaks_${v}_${os}_${ga}.tar.gz" "gitleaks_${v}_checksums.txt" gitleaks "$v"

v="$(version_of golangci-lint)"
gh_tool golangci-lint golangci/golangci-lint "v$v" "golangci-lint-${v}-${os}-${arch}.tar.gz" "golangci-lint-${v}-checksums.txt" "golangci-lint-${v}-${os}-${arch}/golangci-lint" "$v"

v="$(version_of tflint)"
gh_tool tflint terraform-linters/tflint "v$v" "tflint_${os}_${arch}.zip" checksums.txt tflint "$v"

v="$(version_of trivy)"
ta="64bit"; [[ "$arch" == arm64 ]] && ta=ARM64
to="Linux"; [[ "$os" == darwin ]] && to=macOS
gh_tool trivy aquasecurity/trivy "v$v" "trivy_${v}_${to}-${ta}.tar.gz" "trivy_${v}_checksums.txt" trivy "$v"

v="$(version_of actionlint)"
gh_tool actionlint rhysd/actionlint "v$v" "actionlint_${v}_${os}_${arch}.tar.gz" "actionlint_${v}_checksums.txt" actionlint "$v"

v="$(version_of terraform-docs)"
gh_tool terraform-docs terraform-docs/terraform-docs "v$v" "terraform-docs-v${v}-${os}-${arch}.tar.gz" "terraform-docs-v${v}.sha256sum" terraform-docs "$v"

v="$(version_of k6)"
ka="$os-$arch"; [[ "$os" == darwin ]] && ka="macos-$arch"
k6ext=tar.gz; [[ "$os" == darwin ]] && k6ext=zip
gh_tool k6 grafana/k6 "v$v" "k6-v${v}-${ka}.${k6ext}" "k6-v${v}-checksums.txt" "k6-v${v}-${ka}/k6" "$v"

v="$(version_of crane)"
co="Linux"; [[ "$os" == darwin ]] && co=Darwin
ca="x86_64"; [[ "$arch" == arm64 ]] && ca=arm64
gh_tool crane google/go-containerregistry "v$v" "go-containerregistry_${co}_${ca}.tar.gz" checksums.txt crane "$v"

# --- shellcheck (no checksum file is published; digests pinned here) ---------
v="$(version_of shellcheck)"
if need shellcheck "$v"; then
  [[ "$v" == "0.11.0" ]] || { echo "update pinned shellcheck digests for $v" >&2; exit 1; }
  sa="x86_64"; [[ "$arch" == arm64 ]] && sa=aarch64
  case "$os.$sa" in
    linux.x86_64) sum=8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198 ;;
    linux.aarch64) sum=12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588 ;;
    darwin.x86_64) sum=3c89db4edcab7cf1c27bff178882e0f6f27f7afdf54e859fa041fca10febe4c6 ;;
    darwin.aarch64) sum=56affdd8de5527894dca6dc3d7e0a99a873b0f004d7aabc30ae407d3f48b0a79 ;;
  esac
  f="shellcheck-v${v}.${os}.${sa}.tar.xz"
  fetch "https://github.com/koalaman/shellcheck/releases/download/v$v/$f" "$tmp/$f" "$sum"
  tar -C "$tmp" -xJf "$tmp/$f"
  install -m 0755 "$tmp/shellcheck-v$v/shellcheck" "$BIN/shellcheck"
  echo "$v" >"$STAMPS/shellcheck"
fi

# --- govulncheck (Go module; verified by the Go checksum database) ------------
v="$(version_of govulncheck)"
if need govulncheck "$v"; then
  GOBIN="$BIN" GOTOOLCHAIN=local "$BIN/go" install "golang.org/x/vuln/cmd/govulncheck@v$v"
  echo "$v" >"$STAMPS/govulncheck"
fi

# --- Python tools (hash-pinned) -------------------------------------------------
reqhash="$(sha256 "$ROOT/tools/requirements.txt")"
if need python-tools "$reqhash"; then
  rm -rf "$TOOLS/venv"
  python3 -m venv "$TOOLS/venv"
  "$TOOLS/venv/bin/pip" install --quiet --disable-pip-version-check --require-hashes --no-deps -r "$ROOT/tools/requirements.txt"
  for b in checkov zizmor pre-commit; do ln -sf "$TOOLS/venv/bin/$b" "$BIN/$b"; done
  echo "$reqhash" >"$STAMPS/python-tools"
fi

# --- Node tools (lockfile-pinned) ----------------------------------------------
lockhash="$(sha256 "$ROOT/tools/package-lock.json")"
if need node-tools "$lockhash"; then
  rm -rf "$TOOLS/node" && mkdir -p "$TOOLS/node"
  cp "$ROOT/tools/package.json" "$ROOT/tools/package-lock.json" "$TOOLS/node/"
  PUPPETEER_CACHE_DIR="$TOOLS/puppeteer" npm ci --prefix "$TOOLS/node" --no-audit --no-fund --loglevel=error
  for b in markdownlint-cli2 mmdc; do ln -sf "$TOOLS/node/node_modules/.bin/$b" "$BIN/$b"; done
  echo "$lockhash" >"$STAMPS/node-tools"
fi

# --- Headless Chrome shared libraries (Linux hosts without them, no root) ------
if [[ "$os" == linux ]] && ! $CHECK_ONLY; then
  chrome="$(find "$TOOLS/puppeteer" -type f -name 'chrome-headless-shell' 2>/dev/null | head -1)"
  if [[ -n "$chrome" ]]; then
    missing_libs="$(LD_LIBRARY_PATH="$TOOLS/lib" ldd "$chrome" | awk '/not found/ {print $1}')"
    if [[ -n "$missing_libs" ]] && command -v apt-get >/dev/null; then
      echo "==> fetching Chrome runtime libraries into .tools/lib: $(echo "$missing_libs" | tr '\n' ' ')"
      mkdir -p "$tmp/debs" "$TOOLS/lib"
      pkgs=()
      for lib in $missing_libs; do
        case "$lib" in
          libnss3* | libnssutil3* | libsmime3*) pkgs+=(libnss3) ;;
          libnspr4* | libplc4* | libplds4*) pkgs+=(libnspr4) ;;
          libasound*) pkgs+=(libasound2t64) ;;
          *) echo "warning: no package mapping for $lib" >&2 ;;
        esac
      done
      if ((${#pkgs[@]})); then
        mapfile -t pkgs < <(printf '%s\n' "${pkgs[@]}" | sort -u)
        (cd "$tmp/debs" && apt-get download "${pkgs[@]}" >/dev/null)
        for d in "$tmp"/debs/*.deb; do dpkg-deb -x "$d" "$tmp/debs/root"; done
        find "$tmp/debs/root" -name '*.so*' -exec cp -a {} "$TOOLS/lib/" \;
      fi
    fi
  fi
fi

if $CHECK_ONLY; then
  if ((${#missing[@]})); then
    echo "missing or outdated tools: ${missing[*]} (run: make tools)" >&2
    exit 1
  fi
  echo "all pinned tools present"
fi
