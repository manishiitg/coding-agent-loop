package server

import (
	"encoding/json"
	"strings"
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

func TestLegacyLLMFallbacksAreNotRetained(t *testing.T) {
	selected := `{"provider":"codex-cli","model_id":"selected","options":{"reasoning_effort":"high"},"fallbacks":[{"provider":"claude-code","model_id":"backup"}]}`
	for _, tc := range []struct {
		name, raw string
		target    interface{}
	}{
		{"chat", `{"primary":` + selected + `,"fallbacks":[{"provider":"pi-cli","model_id":"backup"}]}`, &orchestrator.LLMConfig{}},
		{"workflow", selected, &workflowtypes.AgentLLMConfig{}},
		{"delegation", selected, &virtualtools.TierModel{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.raw), tc.target); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "fallback") || strings.Contains(string(raw), "backup") {
				t.Fatalf("retained retired config: %s", raw)
			}
			for _, expected := range []string{"codex-cli", "selected", "reasoning_effort", "high"} {
				if !strings.Contains(string(raw), expected) {
					t.Fatalf("lost selected model setting %s: %s", expected, raw)
				}
			}
		})
	}
}

func TestInvalidDefaultProviderIsNotReplaced(t *testing.T) {
	t.Setenv("AGENT_PROVIDER", "unavailable-provider")
	t.Setenv("AGENT_MODEL", "selected")
	provider, model := getPrimaryProviderAndModelFromDefaults()
	if provider != "unavailable-provider" || model != "selected" {
		t.Fatalf("invalid explicit selection silently changed to %s/%s", provider, model)
	}
}
