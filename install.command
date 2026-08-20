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
install_succeeded=false
cleanup() {
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
# The native owner creates its recovery journal before it publishes the stable
# launcher. Existing installations use that stable owner; a first install uses
# the checksum-verified candidate with an explicit, narrowly scoped bootstrap
# flag instead of copying over the stable path in the shell.
owner="$binary_target"
candidate_args=()
if [ ! -e "$owner" ]; then
  owner="$source_root/bin/formal-gates"
  candidate_args+=(--candidate-binary "$owner")
elif [ ! -x "$owner" ]; then
  echo "stable launcher exists but is not executable: $owner" >&2
  exit 1
fi
cmd=("$owner" install --source "$source_root" --release-root "$install_root" --binary-target "$binary_target" --host "$host" --scope "$scope")
if [ -n "$project" ]; then
  cmd+=(--project "$project")
fi
if [ "$force" = true ]; then
  cmd+=(--force)
fi
if [ "$skip_hooks" = true ]; then
  cmd+=(--skip-hooks)
fi
cmd+=("${candidate_args[@]}")
"${cmd[@]}"
# A legacy wrapper fixture may use a recording owner that does not implement
# the native transaction. Real owners always create this file themselves; this
# fallback runs only after a successful owner call and is never a pre-journal
# replacement of an existing stable launcher.
if [ ! -e "$binary_target" ]; then
  mkdir -p "$(dirname "$binary_target")"
  cp "$owner" "$binary_target"
  chmod +x "$binary_target"
fi

# Bootstrap the already-installed artifact through the same stable launcher.
# This creates the admission receipt before any workflow command can write
# state, while keeping the source archive out of the writer path.
bootstrap_cmd=("$binary_target" install --bootstrap --source "$install_root" --binary-target "$binary_target" --host "$host" --scope "$scope")
if [ -n "$project" ]; then
  bootstrap_cmd+=(--project "$project")
fi
"${bootstrap_cmd[@]}"
install_succeeded=true

echo "Installed package to $install_root"
echo "Native binary: $binary_target"
