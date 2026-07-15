package step_based_workflow

import (
	"testing"
)

func TestBrowserAutomationSkillsAreFilesystemAttachable(t *testing.T) {
	// Browser automation skill names also trigger runtime prompt/tool setup, but
	// they are still real workspace skills. Do not filter them out of SKILL.md
	// loading or folder guards.
	browserSkills := []string{"agent-browser"}
	if got := filesystemSkills(browserSkills); len(got) != len(browserSkills) {
		t.Fatalf("expected browser skills to remain filesystem skills, got %v", got)
	}

	readPaths, writePaths := BuildSkillFolderGuardPaths(browserSkills)
	want := []string{
		"skills/agent-browser/", "skills/agent-browser",
	}
	if len(writePaths) != 0 {
		t.Fatalf("expected no write folder guard paths for skills, got %v", writePaths)
	}
	if len(readPaths) != len(want) {
		t.Fatalf("expected read paths %v, got %v", want, readPaths)
	}
	for i := range want {
		if readPaths[i] != want[i] {
			t.Fatalf("expected read paths %v, got %v", want, readPaths)
		}
	}
}

func TestBrowserRuntimeSkillsDoNotFilterOtherSkills(t *testing.T) {
	readPaths, _ := BuildSkillFolderGuardPaths([]string{"agent-browser", "custom-skill"})
	want := []string{
		"skills/agent-browser/", "skills/agent-browser",
		"skills/custom-skill/", "skills/custom-skill",
	}
	if len(readPaths) != len(want) {
		t.Fatalf("expected %v, got %v", want, readPaths)
	}
	for i := range want {
		if readPaths[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, readPaths)
		}
	}
}

func TestSystemSkillsDoNotInstallStaleAgentBrowserSource(t *testing.T) {
	for _, skill := range GetSystemSkills() {
		if skill.Source == "vercel-labs/agent-browser@agent-browser" {
			t.Fatalf("do not install stale external agent-browser skill source: %+v", skill)
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
