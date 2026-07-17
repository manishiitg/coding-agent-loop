#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTI_LLM_DIR="$(cd "$ROOT_DIR/../multi-llm-provider-go" && pwd)"
PROVIDERS="claude-code,codex-cli,cursor-cli,pi-cli"
SERVER_URL="http://localhost:18743"
WORKSPACE_API_URL="http://127.0.0.1:18744"
WORKSPACE_DOCS="$ROOT_DIR/workspace-docs"

usage() {
  cat <<'EOF'
Usage: ./scripts/run-coding-cli-p0.sh [options]

Runs the authenticated, live coding-CLI P0 suite. There is no fast or mocked
P0 mode: every selected provider uses its real CLI and the MCP agent bridge.

Options:
  --providers LIST       Comma-separated providers (default: all active CLIs)
  --server-url URL       Live AgentWorks server (default: http://localhost:18743)
  --workspace-api URL    Live workspace API (default: http://127.0.0.1:18744)
  --workspace-docs PATH  Absolute workspace-docs path
  --multi-llm-dir PATH   multi-llm-provider-go checkout path
  -h, --help             Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --providers)
      PROVIDERS="${2:?--providers requires a value}"
      shift 2
      ;;
    --server-url)
      SERVER_URL="${2:?--server-url requires a value}"
      shift 2
      ;;
    --workspace-api)
      WORKSPACE_API_URL="${2:?--workspace-api requires a value}"
      shift 2
      ;;
    --workspace-docs)
      WORKSPACE_DOCS="$(cd "${2:?--workspace-docs requires a value}" && pwd)"
      shift 2
      ;;
    --multi-llm-dir)
      MULTI_LLM_DIR="$(cd "${2:?--multi-llm-dir requires a value}" && pwd)"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

# Live provider credentials belong to the same local runtime environment as
# the server. The server launcher sources agent_go/.env, so the P0 runner must
# do the same; otherwise a configured Pi/Gemini credential is invisible to the
# test process and every live Pi contract is incorrectly reported as skipped.
# This is credential loading only—the sole test-mode switch remains
# -coding-cli-p0-live, with no provider-specific RUN_* gates.
if [[ -f "$ROOT_DIR/agent_go/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "$ROOT_DIR/agent_go/.env"
  set +a
fi

if [[ -z "${MCP_API_TOKEN:-}" ]]; then
  echo "Live P0 requires MCP_API_TOKEN to match the live server's MCP_SERVER_API_TOKEN." >&2
  echo "Start the isolated server with MCP_SERVER_API_TOKEN set, then run this command with the same value in MCP_API_TOKEN." >&2
  exit 1
fi

if [[ "$(printf '%s' "$PROVIDERS" | tr '[:upper:]' '[:lower:]')" == "all" ]]; then
  PROVIDERS="claude-code,codex-cli,cursor-cli,pi-cli"
fi

for endpoint in "$SERVER_URL" "$WORKSPACE_API_URL"; do
  if ! curl -sS --max-time 3 -o /dev/null "$endpoint"; then
    echo "Live P0 prerequisite is unreachable: $endpoint" >&2
    exit 1
  fi
done

p0_test_regex() {
  local provider="$1"
  local package_path="$2"
  go -C "$MULTI_LLM_DIR" run ./cmd/coding-agent-p0-tests -provider "$provider" -package "$package_path"
}

run_required_go_tests() {
  local receipt
  receipt="$(mktemp)"
  if ! "$@" | tee "$receipt"; then
    rm -f "$receipt"
    return 1
  fi
  if grep -Eq '"Action":"skip".*"Test":' "$receipt"; then
    echo "A required P0 test skipped; authenticated P0 runs must execute every selected test." >&2
    rm -f "$receipt"
    return 1
  fi
  if ! grep -Eq '"Action":"pass".*"Test":' "$receipt"; then
    echo "No required P0 test reported a pass." >&2
    rm -f "$receipt"
    return 1
  fi
  rm -f "$receipt"
}

# Fail before launching CLIs if any registered live P0 test moved outside its
# provider package or cannot be selected by the runner.
p0_test_regex claude-code pkg/adapters/claudecode >/dev/null
p0_test_regex codex-cli pkg/adapters/codexcli >/dev/null
p0_test_regex cursor-cli pkg/adapters/cursorcli >/dev/null
p0_test_regex pi-cli pkg/adapters/picli >/dev/null

IFS=',' read -r -a provider_list <<< "$PROVIDERS"
for raw_provider in "${provider_list[@]}"; do
  provider="$(printf '%s' "$raw_provider" | tr '[:upper:]' '[:lower:]' | xargs)"
  case "$provider" in
    claude-code)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/claudecode)"
      run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/claudecode \
        -run "$test_regex" -count=1 -timeout=35m -args -coding-cli-p0-live
      ;;
    codex-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/codexcli)"
      run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/codexcli \
        -run "$test_regex" -count=1 -timeout=35m -args -coding-cli-p0-live
      ;;
    cursor-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/cursorcli)"
      run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/cursorcli \
        -run "$test_regex" -count=1 -timeout=35m -args -coding-cli-p0-live
      ;;
    pi-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/picli)"
      run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/picli \
        -run "$test_regex" -count=1 -timeout=35m -args -coding-cli-p0-live
      ;;
    *)
      echo "Unknown coding CLI P0 provider: $provider" >&2
      exit 2
      ;;
  esac

  # The release-blocking application contract must exercise the CLI with the
  # real MCP agent bridge active. This launches a plan step, performs a bridge
  # file operation, and proves its completion AUTO-NOTIFICATION contains only
  # the final assistant message rather than the MCP call/output transcript.
  go -C "$ROOT_DIR/agent_go" run . test workflow-auto-notification-e2e \
    --server-url "$SERVER_URL" --workspace-docs "$WORKSPACE_DOCS" \
    --provider "$provider" --timeout 8m
done

run_required_go_tests go -C "$ROOT_DIR/agent_go" test -json ./pkg/orchestrator/types \
  -tags=coding_cli_p0_live -run 'TestCodingCLIWorkflowP0CompletionAdvancesNextStep$' \
  -count=1 -timeout=50m -args \
  -coding-cli-p0-providers="$PROVIDERS" \
  -coding-cli-p0-workspace-api="$WORKSPACE_API_URL" \
  -coding-cli-p0-workspace-docs="$WORKSPACE_DOCS"

echo "P0 live CLI, MCP bridge, and workflow contracts passed for: $PROVIDERS"
