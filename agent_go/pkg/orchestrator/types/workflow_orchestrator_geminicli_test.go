package types

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

// TestWorkflowE2ESingleRegularStepGeminiCLI proves the workflow engine
// runs end-to-end against the gemini-cli structured transport — same
// 1-step happy-path shape as the vertex tracer-bullet, but provider
// swapped to gemini-cli so the structured-CLI path is exercised.
//
// Earlier in this work block a workflow test against gemini-cli hung
// for 10+ min on the FIRST LLM call. The adapter-level cobra command
// (./bin/llm-test gemini-cli gemini-cli-streaming-content) was
// healthy at ~9s/call though, so the hang was specific to the
// workflow orchestrator's wiring. Test surfaces:
//   - Does the engine pass GEMINI_API_KEY through to the gemini binary?
//   - Does the engine's tool-attach path cause a different timeout?
//   - Does workflow execution complete with no LLM tools attached?
//
// Gated on RUN_WORKFLOW_REAL_E2E + RUN_GEMINI_CLI_REAL_E2E +
// GEMINI_API_KEY + gemini binary in PATH.
func TestWorkflowE2ESingleRegularStepGeminiCLI(t *testing.T) {
	if os.Getenv("RUN_WORKFLOW_REAL_E2E") == "" {
		t.Skip("set RUN_WORKFLOW_REAL_E2E=1")
	}
	if os.Getenv("RUN_GEMINI_CLI_REAL_E2E") == "" {
		t.Skip("set RUN_GEMINI_CLI_REAL_E2E=1")
	}
	apiKey := strings.TrimSpace(os.Getenv("GEMINI_API_KEY"))
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY required for gemini-cli workflow e2e")
	}
	if _, err := exec.LookPath("gemini"); err != nil {
		t.Skipf("gemini binary not found: %v", err)
	}

	// Workspace fixture (same shape as the edge-case harness — see
	// workflow_orchestrator_edge_cases_test.go for the full rationale
	// behind each step).
	wsAPI := strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	if wsAPI == "" {
		wsAPI = "http://127.0.0.1:18744"
		_ = os.Setenv("WORKSPACE_API_URL", wsAPI)
	}
	if resp, err := exec.Command("curl", "-fsS", "-o", "/dev/null", "-w", "%{http_code}", wsAPI+"/api/documents/_index.json").Output(); err != nil || len(resp) == 0 {
		t.Skipf("workspace API at %s unreachable: %v", wsAPI, err)
	}
	wsRoot := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH"))
	if wsRoot == "" {
		wsRoot = "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs"
	}
	_ = os.Setenv("WORKSPACE_DOCS_PATH", wsRoot)

	relWorkspace := "Workflow/_e2e_gemini_" + filepath.Base(t.TempDir())
	workspaceDisk := filepath.Join(wsRoot, relWorkspace)
	if err := os.MkdirAll(filepath.Join(workspaceDisk, "planning"), 0o755); err != nil {
		t.Fatalf("mkdir planning: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspaceDisk, "variables"), 0o755); err != nil {
		t.Fatalf("mkdir variables: %v", err)
	}
	if os.Getenv("KEEP_E2E_WORKSPACE") == "" {
		t.Cleanup(func() { _ = os.RemoveAll(workspaceDisk) })
	}
	if err := writeJSON(filepath.Join(workspaceDisk, "variables", "variables.json"), map[string]interface{}{
		"variables":       []interface{}{},
		"groups":          []map[string]interface{}{{"name": "default", "values": map[string]string{}, "enabled": true}},
		"extraction_date": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("write variables.json: %v", err)
	}

	const stepID = "step-compute"
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "plan.json"), map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   stepID,
				"title":                "Compute via gemini-cli",
				"description":          "Compute 6 * 7. Reply with EXACTLY the line RESULT=42 on a line by itself and stop.",
				"context_dependencies": []string{},
				"context_output":       "out.json",
			},
		},
	}); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	// LLM config: gemini-cli everywhere. The adapter shells out to
	// the gemini binary; APIKeys.GeminiCLI is the key the orchestrator
	// passes to the binary's environment.
	model := "gemini-cli" // tier alias resolved by the adapter
	llmCfg := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{Provider: "gemini-cli", ModelID: model},
		APIKeys: &llm.ProviderAPIKeys{GeminiCLI: &apiKey},
	}
	agentLLM := &workflowtypes.AgentLLMConfig{Provider: "gemini-cli", ModelID: model}
	presetCfg := &workflowtypes.PresetLLMConfig{
		Provider: "gemini-cli",
		ModelID:  model,
		PhaseLLM: agentLLM,
		TieredConfig: &workflowtypes.TieredLLMConfig{Tier1: agentLLM, Tier2: agentLLM, Tier3: agentLLM},
	}

	wo, err := NewWorkflowOrchestrator(
		"", 0.7, "workflow",
		loggerv2.NewNoop(),
		nil, nil,
		[]string{}, []string{}, false,
		nil, map[string]interface{}{},
		llmCfg, 10, map[string]string{},
		presetCfg,
	)
	if err != nil {
		t.Fatalf("NewWorkflowOrchestrator: %v", err)
	}

	// Budget: adapter-level smoke was 9s/call; with workflow
	// scaffolding overhead allow up to 4 min for safety. Earlier
	// 10-min hang we want to know if it returns; cap at 5 min so a
	// regression surfaces fast rather than burning the whole test
	// budget.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	t0 := time.Now()
	result, err := wo.Execute(ctx, "Compute 6*7 via gemini-cli", relWorkspace, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	dur := time.Since(t0)
	if err != nil {
		t.Fatalf("Execute: %v (after %s)", err, dur)
	}
	if strings.TrimSpace(result) == "" {
		t.Fatalf("Execute returned empty result (after %s)", dur)
	}

	// Engine completed: assert step_done.json was written.
	stepDoneGlob := filepath.Join(workspaceDisk, "runs", "*", "*", "execution", stepID, "step_done.json")
	matches, _ := filepath.Glob(stepDoneGlob)
	if len(matches) == 0 {
		t.Fatalf("step_done.json not written under %s — engine did not reach completion", stepDoneGlob)
	}

	// Surface the LLM-side captured output so we can compare it
	// against vertex's behavior (does gemini-cli reply with literal
	// text or write a Python script the way vertex did?).
	execLog := filepath.Join(workspaceDisk, "runs", "*", "*", "logs", stepID, "execution", "execution-attempt-*-iteration-*.json")
	if logMatches, _ := filepath.Glob(execLog); len(logMatches) > 0 {
		body, _ := os.ReadFile(logMatches[0])
		var entry struct {
			ExecutionResult string `json:"execution_result"`
			LLMCallCount    int    `json:"llm_call_count"`
			Model           string `json:"model"`
		}
		if err := json.Unmarshal(body, &entry); err == nil {
			t.Logf("✅ gemini-cli workflow e2e: duration=%s, model=%q, llm_calls=%d, result-snippet=%q",
				dur, entry.Model, entry.LLMCallCount, snippet(entry.ExecutionResult, 120))
		}
	} else {
		t.Logf("✅ gemini-cli workflow e2e: duration=%s, result-len=%d (no execution-attempt log)", dur, len(result))
	}
}

func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
