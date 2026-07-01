package server

import (
	"strings"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

func TestBuildWorkflowCostSummaryIncludesRunPhaseAndTopDrivers(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	runs := []workflowRunCostEntry{
		{
			RunFolder:            "iteration-2/group-a",
			TokenUsage:           testTokenUsageFile(base.Add(2*time.Minute), "claude-sonnet", "anthropic", 1.25, 1200, 300, 2),
			EvaluationTokenUsage: testTokenUsageFile(base.Add(3*time.Minute), "gpt-4.1-mini", "openai", 0.25, 500, 80, 1),
		},
		{
			RunFolder:  "iteration-1/group-a",
			TokenUsage: testTokenUsageFile(base.Add(time.Minute), "gpt-4.1-mini", "openai", 0.50, 800, 120, 1),
		},
	}
	phaseUsage := &orchestrator.PhaseTokenUsageFile{
		UpdatedAt: base.Add(4 * time.Minute),
		ByModel: map[string]*orchestrator.ModelTokenUsage{
			"builder-model": {Provider: "openai", TotalCost: 0.10, InputTokens: 300, OutputTokens: 40, LLMCallCount: 1},
		},
	}

	summary := buildWorkflowCostSummary(runs, phaseUsage, nil)

	if summary.RunCount != 2 {
		t.Fatalf("RunCount = %d, want 2", summary.RunCount)
	}
	if summary.LatestRun == nil || summary.LatestRun.RunFolder != "iteration-2/group-a" {
		t.Fatalf("LatestRun = %#v, want iteration-2/group-a", summary.LatestRun)
	}
	assertFloatEqual(t, "ExecutionCostUSD", summary.ExecutionCostUSD, 1.75)
	assertFloatEqual(t, "EvaluationCostUSD", summary.EvaluationCostUSD, 0.25)
	assertFloatEqual(t, "BuilderCostUSD", summary.BuilderCostUSD, 0.10)
	assertFloatEqual(t, "TotalCostUSD", summary.TotalCostUSD, 2.10)
	if len(summary.TopDrivers) == 0 || summary.TopDrivers[0].Name != "claude-sonnet" || summary.TopDrivers[0].Source != "execution" {
		t.Fatalf("TopDrivers[0] = %#v, want execution claude-sonnet", summary.TopDrivers)
	}
}

func TestWorkflowRunCostNotificationFormatting(t *testing.T) {
	runs := []workflowRunCostEntry{{
		RunFolder:            "iteration-2/group-a",
		TokenUsage:           testTokenUsageFile(time.Now(), "claude-sonnet", "anthropic", 0.014, 100, 20, 1),
		EvaluationTokenUsage: testTokenUsageFile(time.Now(), "gpt-4.1-mini", "openai", 0.004, 50, 10, 1),
	}}

	summary, drivers := workflowRunCostSummaryForFolder(runs, "iteration-2")
	if summary == nil {
		t.Fatal("expected run summary")
	}
	line := formatWorkflowRunCostNotification(*summary, drivers)
	for _, want := range []string{"Cost: total $0.02", "execution $0.01", "evaluation $0.0040", "Top driver: execution model claude-sonnet"} {
		if !strings.Contains(line, want) {
			t.Fatalf("expected notification line to contain %q, got %q", want, line)
		}
	}
}

func TestMergeBackgroundAgentMetadataPreservesWorkflowPath(t *testing.T) {
	merged := mergeBackgroundAgentMetadata(
		map[string]string{"workflow_path": "Workflow/demo", "preset_query_id": "preset-1"},
		map[string]string{"run_folder": "iteration-1/group-a", "status": "completed"},
	)

	if merged["workflow_path"] != "Workflow/demo" {
		t.Fatalf("workflow_path = %q, want Workflow/demo", merged["workflow_path"])
	}
	if merged["run_folder"] != "iteration-1/group-a" {
		t.Fatalf("run_folder = %q, want iteration-1/group-a", merged["run_folder"])
	}
}

func testTokenUsageFile(updatedAt time.Time, modelID, provider string, cost float64, inputTokens, outputTokens, calls int) *orchestrator.TokenUsageFile {
	return &orchestrator.TokenUsageFile{
		CreatedAt: updatedAt.Add(-time.Minute),
		UpdatedAt: updatedAt,
		ByModel: map[string]*orchestrator.ModelTokenUsage{
			modelID: {
				Provider:     provider,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
				LLMCallCount: calls,
				TotalCost:    cost,
			},
		},
	}
}

func assertFloatEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if got < want-0.000001 || got > want+0.000001 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}
