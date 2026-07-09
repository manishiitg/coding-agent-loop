package step_based_workflow

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	mcpllm "github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
)

func TestCreateStandardAgentConfigUsesWorkflowFolderForCodingAgentWorkingDir(t *testing.T) {
	docsRoot := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", docsRoot)

	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		[]string{"test-server"},
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	base.SetWorkspacePath("Workflow/testing")
	base.SetMCPSessionID("workflow-session-123")

	config := base.CreateStandardAgentConfigWithLLM(
		"step-agent",
		1,
		agents.OutputFormatStructured,
		&orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: string(mcpllm.ProviderCodexCLI),
				ModelID:  "gpt-5.3-codex-spark",
			},
		},
	)

	want := filepath.Join(docsRoot, "Workflow", "testing")
	if config.CodingAgentWorkingDir != want {
		t.Fatalf("CodingAgentWorkingDir = %q, want %q", config.CodingAgentWorkingDir, want)
	}
}

func TestApplyStepConfigToAgentConfigDefaultsCodingAgentTmuxCloseOnCompletion(t *testing.T) {
	hcpo := newAgentFactoryTestOrchestrator(t)
	config := agents.NewOrchestratorAgentConfig("step-agent")
	config.LLMConfig.Primary.Provider = string(mcpllm.ProviderCodexCLI)

	hcpo.applyStepConfigToAgentConfig(config, &AgentConfigs{}, true)

	if config.CodingAgentKeepAlive {
		t.Fatal("expected workflow step coding-agent tmux lifecycle to close on completion by default")
	}
}

func TestApplyStepConfigToAgentConfigSupportsCodingAgentTmuxKeepAlive(t *testing.T) {
	hcpo := newAgentFactoryTestOrchestrator(t)
	config := agents.NewOrchestratorAgentConfig("step-agent")
	config.LLMConfig.Primary.Provider = string(mcpllm.ProviderCodexCLI)

	hcpo.applyStepConfigToAgentConfig(config, &AgentConfigs{
		CodingAgentTmuxLifecycle: CodingAgentTmuxLifecycleKeepAlive,
	}, true)

	if !config.CodingAgentKeepAlive {
		t.Fatal("expected explicit keep_alive lifecycle to keep coding-agent tmux session alive")
	}
}

func TestEnableWorkflowMainCodingAgentKeepAliveEnablesCLIProvider(t *testing.T) {
	config := agents.NewOrchestratorAgentConfig("workflow-builder-agent")
	config.LLMConfig.Primary.Provider = string(mcpllm.ProviderCodexCLI)

	enableWorkflowMainCodingAgentKeepAlive(config)

	if !config.CodingAgentKeepAlive {
		t.Fatal("expected workflow main CLI coding-agent tmux session to stay alive")
	}
}

func TestEnableWorkflowMainCodingAgentKeepAliveIgnoresAPIProvider(t *testing.T) {
	config := agents.NewOrchestratorAgentConfig("workflow-builder-agent")
	config.LLMConfig.Primary.Provider = "bedrock"

	enableWorkflowMainCodingAgentKeepAlive(config)

	if config.CodingAgentKeepAlive {
		t.Fatal("expected non-CLI workflow main agent to leave coding-agent tmux keep-alive disabled")
	}
}

// TestApplyStepConfigToAgentConfigEnablesWorkspaceIsolation locks in
// the Phase C contract: applying step config to a workflow-step agent
// flips IsolateCodingAgentWorkspace=true. This is what makes the
// coding-CLI session run in a fresh os.MkdirTemp dir instead of
// CodingAgentWorkingDir, protecting the user's workflow files from
// concurrent-step collisions and accidental model writes.
//
// Chat code paths (pkg/agentwrapper/llm_agent.go) do NOT call
// applyStepConfigToAgentConfig, so this flag stays false there —
// chat sessions continue to operate directly on the user's chosen
// workspace dir for the "agent edits my files" UX.
func TestApplyStepConfigToAgentConfigEnablesWorkspaceIsolation(t *testing.T) {
	hcpo := newAgentFactoryTestOrchestrator(t)
	config := agents.NewOrchestratorAgentConfig("step-agent")
	config.LLMConfig.Primary.Provider = string(mcpllm.ProviderCodexCLI)

	if config.IsolateCodingAgentWorkspace {
		t.Fatal("OrchestratorAgentConfig must default IsolateCodingAgentWorkspace=false; chat code paths depend on the zero value being safe")
	}

	hcpo.applyStepConfigToAgentConfig(config, &AgentConfigs{}, true)

	if !config.IsolateCodingAgentWorkspace {
		t.Fatal("expected workflow step to enable IsolateCodingAgentWorkspace; without it, concurrent steps collide on CodingAgentWorkingDir and the model's built-in tools can mutate operator files")
	}
}

