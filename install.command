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
staged_launcher=false
cleanup() {
  if [ "$staged_launcher" = true ]; then
    rm -f "$binary_target"
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

legacy_owner_rejected_new_flags() {
  case "$1" in
    *"flag provided but not defined"*|*"unknown flag"*|*"unknown option"*)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

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
# The stable launcher is the only native writer. An existing launcher is never
# overwritten by the shell; the native journal owns that replacement. On a
# first install only, place the verified candidate at the empty stable path so
# the same stable owner can create the journal and complete the transaction.
mkdir -p "$(dirname "$binary_target")"
if [ -e "$binary_target" ]; then
  if [ ! -x "$binary_target" ]; then
    echo "stable launcher exists but is not executable: $binary_target" >&2
    exit 1
  fi
else
  cp "$source_root/bin/formal-gates" "$binary_target"
  chmod +x "$binary_target"
  staged_launcher=true
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
owner_output=""
retry_with_verified_owner() {
  # Keep the old launcher bytes available while a legacy owner is upgraded.
  # This retry also covers owners that accept the new flags but silently omit
  # the release-root copy introduced by the transaction contract.
  cp "$binary_target" "$tmp/launcher.before"
  if [ -L "$binary_target" ]; then
    rm "$binary_target"
  fi
  staged_launcher=true
  if ! cp "$source_root/bin/formal-gates" "$binary_target"; then
    cp "$tmp/launcher.before" "$binary_target"
    chmod +x "$binary_target"
    staged_launcher=false
    return 1
  fi
  chmod +x "$binary_target"
  if ! "$binary_target" "${cmd[@]:1}"; then
    cp "$tmp/launcher.before" "$binary_target"
    chmod +x "$binary_target"
    staged_launcher=false
    return 1
  fi
  staged_launcher=false
}
if ! owner_output=$("${cmd[@]}" 2>&1); then
  if ! legacy_owner_rejected_new_flags "$owner_output"; then
    printf '%s\n' "$owner_output" >&2
    exit 1
  fi
  # A launcher from before the release-root transaction contract can reject
  # these arguments before its transaction starts. Retry once with the
  # verified current binary at the fixed launcher path. When the legacy
  # launcher is a symbolic link, remove the link before copying so the retry
  # writes a real file instead of writing through the link into the old
  # release. On failure the backed-up bytes are restored as a real file: the
  # fixed launcher path keeps launching the same legacy binary while also
  # satisfying the immutable-real-file pointer invariant.
  if ! retry_with_verified_owner; then
    exit 1
  fi
fi
release_binary="$install_root/bin/formal-gates"
if [ ! -f "$release_binary" ] || ! cmp -s "$binary_target" "$release_binary"; then
  echo "stable launcher and release-root binary diverged; retrying through the verified current owner" >&2
  if ! retry_with_verified_owner; then
    echo "failed to restore launcher/release-root identity" >&2
    exit 1
  fi
fi
staged_launcher=false

# Bootstrap the already-installed artifact through the same stable launcher.
# This creates the admission receipt before any workflow command can write
# state, while keeping the source archive out of the writer path.
bootstrap_cmd=("$binary_target" install --bootstrap --source "$install_root" --release-root "$install_root" --binary-target "$binary_target" --host "$host" --scope "$scope")
if [ -n "$project" ]; then
  bootstrap_cmd+=(--project "$project")
fi
"${bootstrap_cmd[@]}"

if [ ! -f "$release_binary" ] || ! cmp -s "$binary_target" "$release_binary"; then
  echo "stable launcher and release-root binary are not identical after bootstrap" >&2
  exit 1
fi

echo "Installed package to $install_root"
echo "Native binary: $binary_target"
