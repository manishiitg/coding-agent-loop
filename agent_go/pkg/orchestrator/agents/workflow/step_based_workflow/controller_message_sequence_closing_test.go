package step_based_workflow

import (
	"encoding/json"
	"testing"
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
	hcpo := &StepBasedWorkflowOrchestrator{}

	// Configured for BOTH learnings and KB writes -> both items appended, in order.
	both := &MessageSequencePlanStep{
		Type:             StepTypeMessageSeq,
		CommonStepFields: CommonStepFields{ID: "extract-all", Description: "extract portfolio data"},
		AgentConfigs: &AgentConfigs{
			LearningsAccess:           LearningsAccessReadWrite,
			LearningObjective:         "Capture how to extract MyCAMS portfolio data reliably",
			KnowledgebaseAccess:       KBAccessReadWrite,
			KnowledgebaseContribution: "Record portal-specific selectors and quirks",
		},
	}
	items := hcpo.messageSequenceClosingItems(both)
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
	if got := hcpo.messageSequenceClosingItems(&MessageSequencePlanStep{CommonStepFields: CommonStepFields{ID: "x"}}); len(got) != 0 {
		t.Errorf("expected no closing items without agent configs, got %d", len(got))
	}

	// learnings_access=read-write but empty objective -> no learning item (double-gated).
	noObj := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "y", Description: "d"},
		AgentConfigs:     &AgentConfigs{LearningsAccess: LearningsAccessReadWrite},
	}
	if got := hcpo.messageSequenceClosingItems(noObj); len(got) != 0 {
		t.Errorf("expected no items when learning objective empty, got %d", len(got))
	}

	// KB contribution set but access not write-capable -> no KB item.
	kbReadOnly := &MessageSequencePlanStep{
		CommonStepFields: CommonStepFields{ID: "z", Description: "d"},
		AgentConfigs:     &AgentConfigs{KnowledgebaseAccess: "read", KnowledgebaseContribution: "note something"},
	}
	if got := hcpo.messageSequenceClosingItems(kbReadOnly); len(got) != 0 {
		t.Errorf("expected no KB item when access is read-only, got %d", len(got))
	}
}
