package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpexecutor "github.com/manishiitg/mcpagent/executor"
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
		pulseModuleBugReview: {
			Module:       pulseModuleBugReview,
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
	if got := worklist[pulseModuleBugReview].LastDecision; got != "due" {
		t.Fatalf("bug review decision = %q, want due", got)
	}
	if got := worklist[pulseModuleLearningHealth].LastDecision; got != "skipped" {
		t.Fatalf("learning decision = %q, want skipped", got)
	}

	updated, err := markPulseModuleResult(ctx, workspacePath, pulseModuleBugReview, "pulse-run-1", "changed", "Bug Review fixed the skipped step.", []string{"builder/improve.html#decision"})
	if err != nil {
		t.Fatalf("mark result: %v", err)
	}
	if updated.LastDecision != "due" || updated.LastResult != "changed" || updated.LastRanAt == "" {
		t.Fatalf("updated state mismatch: %+v", updated)
	}

	timedOut, err := markPulseModuleResult(ctx, workspacePath, pulseModuleBugReview, "pulse-run-1", "timed_out", "Bug Review exceeded the scheduler wait limit.", []string{"scheduler timeout"})
	if err != nil {
		t.Fatalf("mark timed-out result: %v", err)
	}
	if timedOut.LastResult != "timed_out" || timedOut.LastResultReason == "" {
		t.Fatalf("timed-out state mismatch: %+v", timedOut)
	}
}

func TestPulseWorklistRequiresCompleteModuleSet(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"

	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-1", []PulseWorklistDecision{
		{Module: pulseModuleBugReview, Due: true, Reason: "A step failed."},
	}); err == nil {
		t.Fatal("recordPulseWorklist accepted a partial module list")
	}

	duplicates := completePulseWorklistDecisions(nil)
	duplicates[len(duplicates)-1].Module = pulseModuleBugReview
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-2", duplicates); err == nil {
		t.Fatal("recordPulseWorklist accepted duplicate modules")
	}
}

func TestPulseWorklistValidatesCadenceHints(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"

	missingCadence := completePulseWorklistDecisions(nil)
	missingCadence[0].CooldownRuns = 0
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-missing-cadence", missingCadence); err == nil || !strings.Contains(err.Error(), "must include next_check_at") {
		t.Fatalf("missing skipped cadence error = %v", err)
	}

	invalidDate := completePulseWorklistDecisions(nil)
	invalidDate[0].NextCheckAt = "next Tuesday"
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-invalid-date", invalidDate); err == nil || !strings.Contains(err.Error(), "must be RFC3339 or YYYY-MM-DD") {
		t.Fatalf("invalid next-check date error = %v", err)
	}

	negativeCooldown := completePulseWorklistDecisions(nil)
	negativeCooldown[0].CooldownRuns = -1
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-negative-cooldown", negativeCooldown); err == nil || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("negative cooldown error = %v", err)
	}

	dateOnly := completePulseWorklistDecisions(nil)
	dateOnly[0].CooldownRuns = 0
	dateOnly[0].NextCheckAt = "2026-07-12"
	if _, err := recordPulseWorklist(ctx, workspacePath, "pulse-run-date-only", dateOnly); err != nil {
		t.Fatalf("date-only cadence rejected: %v", err)
	}
}

