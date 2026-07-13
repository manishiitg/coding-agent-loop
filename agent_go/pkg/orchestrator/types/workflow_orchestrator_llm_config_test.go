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
		Fallbacks: []workflowtypes.AgentLLMFallback{
			{
				PublishedLLMID: "claude-high",
				Provider:       "claude-code",
				ModelID:        "opus",
				Options: map[string]interface{}{
					"reasoning_effort": "high",
				},
			},
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
	if len(converted.Fallbacks) != 1 {
		t.Fatalf("expected one fallback, got %d", len(converted.Fallbacks))
	}
	if converted.Fallbacks[0].PublishedLLMID != "claude-high" {
		t.Fatalf("expected fallback published id to be preserved, got %q", converted.Fallbacks[0].PublishedLLMID)
	}
	if got := converted.Fallbacks[0].Options["reasoning_effort"]; got != "high" {
		t.Fatalf("expected fallback reasoning_effort=high, got %v", got)
	}
}
