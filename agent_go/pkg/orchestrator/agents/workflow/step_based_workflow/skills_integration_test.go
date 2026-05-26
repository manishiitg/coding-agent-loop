package step_based_workflow

import (
	"testing"
)

func TestAgentBrowserIsRuntimeSkillNotFilesystemSkill(t *testing.T) {
	// agent-browser is built into the CLI runtime, not a markdown skill.
	// It must be filtered out of every skill resolution path so we
	// don't try to read a non-existent SKILL.md or grant filesystem
	// guard paths for it.
	if got := filesystemSkills([]string{"agent-browser"}); len(got) != 0 {
		t.Fatalf("expected agent-browser to be filtered from filesystem skills, got %v", got)
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

// TestPhase5StepSkillsDoNotInheritFromOrchestrator locks in the hard-cut
// behavior from Phase 5: step-level skill resolution never falls back to
// orchestrator.SelectedSkills. Workflows that used to rely on the
// cascade need to either (a) declare skills per step in EnabledSkills,
// or (b) put shared know-how in learnings/_global/SKILL.md which is
// auto-attached separately.
//
// If this test ever fails by returning the orchestrator's skills, it
// means someone reintroduced the inheritance fallback — that breaks the
// bucket model the user signed off on.
func TestPhase5StepSkillsDoNotInheritFromOrchestrator(t *testing.T) {
	// Case 1: step config is nil → no skills (no orchestrator fallback).
	if got := GetEffectiveSkills(nil, nil); got != nil {
		t.Errorf("expected nil skills when stepConfig is nil; got %v", got)
	}

	// Case 2: step config has empty EnabledSkills → still no skills.
	emptyStep := &AgentConfigs{EnabledSkills: nil}
	if got := GetEffectiveSkills(emptyStep, nil); got != nil {
		t.Errorf("expected nil skills when EnabledSkills is empty; got %v", got)
	}

	emptyStepSlice := &AgentConfigs{EnabledSkills: []string{}}
	if got := GetEffectiveSkills(emptyStepSlice, nil); got != nil {
		t.Errorf("expected nil skills when EnabledSkills is empty slice; got %v", got)
	}

	// Case 3: step config has explicit EnabledSkills → returned as-is.
	step := &AgentConfigs{EnabledSkills: []string{"pdf-extract", "agent-browser"}}
	got := GetEffectiveSkills(step, nil)
	if len(got) != 2 || got[0] != "pdf-extract" || got[1] != "agent-browser" {
		t.Errorf("expected step's EnabledSkills returned verbatim; got %v", got)
	}
}
