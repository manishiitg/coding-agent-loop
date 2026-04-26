package step_based_workflow

import (
	"testing"
	"time"
)

func TestBuildTimingTraceComputesUnionAndTokenBreakdown(t *testing.T) {
	start := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)

	llmTiming := persistedLLMTimingSummary{
		Count:           1,
		TotalDurationMs: 1000,
		Calls: []persistedLLMCallTiming{{
			ModelID:                "test-model",
			Status:                 "success",
			DurationMs:             1000,
			OffsetFromAgentStartMs: 0,
			PromptTokens:           100,
			CompletionTokens:       25,
			TotalTokens:            125,
		}},
	}
	toolTiming := persistedToolTimingSummary{
		Count:           1,
		TotalDurationMs: 1000,
		Calls: []persistedToolCallTiming{{
			ToolCallID:             "tool-1",
			ToolName:               "execute_shell_command",
			Status:                 "success",
			DurationMs:             1000,
			OffsetFromAgentStartMs: 500,
			ArgsBytes:              40,
			ResultBytes:            400,
			EstimatedArgsTokens:    10,
			EstimatedResultTokens:  100,
		}},
	}

	spans, breakdown := buildTimingTrace("step-test", "agent-test", "test-model", start, end, 2*time.Second, llmTiming, toolTiming)

	if breakdown.WallDurationMs != 2000 {
		t.Fatalf("expected wall duration 2000ms, got %d", breakdown.WallDurationMs)
	}
	if breakdown.TrackedUnionDurationMs != 1500 {
		t.Fatalf("expected tracked union 1500ms, got %d", breakdown.TrackedUnionDurationMs)
	}
	if breakdown.UntrackedDurationMs != 500 {
		t.Fatalf("expected untracked 500ms, got %d", breakdown.UntrackedDurationMs)
	}
	if breakdown.TotalInputTokens != 100 || breakdown.TotalOutputTokens != 25 || breakdown.TotalTokens != 125 {
		t.Fatalf("unexpected token breakdown: %+v", breakdown)
	}
	if breakdown.ToolArgsBytes != 40 || breakdown.ToolResultBytes != 400 {
		t.Fatalf("unexpected tool byte breakdown: %+v", breakdown)
	}

	if len(spans) != 4 {
		t.Fatalf("expected agent, llm, tool, and overhead spans, got %d", len(spans))
	}
	if spans[0].Type != "agent" || spans[0].SpanID != "step-test:agent" {
		t.Fatalf("expected first span to be root agent span, got %+v", spans[0])
	}

	foundOverhead := false
	for _, span := range spans {
		if span.Type == "overhead" {
			foundOverhead = true
			if span.Status != "inferred" || span.DurationMs != 500 {
				t.Fatalf("unexpected overhead span: %+v", span)
			}
		}
	}
	if !foundOverhead {
		t.Fatal("expected inferred overhead span")
	}
}
