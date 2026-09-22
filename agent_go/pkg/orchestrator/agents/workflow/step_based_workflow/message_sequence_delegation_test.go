package step_based_workflow

import (
	"encoding/json"
	"testing"
)

func TestMessageSequenceParsesBoundedAgentRoutes(t *testing.T) {
	var plan PlanningResponse
	err := json.Unmarshal([]byte(`{
		"steps": [{
			"type": "message_sequence",
			"id": "investigate",
			"title": "Investigate",
			"description": "Investigate the evidence and decide which specialists are needed.",
			"context_dependencies": [],
			"items": [{"id":"synthesize","type":"user_message","message":"Synthesize and verify the result."}],
			"predefined_routes": [{
				"route_id": "researcher",
				"route_name": "Researcher",
				"condition": "When more evidence is required",
				"sub_agent_step": {
					"type": "message_sequence",
					"id": "researcher",
					"title": "Researcher",
					"description": "Collect the requested evidence.",
					"context_dependencies": [],
					"items": [{"id":"check","type":"user_message","message":"Verify the evidence."}]
				}
			}]
		}]
	}`), &plan)
	if err != nil {
		t.Fatalf("unmarshal delegating message_sequence: %v", err)
	}
	if len(plan.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(plan.Steps))
	}
	step, ok := plan.Steps[0].(*MessageSequencePlanStep)
	if !ok {
		t.Fatalf("step type = %T, want *MessageSequencePlanStep", plan.Steps[0])
	}
	if len(step.PredefinedRoutes) != 1 {
		t.Fatalf("routes = %d, want 1", len(step.PredefinedRoutes))
	}
	if _, ok := step.PredefinedRoutes[0].SubAgentStep.(*MessageSequencePlanStep); !ok {
		t.Fatalf("route step type = %T, want *MessageSequencePlanStep", step.PredefinedRoutes[0].SubAgentStep)
	}
	if err := validateLoadedPlanStructure(&plan); err != nil {
		t.Fatalf("validate delegating message_sequence: %v", err)
	}
}

func TestDelegatingMessageSequenceUsesAgentRuntimeAdapter(t *testing.T) {
	sequence := &MessageSequencePlanStep{
		Type: StepTypeMessageSeq,
		CommonStepFields: CommonStepFields{
			ID:          "agent",
			Title:       "Agent",
			Description: "Choose specialists and complete the outcome.",
		},
		Items: []MessageSequenceItem{{ID: "verify", Type: "user_message", Message: "Verify the result."}},
		PredefinedRoutes: []PlanOrchestrationRoute{{
			RouteID:   "specialist",
			RouteName: "Specialist",
			Condition: "When specialist work is needed",
			SubAgentStep: &MessageSequencePlanStep{
				Type:             StepTypeMessageSeq,
				CommonStepFields: CommonStepFields{ID: "specialist", Title: "Specialist", Description: "Do specialist work."},
				Items:            []MessageSequenceItem{{ID: "done", Type: "user_message", Message: "Finish."}},
			},
		}},
		NextStepID: "end",
	}

	adapted := delegatingMessageSequenceAsOrchestrator(sequence)
	if adapted == nil || adapted.ID != sequence.ID {
		t.Fatalf("adapter did not preserve sequence identity: %#v", adapted)
	}
	if len(adapted.PredefinedRoutes) != 1 || adapted.PredefinedRoutes[0].RouteID != "specialist" {
		t.Fatalf("adapter routes = %#v", adapted.PredefinedRoutes)
	}
	if len(adapted.Messages) != 1 || adapted.Messages[0].ID != "verify" {
		t.Fatalf("adapter messages = %#v", adapted.Messages)
	}
	if !messageSequenceDelegationItemAllowed(MessageSequenceItem{Type: "scripted"}) {
		t.Fatal("delegating agent sequence should retain safe declared scripted batches")
	}
}
