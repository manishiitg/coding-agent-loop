package step_based_workflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
)

// This is the regression that the old run_folders schema could not express:
// two real executions reuse iteration-0/default, then rotation archives the
// first path. Their spend must remain two records, never one mixed aggregate.
func TestExecutionKeyedCostLedgerSeparatesIterationZeroReuse(t *testing.T) {
	hcpo := &StepBasedWorkflowOrchestrator{BaseOrchestrator: newFakeWorkspaceAPIWithContent(t, map[string]string{})}
	ctx := context.Background()
	runFolder := "iteration-0/default"

	for _, event := range []struct {
		executionID string
		input       int
	}{
		{executionID: "execution-A", input: 100},
		{executionID: "execution-B", input: 250},
	} {
		err := hcpo.PersistTokenUsage(ctx, runFolder,
			&orchestrator.StepTokenData{Phase: "execution_only", StepID: "collect", ExecutionID: event.executionID},
			&orchestrator.ModelTokenData{Provider: "codex-cli", ModelID: "gpt-5.6-terra", InputTokens: event.input, LLMCallCount: 1},
		)
		if err != nil {
			t.Fatalf("PersistTokenUsage(%s): %v", event.executionID, err)
		}
	}

	path := filepath.Join("costs", "execution", "default", orchestrator.CostDateKey(time.Now())+".json")
	content, err := hcpo.ReadWorkspaceFile(ctx, path)
	if err != nil {
		t.Fatalf("read execution ledger: %v", err)
	}
	var daily orchestrator.DailyGroupTokenUsageFile
	if err := json.Unmarshal([]byte(content), &daily); err != nil {
		t.Fatalf("decode execution ledger: %v", err)
	}
	if len(daily.Executions) != 2 || daily.Executions["execution-A"] == nil || daily.Executions["execution-B"] == nil {
		t.Fatalf("executions = %#v, want separate execution-A and execution-B records", daily.Executions)
	}
	if daily.Executions["execution-A"].TokenUsage.ByModel["gpt-5.6-terra"].InputTokens != 100 {
		t.Fatal("execution-A was merged with a later reuse of iteration-0/default")
	}
	if daily.Executions["execution-B"].TokenUsage.ByModel["gpt-5.6-terra"].InputTokens != 250 {
		t.Fatal("execution-B token total was not retained independently")
	}

}