// TestAllWorkflowAgentFactoriesEnableWorkspaceIsolation verifies that every
// agent factory that produces a long-lived coding-CLI session flips
// IsolateCodingAgentWorkspace=true so the session runs in os.MkdirTemp instead
// of CodingAgentWorkingDir. These factories live in two files:
//   - controller_agent_factory.go (2): regular-step path (applyStepConfigToAgentConfig)
//     and the todo-task orchestrator (createTodoTaskOrchestratorAgent).
//   - interactive_workshop_manager.go (10): the workshop background agents — the
//     `run_in_background` task agent plus the Goal Advisor stage runner and
//     the improve-db / review-plan /
//     review-artifact-sync / review-results / review-timing / review-costs /
//     review-step-code / harden agents — each spawns a coding-CLI
//     session for a workflow task and must isolate its workspace.
//
// Without isolation on any of these, an agy-cli orchestrator / workshop
// background agent collides with the workshop chat's agy session in the
// same workflow folder and the step fails with "agy-cli does not support
// concurrent sessions in working directory ... with different MCP configs".
//
// The test asserts a specific count per file. A new factory added later
// without isolation trips this test rather than shipping a silent regression.
// Conversely, removing one of these call sites by accident is also caught.
func TestAllWorkflowAgentFactoriesEnableWorkspaceIsolation(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{path: "controller_agent_factory.go", want: 2},      // regular + todo-task orchestrator
		{path: "interactive_workshop_manager.go", want: 10}, // run_in_background + goal-advisor + improve/review/harden/db workshop agents
	}
	const needle = "config.IsolateCodingAgentWorkspace = true"
	for _, tc := range cases {
		source, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatalf("read %s: %v", tc.path, err)
		}
		if got := strings.Count(string(source), needle); got != tc.want {
			t.Fatalf("%s: %q occurrences = %d, want %d (a workflow agent factory in this file must enable isolation; chat code paths in other files must not)", tc.path, needle, got, tc.want)
		}
	}
}

func TestApplyStepConfigToAgentConfigGeminiWorkflowStepsDefaultToTmux(t *testing.T) {
	tests := []struct {
		name                string
		stepConfig          *AgentConfigs
		wantForceStructured bool
	}{
		{name: "no step config", stepConfig: nil},
		{name: "default transport", stepConfig: &AgentConfigs{}},
		{name: "explicit tmux honored", stepConfig: &AgentConfigs{Transport: "tmux"}},
		{name: "legacy structured ignored", stepConfig: &AgentConfigs{Transport: "structured"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hcpo := newAgentFactoryTestOrchestrator(t)
			config := agents.NewOrchestratorAgentConfig("step-agent")
			config.LLMConfig.Primary.Provider = string(mcpllm.ProviderGeminiCLI)

			hcpo.applyStepConfigToAgentConfig(config, tt.stepConfig, false)

			if !config.UseCodeExecutionMode {
				t.Fatal("expected Gemini CLI workflow step to use code execution mode")
			}
			if config.ForceStructuredCodingAgent != tt.wantForceStructured {
				t.Fatalf("ForceStructuredCodingAgent = %v, want %v (Gemini follows the standard CLI tmux-default policy)", config.ForceStructuredCodingAgent, tt.wantForceStructured)
			}
		})
	}
}

