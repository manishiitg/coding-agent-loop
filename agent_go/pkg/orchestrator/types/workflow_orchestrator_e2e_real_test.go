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

// TestWorkflowE2ESingleRegularStepVertex is the tracer-bullet for
// the workflow engine. Smallest possible end-to-end: one regular
// step, real Vertex/Gemini direct-API call, no MCP servers, no
// skills, no browser. If this passes, we extend with routing +
// conditional steps and additional transports (gemini-cli
// structured, opencode-cli structured, cursor-cli tmux) in
// follow-up tests.
//
// Why vertex for the first transport: GEMINI_API_KEY is already in
// the dev .env, and the API path returns in seconds (gemini-cli was
// hanging the CLI invocation for 10+ min on this host, blocking
// iteration). The transport-diversity coverage the user asked for
// rides on three sibling tests once this baseline is green.
//
// Gated on RUN_WORKFLOW_REAL_E2E=1 + RUN_VERTEX_REAL_E2E=1 +
// GEMINI_API_KEY (or VERTEX_API_KEY / GOOGLE_API_KEY).
func TestWorkflowE2ESingleRegularStepVertex(t *testing.T) {
	if os.Getenv("RUN_WORKFLOW_REAL_E2E") == "" {
		t.Skip("set RUN_WORKFLOW_REAL_E2E=1 to run the workflow e2e")
	}
	if os.Getenv("RUN_VERTEX_REAL_E2E") == "" {
		t.Skip("set RUN_VERTEX_REAL_E2E=1 to run the vertex variant")
	}
	var apiKey string
	for _, name := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY", "GOOGLE_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			apiKey = v
			break
		}
	}
	if apiKey == "" {
		t.Skip("GEMINI_API_KEY (or VERTEX_API_KEY / GOOGLE_API_KEY) required")
	}

	// The workflow engine reads workspace files via an HTTP documents
	// API (see base_orchestrator.go:250 — WORKSPACE_API_URL). The
	// user's running mcp-agent-builder-go server hosts this API at
	// 127.0.0.1:18744, serving files relative to workspace-docs/. We
	// must therefore:
	//   1. Point WORKSPACE_API_URL at the running server.
	//   2. Use a RELATIVE workspace path (e.g. Workflow/_e2e_test/<id>).
	//   3. Write fixture files into the real workspace-docs/ tree so
	//      the API can read them.
	// The temp workspace path is computed but the fixture is on disk.
	wsAPI := strings.TrimSpace(os.Getenv("WORKSPACE_API_URL"))
	if wsAPI == "" {
		// Default to the dev server port the user runs locally.
		wsAPI = "http://127.0.0.1:18744"
		_ = os.Setenv("WORKSPACE_API_URL", wsAPI)
	}
	if resp, err := exec.Command("curl", "-fsS", "-o", "/dev/null", "-w", "%{http_code}", wsAPI+"/api/documents/_index.json").Output(); err != nil || len(resp) == 0 {
		t.Skipf("workspace API at %s unreachable (is the dev server running?): %v", wsAPI, err)
	}

	// Pick a workspace path under workspace-docs so the API can serve it.
	wsRoot := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH"))
	if wsRoot == "" {
		wsRoot = "/Users/mipl/ai-work/mcp-agent-builder-go/workspace-docs"
	}
	relWorkspace := "Workflow/_e2e_test_" + filepath.Base(t.TempDir())
	workspace := relWorkspace
	planningDir := filepath.Join(wsRoot, relWorkspace, "planning")
	// Keep workspace on disk if KEEP_E2E_WORKSPACE=1 so we can inspect
	// what the LLM actually wrote during debugging.
	if os.Getenv("KEEP_E2E_WORKSPACE") == "" {
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(wsRoot, relWorkspace))
		})
	}
	if err := os.MkdirAll(planningDir, 0o755); err != nil {
		t.Fatalf("mkdir planning: %v", err)
	}

	// Plan covers every non-human step type in one workflow:
	//   regular → routing → conditional → todo_task → message_sequence
	// Each step's prompt is intentionally trivial so the LLM completes
	// fast (no tools attached anyway — see baseline e2e doc above).
	// Step IDs are stable so the per-step step_done.json assertion
	// below can address each one.
	stepIDs := []string{"step-regular", "step-routing", "step-conditional", "step-todo", "step-message-seq"}
	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   stepIDs[0],
				"title":                "Regular sanity step",
				"description":          "Acknowledge with the single word ACK and stop. No tool calls needed.",
				"context_dependencies": []string{},
				"context_output":       "step1.json",
			},
			{
				"type":                 "routing",
				"id":                   stepIDs[1],
				"title":                "Pick a path",
				"description":          "",
				"context_dependencies": []string{},
				"context_output":       "step2.json",
				"routing_question":     "Pick route_a unconditionally.",
				"routes": []map[string]interface{}{
					{"route_id": "route_a", "route_name": "A", "condition": "Always pick this", "next_step_id": stepIDs[2]},
					{"route_id": "route_b", "route_name": "B", "condition": "Never pick this", "next_step_id": stepIDs[2]},
				},
				"default_route_id": "route_a",
			},
			{
				"type":                 "conditional",
				"id":                   stepIDs[2],
				"title":                "Branch on a trivial condition",
				"description":          "",
				"context_dependencies": []string{},
				"context_output":       "step3.json",
				"condition_question":   "Is the string 'yes' equal to the string 'yes'? Answer true.",
				"condition_context":    "",
				// Empty branches require NextStepID per planning_agent.go:334.
				"if_true_steps":         []map[string]interface{}{},
				"if_false_steps":        []map[string]interface{}{},
				"if_true_next_step_id":  stepIDs[3],
				"if_false_next_step_id": stepIDs[3],
			},
			{
				"type":                 "todo_task",
				"id":                   stepIDs[3],
				"title":                "Trivial todo task",
				"description":          "Create one todo and mark it done. Then complete the step.",
				"context_dependencies": []string{},
				"context_output":       "step4.json",
				// At least one predefined route is needed so the orchestrator
				// has a sub-agent to delegate to. Use a nested regular step.
				"predefined_routes": []map[string]interface{}{
					{
						"route_id":   "ack-route",
						"route_name": "Ack",
						"condition":  "Always pick this route to acknowledge the todo",
						"sub_agent_step": map[string]interface{}{
							"type":                 "regular",
							"id":                   "step-todo-subagent",
							"title":                "Acknowledge",
							"description":          "Reply with the single word ACK and stop.",
							"context_dependencies": []string{},
							"context_output":       "step4_sub.json",
						},
					},
				},
				"next_step_id": stepIDs[4],
			},
			{
				"type":                 "message_sequence",
				"id":                   stepIDs[4],
				"title":                "Single user message",
				"description":          "Multi-turn sanity",
				"context_dependencies": []string{},
				"context_output":       "step5.json",
				"items": []map[string]interface{}{
					{
						"id":      "msg-1",
						"type":    "user_message",
						"title":   "Sanity",
						"message": "Reply with the single word DONE and stop.",
					},
				},
				"next_step_id": "end",
			},
		},
	}
	if err := writeJSON(filepath.Join(planningDir, "plan.json"), plan); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}
	// variables.json lives at <workspace>/variables/variables.json
	// (controller.go:669) and the execution path requires at least
	// one enabled VariableGroup or it bails with "no enabled variable
	// groups found for execution" (controller.go:1160).
	variablesDir := filepath.Join(wsRoot, relWorkspace, "variables")
	if err := os.MkdirAll(variablesDir, 0o755); err != nil {
		t.Fatalf("mkdir variables: %v", err)
	}
	variablesManifest := map[string]interface{}{
		"variables":       []interface{}{},
		"groups":          []map[string]interface{}{{"name": "default", "values": map[string]string{}, "enabled": true}},
		"extraction_date": time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(variablesDir, "variables.json"), variablesManifest); err != nil {
		t.Fatalf("write variables.json: %v", err)
	}

	// Vertex direct-API config: GEMINI_API_KEY in APIKeys, model id
	// passed through Primary. The vertex adapter reads APIKeys at
	// call time so the merged map is what matters here.
	model := strings.TrimSpace(os.Getenv("VERTEX_REAL_E2E_MODEL"))
	if model == "" {
		model = "gemini-3.5-flash"
	}
	llmCfg := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{
			Provider: "vertex",
			ModelID:  model,
		},
		APIKeys: &llm.ProviderAPIKeys{
			Vertex: &apiKey,
		},
	}
	// Step execution agent selection (controller_agent_factory.go:575
	// selectExecutionLLM) consults, in order: per-step AgentConfigs.
	// ExecutionLLM → sub-agent context override → TieredConfig via
	// tierResolver → none. Without a TieredConfig the engine bails
	// with "no valid LLM configuration found for execution agent".
	// So a minimal e2e MUST supply a 3-tier config even if every tier
	// points at the same model. PhaseLLM separately handles planning
	// and evaluation phase agents (workflow_orchestrator.go:316).
	agentLLM := &workflowtypes.AgentLLMConfig{
		Provider: "vertex",
		ModelID:  model,
	}
	presetCfg := &workflowtypes.PresetLLMConfig{
		Provider: "vertex",
		ModelID:  model,
		PhaseLLM: agentLLM,
		TieredConfig: &workflowtypes.TieredLLMConfig{
			Tier1: agentLLM,
			Tier2: agentLLM,
			Tier3: agentLLM,
		},
	}

	wo, err := NewWorkflowOrchestrator(
		"",                  // mcpConfigPath — empty: no MCP servers
		0.7,                 // temperature
		"workflow",          // agentMode
		loggerv2.NewNoop(),  // logger
		nil,                 // eventBridge — no-op
		nil,                 // tracer
		[]string{},          // selectedServers
		[]string{},          // selectedTools
		false,               // useCodeExecutionMode
		nil,                 // customTools
		map[string]interface{}{}, // customToolExecutors
		llmCfg,              // llmConfig
		10,                  // maxTurns
		map[string]string{}, // toolCategories
		presetCfg,           // presetLLMConfig
	)
	if err != nil {
		t.Fatalf("NewWorkflowOrchestrator: %v", err)
	}

	// Generous budget: 5 step types × ~30-60s each on vertex/Flash
	// plus workflow scaffolding overhead. Cold runs land in ~3-5 min;
	// budget 15 min for safety.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	result, err := wo.Execute(ctx, "Write PINGPONG to output.txt", workspace, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.TrimSpace(result) == "" {
		t.Fatal("Execute returned empty result")
	}

	// Engine-level assertion: the step must be marked done by the
	// executor. The engine writes step_done.json under
	// runs/iteration-0/<group>/execution/<step-id>/ when the step
	// finishes — regardless of whether the LLM's content was a
	// success or failure result. This is the right proof-of-engine
	// signal: it shows the orchestrator drove plan → step executor →
	// completion path, which is what the e2e is testing.
	//
	// We do NOT assert that output.txt was created by the LLM,
	// because this baseline test attaches no file-writing tools
	// (no MCP, no skills, no customTools). Step-content assertions
	// belong on a separate test that wires up workspace tools.
	walkRoot := filepath.Join(wsRoot, relWorkspace)
	assertAllStepsExecutedAndDecisionsMatch(t, walkRoot, stepIDs)
	t.Logf("✅ workflow e2e (%d step types, vertex): result-len=%d", len(stepIDs), len(result))
}

