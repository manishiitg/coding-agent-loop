package types

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

// Tests exercising specific step-execution modes the main 5-step e2e
// doesn't cover. Same env gates + harness as edge_cases_test.go.
//
// Every test here uses tightened assertions (post the realisation in
// the prior round that "tokens appear in session.json" doesn't prove
// sequencing — the engine could batch prompts into one LLM turn and
// still produce all tokens). The new shape parses the session
// conversation_history directly and asserts on per-turn structure.

// ──────────────────────────────────────────────────────────────────────
// Shared session parsing — used by the message_sequence tests below.

// sessionFile mirrors the on-disk shape of session.json the engine
// writes under execution/message_sequences/<step-path>/<step-id>/.
// Field names are PascalCase because the engine marshals
// llmtypes.MessageContent that way.
type sessionFile struct {
	SessionID           string           `json:"session_id"`
	StepID              string           `json:"step_id"`
	Status              string           `json:"status"`
	ConversationHistory []sessionMessage `json:"conversation_history"`
}

type sessionMessage struct {
	Role  string        `json:"Role"`
	Parts []sessionPart `json:"Parts"`
}

type sessionPart struct {
	Text string `json:"Text,omitempty"`
	Type string `json:"Type,omitempty"`
}

func (m sessionMessage) text() string {
	var b strings.Builder
	for _, p := range m.Parts {
		b.WriteString(p.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

func loadSessionJSON(t *testing.T, workspaceDisk, stepID string) *sessionFile {
	t.Helper()
	pat := filepath.Join(workspaceDisk, "runs", "*", "*", "execution", "message_sequences", "*", stepID, "session.json")
	matches, _ := filepath.Glob(pat)
	if len(matches) == 0 {
		t.Fatalf("session.json not written under %s — message_sequence step did not run", pat)
	}
	body, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	var sess sessionFile
	if err := json.Unmarshal(body, &sess); err != nil {
		t.Fatalf("parse session.json: %v\nbody=%s", err, body)
	}
	return &sess
}

// roleTurns extracts the messages with the given role in document order.
func (s *sessionFile) roleTurns(role string) []sessionMessage {
	out := make([]sessionMessage, 0)
	for _, m := range s.ConversationHistory {
		if strings.EqualFold(m.Role, role) {
			out = append(out, m)
		}
	}
	return out
}

// ──────────────────────────────────────────────────────────────────────
// Multi-item sequencing (tightened from the original broken version)

// TestWorkflowMessageSequenceMultiItem proves the engine **actually
// sequences** — not just that all expected tokens appear in
// session.json. Specifically:
//
//	(1) conversation_history contains AT LEAST 3 distinct human turns —
//	    one per item. A batching engine that collapses items into a
//	    single prompt would have only 1 human turn and fail here.
//	(2) Each of the 3 deterministic tokens appears in EXACTLY ONE ai
//	    reply, AND each reply gets a different token. A single ai turn
//	    dumping all three tokens (e.g. because the prompts were
//	    concatenated) would fail.
//	(3) The tokens appear in the right turn order: ROUND_1 in the 1st
//	    relevant ai turn, ROUND_2_AFTER_1 in the 2nd, ROUND_3_FINAL in
//	    the 3rd.
//
// Gated on RUN_WORKFLOW_REAL_E2E + RUN_VERTEX_REAL_E2E + GEMINI_API_KEY.
func TestWorkflowMessageSequenceMultiItem(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	writeEdgePlan(t, wo.workspaceDisk, map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "message_sequence",
				"id":                   "multi-seq",
				"title":                "Multi-item sequence",
				"description":          "Three back-and-forth user_message items",
				"context_dependencies": []string{},
				"context_output":       "ms.json",
				"items": []map[string]interface{}{
					{
						"id":      "msg-1",
						"type":    "user_message",
						"title":   "Round 1",
						"message": "First turn of a three-turn conversation. Reply with EXACTLY the line ROUND_1 on a line by itself and stop. Do not write any code; do not call any tools.",
					},
					{
						"id":      "msg-2",
						"type":    "user_message",
						"title":   "Round 2",
						"message": "Second turn. Earlier you replied ROUND_1. Now reply with EXACTLY the line ROUND_2_AFTER_1 on a line by itself and stop. Do not write any code; do not call any tools.",
					},
					{
						"id":      "msg-3",
						"type":    "user_message",
						"title":   "Round 3",
						"message": "Third and final turn. Reply with EXACTLY the line ROUND_3_FINAL on a line by itself and stop. Do not write any code; do not call any tools.",
					},
				},
				"next_step_id": "end",
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := wo.orchestrator.Execute(ctx, "Three-turn message sequence", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sess := loadSessionJSON(t, wo.workspaceDisk, "multi-seq")
	if sess.Status != "completed" {
		t.Errorf("session.status = %q, want completed", sess.Status)
	}

	// (1) Three distinct human turns. Each item should land as one
	//     human message.
	humans := sess.roleTurns("human")
	if len(humans) < 3 {
		t.Fatalf("expected >=3 human turns (one per item); got %d — engine likely batched prompts", len(humans))
	}

	// (2) Each token appears in exactly one ai reply, and (3) in the
	//     right order. Walk the ai turns in document order, look for
	//     the next expected token in each. A reply containing two of
	//     the tokens at once is a batching failure.
	wantTokens := []string{"ROUND_1", "ROUND_2_AFTER_1", "ROUND_3_FINAL"}
	ais := sess.roleTurns("ai")
	if len(ais) < 3 {
		t.Fatalf("expected >=3 ai turns; got %d — engine did not let the LLM reply per item", len(ais))
	}
	// For each token, find the FIRST ai turn that contains it. Assert
	// they are different turns AND in order.
	tokenTurn := make(map[string]int)
	for ti, tok := range wantTokens {
		for i, ai := range ais {
			if strings.Contains(ai.text(), tok) {
				tokenTurn[tok] = i
				break
			}
		}
		if _, ok := tokenTurn[tok]; !ok {
			t.Errorf("token %q not in any ai turn — item %d either didn't run or LLM didn't follow the prompt", tok, ti+1)
		}
	}
	if len(tokenTurn) == 3 {
		if tokenTurn["ROUND_1"] >= tokenTurn["ROUND_2_AFTER_1"] {
			t.Errorf("ROUND_1 turn=%d, ROUND_2_AFTER_1 turn=%d — items ran out of order", tokenTurn["ROUND_1"], tokenTurn["ROUND_2_AFTER_1"])
		}
		if tokenTurn["ROUND_2_AFTER_1"] >= tokenTurn["ROUND_3_FINAL"] {
			t.Errorf("ROUND_2_AFTER_1 turn=%d, ROUND_3_FINAL turn=%d — items ran out of order", tokenTurn["ROUND_2_AFTER_1"], tokenTurn["ROUND_3_FINAL"])
		}
		// Distinctness: each token in its own ai turn.
		seen := map[int]string{}
		for tok, turn := range tokenTurn {
			if other, dup := seen[turn]; dup {
				t.Errorf("tokens %q and %q both appear in ai turn %d — engine collapsed items into one LLM reply", tok, other, turn)
			}
			seen[turn] = tok
		}
	}
	t.Logf("✅ multi-item sequence: %d human turns, %d ai turns, token→turn: %v", len(humans), len(ais), tokenTurn)
}

// TestWorkflowMessageSequenceItemTypeInvalidRejected proves the
// engine refuses a message_sequence item with an unknown `type` field.
// Today valid types are exactly user_message | code | prevalidation
// (planning_agent.go:594). A typo or hallucinated item type should
// fail at plan load, not be silently dropped or executed as
// user_message.
func TestWorkflowMessageSequenceItemTypeInvalidRejected(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	writeEdgePlan(t, wo.workspaceDisk, map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "message_sequence",
				"id":                   "bad-type-seq",
				"title":                "Bad item type",
				"description":          "first item is a valid user_message, second has a hallucinated type",
				"context_dependencies": []string{},
				"context_output":       "bt.json",
				"items": []map[string]interface{}{
					{"id": "ok", "type": "user_message", "title": "OK", "message": "Reply ACK."},
					{"id": "bad", "type": "telepathy", "title": "Bad", "message": "this should never run"},
				},
				"next_step_id": "end",
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, err := wo.orchestrator.Execute(ctx, "invalid item type", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	})
	if err == nil {
		t.Fatal("expected engine to reject item with unknown type=\"telepathy\"; got nil error")
	}
	if !containsAny(err.Error(), "type", "telepathy", "invalid", "unknown", "unsupported") {
		t.Errorf("error should pinpoint the bad item type; got: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────
// learn_code mode (tightened from the original broken version)

// TestWorkflowLearnCodeFastPathRunsPreSeededScript proves the engine's
// learn_code FAST PATH actually runs a pre-existing main.py — which
// is the entire point of the mode (controller_learn_code.go:838,
// tryRunSavedLearnCodeScript). We:
//
//	(1) pre-seed learnings/<step-id>/main.py with a known Python script
//	    that prints a deterministic token to stdout.
//	(2) configure the step with declared_execution_mode="learn_code".
//	(3) run the workflow.
//	(4) assert the engine ran the script — proof is the script's token
//	    in the step's execution_result AND the engine's "Executing
//	    saved script" branch artifact (execution/code/main.py copied
//	    from learnings/, see controller_learn_code.go:887).
//
// This pair of checks is the load-bearing assertion for learn_code:
// "given a saved script, the engine reuses it instead of calling the
// LLM." An earlier shape of this test only verified that some file
// was written under learnings/ — which a metadata-only no-tools run
// also satisfies (the engine writes .learning_metadata.json for any
// learn_code step regardless of whether main.py was authored). The
// looser test was therefore a false positive; this version proves
// real script execution.
func TestWorkflowLearnCodeFastPathRunsPreSeededScript(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	const stepID = "learn-code-step"
	const scriptToken = "PRE_SEEDED_SCRIPT_RAN_42"

	// 1) Pre-seed the saved script. The engine's
	//    hasValidLearnedScriptAPI gate only checks for the file's
	//    existence (controller_learn_code.go:597) so writing main.py
	//    on disk is sufficient — no need to pre-seed metadata.
	learningsDir := filepath.Join(wo.workspaceDisk, "learnings", stepID)
	if err := os.MkdirAll(learningsDir, 0o755); err != nil {
		t.Fatalf("mkdir learnings: %v", err)
	}
	// The engine runs reviewMainPyScript (controller_learn_code.go:352)
	// as a static check before executing main.py. It rejects scripts
	// that use os.environ.get(KEY, fallback) for required env vars
	// because fallbacks silently hide misconfiguration. Use the
	// throws-on-missing form os.environ["KEY"] instead.
	mainPyContent := `#!/usr/bin/env python3
import os, sys
out_dir = os.environ["STEP_OUTPUT_DIR"]
os.makedirs(out_dir, exist_ok=True)
result = 6 * 7
print("` + scriptToken + `=" + str(result))
with open(os.path.join(out_dir, "computed.txt"), "w") as f:
    f.write("computed=" + str(result) + "\n")
`
	if err := os.WriteFile(filepath.Join(learningsDir, "main.py"), []byte(mainPyContent), 0o644); err != nil {
		t.Fatalf("seed main.py: %v", err)
	}

	// 2) Plan + step_config declaring learn_code mode.
	writeEdgePlan(t, wo.workspaceDisk, map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   stepID,
				"title":                "Learn-code fast path",
				"description":          "Use the saved Python script under learnings/learn-code-step/main.py to compute 6*7.",
				"context_dependencies": []string{},
				"context_output":       "out.json",
			},
		},
	})
	stepConfig := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"id":    stepID,
				"title": "Learn-code fast path",
				"agent_configs": map[string]interface{}{
					"declared_execution_mode":        "learn_code",
					"declared_execution_mode_reason": "test fixture pinning learn_code mode",
					"learning_objective":             "Compute the constant 42 via 6*7.",
				},
			},
		},
	}
	if err := writeJSON(filepath.Join(wo.workspaceDisk, "planning", "step_config.json"), stepConfig); err != nil {
		t.Fatalf("write step_config.json: %v", err)
	}

	// 3) Run.
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	if _, err := wo.orchestrator.Execute(ctx, "learn_code fast-path test", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// 4a) Engine copies the saved script to execution/code/main.py
	//     before running it (controller_learn_code.go:887). Presence
	//     of that copy is proof the fast path triggered.
	execCodeGlob := filepath.Join(wo.workspaceDisk, "runs", "*", "*", "execution", "*", "code", "main.py")
	if execMatches, _ := filepath.Glob(execCodeGlob); len(execMatches) == 0 {
		t.Errorf("execution/code/main.py was NOT copied — engine did not take the saved-script fast path. (Pattern: %s)", execCodeGlob)
	} else {
		t.Logf("✅ execution/code/main.py copy present: %s", execMatches[0])
	}

	// 4b) The fast path writes a DEDICATED artifact —
	//     logs/<step-id>/execution/learn_code_fast_path.json — that
	//     records the script's exit_code, captured stdout, and the
	//     success/failure verdict. (execution-attempt-*.json is for
	//     LLM-driven execution; the fast path skips it.) The script's
	//     token MUST appear in `output` and `success` must be true.
	fastPathGlob := filepath.Join(wo.workspaceDisk, "runs", "*", "*", "logs", stepID, "execution", "learn_code_fast_path.json")
	fastMatches, _ := filepath.Glob(fastPathGlob)
	if len(fastMatches) == 0 {
		t.Fatalf("no learn_code_fast_path.json at %s — engine did not record a fast-path attempt", fastPathGlob)
	}
	body, err := os.ReadFile(fastMatches[0])
	if err != nil {
		t.Fatalf("read learn_code_fast_path.json: %v", err)
	}
	var fp struct {
		Mode       string `json:"mode"`
		ExitCode   int    `json:"exit_code"`
		Success    bool   `json:"success"`
		Output     string `json:"output"`
		ErrorField string `json:"error"`
		ScriptPath string `json:"script_path"`
	}
	if err := json.Unmarshal(body, &fp); err != nil {
		t.Fatalf("parse learn_code_fast_path.json: %v\nbody=%s", err, body)
	}
	if fp.Mode != "learn_code_fast_path" {
		t.Errorf("fast-path mode = %q, want learn_code_fast_path", fp.Mode)
	}
	if fp.ExitCode != 0 {
		t.Errorf("fast-path exit_code = %d, want 0\n  output=%q\n  error=%q", fp.ExitCode, fp.Output, fp.ErrorField)
	}
	if !fp.Success {
		t.Errorf("fast-path success = false\n  output=%q\n  error=%q", fp.Output, fp.ErrorField)
	}
	if !strings.Contains(fp.Output, scriptToken) {
		t.Errorf("fast-path output missing script token %q\n  output: %q", scriptToken, fp.Output)
	}
	t.Logf("✅ learn_code fast path: exit_code=0, success=true, output=%q", strings.TrimSpace(fp.Output))
}

// TestWorkflowLearnCodeControlNoModeWritesNoScript is the control
// counterpart of the test above: run the same step with the SAME
// plan.json but NO step_config.json (so declared_execution_mode is
// absent). The engine must NOT enter the learn_code path and must
// NOT create learnings/<step-id>/main.py.
//
// Together with the positive test, this pair proves the learn_code
// behavior is gated on declared_execution_mode rather than the
// engine writing main.py for every regular step.
func TestWorkflowLearnCodeControlNoModeWritesNoScript(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	const stepID = "control-no-learn-code"
	writeEdgePlan(t, wo.workspaceDisk, map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   stepID,
				"title":                "Control: no learn_code",
				"description":          "Reply with RESULT=42 and stop.",
				"context_dependencies": []string{},
				"context_output":       "out.json",
			},
		},
	})
	// Intentionally: NO step_config.json.

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if _, err := wo.orchestrator.Execute(ctx, "control run", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	mainPyPath := filepath.Join(wo.workspaceDisk, "learnings", stepID, "main.py")
	if _, err := os.Stat(mainPyPath); err == nil {
		body, _ := os.ReadFile(mainPyPath)
		t.Fatalf("control: learnings/%s/main.py exists when learn_code mode is NOT declared — engine is writing main.py for plain regular steps. main.py:\n%s", stepID, string(body))
	}
	t.Logf("✅ control: no learn_code mode → no main.py written")
}

// TestWorkflowLearnCodeStepConfigIDMismatchNotApplied proves that a
// step_config.json entry whose `id` doesn't match any plan.json step
// is harmlessly ignored — declared_execution_mode is NOT silently
// applied to a step it wasn't intended for, and no spurious learn_code
// artifacts get written.
func TestWorkflowLearnCodeStepConfigIDMismatchNotApplied(t *testing.T) {
	wo, cleanup, ok := buildEdgeCaseOrchestrator(t)
	if !ok {
		return
	}
	defer cleanup()

	const planStepID = "step-x"
	writeEdgePlan(t, wo.workspaceDisk, map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   planStepID,
				"title":                "Plain step",
				"description":          "Reply with RESULT=42 and stop.",
				"context_dependencies": []string{},
				"context_output":       "out.json",
			},
		},
	})

	// step_config.json references a DIFFERENT step id — this should
	// not affect "step-x".
	stepConfig := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"id": "step-y", // mismatched
				"agent_configs": map[string]interface{}{
					"declared_execution_mode": "learn_code",
				},
			},
		},
	}
	if err := writeJSON(filepath.Join(wo.workspaceDisk, "planning", "step_config.json"), stepConfig); err != nil {
		t.Fatalf("write step_config.json: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if _, err := wo.orchestrator.Execute(ctx, "mismatched step_config", wo.workspaceRel, map[string]interface{}{
		"workflowStatus": workflowtypes.WorkflowStatusPreVerification,
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if _, err := os.Stat(filepath.Join(wo.workspaceDisk, "learnings", planStepID, "main.py")); err == nil {
		t.Fatalf("learnings/%s/main.py exists — engine applied learn_code mode to the wrong step (step_config.id was \"step-y\")", planStepID)
	}
	if _, err := os.Stat(filepath.Join(wo.workspaceDisk, "learnings", "step-y", "main.py")); err == nil {
		t.Fatalf("learnings/step-y/main.py exists — engine wrote artifacts for a step that doesn't appear in plan.json")
	}
	t.Logf("✅ step_config.json with mismatched id: no learn_code artifacts written")
}

// min is the local int helper used by the main.py text snippet log.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
