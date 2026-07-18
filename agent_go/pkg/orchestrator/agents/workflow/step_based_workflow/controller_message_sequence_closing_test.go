package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// Validates sequence-unification Stage 3: a todo_task's `messages` (old
// TodoTaskMessage JSON shape) deserializes, back-compat, into the unified
// []MessageSequenceItem — no plan migration needed.
func TestTodoTaskMessagesDeserializeToUnifiedItem(t *testing.T) {
	planJSON := `{"steps":[{
		"type":"todo_task","id":"orch","title":"o","description":"d",
		"predefined_routes":[{"route_id":"w","route_name":"W","condition":"c",
			"sub_agent_step":{"type":"regular","id":"w","title":"w","description":"d"}}],
		"messages":[
			{"id":"m1","type":"message","message":"hello","max_corrections":2},
			{"id":"m2","type":"foreach","source_sql":"SELECT id FROM t","max_iterations":5,"message":"row {{.id}}"},
			{"id":"m3","type":"prevalidation","validation_schema":{"files":[]}}
		]
	}]}`
	var pr PlanningResponse
	if err := json.Unmarshal([]byte(planJSON), &pr); err != nil {
		t.Fatalf("unmarshal plan: %v", err)
	}
	var todo *TodoTaskPlanStep
	for _, s := range pr.Steps {
		if tt, ok := s.(*TodoTaskPlanStep); ok {
			todo = tt
		}
	}
	if todo == nil {
		t.Fatal("todo_task step not found after unmarshal")
	}
	// Compile-time + runtime proof the field is the unified type.
	var msgs []MessageSequenceItem = todo.Messages
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	if msgs[0].Type != "message" || msgs[0].Message != "hello" || msgs[0].MaxCorrections != 2 {
		t.Errorf("message[0] back-compat fields lost: %+v", msgs[0])
	}
	if msgs[1].Type != "foreach" || msgs[1].SourceSQL != "SELECT id FROM t" || msgs[1].MaxIterations != 5 {
		t.Errorf("foreach[1] back-compat fields lost: %+v", msgs[1])
	}
	if msgs[2].Type != "prevalidation" || msgs[2].ValidationSchema == nil {
		t.Errorf("prevalidation[2] back-compat fields lost: %+v", msgs[2])
	}
}

