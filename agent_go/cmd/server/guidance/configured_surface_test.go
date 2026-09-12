package guidance

import (
	"reflect"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentworksproduct"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestConfiguredReferenceSurface(t *testing.T) {
	for _, mode := range []string{"builder", "run"} {
		legacy := "workshop"
		if mode == "run" {
			legacy = "run"
		}
		for _, mcp := range []bool{true, false} {
			var names []string
			err := AttachConfiguredReferenceSurface(legacy, mcp, agentworksproduct.ChatSkills(mode), func(s *llmtypes.Skill) error {
				names = append(names, s.Name)
				if s.Name == "builder-reference" && !mcp && strings.Contains(s.Content, "references/integration-discovery.md") {
					t.Fatal("restricted session advertises installer guidance")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(names, agentworksproduct.ChatSkills(mode)) {
				t.Fatalf("%s: %v", mode, names)
			}
		}
	}
	var names []string
	if err := AttachConfiguredReferenceSurface("run", false, []string{"builder-reference"}, func(s *llmtypes.Skill) error { names = append(names, s.Name); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"builder-reference"}) {
		t.Fatal("ignored configured subset", names)
	}
	calls := 0
	if err := AttachConfiguredReferenceSurface("run", false, []string{"system-tools", "typo"}, func(*llmtypes.Skill) error { calls++; return nil }); err == nil || calls != 0 {
		t.Fatal("invalid configuration partially attached")
	}
}
