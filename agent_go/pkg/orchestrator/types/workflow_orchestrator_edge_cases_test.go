package types

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

// Edge-case suite for the workflow engine.
//
// These tests deliberately construct plans/states that have caused
// silent passes in the past or are documented invariants the engine
// is supposed to enforce. Each test is one assertion shape: feed the
// engine a malformed/edge input, assert it FAILS with a meaningful
// error (not a hang, not a silent success, not a panic).
//
// All share the gating contract of the main e2e: RUN_WORKFLOW_REAL_E2E
// + RUN_VERTEX_REAL_E2E + GEMINI_API_KEY. They DO spin up the same
// fixture-via-running-server wiring as the main e2e (so the user's
// workspace API must be up); they should fail BEFORE any LLM call
// because plan validation rejects the input.

// TestWorkflowEdgeDuplicateStepIDsRejected proves the engine refuses
// a plan with two steps sharing the same ID. Duplicate IDs silently
// collide on step_done.json paths and learnings/ folders, which is a
// data-corruption class bug.
func TestWorkflowEdgeDuplicateStepIDsRejected(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{"type": "regular", "id": "dup", "title": "First", "description": "Reply ACK", "context_dependencies": []string{}, "context_output": "a.json"},
			{"type": "regular", "id": "dup", "title": "Second", "description": "Reply ACK", "context_dependencies": []string{}, "context_output": "b.json"},
		},
	}
	writeEdgePlan(t, wo.workspaceDisk, plan)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := wo.orchestrator.Execute(ctx, "duplicate-id test", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	if err == nil {
		t.Fatal("expected engine to reject plan with duplicate step IDs; got nil error")
	}
	if !containsAny(err.Error(), "duplicate", "unique", "already exists", "conflict") {
		t.Errorf("error message should mention the duplicate-ID failure mode; got: %v", err)
	}
}

// TestWorkflowEdgeRoutingNextStepIDMustExist proves the engine refuses
// a routing plan whose route.next_step_id points to a step that
// doesn't exist. A dangling next_step_id would otherwise be discovered
// at run time (after the routing LLM has been billed for) and is
// trivially detectable at plan load.
func TestWorkflowEdgeRoutingNextStepIDMustExist(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "routing",
				"id":                   "router",
				"title":                "Pick path",
				"description":          "",
				"context_dependencies": []string{},
				"context_output":       "r.json",
				"routing_question":     "Pick route_a",
				"routes": []map[string]interface{}{
					{"route_id": "route_a", "route_name": "A", "condition": "always", "next_step_id": "step-that-does-not-exist"},
					{"route_id": "route_b", "route_name": "B", "condition": "never", "next_step_id": "end"},
				},
				"default_route_id": "route_a",
			},
		},
	}
	writeEdgePlan(t, wo.workspaceDisk, plan)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := wo.orchestrator.Execute(ctx, "dangling next_step_id test", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	if err == nil {
		t.Fatal("expected engine to reject plan with dangling next_step_id; got nil error")
	}
	if !containsAny(err.Error(), "next_step_id", "not found", "unknown step", "does not exist") {
		t.Errorf("error message should pinpoint the missing next_step_id; got: %v", err)
	}
}

// TestWorkflowEdgeConditionalEmptyBranchRequiresNextStepID proves the
// engine refuses a conditional with `if_true_steps: []` AND no
// `if_true_next_step_id` set. Per planning_agent.go:334, NextStepID is
// REQUIRED in this case; a missing one would leave the workflow with
// no successor and the engine would silently end mid-plan.
func TestWorkflowEdgeConditionalEmptyBranchRequiresNextStepID(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "conditional",
				"id":                   "cond",
				"title":                "branch",
				"description":          "",
				"context_dependencies": []string{},
				"context_output":       "c.json",
				"condition_question":   "Is true equal to true?",
				"condition_context":    "",
				"if_true_steps":        []map[string]interface{}{},
				"if_false_steps":       []map[string]interface{}{},
				// Both *_next_step_id intentionally omitted.
			},
		},
	}
	writeEdgePlan(t, wo.workspaceDisk, plan)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := wo.orchestrator.Execute(ctx, "empty-branch test", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	if err == nil {
		t.Fatal("expected engine to reject conditional with empty branches and no next_step_id; got nil error")
	}
	if !containsAny(err.Error(), "next_step_id", "required", "branch", "empty") {
		t.Errorf("error message should explain the empty-branch + missing-next_step_id violation; got: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Shared helpers — kept inside _test.go so they don't leak into the
// production build.

type edgeCaseHarness struct {
	orchestrator  *WorkflowOrchestrator
	workspaceRel  string
	workspaceDisk string
}

// buildEdgeCaseOrchestrator returns a fully-wired WorkflowOrchestrator
// pointing at a per-test workspace under workspace-docs/. Returns
// ok=false (and Skips) when env gates / running server are missing so
// these tests behave identically to the main e2e in CI.
func buildEdgeCaseOrchestrator(t *testing.T) (*edgeCaseHarness, func(), bool) {
	t.Helper()
	if os.Getenv("RUN_WORKFLOW_REAL_E2E") == "" {
		t.Skip("set RUN_WORKFLOW_REAL_E2E=1")
		return nil, func() {}, false
	}
	if os.Getenv("RUN_VERTEX_REAL_E2E") == "" {
		t.Skip("set RUN_VERTEX_REAL_E2E=1")
		return nil, func() {}, false
	}
	var apiKey string
	for _, name := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY", "GOOGLE_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			apiKey = v
			break
		}
	}
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY required")
		return nil, func() {}, false
	}

	wsAPI := strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	if wsAPI == "" {
		wsAPI = "http://127.0.0.1:18744"
		_ = os.Setenv("WORKSPACE_API_URL", wsAPI)
	}
	wsRoot := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH"))
	if wsRoot == "" {
		wsRoot = "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs"
	}

	relWorkspace := "Workflow/_e2e_edge_" + filepath.Base(t.TempDir())
	workspaceDisk := filepath.Join(wsRoot, relWorkspace)
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

	model := strings.TrimSpace(os.Getenv("VERTEX_REAL_E2E_MODEL"))
	if model == "" {
		model = "gemini-3.5-flash"
	}
	llmCfg := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{Provider: "vertex", ModelID: model},
		APIKeys: &llm.ProviderAPIKeys{Vertex: &apiKey},
	}
	agentLLM := &workflowtypes.AgentLLMConfig{Provider: "vertex", ModelID: model}
	presetCfg := &workflowtypes.PresetLLMConfig{
		Provider:     "vertex",
		ModelID:      model,
		PhaseLLM:     agentLLM,
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

	cleanup := func() {
		if os.Getenv("KEEP_E2E_WORKSPACE") == "" {
			_ = os.RemoveAll(workspaceDisk)
		}
	}
	return &edgeCaseHarness{
		orchestrator:  wo,
		workspaceRel:  relWorkspace,
		workspaceDisk: workspaceDisk,
	}, cleanup, true
}

func writeEdgePlan(t *testing.T, workspaceDisk string, plan map[string]interface{}) {
	t.Helper()
	if err := writeJSON(filepath.Join(workspaceDisk, "planning", "plan.json"), plan); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}
}

func containsAny(haystack string, needles ...string) bool {
	lower := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) {
			return true
		}
	}
	return false
}