// Verifies a standalone message_sequence gets synthetic learnings/KB contribution
// turns appended when (and only when) the step is configured for those writes —
// so it honors step-level learning_objective / knowledgebase_contribution like a
// regular step instead of silently skipping the post-step learnings/KB phase.
func TestMessageSequenceClosingItems(t *testing.T) {
	hcpo := newMessageSequenceClosingTestOrchestrator(t)

	// Configured for BOTH learnings and KB writes (direct method) -> both items
	// appended, in order. The KB closing turn exists ONLY for write_method=direct:
	// under the default agent method the constraint layer strips KB from every
	// item's guard, so the turn would be a guaranteed-denied write.
	both := &MessageSequencePlanStep{
		Type:             StepTypeMessageSeq,
		CommonStepFields: CommonStepFields{ID: "extract-all", Description: "extract portfolio data"},
		AgentConfigs: &AgentConfigs{
			LearningsAccess:           LearningsAccessReadWrite,
			LearningObjective:         "Capture how to extract MyCAMS portfolio data reliably",
			KnowledgebaseAccess:       KBAccessReadWrite,
			KnowledgebaseContribution: "Record portal-specific selectors and quirks",
			KnowledgebaseWriteMethod:  KBWriteMethodDirect,
		},
	}
	items := hcpo.messageSequenceClosingItems(context.Background(), both, 0)
	if len(items) != 2 {
		t.Fatalf("expected 2 closing items (learning + kb), got %d", len(items))
	}
	if items[0].Type != "user_message" || items[0].Kind != "learning" || !items[0].WriteAccess.Learnings {
		t.Errorf("item[0] should be a learning user_message with learnings write access: %+v", items[0])
	}
	if items[0].Message == "" {
		t.Errorf("learning item should carry a contribution message")
	}
	if items[1].Type != "user_message" || items[1].Kind != "knowledgebase" || !items[1].WriteAccess.Knowledgebase {
		t.Errorf("item[1] should be a knowledgebase user_message with kb write access: %+v", items[1])
	}

	// No agent configs -> no synthetic items.
	if got := hcpo.messageSequenceClosingItems(context.Background(), &MessageSequencePlanStep{CommonStepFields: CommonStepFields{ID: "x"}}, 0); len(got) != 0 {
		t.Errorf("expected no closing items without agent configs, got %d", len(got))
	}

	// learnings_access=read-write but empty objective -> no learning item (double-gated).
	noObj := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "y", Description: "d"},
		AgentConfigs:     &AgentConfigs{LearningsAccess: LearningsAccessReadWrite},
	}
	if got := hcpo.messageSequenceClosingItems(context.Background(), noObj, 0); len(got) != 0 {
		t.Errorf("expected no items when learning objective empty, got %d", len(got))
	}

	// KB contribution set but access not write-capable -> no KB item.
	kbReadOnly := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "z", Description: "d"},
		AgentConfigs:     &AgentConfigs{KnowledgebaseAccess: "read", KnowledgebaseContribution: "note something"},
	}
	if got := hcpo.messageSequenceClosingItems(context.Background(), kbReadOnly, 0); len(got) != 0 {
		t.Errorf("expected no KB item when access is read-only, got %d", len(got))
	}

	// KB write-capable but write_method=agent (the default when unset) -> NO KB
	// closing item: notes/ is not writable by the step agent under agent mode, so
	// the turn would be a guaranteed-denied write. The post-step KB update agent
	// (maybeEnqueueKBUpdate, called from the message-sequence dispatch path) owns
	// the contribution instead.
	kbAgentMethod := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "w", Description: "d"},
		AgentConfigs: &AgentConfigs{
			KnowledgebaseAccess:       KBAccessReadWrite,
			KnowledgebaseContribution: "note something",
		},
	}
	if got := hcpo.messageSequenceClosingItems(context.Background(), kbAgentMethod, 0); len(got) != 0 {
		t.Errorf("expected no KB closing item under write_method=agent (default), got %d", len(got))
	}
}

func TestAppendMessageSequenceFinalValidation(t *testing.T) {
	topLevel := &ValidationSchema{Files: []FileValidationRule{{FileName: "result.json", MustExist: true}}}
	other := &ValidationSchema{Files: []FileValidationRule{{FileName: "intermediate.json", MustExist: true}}}

	tests := []struct {
		name        string
		items       []MessageSequenceItem
		schema      *ValidationSchema
		wantLen     int
		wantAdded   bool
		wantFinalID string
	}{
		{
			name:      "no top-level schema",
			items:     []MessageSequenceItem{{ID: "work", Type: "user_message"}},
			wantLen:   1,
			wantAdded: false,
		},
		{
			name:      "adds final gate",
			items:     []MessageSequenceItem{{ID: "work", Type: "user_message"}},
			schema:    topLevel,
			wantLen:   2,
			wantAdded: true,
		},
		{
			name: "final gate falls back to top-level schema",
			items: []MessageSequenceItem{
				{ID: "work", Type: "user_message"},
				{ID: "verify", Type: "prevalidation"},
			},
			schema:      topLevel,
			wantLen:     2,
			wantAdded:   false,
			wantFinalID: "verify",
		},
		{
			name: "final gate has equal item schema",
			items: []MessageSequenceItem{
				{ID: "work", Type: "user_message"},
				{ID: "verify", Type: "prevalidation", ValidationSchema: topLevel},
			},
			schema:      topLevel,
			wantLen:     2,
			wantAdded:   false,
			wantFinalID: "verify",
		},
		{
			name: "different intermediate gate does not replace final gate",
			items: []MessageSequenceItem{
				{ID: "work", Type: "user_message"},
				{ID: "verify-intermediate", Type: "prevalidation", ValidationSchema: other},
			},
			schema:    topLevel,
			wantLen:   3,
			wantAdded: true,
		},
		{
			name: "synthetic id avoids configured collision",
			items: []MessageSequenceItem{
				{ID: "__automatic_final_validation__", Type: "user_message"},
			},
			schema:      topLevel,
			wantLen:     2,
			wantAdded:   true,
			wantFinalID: "__automatic_final_validation_2__",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalLen := len(tt.items)
			got := appendMessageSequenceFinalValidation(tt.items, tt.schema)
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d, want %d: %+v", len(got), tt.wantLen, got)
			}
			if len(tt.items) != originalLen {
				t.Fatalf("input items mutated: len = %d, want %d", len(tt.items), originalLen)
			}
			if len(got) == 0 {
				return
			}
			final := got[len(got)-1]
			if tt.wantAdded {
				if final.Type != "prevalidation" || !equalValidationSchemas(final.ValidationSchema, tt.schema) {
					t.Fatalf("final item is not the automatic top-level gate: %+v", final)
				}
			}
			if tt.wantFinalID != "" && final.ID != tt.wantFinalID {
				t.Fatalf("final id = %q, want %q", final.ID, tt.wantFinalID)
			}
		})
	}
}

