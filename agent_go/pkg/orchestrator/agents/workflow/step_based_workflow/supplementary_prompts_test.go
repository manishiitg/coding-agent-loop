package step_based_workflow

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"
)

func TestProjectedWorkflowReferenceSkillIsCLIOnlyAndContainsExecutionDocs(t *testing.T) {
	cliConfig := &agents.OrchestratorAgentConfig{}
	cliConfig.LLMConfig.Primary.Provider = "codex-cli"
	skill := projectedWorkflowReferenceSkill(cliConfig)
	if skill == nil {
		t.Fatal("coding CLI step must receive the workflow reference skill")
	}
	if skill.Name != "workflow-reference" {
		t.Fatalf("projected skill name = %q, want workflow-reference", skill.Name)
	}

	wantFiles := map[string]bool{
		"references/code-authoring.md": false,
		"references/browser-usage.md":  false,
		"references/mcp-bridge.md":     false,
		"references/stores.md":         false,
	}
	for _, file := range skill.SupportingFiles {
		if _, ok := wantFiles[file.RelPath]; ok {
			wantFiles[file.RelPath] = true
		}
	}
	for file, found := range wantFiles {
		if !found {
			t.Errorf("projected workflow reference is missing %s", file)
		}
	}

	apiConfig := &agents.OrchestratorAgentConfig{}
	apiConfig.LLMConfig.Primary.Provider = "anthropic"
	if got := projectedWorkflowReferenceSkill(apiConfig); got != nil {
		t.Fatalf("API provider should keep inline fallback, got projected skill %q", got.Name)
	}
}