func TestApplyStepConfigToAgentConfigKeepsTmuxDefaultForOtherCLIWorkflowSteps(t *testing.T) {
	tests := []struct {
		name                string
		provider            string
		stepConfig          *AgentConfigs
		wantForceStructured bool
	}{
		{name: "codex default", provider: string(mcpllm.ProviderCodexCLI), stepConfig: nil},
		{name: "codex explicit tmux", provider: string(mcpllm.ProviderCodexCLI), stepConfig: &AgentConfigs{Transport: "tmux"}},
		{name: "codex explicit structured is ignored", provider: string(mcpllm.ProviderCodexCLI), stepConfig: &AgentConfigs{Transport: "structured"}},
		{name: "claude default", provider: string(mcpllm.ProviderClaudeCode), stepConfig: nil},
		{name: "claude explicit structured is ignored", provider: string(mcpllm.ProviderClaudeCode), stepConfig: &AgentConfigs{Transport: "structured"}},
		{name: "cursor default", provider: string(mcpllm.ProviderCursorCLI), stepConfig: nil},
		{name: "cursor explicit structured is ignored", provider: string(mcpllm.ProviderCursorCLI), stepConfig: &AgentConfigs{Transport: "structured"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hcpo := newAgentFactoryTestOrchestrator(t)
			config := agents.NewOrchestratorAgentConfig("step-agent")
			config.LLMConfig.Primary.Provider = tt.provider

			hcpo.applyStepConfigToAgentConfig(config, tt.stepConfig, false)

			if config.ForceStructuredCodingAgent != tt.wantForceStructured {
				t.Fatalf("ForceStructuredCodingAgent = %v, want %v", config.ForceStructuredCodingAgent, tt.wantForceStructured)
			}
		})
	}
}

func TestWorkflowTransportResolverAppliesDedicatedModes(t *testing.T) {
	tests := []struct {
		name                string
		provider            string
		stepConfig          *AgentConfigs
		wantTransport       string
		wantForceStructured bool
	}{
		{name: "gemini defaults tmux", provider: string(mcpllm.ProviderGeminiCLI), wantTransport: "tmux"},
		{name: "gemini honors tmux", provider: string(mcpllm.ProviderGeminiCLI), stepConfig: &AgentConfigs{Transport: "tmux"}, wantTransport: "tmux"},
		{name: "gemini explicit structured ignored", provider: string(mcpllm.ProviderGeminiCLI), stepConfig: &AgentConfigs{Transport: "structured"}, wantTransport: "tmux"},
		{name: "codex defaults tmux", provider: string(mcpllm.ProviderCodexCLI), wantTransport: "tmux"},
		{name: "codex explicit structured ignored", provider: string(mcpllm.ProviderCodexCLI), stepConfig: &AgentConfigs{Transport: "structured"}, wantTransport: "tmux"},
		{name: "claude explicit structured ignored", provider: string(mcpllm.ProviderClaudeCode), stepConfig: &AgentConfigs{Transport: "structured"}, wantTransport: "tmux"},
		{name: "cursor explicit structured ignored", provider: string(mcpllm.ProviderCursorCLI), stepConfig: &AgentConfigs{Transport: "structured"}, wantTransport: "tmux"},
		{name: "api ignores tmux", provider: "anthropic", stepConfig: &AgentConfigs{Transport: "tmux"}, wantTransport: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hcpo := newAgentFactoryTestOrchestrator(t)
			config := agents.NewOrchestratorAgentConfig("step-agent")
			config.LLMConfig.Primary.Provider = tt.provider

			got := hcpo.applyWorkflowTransportToAgentConfig(config, tt.stepConfig, "test agent")

			if got != tt.wantTransport {
				t.Fatalf("transport = %q, want %q", got, tt.wantTransport)
			}
			if config.ForceStructuredCodingAgent != tt.wantForceStructured {
				t.Fatalf("ForceStructuredCodingAgent = %v, want %v", config.ForceStructuredCodingAgent, tt.wantForceStructured)
			}
		})
	}
}

func newAgentFactoryTestOrchestrator(t *testing.T) *StepBasedWorkflowOrchestrator {
	t.Helper()
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		[]string{"api-bridge"},
		[]string{"api-bridge:execute_shell_command"},
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	return &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}
}

