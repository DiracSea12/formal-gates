#!/usr/bin/env bash
set -euo pipefail

repo="DiracSea12/formal-gates"
tag="${FORMAL_GATES_VERSION:-v0.1.0}"
host="claude"
scope="global"
project=""
force=false
skip_hooks=false

usage() {
  cat <<'EOF'
Usage: install.command [--version vX.Y.Z] [--host claude|codex|cursor|dsh|both] [--scope global|project] [--project PATH] [--force] [--skip-hooks]

Downloads the release source snapshot and the matching native binary for the current platform,
assembles a local package copy, and runs formal-gates install against the selected target host.
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version) tag="$2"; shift 2 ;;
    --host) host="$2"; shift 2 ;;
    --scope) scope="$2"; shift 2 ;;
    --project) project="$2"; shift 2 ;;
    --force) force=true; shift ;;
    --skip-hooks) skip_hooks=true; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown argument: $1" >&2; usage >&2; exit 1 ;;
  esac
done

os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os="macos" ;;
  Linux) os="linux" ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
  arm64|aarch64) arch="arm64" ;;
  x86_64|amd64) arch="amd64" ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac

suffix="${os}-${arch}"
case "$suffix" in
  macos-arm64|macos-amd64|linux-amd64) ;;
  *) echo "unsupported release platform: $suffix" >&2; exit 1 ;;
esac
binary="formal-gates-${suffix}"
canary="portable-canary-${suffix}.json"
checksums="SHA256SUMS-${suffix}.txt"

tmp="$(mktemp -d)"
staged_launcher=""
install_succeeded=false
cleanup() {
  if [ -n "$staged_launcher" ] && [ "$install_succeeded" != true ]; then
    rm -f "$staged_launcher"
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

curl -fsSL "https://api.github.com/repos/${repo}/zipball/${tag}" -o "$tmp/source.zip"
curl -fsSL "https://github.com/${repo}/releases/download/${tag}/${binary}" -o "$tmp/${binary}"
curl -fsSL "https://github.com/${repo}/releases/download/${tag}/${canary}" -o "$tmp/${canary}"
curl -fsSL "https://github.com/${repo}/releases/download/${tag}/${checksums}" -o "$tmp/${checksums}"

unzip -q "$tmp/source.zip" -d "$tmp/source"
source_root="$(find "$tmp/source" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
if [ -z "$source_root" ]; then
  echo "failed to unpack source zip" >&2
  exit 1
fi

(
  cd "$tmp"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c "$checksums"
  else
    shasum -a 256 -c "$checksums"
  fi
)

mkdir -p "$source_root/bin"
cp "$tmp/${binary}" "$source_root/bin/formal-gates"
chmod +x "$source_root/bin/formal-gates"

home="${HOME:-$(python3 - <<'PY'
import pathlib
print(pathlib.Path.home())
PY
)}"
install_root="$home/.formal-gates/releases/${tag#v}-${suffix}"
binary_target="$home/.local/bin/formal-gates"
# The checksum-verified executable is staged once at the fixed launcher path.
# The source-tree copy is install input only and never owns registry/workflow
# writes.  On a failed first install cleanup removes the staged launcher.
mkdir -p "$(dirname "$binary_target")"
if [ -e "$binary_target" ] && [ ! -x "$binary_target" ]; then
  echo "stable launcher exists but is not executable: $binary_target" >&2
  exit 1
fi
if [ ! -e "$binary_target" ]; then
  cp "$source_root/bin/formal-gates" "$binary_target"
  chmod +x "$binary_target"
  staged_launcher="$binary_target"
fi
cmd=("$binary_target" install --source "$source_root" --release-root "$install_root" --binary-target "$binary_target" --host "$host" --scope "$scope")
if [ -n "$project" ]; then
  cmd+=(--project "$project")
fi
if [ "$force" = true ]; then
  cmd+=(--force)
fi
if [ "$skip_hooks" = true ]; then
  cmd+=(--skip-hooks)
fi
"${cmd[@]}"
install_succeeded=true

echo "Installed package to $install_root"
echo "Native binary: $binary_target"