func TestPulseWorklistToolArgumentsFailClosed(t *testing.T) {
	tests := []struct {
		name string
		item map[string]interface{}
		want string
	}{
		{
			name: "missing due",
			item: map[string]interface{}{"module": pulseModuleBugReview, "reason": "test"},
			want: ".due is required and must be boolean",
		},
		{
			name: "decision alias",
			item: map[string]interface{}{"module": pulseModuleBugReview, "decision": "due", "reason": "test"},
			want: `unknown field "decision"`,
		},
		{
			name: "status alias",
			item: map[string]interface{}{"module": pulseModuleBugReview, "status": "due", "reason": "test"},
			want: `unknown field "status"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pulseWorklistDecisionsFromArgs([]interface{}{tt.item})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parse error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRecordPulseWorklistToolRequiresActiveScheduledRunID(t *testing.T) {
	_, executors, _ := createPulseWorklistTools()
	execute := executors["record_pulse_worklist"].(func(context.Context, map[string]interface{}) (string, error))

	if _, err := execute(context.Background(), map[string]interface{}{"pulse_run_id": "probe"}); err == nil || !strings.Contains(err.Error(), "scheduler-issued") {
		t.Fatalf("probe run id error = %v", err)
	}

	ctx := mcpexecutor.WithSessionID(context.Background(), "schedule-manual--trusted")
	if _, err := execute(ctx, map[string]interface{}{"pulse_run_id": "schedule-manual--different"}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched run id error = %v", err)
	}
}

func TestValidatePulseGateCompletionRequiresWorklistAndCurrentHandoff(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/gate-contract"
	pulseRunID := "schedule-manual--gate-contract"
	htmlPath := workspacePath + "/builder/improve.html"
	workspaceState := &mockWorkspaceAPI{files: map[string]string{
		htmlPath: `<html><details id="pulse-agent-handoff">old run</details></html>`,
	}}
	workspace := httptest.NewServer(workspaceState)
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)

	previousHTML := workspaceState.files[htmlPath]
	if err := validatePulseGateCompletion(ctx, workspacePath, pulseRunID, previousHTML, true); err == nil || !strings.Contains(err.Error(), "complete worklist") {
		t.Fatalf("missing worklist error = %v", err)
	}
	if _, err := recordPulseWorklist(ctx, workspacePath, pulseRunID, completePulseWorklistDecisions(nil)); err != nil {
		t.Fatalf("record complete worklist: %v", err)
	}
	if err := validatePulseGateCompletion(ctx, workspacePath, pulseRunID, previousHTML, true); err == nil || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("unchanged handoff error = %v", err)
	}

	workspaceState.mu.Lock()
	workspaceState.files[htmlPath] = `<html><div>new entry ` + pulseRunID + `</div><details id="pulse-agent-handoff">old run</details></html>`
	workspaceState.mu.Unlock()
	if err := validatePulseGateCompletion(ctx, workspacePath, pulseRunID, previousHTML, true); err == nil || !strings.Contains(err.Error(), "handoff") {
		t.Fatalf("stale handoff error = %v", err)
	}

	workspaceState.mu.Lock()
	workspaceState.files[htmlPath] = `<html><div>new entry</div><details id="pulse-agent-handoff">` + pulseRunID + `</details></html>`
	workspaceState.mu.Unlock()
	if err := validatePulseGateCompletion(ctx, workspacePath, pulseRunID, previousHTML, true); err != nil {
		t.Fatalf("valid Gate completion rejected: %v", err)
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
	if err := initializePulseFinalCommandStates(ctx, workspacePath, "pulse-run-1"); err != nil {
		t.Fatalf("initialize final commands: %v", err)
	}
	if _, err := markPulseFinalCommandState(ctx, workspacePath, pulseFinalCommandDashboard, "pulse-run-1", "done", "Dashboard updated"); err != nil {
		t.Fatalf("mark dashboard: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workflow/pulse-module-state?workspace_path=Workflow/example", nil)
	rec := httptest.NewRecorder()
	(&StreamingAPI{}).handleGetPulseModuleState(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Success  bool                     `json:"success"`
		Modules  []PulseModuleState       `json:"modules"`
		Commands []PulseFinalCommandState `json:"commands"`
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
	if len(payload.Commands) != len(pulseFinalCommandOrder) {
		t.Fatalf("commands = %d, want %d", len(payload.Commands), len(pulseFinalCommandOrder))
	}
	if payload.Commands[0].Command != pulseFinalCommandDashboard || payload.Commands[0].Status != "done" {
		t.Fatalf("dashboard command mismatch: %+v", payload.Commands[0])
	}
}

func TestPulseFinalCommandStatesTrackAndReconcileOutcomes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	workspacePath := "Workflow/example"
	pulseRunID := "pulse-final-1"

	if err := initializePulseFinalCommandStates(ctx, workspacePath, pulseRunID); err != nil {
		t.Fatalf("initialize final commands: %v", err)
	}
	states, err := getPulseFinalCommandStates(ctx, workspacePath)
	if err != nil {
		t.Fatalf("get final commands: %v", err)
	}
	if len(states) != len(pulseFinalCommandOrder) {
		t.Fatalf("states = %d, want %d", len(states), len(pulseFinalCommandOrder))
	}
	for _, state := range states {
		if state.Status != "waiting" || state.PulseRunID != pulseRunID {
			t.Fatalf("unexpected initialized state: %+v", state)
		}
	}

	running, err := markPulseFinalCommandState(ctx, workspacePath, pulseFinalCommandDashboard, pulseRunID, "running", "Updating dashboard")
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if running.StartedAt == "" || running.FinishedAt != "" {
		t.Fatalf("running timestamps mismatch: %+v", running)
	}
	done, err := markPulseFinalCommandState(ctx, workspacePath, pulseFinalCommandDashboard, pulseRunID, "done", "Dashboard updated")
	if err != nil {
		t.Fatalf("mark done: %v", err)
	}
	if done.FinishedAt == "" {
		t.Fatalf("done state missing finished_at: %+v", done)
	}

	if err := finalizeUnresolvedPulseFinalCommands(ctx, workspacePath, pulseRunID, "timed_out", "Finalizer timed out"); err != nil {
		t.Fatalf("reconcile unresolved: %v", err)
	}
	states, err = getPulseFinalCommandStates(ctx, workspacePath)
	if err != nil {
		t.Fatalf("get reconciled commands: %v", err)
	}
	for _, state := range states {
		if state.Command == pulseFinalCommandDashboard {
			if state.Status != "done" {
				t.Fatalf("completed dashboard was overwritten: %+v", state)
			}
			continue
		}
		if state.Status != "timed_out" {
			t.Fatalf("unresolved command not timed out: %+v", state)
		}
	}
}

func completePulseWorklistDecisions(overrides map[string]PulseWorklistDecision) []PulseWorklistDecision {
	out := make([]PulseWorklistDecision, 0, len(pulseModuleOrder))
	for _, module := range pulseModuleOrder {
		decision := PulseWorklistDecision{
			Module:       module,
			Due:          false,
			Reason:       "No evidence requires this module this run.",
			CooldownRuns: 1,
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
