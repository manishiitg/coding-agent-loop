#!/usr/bin/env bash
set -euo pipefail

# Live acceptance test for Meta's public Muse Code installer. Everything is
# confined to a disposable HOME/install root: no login, shell profile, or
# existing Muse installation is read or changed.

case "$(uname -s):$(uname -m)" in
  Linux:x86_64|Linux:amd64|Linux:arm64|Linux:aarch64) ;;
  *)
    echo "This server-install test requires supported Linux (x86_64 or arm64)." >&2
    exit 1
    ;;
esac

for command_name in bash curl mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || {
    echo "Missing required command: $command_name" >&2
    exit 1
  }
done

scratch="$(mktemp -d "${TMPDIR:-/tmp}/agentworks-muse-install.XXXXXX")"
cleanup() {
  rm -rf -- "$scratch"
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$scratch/home" "$scratch/config" "$scratch/data"

export HOME="$scratch/home"
export XDG_CONFIG_HOME="$scratch/config"
export XDG_DATA_HOME="$scratch/data"
export MUSE_INSTALL_DIR="$scratch/bin"
export MUSE_NO_MODIFY_PATH=1
export SHELL=/bin/bash

curl \
  --fail \
  --silent \
  --show-error \
  --location \
  --max-redirs 3 \
  --proto '=https' \
  --proto-redir '=https' \
  --tlsv1.2 \
  https://dev.meta.ai/install.sh |
  bash

test -x "$MUSE_INSTALL_DIR/muse"

version="$($MUSE_INSTALL_DIR/muse --version)"
case "$version" in
  "Muse Code "*) ;;
  *)
    echo "Unexpected Muse version response: $version" >&2
    exit 1
    ;;
esac

resolved="$(PATH="$MUSE_INSTALL_DIR:/usr/bin:/bin" command -v muse)"
test "$resolved" = "$MUSE_INSTALL_DIR/muse"

printf 'Verified Muse server install: %s (%s %s)\n' \
  "$version" "$(uname -s)" "$(uname -m)"
