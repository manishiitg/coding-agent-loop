package step_based_workflow

import (
	"context"
	"testing"
)

func TestAgentBrowserIsRuntimeSkillNotFilesystemSkill(t *testing.T) {
	if got := BuildWorkflowSkillPrompt(context.Background(), []string{"agent-browser"}, nil, "/workspace-docs"); got != "" {
		t.Fatalf("expected agent-browser to be omitted from filesystem skill prompt, got %q", got)
	}

	readPaths, writePaths := BuildSkillFolderGuardPaths([]string{"agent-browser"})
	if len(readPaths) != 0 || len(writePaths) != 0 {
		t.Fatalf("expected no folder guard paths for agent-browser, got read=%v write=%v", readPaths, writePaths)
	}
}

func TestAgentBrowserDoesNotFilterOtherSkills(t *testing.T) {
	readPaths, _ := BuildSkillFolderGuardPaths([]string{"agent-browser", "custom-skill"})
	want := []string{"skills/custom-skill/", "skills/custom-skill"}
	if len(readPaths) != len(want) {
		t.Fatalf("expected %v, got %v", want, readPaths)
	}
	for i := range want {
		if readPaths[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, readPaths)
		}
	}
}

func TestSystemSkillsDoNotInstallAgentBrowserFilesystemSkill(t *testing.T) {
	for _, skill := range GetSystemSkills() {
		if skill.Name == "agent-browser" || skill.Source == "vercel-labs/agent-browser@agent-browser" {
			t.Fatalf("agent-browser docs are built into the CLI; do not install filesystem skill: %+v", skill)
		}
	}
}
