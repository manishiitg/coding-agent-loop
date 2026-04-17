#!/usr/bin/env bash
# Sync a single Workflow folder between local and the Hetzner VM.
# Local:  <repo>/workspace-docs/Workflow/<name>/
# Remote: /data/docs/Workflow/<name>/
#
# Usage:
#   ./sync-workflow.sh push <name> [--delete]   # local  -> server
#   ./sync-workflow.sh pull <name> [--delete]   # server -> local
#   ./sync-workflow.sh push <name> --dry-run    # preview only
#
# --delete mirrors the source (removes files on the target that aren't in source).
# Without --delete, rsync only adds/updates; it never removes.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

VM=138.201.227.99
KEY="$HOME/.ssh/hetzner_mcp"
RSYNC_SSH="ssh -i $KEY -o StrictHostKeyChecking=no"

LOCAL_BASE="$REPO_ROOT/workspace-docs/Workflow"
REMOTE_BASE="/data/docs/Workflow"

usage() {
  grep '^#' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

DIRECTION="${1:-}"
NAME="${2:-}"
shift 2 2>/dev/null || usage

EXTRA_FLAGS=()
for arg in "$@"; do
  case "$arg" in
    --delete)  EXTRA_FLAGS+=("--delete") ;;
    --dry-run) EXTRA_FLAGS+=("--dry-run" "--itemize-changes") ;;
    *) echo "Unknown flag: $arg"; usage ;;
  esac
done

if [ -z "$DIRECTION" ] || [ -z "$NAME" ]; then
  usage
fi

LOCAL_DIR="$LOCAL_BASE/$NAME/"
REMOTE_DIR="$REMOTE_BASE/$NAME/"

case "$DIRECTION" in
  push)
    if [ ! -d "$LOCAL_DIR" ]; then
      echo "Error: local folder not found: $LOCAL_DIR"
      exit 1
    fi
    echo "==> push: $LOCAL_DIR -> root@$VM:$REMOTE_DIR"
    ssh -i "$KEY" -o StrictHostKeyChecking=no "root@$VM" "mkdir -p '$REMOTE_DIR'"
    rsync -az --progress ${EXTRA_FLAGS[@]+"${EXTRA_FLAGS[@]}"} \
      -e "$RSYNC_SSH" \
      "$LOCAL_DIR" "root@$VM:$REMOTE_DIR"
    ;;
  pull)
    mkdir -p "$LOCAL_DIR"
    echo "==> pull: root@$VM:$REMOTE_DIR -> $LOCAL_DIR"
    rsync -az --progress ${EXTRA_FLAGS[@]+"${EXTRA_FLAGS[@]}"} \
      -e "$RSYNC_SSH" \
      "root@$VM:$REMOTE_DIR" "$LOCAL_DIR"
    ;;
  *)
    echo "Error: direction must be 'push' or 'pull' (got '$DIRECTION')"
    usage
    ;;
esac

echo "==> Done"
