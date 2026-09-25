package server

import (
	"strings"
	"testing"
)

func TestDecisionApplyChatMessageReusesTheSchedulerInstructions(t *testing.T) {
	base := ReportHumanInput{ID: "goal-work-shopify-filter", WorkspacePath: "Workflow/salesoutreach", Status: "answered", SelectedOptionID: "approve", Source: "strategic_review"}

	direct := base
	direct.ApplyContract.Mode = "direct_apply"
	if msg := decisionApplyChatMessage(direct); !strings.Contains(msg, "Apply it now, here in this chat") || !strings.Contains(msg, "DECISION DRAIN") || !strings.Contains(msg, "goal-work-shopify-filter") {
		t.Fatalf("direct_apply should carry the drain instructions: %q", msg)
	}

	fixer := base
	fixer.ApplyContract.Mode = "targeted_fixer"
	fixer.ApplyContract.ApprovedScope = "add the Apollo technology filter to the Shopify search"
	if msg := decisionApplyChatMessage(fixer); !strings.Contains(msg, "TARGETED FIXER") {
		t.Fatalf("an approved targeted_fixer decision should carry the Fixer instructions: %q", msg)
	}

	legacy := base
	if msg := decisionApplyChatMessage(legacy); !strings.Contains(msg, "get_human_input_request") || !strings.Contains(msg, "mark_human_input_consumed") || strings.Contains(msg, "DECISION DRAIN") {
		t.Fatalf("a decision without an apply type gets the in-person instructions: %q", msg)
	}

	for name, input := range map[string]ReportHumanInput{
		"pending":       {ID: "d1", WorkspacePath: "Workflow/x", Status: "pending"},
		"no answer":     {ID: "d2", WorkspacePath: "Workflow/x", Status: "answered"},
		"external wait": {ID: "d3", WorkspacePath: "Workflow/x", Status: "answered", SelectedOptionID: "approve", ApplyContract: ReportHumanInputApplyContract{Mode: "external_wait"}},
	} {
		if msg := decisionApplyChatMessage(input); msg != "" {
			t.Errorf("%s: expected no chat message, got %q", name, msg)
		}
	}
}
