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
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "step_config.json"), map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"id":    stepID,
				"title": "Compute via gemini-cli",
				"agent_configs": map[string]interface{}{
					"transport": "structured",
				},
			},
		},
	}); err != nil {
		t.Fatalf("write step_config.json: %v", err)
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
		SchemaVersion:  workflowtypes.LLMConfigSchemaVersion,
		Mode:           workflowtypes.LLMConfigModeExplicit,
		BuilderLLM:     agentLLM,
		MaintenanceLLM: agentLLM,
		PulseLLM:       agentLLM,
		TieredConfig:   &workflowtypes.TieredLLMConfig{Tier1: agentLLM, Tier2: agentLLM, Tier3: agentLLM},
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

	execLog := filepath.Join(workspaceDisk, "runs", "*", "*", "logs", stepID, "execution", "execution-attempt-*-iteration-*.json")
	logMatches, _ := filepath.Glob(execLog)
	if len(logMatches) == 0 {
		t.Fatalf("execution log not written under %s — engine did not reach completion", execLog)
	}

	// Surface the LLM-side captured output so we can compare it
	// against vertex's behavior (does gemini-cli reply with literal
	// text or write a Python script the way vertex did?).
	if len(logMatches) > 0 {
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

// TestWorkflowE2ESiblingConvergenceGeminiCLI reproduces the mutual-fund routing
// bug: a router selects ONE of several sibling branches that each sit after it in
// the step list and each converge (next_step_id) to a shared downstream step. The
// engine must run only the selected branch, jump to the shared step, and SKIP the
// other branches. Before the message_sequence next_step_id fix, the selected
// branch ran and then execution fell through into the next sibling branch.
//
// default_route_id selects branch-a deterministically (no LLM in routing). We then
// assert branch-a ran, branch-b/branch-c did NOT, and the shared normalize step did.
//
// Gated identically to TestWorkflowE2ESingleRegularStepGeminiCLI.
func TestWorkflowE2ESiblingConvergenceGeminiCLI(t *testing.T) {
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

	relWorkspace := "Workflow/_e2e_converge_" + filepath.Base(t.TempDir())
	workspaceDisk := filepath.Join(wsRoot, relWorkspace)
	for _, d := range []string{"planning", "variables"} {
		if err := os.MkdirAll(filepath.Join(workspaceDisk, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
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

	const (
		stepCompute   = "step-compute"
		stepRoute     = "step-route"
		branchA       = "branch-a"
		branchB       = "branch-b"
		branchC       = "branch-c"
		stepNormalize = "step-normalize"
	)
	msgBranch := func(id, token string) map[string]interface{} {
		return map[string]interface{}{
			"type":         "message_sequence",
			"id":           id,
			"title":        id,
			"description":  "Branch " + id,
			"items":        []map[string]interface{}{{"id": id + "-msg", "type": "user_message", "title": id, "message": "Reply with EXACTLY the line " + token + " on a line by itself and stop. Do not call any tools."}},
			"next_step_id": stepNormalize, // converge to the shared step
		}
	}
	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{"type": "regular", "id": stepCompute, "title": "Compute", "description": "Reply with EXACTLY the line READY on a line by itself and stop.", "context_output": "c.json"},
			{
				"type": "routing", "id": stepRoute, "title": "Pick branch", "description": "",
				"routing_question": "Which branch to run? (deterministic; default selects branch a)",
				"routes": []map[string]interface{}{
					{"route_id": "route_a", "route_name": "A", "condition": "run branch a", "next_step_id": branchA},
					{"route_id": "route_b", "route_name": "B", "condition": "run branch b", "next_step_id": branchB},
					{"route_id": "route_c", "route_name": "C", "condition": "run branch c", "next_step_id": branchC},
				},
				"default_route_id": "route_a",
			},
			msgBranch(branchA, "BRANCH_A_DONE"),
			msgBranch(branchB, "BRANCH_B_DONE"),
			msgBranch(branchC, "BRANCH_C_DONE"),
			{"type": "regular", "id": stepNormalize, "title": "Normalize", "description": "Reply with EXACTLY the line NORMALIZE_DONE on a line by itself and stop.", "context_output": "n.json", "next_step_id": "end"},
		},
	}
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "plan.json"), plan); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "step_config.json"), map[string]interface{}{
		"steps": []map[string]interface{}{
			{"id": stepCompute, "agent_configs": map[string]interface{}{"transport": "structured"}},
			{"id": branchA, "agent_configs": map[string]interface{}{"transport": "structured"}},
			{"id": stepNormalize, "agent_configs": map[string]interface{}{"transport": "structured"}},
		},
	}); err != nil {
		t.Fatalf("write step_config.json: %v", err)
	}

	model := "gemini-cli"
	llmCfg := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{Provider: "gemini-cli", ModelID: model},
		APIKeys: &llm.ProviderAPIKeys{GeminiCLI: &apiKey},
	}
	agentLLM := &workflowtypes.AgentLLMConfig{Provider: "gemini-cli", ModelID: model}
	presetCfg := &workflowtypes.PresetLLMConfig{
		SchemaVersion: workflowtypes.LLMConfigSchemaVersion, Mode: workflowtypes.LLMConfigModeExplicit,
		BuilderLLM: agentLLM, MaintenanceLLM: agentLLM, PulseLLM: agentLLM,
		TieredConfig: &workflowtypes.TieredLLMConfig{Tier1: agentLLM, Tier2: agentLLM, Tier3: agentLLM},
	}
	wo, err := NewWorkflowOrchestrator("", 0.7, "workflow", loggerv2.NewNoop(), nil, nil,
		[]string{}, []string{}, false, nil, map[string]interface{}{}, llmCfg, 10, map[string]string{}, presetCfg)
	if err != nil {
		t.Fatalf("NewWorkflowOrchestrator: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if _, err := wo.Execute(ctx, "Run the selected branch then normalize", relWorkspace, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// A step "ran" iff it has an artifact folder under runs/. Collect every path
	// component below runs/ and check which step IDs appear.
	ran := func(stepID string) bool {
		found := false
		_ = filepath.Walk(filepath.Join(workspaceDisk, "runs"), func(p string, info os.FileInfo, err error) error {
			if err == nil && info != nil && info.IsDir() && filepath.Base(p) == stepID {
				found = true
			}
			return nil
		})
		return found
	}
	if !ran(branchA) {
		t.Errorf("selected branch %q did not run", branchA)
	}
	if ran(branchB) {
		t.Errorf("non-selected sibling %q RAN — routing fell through instead of converging", branchB)
	}
	if ran(branchC) {
		t.Errorf("non-selected sibling %q RAN — routing fell through instead of converging", branchC)
	}
	if !ran(stepNormalize) {
		t.Errorf("shared convergence step %q did not run", stepNormalize)
	}
	t.Logf("✅ sibling-convergence: branch-a ran, branch-b/c skipped, normalize ran")
}

func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
