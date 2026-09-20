#!/usr/bin/env bash
# Shared deployment entry point. Each server keeps its existing guarded
# implementation; this script only selects the correct one.
set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
SERVER="${1:-}"
[[ $# -eq 0 ]] || shift

usage() {
  cat <<'EOF'
Usage: ./deploy.sh <server> [target]

Servers:
  rts, video-studio     video.realtrainingsys.com
  confida               Confida rootless Linux deployment
  sparkquill            SparkQuill rootless Linux deployment
  dominion              trader.tectonicmarkets.com (isolated Hetzner deployment)

dominion optionally takes --activate (stage-only otherwise):
  ./deploy.sh dominion              # clone/pull, build, stage a release
  ./deploy.sh dominion --activate   # also flip `current` and restart services
EOF
}

reject_extra_arguments() {
  if [[ $# -gt 0 ]]; then
    echo "Server '$SERVER' does not accept additional arguments." >&2
    usage >&2
    exit 2
  fi
}

case "$SERVER" in
  rts|video-studio)
    reject_extra_arguments "$@"
    exec bash "$REPO_ROOT/deploy/aws-ec2/deploy-rootless.sh"
    ;;
  confida|sparkquill)
    reject_extra_arguments "$@"
    exec bash "$REPO_ROOT/deploy/rootless-linux/deploy.sh" "$SERVER"
    ;;
  dominion)
    ACTIVATE_FLAG=""
    if [[ $# -gt 0 ]]; then
      if [[ $# -gt 1 || "$1" != "--activate" ]]; then
        echo "Server 'dominion' only accepts an optional --activate flag." >&2
        usage >&2
        exit 2
      fi
      ACTIVATE_FLAG="--activate"
    fi
    # deploy-dominion.sh runs natively ON the box (native go build, pulls the
    # three public source repos itself) — it is not something to exec
    # locally. Its own on-disk copy at the fixed path below self-updates from
    # the repo it just synced on every run (see the script's own comments),
    # so this only ever needs to invoke that fixed path, never push a copy.
    DOMINION_HOST="${DOMINION_HOST:-116.202.210.102}"
    DOMINION_PORT="${DOMINION_PORT:-2299}"
    DOMINION_USER="${DOMINION_USER:-dominion}"
    DOMINION_REMOTE_SCRIPT="${DOMINION_REMOTE_SCRIPT:-/srv/dominion/deploy-dominion.sh}"
    SSH_ARGS=(-p "$DOMINION_PORT" -o BatchMode=yes -o StrictHostKeyChecking=accept-new)
    [[ -z "${DOMINION_SSH_KEY:-}" ]] || SSH_ARGS+=(-i "$DOMINION_SSH_KEY")
    REMOTE_CMD=(bash "$DOMINION_REMOTE_SCRIPT")
    [[ -z "$ACTIVATE_FLAG" ]] || REMOTE_CMD+=("$ACTIVATE_FLAG")
    exec ssh "${SSH_ARGS[@]}" "$DOMINION_USER@$DOMINION_HOST" "${REMOTE_CMD[@]}"
    ;;
  -h|--help|help)
    usage
    ;;
  --list)
    printf '%s\n' rts confida sparkquill dominion
    ;;
  "")
    usage >&2
    exit 2
    ;;
  *)
    echo "Unknown deployment server: $SERVER" >&2
    usage >&2
    exit 2
    ;;
esac
