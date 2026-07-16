package types

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

// TestCodingCLIWorkflowP0CompletionAdvancesNextStep is the application-level
// P0 contract. Adapter tests prove that a CLI can finish a turn; this test
// proves AgentWorks observes that completion, persists it, and starts the next
// workflow step. A provider is not workflow-certified if this test fails.
func TestCodingCLIWorkflowP0CompletionAdvancesNextStep(t *testing.T) {
	if os.Getenv("RUN_CODING_CLI_WORKFLOW_P0_E2E") == "" {
		t.Skip("set RUN_CODING_CLI_WORKFLOW_P0_E2E=1 to run the real coding-CLI workflow P0 matrix")
	}

	providers := codingCLIP0Providers(t)
	requested := requestedCodingCLIP0Providers(t, providers)
	for _, provider := range requested {
		provider := provider
		t.Run(provider.name, func(t *testing.T) {
			t.Cleanup(func() { provider.cleanup(context.Background()) })
			runCodingCLIWorkflowP0(t, provider)
		})
	}
}

func TestCodingCLIWorkflowP0ProviderMatrix(t *testing.T) {
	providers := codingCLIP0Providers(t)
	for _, name := range []string{"claude-code", "codex-cli", "cursor-cli", "pi-cli"} {
		provider, ok := providers[name]
		if !ok {
			t.Fatalf("active coding CLI %s is missing from the workflow P0 matrix", name)
		}
		if provider.provider == "" || provider.model == "" || provider.requiredBin == "" || provider.cleanup == nil {
			t.Fatalf("coding CLI %s has an incomplete workflow P0 definition: %#v", name, provider)
		}
	}
	if len(providers) != 4 {
		t.Fatalf("workflow P0 matrix has %d providers, want exactly the four active coding CLIs", len(providers))
	}
}

type codingCLIP0Provider struct {
	name        string
	provider    llm.Provider
	model       string
	requiredBin string
	apiKeys     *llm.ProviderAPIKeys
	cleanup     func(context.Context)
}

