#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

command -v agent-browser >/dev/null 2>&1 || {
  echo "agent-browser is required for the live browser E2E" >&2
  exit 1
}

RUN_BROWSER_REAL_E2E=1 go -C "$ROOT_DIR/agent_go" test ./pkg/browser \
  -run '^TestManagedCDPMultiWorkflowRealE2E$' \
  -count=1 \
  -v
