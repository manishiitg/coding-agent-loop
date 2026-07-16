#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTI_LLM_DIR="${MULTI_LLM_DIR:-$(cd "$ROOT_DIR/../multi-llm-provider-go" && pwd)}"
PROVIDERS="${CODING_CLI_P0_PROVIDERS:-claude-code,codex-cli,cursor-cli,pi-cli}"

p0_test_regex() {
  local provider="$1"
  local package_path="$2"
  go -C "$MULTI_LLM_DIR" run ./cmd/coding-agent-p0-tests -provider "$provider" -package "$package_path"
}

run_required_go_tests() {
  local receipt
  receipt="$(mktemp)"
  if ! "$@" -json | tee "$receipt"; then
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

go -C "$MULTI_LLM_DIR" test . -run 'TestActiveCodingAgentProvidersSatisfyP0Contract|TestCodingAgentCertificationReferencesExistingTests' -count=1
go -C "$ROOT_DIR/agent_go" test ./cmd/server -run 'Test(HandleLiveInputMessageBusyCodingAgentDeliversExactlyOnce|HandleLiveInputMessageCompletionBoundaryChoosesExactlyOneRoute|StartNextTurnFromLiveInputAcknowledgesBeforeQueuedTurnRuns)$' -count=1
go -C "$ROOT_DIR/agent_go" test ./pkg/orchestrator/types -run 'TestCodingCLIWorkflowP0(ProviderMatrix|CompletionAdvancesNextStep)$' -count=1
npm --prefix "$ROOT_DIR/frontend" test -- src/utils/liveInputSubmission.test.ts

# Fail the fast gate if any registered P0 test moved outside its provider
# package or cannot be selected by the authenticated runner.
p0_test_regex claude-code pkg/adapters/claudecode >/dev/null
p0_test_regex codex-cli pkg/adapters/codexcli >/dev/null
p0_test_regex cursor-cli pkg/adapters/cursorcli >/dev/null
p0_test_regex pi-cli pkg/adapters/picli >/dev/null

if [[ "${RUN_CODING_CLI_P0_REAL:-}" != "1" ]]; then
  echo "P0 fast contract passed. Set RUN_CODING_CLI_P0_REAL=1 to run authenticated CLI and workflow E2Es."
  exit 0
fi

IFS=',' read -r -a provider_list <<< "$PROVIDERS"
for raw_provider in "${provider_list[@]}"; do
  provider="$(printf '%s' "$raw_provider" | tr '[:upper:]' '[:lower:]' | xargs)"
  case "$provider" in
    claude-code)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/claudecode)"
      RUN_CLAUDE_CODE_TMUX_INTEGRATION=1 \
      RUN_CLAUDE_CODE_TMUX_LIVE_E2E=1 \
      RUN_CLAUDE_CODE_TMUX_PERSISTENT_E2E=1 \
      run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/claudecode \
        -run "$test_regex" -count=1 -timeout=35m
      ;;
    codex-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/codexcli)"
      RUN_CODEX_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/codexcli \
        -run "$test_regex" -count=1 -timeout=35m
      ;;
    cursor-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/cursorcli)"
      RUN_CURSOR_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/cursorcli \
        -run "$test_regex" -count=1 -timeout=35m
      ;;
    pi-cli)
      test_regex="$(p0_test_regex "$provider" pkg/adapters/picli)"
      RUN_PI_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/picli \
        -run "$test_regex" -count=1 -timeout=35m
      ;;
    *)
      echo "Unknown coding CLI P0 provider: $provider" >&2
      exit 2
      ;;
  esac
done

RUN_CODING_CLI_WORKFLOW_P0_E2E=1 \
CODING_CLI_P0_PROVIDERS="$PROVIDERS" \
run_required_go_tests go -C "$ROOT_DIR/agent_go" test ./pkg/orchestrator/types \
  -run 'TestCodingCLIWorkflowP0CompletionAdvancesNextStep$' -count=1 -timeout=50m

echo "P0 real CLI and workflow contracts passed for: $PROVIDERS"