func TestInjectStepEnvIntoShellExecutor_OverridesStaleMCPSessionEnv(t *testing.T) {
	t.Setenv("MCP_API_URL", "http://example.test/s/parent-session")
	t.Setenv("MCP_API_TOKEN", "step-token")

	var capturedArgs map[string]interface{}
	executors := map[string]interface{}{
		"execute_shell_command": func(ctx context.Context, args map[string]interface{}) (string, error) {
			capturedArgs = args
			return "ok", nil
		},
	}

	injectStepEnvIntoShellExecutor(
		executors,
		"/tmp/workflow/execution/math-solver",
		"/tmp/workflow/execution",
		"/tmp/workflow/db/db.sqlite",
		"step-session-123",
	)

	execFn, ok := executors["execute_shell_command"].(func(context.Context, map[string]interface{}) (string, error))
	if !ok {
		t.Fatal("expected wrapped execute_shell_command executor")
	}

	_, err := execFn(context.Background(), map[string]interface{}{
		"command": "env",
		"extra_env": map[string]interface{}{
			"MCP_SESSION_ID":     "parent-session",
			"MCP_API_URL":        "http://example.test/s/parent-session",
			"MCP_API_TOKEN":      "step-token",
			"STEP_OUTPUT_DIR":    "/stale/output",
			"STEP_EXECUTION_DIR": "/stale/execution",
		},
	})
	if err != nil {
		t.Fatalf("wrapped execute_shell_command returned error: %v", err)
	}

	rawExtraEnv, ok := capturedArgs["extra_env"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected extra_env map, got %#v", capturedArgs["extra_env"])
	}

	if got := rawExtraEnv["STEP_OUTPUT_DIR"]; got != "/tmp/workflow/execution/math-solver" {
		t.Fatalf("expected STEP_OUTPUT_DIR override, got %#v", got)
	}
	if got := rawExtraEnv["STEP_EXECUTION_DIR"]; got != "/tmp/workflow/execution" {
		t.Fatalf("expected STEP_EXECUTION_DIR override, got %#v", got)
	}
	if got := rawExtraEnv["MCP_SESSION_ID"]; got != "step-session-123" {
		t.Fatalf("expected MCP_SESSION_ID override, got %#v", got)
	}
	if got := rawExtraEnv["MCP_API_URL"]; got != "http://example.test/s/step-session-123" {
		t.Fatalf("expected step-scoped MCP_API_URL, got %#v", got)
	}
	if got := rawExtraEnv["MCP_CUSTOM"]; got != "http://example.test/s/step-session-123/tools/custom" {
		t.Fatalf("expected step-scoped MCP_CUSTOM, got %#v", got)
	}
	if got := rawExtraEnv["MCP_AUTH"]; got != "Authorization: Bearer step-token" {
		t.Fatalf("expected MCP_AUTH, got %#v", got)
	}
}

func TestSetupExecutionFolderGuardHonorsLearningsAndKBNone(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	base.SetWorkspacePath("Workflow/testing")

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  base,
		selectedRunFolder: "iteration-0/test-group",
	}

	readPaths, writePaths := hcpo.setupExecutionFolderGuard(
		"step-1",
		"forbidden-probe",
		KBAccessNone,
		LearningsAccessNone,
		KBWriteMethodAgent,
		DBAccessReadWrite,
	)

	forbiddenReads := []string{
		"Workflow/testing/learnings/_global",
		"Workflow/testing/knowledgebase",
		"Workflow/testing",
	}
	for _, forbidden := range forbiddenReads {
		if slices.Contains(readPaths, forbidden) {
			t.Fatalf("expected read paths not to include %q, got %v", forbidden, readPaths)
		}
	}
	forbiddenWrites := []string{
		"Workflow/testing/learnings/_global",
		"Workflow/testing/learnings/forbidden-probe",
		"Workflow/testing/knowledgebase",
		"Workflow/testing/knowledgebase/notes",
	}
	for _, forbidden := range forbiddenWrites {
		if slices.Contains(writePaths, forbidden) {
			t.Fatalf("expected write paths not to include %q, got %v", forbidden, writePaths)
		}
	}
}