func codingCLIP0Providers(t *testing.T) map[string]codingCLIP0Provider {
	t.Helper()
	optional := func(value string) *string {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil
		}
		return &value
	}
	model := func(env, fallback string) string {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			return value
		}
		return fallback
	}
	piKey := firstNonEmptyEnv("PI_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY")

	return map[string]codingCLIP0Provider{
		"claude-code": {
			name:        "claude-code",
			provider:    llm.ProviderClaudeCode,
			model:       model("CLAUDE_CODE_WORKFLOW_P0_MODEL", "claude-haiku-4-5-20251001"),
			requiredBin: "claude",
			apiKeys:     &llm.ProviderAPIKeys{ClaudeCodeOAuthToken: optional(os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"))},
			cleanup:     func(ctx context.Context) { _ = llmproviders.CleanupClaudeCodeTmuxSessions(ctx) },
		},
		"codex-cli": {
			name:        "codex-cli",
			provider:    llm.ProviderCodexCLI,
			model:       model("CODEX_CLI_WORKFLOW_P0_MODEL", "gpt-5.4-mini"),
			requiredBin: "codex",
			apiKeys:     &llm.ProviderAPIKeys{CodexCLI: optional(os.Getenv("CODEX_API_KEY"))},
			cleanup:     func(ctx context.Context) { _ = llmproviders.CleanupCodexCLIInteractiveSessions(ctx) },
		},
		"cursor-cli": {
			name:        "cursor-cli",
			provider:    llm.ProviderCursorCLI,
			model:       model("CURSOR_CLI_WORKFLOW_P0_MODEL", "composer-2.5"),
			requiredBin: "cursor-agent",
			apiKeys:     &llm.ProviderAPIKeys{CursorCLI: optional(os.Getenv("CURSOR_API_KEY"))},
			cleanup:     func(ctx context.Context) { _ = llmproviders.CleanupCursorCLIInteractiveSessions(ctx) },
		},
		"pi-cli": {
			name:        "pi-cli",
			provider:    llm.ProviderPiCLI,
			model:       model("PI_CLI_WORKFLOW_P0_MODEL", "google/gemini-3.5-flash"),
			requiredBin: "pi",
			apiKeys:     &llm.ProviderAPIKeys{PiCLI: optional(piKey)},
			cleanup:     func(ctx context.Context) { _ = llmproviders.CleanupPiCLIInteractiveSessions(ctx) },
		},
	}
}

func requestedCodingCLIP0Providers(t *testing.T, providers map[string]codingCLIP0Provider) []codingCLIP0Provider {
	t.Helper()
	requested := strings.TrimSpace(os.Getenv("CODING_CLI_P0_PROVIDERS"))
	if requested == "" || requested == "all" {
		requested = "claude-code,codex-cli,cursor-cli,pi-cli"
	}

	var out []codingCLIP0Provider
	for _, name := range strings.Split(requested, ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		provider, ok := providers[name]
		if !ok {
			t.Fatalf("unknown CODING_CLI_P0_PROVIDERS entry %q", name)
		}
		if _, err := exec.LookPath(provider.requiredBin); err != nil {
			if provider.name == "pi-cli" {
				if _, npxErr := exec.LookPath("npx"); npxErr == nil {
					out = append(out, provider)
					continue
				}
			}
			t.Fatalf("P0 provider %s requires %s in PATH: %v", provider.name, provider.requiredBin, err)
		}
		if provider.name == "pi-cli" && provider.apiKeys.PiCLI == nil {
			t.Fatal("P0 provider pi-cli requires PI_API_KEY, GEMINI_API_KEY, or GOOGLE_API_KEY")
		}
		out = append(out, provider)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func runCodingCLIWorkflowP0(t *testing.T, provider codingCLIP0Provider) {
	t.Helper()
	for _, bin := range []string{"tmux", "node"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("P0 workflow requires %s in PATH: %v", bin, err)
		}
	}
	requireLocalMCPBridgeForPiWorkflowE2E(t)

	wsAPI := strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	if wsAPI == "" {
		wsAPI = "http://127.0.0.1:18744"
		t.Setenv("WORKSPACE_API_URL", wsAPI)
	}
	if err := requireWorkspaceAPIReachable(wsAPI); err != nil {
		t.Fatalf("P0 workspace API at %s is unreachable: %v", wsAPI, err)
	}
	wsRoot := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH"))
	if wsRoot == "" {
		wsRoot = "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs"
	}
	t.Setenv("WORKSPACE_DOCS_PATH", wsRoot)

	relWorkspace := "Workflow/_p0_cli_" + provider.name + "_" + filepath.Base(t.TempDir())
	workspaceDisk := filepath.Join(wsRoot, relWorkspace)
	if os.Getenv("KEEP_E2E_WORKSPACE") == "" {
		t.Cleanup(func() { _ = os.RemoveAll(workspaceDisk) })
	}
	if err := os.MkdirAll(filepath.Join(workspaceDisk, "planning"), 0o755); err != nil {
		t.Fatalf("mkdir planning: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceDisk, "variables"), 0o755); err != nil {
		t.Fatalf("mkdir variables: %v", err)
	}
	if err := writeJSON(filepath.Join(workspaceDisk, "variables", "variables.json"), map[string]interface{}{
		"variables":       []interface{}{},
		"groups":          []map[string]interface{}{{"name": "default", "values": map[string]string{}, "enabled": true}},
		"extraction_date": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("write variables: %v", err)
	}
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "plan.json"), map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type": "regular", "id": "p0-first", "title": "First P0 turn",
				"description":          "Reply with exactly P0_FIRST_COMPLETED on one line, then stop.",
				"context_dependencies": []string{}, "context_output": "first.json", "next_step_id": "p0-second",
			},
			{
				"type": "regular", "id": "p0-second", "title": "Second P0 turn",
				"description":          "Reply with exactly P0_SECOND_STARTED_AFTER_FIRST on one line, then stop.",
				"context_dependencies": []string{"p0-first"}, "context_output": "second.json", "next_step_id": "end",
			},
		},
	}); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	model := &workflowtypes.AgentLLMConfig{Provider: string(provider.provider), ModelID: provider.model}
	preset := &workflowtypes.PresetLLMConfig{
		SchemaVersion: workflowtypes.LLMConfigSchemaVersion, Mode: workflowtypes.LLMConfigModeExplicit,
		BuilderLLM: model, MaintenanceLLM: model, PulseLLM: model,
		TieredConfig: &workflowtypes.TieredLLMConfig{Tier1: model, Tier2: model, Tier3: model},
	}
	workflowLLM := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{Provider: string(provider.provider), ModelID: provider.model},
		APIKeys: provider.apiKeys,
	}
	wo, err := NewWorkflowOrchestrator("", 0.2, "workflow", loggerv2.NewNoop(), nil, nil, nil, nil, false, nil, map[string]interface{}{}, workflowLLM, 6, map[string]string{}, preset)
	if err != nil {
		t.Fatalf("NewWorkflowOrchestrator: %v", err)
	}
	wo.SetExecutionOptions(&stepworkflow.ExecutionOptions{
		RunMode: "use_same_run", SelectedRunFolder: "iteration-0",
		ExecutionStrategy: stepworkflow.ExecutionStrategyStartFromBeginningNoHuman,
		EnabledGroupNames: []string{"default"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if _, err := wo.Execute(ctx, "Run the two-step coding CLI P0 completion contract.", relWorkspace, map[string]interface{}{"workflowStatus": workflowtypes.WorkflowStatusPreVerification}); err != nil {
		t.Fatalf("workflow did not advance after %s completion: %v", provider.name, err)
	}
	assertStepExecutionResultContains(t, workspaceDisk, "p0-first", "P0_FIRST_COMPLETED")
	assertStepExecutionResultContains(t, workspaceDisk, "p0-second", "P0_SECOND_STARTED_AFTER_FIRST")
	t.Logf("P0 workflow advance passed for %s/%s", provider.name, provider.model)
}