func TestMessageSequenceFinalValidationPrecedesClosingItems(t *testing.T) {
	hcpo := newMessageSequenceClosingTestOrchestrator(t)
	step := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{
			ID:               "work-and-learn",
			Description:      "do the work",
			ValidationSchema: &ValidationSchema{Files: []FileValidationRule{{FileName: "result.json", MustExist: true}}},
		},
		Items: []MessageSequenceItem{{ID: "work", Type: "user_message", Message: "Produce result.json"}},
		AgentConfigs: &AgentConfigs{
			LearningsAccess:   LearningsAccessReadWrite,
			LearningObjective: "Capture the durable pattern",
		},
	}

	planned := appendMessageSequenceFinalValidation(step.Items, step.ValidationSchema)
	planned = append(planned, hcpo.messageSequenceClosingItems(context.Background(), step, 0)...)
	if len(planned) != 3 {
		t.Fatalf("planned len = %d, want 3: %+v", len(planned), planned)
	}
	if planned[1].Type != "prevalidation" || planned[2].Kind != "learning" {
		t.Fatalf("expected work -> final validation -> learning, got %+v", planned)
	}
}

func TestFormatMessageSequenceTurnLogResult(t *testing.T) {
	item := MessageSequenceItem{ID: "review", Type: "user_message"}
	if got := formatMessageSequenceTurnLogResult(item, "STATUS: COMPLETED\nReport updated", nil); got != "Message sequence item: review (user_message)\nSTATUS: COMPLETED\nReport updated" {
		t.Fatalf("success log result = %q", got)
	}

	got := formatMessageSequenceTurnLogResult(item, "partial output", fmt.Errorf("provider disconnected"))
	for _, want := range []string{"Message sequence item: review (user_message)", "STATUS: FAILED", "provider disconnected", "partial output"} {
		if !strings.Contains(got, want) {
			t.Fatalf("failure log result %q missing %q", got, want)
		}
	}
}

func TestMessageSequenceClosingItemsHonorLockLearnings(t *testing.T) {
	hcpo := newMessageSequenceClosingTestOrchestrator(t)
	previousCheck := directLearningsGlobalEmptyForLock
	directLearningsGlobalEmptyForLock = func(_ *StepBasedWorkflowOrchestrator, _ context.Context) (bool, error) {
		return false, nil
	}
	defer func() { directLearningsGlobalEmptyForLock = previousCheck }()

	locked := true
	step := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "seq", Description: "do work"},
		AgentConfigs: &AgentConfigs{
			LearningsAccess:   LearningsAccessReadWrite,
			LearningObjective: "capture durable execution patterns",
			LockLearnings:     &locked,
		},
	}

	if got := hcpo.messageSequenceClosingItems(context.Background(), step, 0); len(got) != 0 {
		t.Fatalf("locked existing _global should suppress synthetic learning item, got %+v", got)
	}
}

func newMessageSequenceClosingTestOrchestrator(t *testing.T) *StepBasedWorkflowOrchestrator {
	t.Helper()
	base, err := orchestrator.NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, orchestrator.OrchestratorTypeWorkflow, "", 0, "",
		[]string{"test-server"}, nil, false, &orchestrator.LLMConfig{}, 1, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("NewBaseOrchestrator: %v", err)
	}
	base.SetWorkspacePath("Workflow/test-flow")
	return &StepBasedWorkflowOrchestrator{BaseOrchestrator: base}
}
