package step_based_workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestExecutionEvidencePathsMatchPulseContract(t *testing.T) {
	logsRoot := getExecutionFolderPathForLogs("Workflow/demo/runs/iteration-0/default", "collect-price", "step-1")
	got := filepath.Join(logsRoot, finalExecutionSummaryFilename)
	want := "Workflow/demo/runs/iteration-0/default/logs/collect-price/execution/execution-final-summary.json"
	if got != want {
		t.Fatalf("final summary path = %q, want %q", got, want)
	}
}

func TestTodoTaskExecutionLogFilenamePreservesRetryIdentity(t *testing.T) {
	if got, want := todoTaskExecutionLogFilename(3, 0), "execution-attempt-3-iteration-0.json"; got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}
}

func TestFinalExecutionSummaryPreservesContributionConcerns(t *testing.T) {
	got := buildDirectModeCompletionSummary(
		"Produced the report.\nSTATUS: COMPLETED",
		"CONCERNS: knowledgebase write failed.\nSTATUS: COMPLETED",
		"CONCERNS: learnings write failed.\nSTATUS: COMPLETED",
	)
	for _, want := range []string{
		"Execution: Produced the report.",
		"KB review: CONCERNS: knowledgebase write failed.",
		"Learnings: CONCERNS: learnings write failed.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("final summary missing %q: %s", want, got)
		}
	}
}

func TestSummarizeExecutionResultPreservesOutcomeConcernsAndStatus(t *testing.T) {
	result := "Published 12 records to db/db.sqlite.\n" +
		"CONCERNS: knowledgebase/notes/acme.md was stale and read-only.\n" +
		"STATUS: COMPLETED"

	if got := summarizeExecutionResultForNotification(result); got != result {
		t.Fatalf("summary = %q, want complete final response %q", got, result)
	}
}

func TestSummarizeExecutionResultCapsFromTailWithoutLosingConcern(t *testing.T) {
	result := strings.Repeat("x", 5000) + "\nCONCERNS: final evidence\nSTATUS: COMPLETED"
	got := summarizeExecutionResultForNotification(result)
	if !strings.HasPrefix(got, "… (earlier response omitted)\n") {
		t.Fatalf("expected bounded summary prefix, got %q", got[:min(len(got), 80)])
	}
	for _, want := range []string{"CONCERNS: final evidence", "STATUS: COMPLETED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("bounded summary lost %q: %s", want, got)
		}
	}
}

func TestLatestAssistantExecutionSummaryUsesFinalAssistantTurn(t *testing.T) {
	history := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: strings.Repeat("old narration ", 300)},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Finish the task."},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "Wrote the report.\nCONCERNS: source B timed out.\nSTATUS: COMPLETED"},
			},
		},
	}

	got := latestAssistantExecutionSummary(history)
	if strings.Contains(got, "old narration") {
		t.Fatalf("summary included earlier narration: %s", got)
	}
	for _, want := range []string{"Wrote the report.", "CONCERNS: source B timed out.", "STATUS: COMPLETED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %s", want, got)
		}
	}
}

func TestMessageSequenceSummaryPropagatesItemConcerns(t *testing.T) {
	session := &messageSequenceSession{
		StepID: "publish-sequence",
		Entries: []messageSequenceEntry{
			{ItemID: "draft", Status: "completed", Summary: "Drafted post.\nSTATUS: COMPLETED"},
			{ItemID: "publish", Status: "completed", Summary: "Published post.\nCONCERNS: analytics receipt was unavailable.\nSTATUS: COMPLETED"},
		},
	}

	got := (&StepBasedWorkflowOrchestrator{}).summarizeMessageSequenceSession(session)
	for _, want := range []string{
		"Message sequence publish-sequence completed: 2 item(s) completed",
		"CONCERNS: publish: analytics receipt was unavailable.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("message-sequence summary missing %q: %s", want, got)
		}
	}
}
