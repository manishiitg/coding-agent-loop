#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MULTI_LLM_DIR="${MULTI_LLM_DIR:-$(cd "$ROOT_DIR/../multi-llm-provider-go" && pwd)}"
PROVIDERS="${CODING_CLI_P0_PROVIDERS:-claude-code,codex-cli,cursor-cli,pi-cli}"

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
go -C "$ROOT_DIR/agent_go" test ./cmd/server -run 'TestStartNextTurnFromLiveInputAcknowledgesBeforeQueuedTurnRuns$' -count=1
go -C "$ROOT_DIR/agent_go" test ./pkg/orchestrator/types -run 'TestCodingCLIWorkflowP0(ProviderMatrix|CompletionAdvancesNextStep)$' -count=1

if [[ "${RUN_CODING_CLI_P0_REAL:-}" != "1" ]]; then
  echo "P0 fast contract passed. Set RUN_CODING_CLI_P0_REAL=1 to run authenticated CLI and workflow E2Es."
  exit 0
fi

IFS=',' read -r -a provider_list <<< "$PROVIDERS"
for raw_provider in "${provider_list[@]}"; do
  provider="$(printf '%s' "$raw_provider" | tr '[:upper:]' '[:lower:]' | xargs)"
  case "$provider" in
    claude-code)
      RUN_CLAUDE_CODE_TMUX_INTEGRATION=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/claudecode \
        -run 'TestClaudeCodeTmux(RuntimeSelfCheckContract|IntegrationFreshPromptCarriesUserText|IntegrationHaikuWorkingDirectory|IntegrationHaikuMCPBridgeContract|IntegrationHaikuLiveInputAndEscape|RealFinalExtractionFromTmuxVertexJudgeE2E|IntegrationPersistentCancelDoesNotLeaveBusySessionReusable|IntegrationParallelIsolation)$' -count=1 -timeout=35m
      ;;
    codex-cli)
      RUN_CODEX_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/codexcli \
        -run 'TestCodexCLIReal(RuntimeSelfCheckContract|InteractiveTmuxFullContract|InteractiveWorkingDirectoryContract|InteractiveMCPBridgeContract|InteractiveQueuedValidationDoesNotCompleteDuringMCPTool|FinalExtractionFromTmuxVertexJudgeE2E|InteractiveLiveInputAndEscapeContract|InteractiveParallelIsolation)$' -count=1 -timeout=35m
      ;;
    cursor-cli)
      RUN_CURSOR_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/cursorcli \
        -run 'TestCursorCLIReal(RuntimeSelfCheckContract|InteractiveTmuxFullContract|IsolatedTmpDirDoesNotTouchOuterWorkspace|InteractiveMCPBridgeContractTmux|InteractiveQueuedValidationDoesNotCompleteDuringMCPTool|CompletionDetection|FinalExtractionFromTmuxVertexJudgeE2E|InteractiveLiveInputAndEscapeContract|InteractiveParallelIsolation)$' -count=1 -timeout=35m
      ;;
    pi-cli)
      RUN_PI_CLI_REAL_E2E=1 run_required_go_tests go -C "$MULTI_LLM_DIR" test ./pkg/adapters/picli \
        -run 'TestPiCLIReal(RuntimeSelfCheckContract|TmuxFullContract|WorkingDirectoryMCPContract|MCPBridgeOnlyToolsContract|SlowMCPToolDoneDetectionContract|SlowToolLiveInputAndCancellationContract|ParallelIsolationContract)$' -count=1 -timeout=35m
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