func writeJSON(path string, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// assertAllStepsExecutedAndDecisionsMatch is the Tier-1 assertion: for
// each step type it confirms (a) the engine wrote the per-type
// completion marker AND (b) where the marker carries the LLM's
// decision (routing, conditional), the decision matches the
// deterministic value our prompts steered the LLM toward. Catches
// silent-skip bugs (smoke-pass with no execution) and LLM/prompt
// drift that previously would have passed the lower-bar "did it
// produce a file" check.
func assertAllStepsExecutedAndDecisionsMatch(t *testing.T, walkRoot string, stepIDs []string) {
	t.Helper()
	type marker struct {
		// globs lists candidate file paths; first match wins.
		globs []string
		// assertField is optional: when set, the marker JSON must
		// decode and the field must equal wantValue. Used to verify
		// LLM decisions, not just file presence.
		assertField string
		wantValue   interface{}
	}
	markers := map[string]marker{
		// regular and conditional both write step_done.json on completion
		// (controller_execution.go:2792 and controller_conditional.go:484).
		"step-regular":     {globs: []string{filepath.Join(walkRoot, "runs", "*", "*", "execution", "step-regular", "step_done.json")}},
		"step-conditional": {globs: []string{filepath.Join(walkRoot, "runs", "*", "*", "execution", "step-conditional", "step_done.json")}},
		// routing-evaluation.json records the LLM's selected_route_id.
		// We prompted "Pick route_a unconditionally"; the engine must
		// actually pick route_a or the routing path is broken.
		"step-routing": {
			globs:       []string{filepath.Join(walkRoot, "runs", "*", "*", "logs", "step-routing", "routing-evaluation.json")},
			assertField: "selected_route_id",
			wantValue:   "route_a",
		},
		// todo_task: prompts.json is the cheapest proof the step
		// reached its agent factory. The richer assertion (todos
		// created+completed) requires reading runtime fields that
		// the engine does not persist to disk (TodoTaskResponse is
		// runtime-only per planning_agent.go:646).
		"step-todo": {globs: []string{filepath.Join(walkRoot, "runs", "*", "*", "logs", "step-todo", "execution", "todo-task-prompts.json")}},
		// message_sequence persists session.json under
		// execution/message_sequences/<step-path>/<step-id>/.
		"step-message-seq": {globs: []string{filepath.Join(walkRoot, "runs", "*", "*", "execution", "message_sequences", "*", "step-message-seq", "session.json")}},
	}
	// Conditional also drops a decision file at
	// logs/step-conditional/conditional-evaluation.json with
	// condition_result. Assert the LLM picked TRUE because our prompt
	// asked "Is 'yes' equal to 'yes'?".
	conditionalDecisionGlob := filepath.Join(walkRoot, "runs", "*", "*", "logs", "step-conditional", "conditional-evaluation.json")

	var completed, missing []string
	for _, id := range stepIDs {
		m := markers[id]
		var match string
		for _, g := range m.globs {
			matches, _ := filepath.Glob(g)
			if len(matches) > 0 {
				match = matches[0]
				break
			}
		}
		if match == "" {
			missing = append(missing, id)
			continue
		}
		completed = append(completed, id)
		if m.assertField == "" {
			continue
		}
		body, err := os.ReadFile(match)
		if err != nil {
			t.Errorf("read %s: %v", match, err)
			continue
		}
		var decoded map[string]interface{}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode %s: %v\nbody=%s", match, err, body)
			continue
		}
		got := decoded[m.assertField]
		if got != m.wantValue {
			t.Errorf("%s: %s = %v, want %v (full file %s)", id, m.assertField, got, m.wantValue, match)
		}
	}
	if matches, _ := filepath.Glob(conditionalDecisionGlob); len(matches) > 0 {
		body, _ := os.ReadFile(matches[0])
		var decoded map[string]interface{}
		if err := json.Unmarshal(body, &decoded); err == nil {
			if got, _ := decoded["condition_result"].(bool); !got {
				t.Errorf("step-conditional: condition_result = %v, want true (full file %s)", decoded["condition_result"], matches[0])
			}
		}
	} else {
		t.Errorf("step-conditional: conditional-evaluation.json missing at %s", conditionalDecisionGlob)
	}

	if len(missing) > 0 {
		t.Errorf("completion marker missing for steps: %v (completed: %v)", missing, completed)
	}
	t.Logf("completed steps: %v", completed)
}
