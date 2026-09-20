package server

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/costledger"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestCrewRunTokenUsageFromSummary(t *testing.T) {
	ledger, err := costledger.NewSQLiteLedger(filepath.Join(t.TempDir(), "costs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	entries := []costledger.Entry{
		{EventID: "e1", IdempotencyKey: "e1", ExecutionID: "turn-1", Scope: "chat", EffectiveModelID: "model-a", EffectiveProvider: "prov", LLMCallCount: 1, PromptTokens: 100, CompletionTokens: 10, TotalCostUSD: 0.5, BillingBasis: "provider_actual"},
		{EventID: "e2", IdempotencyKey: "e2", ExecutionID: "turn-1", Scope: "chat", EffectiveModelID: "model-b", EffectiveProvider: "prov", LLMCallCount: 2, PromptTokens: 50, CompletionTokens: 5, CacheReadTokens: 20, TotalCostUSD: 0.25, BillingBasis: "provider_actual"},
		{EventID: "e3", IdempotencyKey: "e3", ExecutionID: "other-turn", Scope: "chat", EffectiveModelID: "model-a", LLMCallCount: 1, PromptTokens: 9999},
	}
	for i := range entries {
		entries[i].Timestamp = time.Date(2026, 9, 19, 1, i, 0, 0, time.UTC)
		if err := ledger.Append(entries[i]); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := ledger.SummarizeExecution("turn-1")
	if err != nil {
		t.Fatal(err)
	}
	usage := crewRunTokenUsageFromSummary(summary)
	if usage.PromptTokens != 150 || usage.CompletionTokens != 15 || usage.CacheReadTokens != 20 || usage.LLMCallCount != 3 {
		t.Fatalf("totals = %+v", usage)
	}
	if usage.CostUSD != 0.75 {
		t.Fatalf("cost = %v, want 0.75", usage.CostUSD)
	}
	if usage.ByModel["model-a"] == nil || usage.ByModel["model-a"].PromptTokens != 100 {
		t.Fatalf("model-a = %+v", usage.ByModel)
	}
	if usage.ByModel["model-b"] == nil || usage.ByModel["model-b"].LLMCallCount != 2 || usage.ByModel["model-b"].Provider != "prov" {
		t.Fatalf("model-b = %+v", usage.ByModel)
	}
	if got := crewRunTokenUsageFromSummary(nil); !got.Empty() {
		t.Fatalf("nil summary = %+v, want empty", got)
	}
}

func TestAccumulateAutomationTurnUsageNoops(t *testing.T) {
	total := &workflowtypes.CrewRunTokenUsage{}
	accumulateAutomationTurnUsage(nil, total, "turn-1")
	ledger, err := costledger.NewSQLiteLedger(filepath.Join(t.TempDir(), "costs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	accumulateAutomationTurnUsage(ledger, total, "")
	accumulateAutomationTurnUsage(ledger, total, "unknown-turn")
	accumulateAutomationTurnUsage(ledger, nil, "turn-1")
	if !total.Empty() {
		t.Fatalf("total = %+v, want empty", total)
	}
}

func TestUpdateScheduleRunTokenUsage(t *testing.T) {
	mock := &mockWorkspaceAPI{files: map[string]string{}}
	ws := httptest.NewServer(mock)
	defer ws.Close()
	t.Setenv("WORKSPACE_API_URL", ws.URL)
	ctx := context.Background()
	if err := AppendScheduleRun(ctx, "Crew/rts", &ScheduleRunEntry{ID: "run-1", ScheduleID: "sched-1", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	usage := &workflowtypes.CrewRunTokenUsage{}
	usage.AddModel("model-a", "prov", 10, 5, 0, 0, 0, 0.1, 1)
	if err := UpdateScheduleRunTokenUsage(ctx, "Crew/rts", "run-1", usage); err != nil {
		t.Fatal(err)
	}
	entry, err := FindScheduleRun(ctx, "Crew/rts", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Usage == nil || entry.Usage.PromptTokens != 10 || entry.Usage.ByModel["model-a"] == nil {
		t.Fatalf("entry usage = %+v", entry.Usage)
	}
	if err := UpdateScheduleRunTokenUsage(ctx, "Crew/rts", "missing", usage); err == nil {
		t.Fatal("missing run must fail")
	}
}

func TestDispatchInternalProductTriggerStampsCaller(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, internalDispatchCrewTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx := context.Background()
	result, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:         triggerCaller{Type: "workflow", ID: "wf-1"},
		WorkflowRunID:  "iteration-0",
		WorkflowStepID: "crew-1",
		DeliveryID:     "d-caller", Payload: []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	entry, err := FindScheduleRun(ctx, runsWorkspace, result.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Caller == nil || entry.Caller.WorkflowID != "wf-1" || entry.Caller.RunID != "iteration-0" || entry.Caller.StepID != "crew-1" {
		t.Fatalf("caller = %+v", entry.Caller)
	}
}

func TestRunCrewStepAdoptsRunUsage(t *testing.T) {
	svc, _ := newInternalDispatchCrew(t, crewRunnerTriggers)
	svc.conversations = map[string]bool{"owner\x1fconversation:crewx:rts": true}
	ctx := context.Background()
	req := testCrewStepRequest()
	runID := webhookDeliveryRunID("rts", "trig-1", crewRunnerDeliveryBase(req))
	first, err := svc.dispatchInternalProductTrigger(ctx, internalCrewTriggerCall{
		UserID: "owner", ProfileID: "crewx", ProjectID: "rts", TriggerID: "trig-1",
		Caller:     triggerCaller{Type: "workflow", ID: "wf-1"},
		DeliveryID: crewRunnerDeliveryBase(req), Payload: []byte(`{}`),
	})
	if err != nil || first.RunID != runID {
		t.Fatalf("seed dispatch = %+v err=%v", first, err)
	}
	runsWorkspace := agentProfileRuntimeWorkspace("owner", "_users/owner/Chats/Work/projects/rts")
	if err := UpdateScheduleRun(ctx, runsWorkspace, runID, "success", "", nil, "", "sess-9"); err != nil {
		t.Fatal(err)
	}
	usage := &workflowtypes.CrewRunTokenUsage{}
	usage.AddModel("model-a", "prov", 10, 5, 0, 0, 0, 0.1, 1)
	if err := UpdateScheduleRunTokenUsage(ctx, runsWorkspace, runID, usage); err != nil {
		t.Fatal(err)
	}
	runner := newCrewStepRunner(svc, "owner")
	result, err := runner.RunCrewStep(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Usage == nil || result.Usage.PromptTokens != 10 || result.Usage.ByModel["model-a"] == nil {
		t.Fatalf("result usage = %+v", result.Usage)
	}
}
