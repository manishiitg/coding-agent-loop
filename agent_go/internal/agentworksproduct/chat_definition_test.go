package agentworksproduct

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
)

func TestChatDefinitions(t *testing.T) {
	m := mustAgentWorksManifest()
	for _, mode := range []string{"builder", "run"} {
		if ChatPromptTemplate(mode) == "" || ChatDefinitionKey(mode) == "" {
			t.Fatal("empty chat definition")
		}
		skills := ChatSkills(mode)
		skills[0] = "changed"
		if ChatSkills(mode)[0] == "changed" {
			t.Fatal("caller changed configured skills")
		}
	}
	if ChatDefinitionKey("builder") == ChatDefinitionKey("run") {
		t.Fatal("mode definitions must differ")
	}
	for _, names := range [][]string{{"missing"}, {"system-tools", "system-tools"}, {}} {
		copy := m
		copy.Chat = map[string]agentprofiles.ChatModeDefinition{"builder": m.Chat["builder"], "run": m.Chat["run"]}
		def := copy.Chat["builder"]
		def.Skills = names
		copy.Chat["builder"] = def
		if validateChatDefinitions(productConfigFiles, copy) == nil {
			t.Fatalf("accepted skills %v", names)
		}
	}
}