func TestSetupExecutionFolderGuardAddsOnlyConfiguredStores(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}
	base.SetWorkspacePath("Workflow/testing")

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator:  base,
		selectedRunFolder: "iteration-0/test-group",
	}

	readPaths, writePaths := hcpo.setupExecutionFolderGuard(
		"step-1",
		"kb-direct",
		KBAccessReadWrite,
		LearningsAccessRead,
		KBWriteMethodDirect,
		DBAccessReadWrite,
	)

	for _, expected := range []string{
		"Workflow/testing/learnings/_global",
		"Workflow/testing/knowledgebase",
	} {
		if !slices.Contains(readPaths, expected) {
			t.Fatalf("expected read paths to include %q, got %v", expected, readPaths)
		}
	}
	if !slices.Contains(writePaths, "Workflow/testing/knowledgebase/notes") {
		t.Fatalf("expected write paths to include KB notes for direct writes, got %v", writePaths)
	}
	if slices.Contains(writePaths, "Workflow/testing/learnings/_global") {
		t.Fatalf("main execution should not write global learnings, got %v", writePaths)
	}
}

func TestApplyStepConfigToAgentConfigForcesCodeExecForCLIProviders(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		[]string{"test-server"},
		nil,
		false,
		nil,
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}
	config := agents.NewOrchestratorAgentConfig("test-agent")
	config.LLMConfig.Primary.Provider = "gemini-cli"

	hcpo.applyStepConfigToAgentConfig(config, nil, false)

	if !config.UseCodeExecutionMode {
		t.Fatalf("expected CLI providers to have code execution mode enabled")
	}
}

func TestClaudeCodeTransportHelpers(t *testing.T) {
	stepConfig := agents.NewOrchestratorAgentConfig("step-agent")
	stepConfig.LLMConfig.Primary.Provider = string(mcpllm.ProviderClaudeCode)
	forceWorkflowClaudeCodeInteractiveTransport(stepConfig)
	if stepConfig.ClaudeCodeTransport != mcpllm.ClaudeCodeTransportTmux {
		t.Fatalf("step ClaudeCodeTransport = %q, want %q", stepConfig.ClaudeCodeTransport, mcpllm.ClaudeCodeTransportTmux)
	}

	chatConfig := agents.NewOrchestratorAgentConfig("workflow-builder-agent")
	chatConfig.LLMConfig.Primary.Provider = string(mcpllm.ProviderClaudeCode)
	forceWorkflowClaudeCodeInteractiveTransport(chatConfig)
	if chatConfig.ClaudeCodeTransport != mcpllm.ClaudeCodeTransportTmux {
		t.Fatalf("chat ClaudeCodeTransport = %q, want %q", chatConfig.ClaudeCodeTransport, mcpllm.ClaudeCodeTransportTmux)
	}
}

// Gemini CLI follows the same workflow-step policy as every other CLI:
// tmux by default, including when an old step_config still says structured.
func TestWorkflowGeminiUsesTmuxLikeOtherCLIs(t *testing.T) {
	hcpo := newAgentFactoryTestOrchestrator(t)

	newGemini := func() *agents.OrchestratorAgentConfig {
		c := agents.NewOrchestratorAgentConfig("workflow-runtime-agent")
		c.LLMConfig.Primary.Provider = string(mcpllm.ProviderGeminiCLI)
		return c
	}

	// Default (no per-step transport) → tmux, like every other CLI.
	c := newGemini()
	if got := hcpo.applyWorkflowTransportToAgentConfig(c, nil, "runtime"); got != "tmux" {
		t.Fatalf("default transport = %q, want tmux", got)
	}
	if c.ForceStructuredCodingAgent {
		t.Fatal("default ForceStructuredCodingAgent = true, want false")
	}

	// Explicit transport=tmux is now honored (previously forced to structured).
	c = newGemini()
	if got := hcpo.applyWorkflowTransportToAgentConfig(c, &AgentConfigs{Transport: "tmux"}, "runtime"); got != "tmux" {
		t.Fatalf("transport=tmux = %q, want tmux", got)
	}
	if c.ForceStructuredCodingAgent {
		t.Fatal("transport=tmux ForceStructuredCodingAgent = true, want false")
	}

	// Legacy structured is ignored and leaves keep-alive untouched.
	c = newGemini()
	c.CodingAgentKeepAlive = true
	if got := hcpo.applyWorkflowTransportToAgentConfig(c, &AgentConfigs{Transport: "structured"}, "runtime"); got != "tmux" {
		t.Fatalf("transport=structured = %q, want tmux", got)
	}
	if c.ForceStructuredCodingAgent {
		t.Fatal("transport=structured ForceStructuredCodingAgent = true, want false")
	}
	if !c.CodingAgentKeepAlive {
		t.Fatal("transport=structured CodingAgentKeepAlive = false, want true")
	}
}

