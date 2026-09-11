package types

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestConvertDBAgentLLMConfigPreservesPublishedOptions(t *testing.T) {
	converted := convertDBAgentLLMConfig(&workflowtypes.AgentLLMConfig{
		PublishedLLMID: "claude-low",
		Provider:       "claude-code",
		ModelID:        "sonnet",
		Options: map[string]interface{}{
			"reasoning_effort": "low",
		},
	})

	if converted == nil {
		t.Fatal("expected converted config")
	}
	if converted.PublishedLLMID != "claude-low" {
		t.Fatalf("expected published id to be preserved, got %q", converted.PublishedLLMID)
	}
	if got := converted.Options["reasoning_effort"]; got != "low" {
		t.Fatalf("expected primary reasoning_effort=low, got %v", got)
	}

}
