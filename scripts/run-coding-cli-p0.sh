#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTI_LLM_DIR="$(cd "$ROOT_DIR/../multi-llm-provider-go" && pwd)"
MCPAGENT_DIR="$(cd "$ROOT_DIR/../mcpagent" && pwd)"
PROVIDERS="claude-code,codex-cli,cursor-cli,pi-cli,muse-cli"
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
  --mcpagent-dir PATH    mcpagent checkout path
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
    --mcpagent-dir)
      MCPAGENT_DIR="$(cd "${2:?--mcpagent-dir requires a value}" && pwd)"
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

# Match the coding-CLI release selected by the live AgentWorks server. The
# server prepends this private shim directory during startup, but a separately
# launched P0 shell cannot inherit environment changes from that process.
# Without this, P0 can silently exercise an older CLI from the developer's
# PATH while the application is using the current managed release.
if [[ -z "${AGENTWORKS_MANAGED_CLI_BIN:-}" ]]; then
  if [[ -n "${AGENTWORKS_STATE_ROOT:-}" ]]; then
    managed_cli_bin="$AGENTWORKS_STATE_ROOT/cli-updates/bin"
  elif [[ "$(uname -s)" == "Darwin" ]]; then
    managed_cli_bin="$HOME/Library/Application Support/agentworks/cli-updates/bin"
  else
    managed_cli_bin="${XDG_CONFIG_HOME:-$HOME/.config}/agentworks/cli-updates/bin"
  fi
  if [[ -d "$managed_cli_bin" ]]; then
    export AGENTWORKS_MANAGED_CLI_BIN="$managed_cli_bin"
  fi
fi
if [[ -n "${AGENTWORKS_MANAGED_CLI_BIN:-}" ]]; then
  export PATH="$AGENTWORKS_MANAGED_CLI_BIN:$PATH"
fi

# Keep release certification on the same Go line required by AgentWorks.
# Developer machines may have a newer auto-selected toolchain installed; a
# Go 1.27.1 runtime regression currently corrupts the heap after a completed
# interactive Codex turn, while the identical contract is stable on 1.26.0.
# The override remains available for validating a future fixed toolchain.
export GOTOOLCHAIN="${CODING_CLI_P0_GO_TOOLCHAIN:-go1.26.0}"

if [[ -z "${MCP_API_TOKEN:-}" ]]; then
  echo "Live P0 requires MCP_API_TOKEN to match the live server's MCP_SERVER_API_TOKEN." >&2
  echo "Start the isolated server with MCP_SERVER_API_TOKEN set, then run this command with the same value in MCP_API_TOKEN." >&2
  exit 1
fi

if [[ "$(printf '%s' "$PROVIDERS" | tr '[:upper:]' '[:lower:]')" == "all" ]]; then
  PROVIDERS="claude-code,codex-cli,cursor-cli,pi-cli,muse-cli"
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
p0_test_regex muse-cli pkg/adapters/musecli >/dev/null

# IC-12 is provider-neutral and credential-free. Run it before spending any
# live-provider capacity so a missing turn ID or duplicate/missing canonical
# completion blocks the release immediately.
run_required_go_tests go -C "$MCPAGENT_DIR" test -json ./agent \
  -run '^TestP0CanonicalTurnContract$' -count=1

# Deterministic reproductions accompany the live proof: force the late-commit
# steering boundary and completion-flush races rather than relying on timing.
run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/cursorcli \
  -run '^(TestRetainedProgressKeepsNarrationAcrossSteerAndNormalStream|TestCursorRetainedControlsSteerQueuedFollowups|TestCursorStreamNarrationRejectsPartialStreams|TestCursorStreamNarrationAllowsSchemaDiscovery)$' -count=1
run_required_go_tests go -C "$MCPAGENT_DIR" test -json ./agent \
  -run '^(TestRetainedProgressStreamsWhileFinalIsPending|TestRetainedCompletionFlushesProgressCommittedAfterPoll)$' -count=1
run_required_go_tests go -C "$ROOT_DIR/agent_go" test -json ./cmd/testing \
  -run '^TestProgressP0' -count=1
npm --prefix "$ROOT_DIR/frontend" test -- src/utils/transcriptChunkUpdates.test.ts

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
      # Codex P0 cases own global tmux/session cleanup. Keep each live case in
      # its own Go process so cleanup from a completed test cannot corrupt or
      # tear down state belonging to the next one. This also gives each failed
      # contract an exact receipt instead of losing it in a package crash.
      while IFS= read -r test_name; do
        [[ -n "$test_name" ]] || continue
        run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/codexcli \
          -run "^${test_name}$" -count=1 -timeout=8m -args -coding-cli-p0-live
      done < <(printf '%s\n' "$test_regex" | sed -e 's/^\^(//' -e 's/)\$$//' -e 's/|/\
/g')
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
    muse-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/musecli)"
      run_required_go_tests go -C "$MULTI_LLM_DIR" test -json ./pkg/adapters/musecli \
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

  # IC-11 + IC-12: cross the AgentWorks -> mcpagent boundary after a real first turn
  # has completed and the provider tmux is retained. This proves the warm
  # follow-up uses the durable Session, returns one authoritative tool receipt
  # (arguments/result/duration), persists exactly one final response, avoids
  # Agent reconstruction, requires stable same-turn IDs plus the canonical
  # completion marker, and leaves the tmux reusable. Cursor also requires
  # every retained narration over the chat SSE stream, including a busy steer.
  go -C "$ROOT_DIR/agent_go" run . test coding-agent-chat-e2e \
    --server-url "$SERVER_URL" --provider "$provider" \
    --selected-folder "_users/default/Chats" --workspace-docs "$WORKSPACE_DOCS" \
    --retained-window-p0-only --timeout 8m
done

run_required_go_tests go -C "$ROOT_DIR/agent_go" test -json ./pkg/orchestrator/types \
  -tags=coding_cli_p0_live -run 'TestCodingCLIWorkflowP0CompletionAdvancesNextStep$' \
  -count=1 -timeout=50m -args \
  -coding-cli-p0-providers="$PROVIDERS" \
  -coding-cli-p0-workspace-api="$WORKSPACE_API_URL" \
  -coding-cli-p0-workspace-docs="$WORKSPACE_DOCS"

echo "P0 live CLI, retained-session, MCP bridge, and workflow contracts passed for: $PROVIDERS"