func TestWorkflowClaudeCodeIgnoresLegacyStructuredTransport(t *testing.T) {
	hcpo := newAgentFactoryTestOrchestrator(t)
	c := agents.NewOrchestratorAgentConfig("workflow-runtime-agent")
	c.LLMConfig.Primary.Provider = string(mcpllm.ProviderClaudeCode)

	got := hcpo.applyWorkflowTransportToAgentConfig(c, &AgentConfigs{Transport: "structured"}, "runtime")

	if got != "tmux" {
		t.Fatalf("transport=structured = %q, want tmux", got)
	}
	if c.ForceStructuredCodingAgent {
		t.Fatal("ForceStructuredCodingAgent = true, want false")
	}
	if c.ClaudeCodeTransport != mcpllm.ClaudeCodeTransportTmux {
		t.Fatalf("ClaudeCodeTransport = %q, want %q", c.ClaudeCodeTransport, mcpllm.ClaudeCodeTransportTmux)
	}
}

func TestSelectExecutionLLM_PrefersStepExecutionLLMOverSubAgentAndTiered(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	ctx := context.WithValue(context.Background(), workshopTierContextKey{}, int(TierLow))
	ctx = context.WithValue(ctx, virtualtools.SubAgentLLMContextKey, &AgentLLMConfig{
		Provider: "openai",
		ModelID:  "sub-agent",
	})

	cfg := &AgentConfigs{
		ExecutionLLM: &AgentLLMConfig{
			Provider: "openai",
			ModelID:  "step-override",
			Fallbacks: []AgentLLMFallback{
				{Provider: "openai", ModelID: "step-fallback"},
			},
		},
	}

	llm := hcpo.selectExecutionLLM(ctx, cfg, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "step-override" {
		t.Fatalf("expected step override model, got %q", llm.Primary.ModelID)
	}
	if len(llm.Fallbacks) != 1 || llm.Fallbacks[0].ModelID != "step-fallback" {
		t.Fatalf("expected step fallback to be preserved, got %+v", llm.Fallbacks)
	}
}

func TestSelectExecutionLLM_UsesTierResolverWhenStepExecutionLLMIsUnset(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	llm := hcpo.selectExecutionLLM(context.Background(), &AgentConfigs{}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-1" {
		t.Fatalf("expected tier-1 model for no learnings path, got %q", llm.Primary.ModelID)
	}
}

func TestSelectExecutionLLM_UsesFixedExecutionTier(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	llm := hcpo.selectExecutionLLM(context.Background(), &AgentConfigs{ExecutionTier: "medium"}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-2" {
		t.Fatalf("expected tier-2 model for fixed medium execution_tier, got %q", llm.Primary.ModelID)
	}
}

func TestSelectExecutionLLM_WorkshopTierOverrideBeatsFixedExecutionTier(t *testing.T) {
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(),
		nil,
		orchestrator.OrchestratorTypeWorkflow,
		"",
		0,
		"",
		nil,
		nil,
		false,
		&orchestrator.LLMConfig{},
		1,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator returned error: %v", err)
	}

	hcpo := &StepBasedWorkflowOrchestrator{
		BaseOrchestrator: base,
		tierResolver: NewTierResolver(&TieredLLMConfig{
			Tier1: &AgentLLMConfig{Provider: "openai", ModelID: "tier-1"},
			Tier2: &AgentLLMConfig{Provider: "openai", ModelID: "tier-2"},
			Tier3: &AgentLLMConfig{Provider: "openai", ModelID: "tier-3"},
		}, nil),
	}

	ctx := context.WithValue(context.Background(), workshopTierContextKey{}, int(TierLow))
	llm := hcpo.selectExecutionLLM(ctx, &AgentConfigs{ExecutionTier: "medium"}, "step-1")
	if llm == nil {
		t.Fatal("expected execution llm config, got nil")
	}
	if llm.Primary.ModelID != "tier-3" {
		t.Fatalf("expected workshop override to win with tier-3 model, got %q", llm.Primary.ModelID)
	}
}
