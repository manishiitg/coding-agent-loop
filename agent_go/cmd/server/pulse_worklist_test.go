package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPulseWorklistUsesWorkflowLocalDB(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"
	dbPath := filepath.Join(root, "Workflow", "example", "db", "db.sqlite")

	states, err := getPulseModuleStates(ctx, workspacePath)
	if err != nil {
		t.Fatalf("list before create: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("list before create returned %d states, want 0", len(states))
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("read-only list created db unexpectedly: stat err=%v", err)
	}

	recorded, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-1", completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModuleHarden: {
			Module:       pulseModuleHarden,
			Due:          true,
			Reason:       "Latest run skipped a required step.",
			Evidence:     []string{"runs/iteration-0/logs/step-a"},
			CooldownRuns: 0,
		},
		pulseModuleLearningHealth: {
			Module:       pulseModuleLearningHealth,
			Due:          false,
			Reason:       "No plan or selector change since the last reviewed run.",
			Evidence:     []string{"planning/changelog"},
			CooldownRuns: 2,
		},
	}))
	if err != nil {
		t.Fatalf("record worklist: %v", err)
	}
	if len(recorded) != len(pulseModuleOrder) {
		t.Fatalf("recorded states = %d, want %d", len(recorded), len(pulseModuleOrder))
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected workflow-local db at %s: %v", dbPath, err)
	}

	worklist, ok, err := getPulseWorklistForRun(ctx, workspacePath, "pulse-run-1")
	if err != nil {
		t.Fatalf("get worklist: %v", err)
	}
	if !ok {
		t.Fatal("get worklist ok=false, want true")
	}
	if got := worklist[pulseModuleHarden].LastDecision; got != "due" {
		t.Fatalf("harden decision = %q, want due", got)
	}
	if got := worklist[pulseModuleLearningHealth].LastDecision; got != "skipped" {
		t.Fatalf("learning decision = %q, want skipped", got)
	}

	updated, err := markPulseModuleResult(ctx, workspacePath, pulseModuleHarden, "pulse-run-1", "changed", "Harden fixed the skipped step.", []string{"builder/improve.html#decision"})
	if err != nil {
		t.Fatalf("mark result: %v", err)
	}
	if updated.LastDecision != "due" || updated.LastResult != "changed" || updated.LastRanAt == "" {
		t.Fatalf("updated state mismatch: %+v", updated)
	}
}

func TestPulseWorklistRequiresCompleteModuleSet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"

	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-1", []PulseWorklistDecision{
		{Module: pulseModuleHarden, Due: true, Reason: "A step failed."},
	}); err == nil {
		t.Fatal("recordPulseWorklist accepted a partial module list")
	}

	duplicates := completePulseWorklistDecisions(nil)
	duplicates[len(duplicates)-1].Module = pulseModuleHarden
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-2", duplicates); err == nil {
		t.Fatal("recordPulseWorklist accepted duplicate modules")
	}
}

func TestHandleGetPulseModuleState(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"

	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-1", completePulseWorklistDecisions(map[string]PulseWorklistDecision{
		pulseModuleGoalAdvisor: {
			Module: pulseModuleGoalAdvisor,
			Due:    true,
			Reason: "Goal trend is below target for two runs.",
		},
	})); err != nil {
		t.Fatalf("record worklist: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/pulse-module-state?workspace_path=Workflow/example", nil)
	rec := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetPulseModuleState(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success bool               `json:"success"`
		Modules []PulseModuleState `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Success {
		t.Fatal("success=false")
	}
	if len(payload.Modules) != len(pulseModuleOrder) {
		t.Fatalf("modules = %d, want %d", len(payload.Modules), len(pulseModuleOrder))
	}
}

func completePulseWorklistDecisions(overrides map[string]PulseWorklistDecision) []PulseWorklistDecision {
	out := make([]PulseWorklistDecision, 0, len(pulseModuleOrder))
	for _, module := range pulseModuleOrder {
		decision := PulseWorklistDecision{
			Module: module,
			Due:    false,
			Reason: "No evidence requires this module this run.",
		}
		if overrides != nil {
			if override, ok := overrides[module]; ok {
				if override.Module == "" {
					override.Module = module
				}
				decision = override
			}
		}
		out = append(out, decision)
	}
	return out
}
