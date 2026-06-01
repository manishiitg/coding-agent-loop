package guidance

import (
	"strings"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func materializedFileContent(t *testing.T, skill *llmtypes.Skill, relPath string) string {
	t.Helper()
	if skill == nil {
		t.Fatal("skill is nil")
	}
	for _, file := range skill.SupportingFiles {
		if file.RelPath == relPath {
			return string(file.Content)
		}
	}
	t.Fatalf("missing supporting file %q; files=%v", relPath, skill.SupportingFiles)
	return ""
}

func TestMaterializedReferenceSkillIncludesConfigToolOnlyDocs(t *testing.T) {
	skill := MaterializeReferenceSkill("workshop")
	if skill == nil {
		t.Fatal("expected workflow-reference skill")
	}

	for _, want := range []string{"LLM/provider configuration via tools", "not by reading or editing `config/` files", "references/llm-selection.md", "references/workspace-media-tools.md"} {
		if !strings.Contains(skill.Description+skill.Content, want) {
			t.Fatalf("workflow-reference skill should contain %q\nDescription:\n%s\nContent:\n%s", want, skill.Description, skill.Content)
		}
	}

	llmSelection := materializedFileContent(t, skill, "references/llm-selection.md")
	for _, want := range []string{"list_published_llms", "set_provider_auth", "never paste API keys into shell or config files"} {
		if !strings.Contains(llmSelection, want) {
			t.Fatalf("llm-selection reference should contain %q\n%s", want, llmSelection)
		}
	}
	for _, banned := range []string{"config/published-llms.json", "config/provider-api-keys.json"} {
		if strings.Contains(llmSelection, banned) {
			t.Fatalf("llm-selection reference should not expose raw config file %q\n%s", banned, llmSelection)
		}
	}

	mediaTools := materializedFileContent(t, skill, "references/workspace-media-tools.md")
	for _, want := range []string{"set_provider_auth", "workspace-backed image generation defaults", "**Search provider routing** comes from the published LLM set"} {
		if !strings.Contains(mediaTools, want) {
			t.Fatalf("workspace-media-tools reference should contain %q\n%s", want, mediaTools)
		}
	}
	for _, banned := range []string{"config/published-llms.json", "config/provider-api-keys.json", "config/image-generation-config.json", "config/image-analysis-config.json"} {
		if strings.Contains(mediaTools, banned) {
			t.Fatalf("workspace-media-tools reference should not expose raw config file %q\n%s", banned, mediaTools)
		}
	}
}

func TestMaterializedReferenceSkillUsesMultiAgentSurface(t *testing.T) {
	skill := MaterializeReferenceSkill("multi-agent")
	if skill == nil {
		t.Fatal("expected multiagent-reference skill")
	}
	if skill.Name != "multiagent-reference" {
		t.Fatalf("skill name = %q, want multiagent-reference", skill.Name)
	}

	for _, want := range []string{"Multi-agent chat reference docs", "references/llm-provider-config.md", "references/delegation.md"} {
		if !strings.Contains(skill.Description+skill.Content, want) {
			t.Fatalf("multiagent-reference skill should contain %q\nDescription:\n%s\nContent:\n%s", want, skill.Description, skill.Content)
		}
	}
	if strings.Contains(skill.Content, "references/llm-selection.md") {
		t.Fatalf("multiagent-reference should not expose workflow-only llm-selection\n%s", skill.Content)
	}

	llmProviderConfig := materializedFileContent(t, skill, "references/llm-provider-config.md")
	for _, want := range []string{"list_published_llms", "list_provider_models", "save_published_llm", "reasoning_effort", "Never inspect or edit `config/` files"} {
		if !strings.Contains(llmProviderConfig, want) {
			t.Fatalf("llm-provider-config reference should contain %q\n%s", want, llmProviderConfig)
		}
	}
	for _, banned := range []string{"context_window", "input_cost_per_1m", "temperature", "tool-call settings"} {
		if strings.Contains(llmProviderConfig, `"`+banned+`"`) {
			t.Fatalf("llm-provider-config should not present %q as a stored JSON field\n%s", banned, llmProviderConfig)
		}
	}
}

func TestSystemToolsSkillTeachesConfigToolOnlyAccess(t *testing.T) {
	skill := BuildSystemToolsSkill("workshop")
	if skill == nil {
		t.Fatal("expected system-tools skill")
	}
	for _, want := range []string{"## Configuration access", "LLM/provider configuration is tool-managed", "Do not read or edit `config/` files", "get_reference_doc(kind=\"llm-provider-config\")", "get_reference_doc(kind=\"llm-selection\")", "get_reference_doc(kind=\"workspace-media-tools\")"} {
		if !strings.Contains(skill.Content, want) {
			t.Fatalf("system-tools skill should contain %q\n%s", want, skill.Content)
		}
	}
}

func TestSystemToolsSkillDoesNotPointMultiAgentAtWorkflowOnlyLLMSelection(t *testing.T) {
	skill := BuildSystemToolsSkill("multi-agent")
	if skill == nil {
		t.Fatal("expected system-tools skill")
	}
	for _, want := range []string{"get_reference_doc(kind=\"llm-provider-config\")", "list_published_llms", "save_published_llm"} {
		if !strings.Contains(skill.Content, want) {
			t.Fatalf("multi-agent system-tools skill should contain %q\n%s", want, skill.Content)
		}
	}
	if strings.Contains(skill.Content, "get_reference_doc(kind=\"llm-selection\")") {
		t.Fatalf("multi-agent system-tools should not mention workflow-only llm-selection\n%s", skill.Content)
	}
}
