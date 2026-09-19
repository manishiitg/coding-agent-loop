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
  agents, dedicated-vm  agents.excellencetechnologies.in

The optional target is supported only by dedicated-vm:
  all (default), frontend, agent, workspace
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
  agents|dedicated-vm)
    if [[ $# -gt 1 ]]; then
      echo "Server '$SERVER' accepts at most one deployment target." >&2
      usage >&2
      exit 2
    fi
    TARGET="${1:-all}"
    case "$TARGET" in
      all|frontend|agent|workspace) ;;
      *)
        echo "Unknown dedicated-vm target: $TARGET" >&2
        usage >&2
        exit 2
        ;;
    esac
    exec bash "$REPO_ROOT/deploy/dedicated-vm/quick-deploy.sh" "$TARGET"
    ;;
  -h|--help|help)
    usage
    ;;
  --list)
    printf '%s\n' rts confida sparkquill agents
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
