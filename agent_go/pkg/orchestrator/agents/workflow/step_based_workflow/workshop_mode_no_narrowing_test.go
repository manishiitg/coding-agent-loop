package step_based_workflow

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
)

// Workshop mode no longer decides which tools exist.
//
// It used to: GetToolsForWorkshopMode returned a per-mode name list that
// SetToolPolicy applied every turn. Because that list also filtered
// buildToolIndex and get_api_spec — the catalog a coding CLI caches once at
// launch — a tool it omitted was undiscoverable rather than refused. The agent
// never learned the tool existed, so it shelled out instead of calling it. That
// is how set_workflow_secret and list_llm_capabilities were each lost for a while,
// and it is unrecoverable in a way a rejected call is not: you can always take a
// tool away from an agent that knows about it, but you can never add one it was
// never told about.
//
// Mode is a focus rule now and lives in the Builder system prompt. These tests
// pin the property that replaced the allow-list: registration is the whole
// story, so anything registered is discoverable.
func registeredWorkshopSurface(t *testing.T) map[string]bool {
	t.Helper()
	agent := newWorkshopDefinitionDraft()
	workspacePath := t.TempDir()
	base := &orchestrator.BaseOrchestrator{}
	base.SetWorkspacePath(workspacePath)
	session := &WorkshopChatSession{
		controller:   &StepBasedWorkflowOrchestrator{BaseOrchestrator: base},
		StepRegistry: NewWorkshopStepRegistry(),
		config:       &WorkshopConfig{WorkspacePath: workspacePath},
	}

	RegisterWorkshopChatTools(agent, session, workshopToolTestLogger{})
	RegisterRunFullWorkflowTool(agent, session, workshopToolTestLogger{})

	surface := make(map[string]bool, len(agent.tools))
	for name := range agent.tools {
		surface[name] = true
	}
	return surface
}

// Where the Pulse lifecycle tools live is worth stating, because it is what
// made the old design dangerous. They are not registered here — they come from
// the workflow tool pool in cmd/server (createCustomTools with workflowMode
// true), and cmd/server's TestToolSetInvariants asserts their pool membership
// and category.
//
// So under the old design a tool registered by one subsystem was withheld by an
// allow-list maintained in another, in a third file. Nothing connected them,
// which is exactly how they drifted. There is now no mode-keyed filter between
// the pool and the agent, so pool membership is the whole answer.

// Registration takes no mode argument, so there is no seam left where a mode
// could subset the surface. This asserts the absence structurally: the same
// construction path is the only one there is, and it produces a stable set.
func TestWorkshopRegistrationIsNotParameterizedByMode(t *testing.T) {
	first := registeredWorkshopSurface(t)
	second := registeredWorkshopSurface(t)

	if len(first) == 0 {
		t.Fatal("workshop registration produced no tools; the harness is not exercising the real path")
	}
	if len(first) != len(second) {
		t.Fatalf("registration is not deterministic: %d vs %d tools", len(first), len(second))
	}
	for name := range first {
		if !second[name] {
			t.Errorf("%q registered once but not again; registration must not depend on ambient state", name)
		}
	}
}

// The tools a prompt tells an agent to call must exist. This is the invariant
// that the deleted allow-list kept breaking: guidance and the grant lived in
// different files, so they drifted twice.
func TestWorkshopSurfaceCoversWhatTheBuilderPromptsPromise(t *testing.T) {
	surface := registeredWorkshopSurface(t)

	for _, name := range []string{
		"run_full_workflow", // the Builder prompt's main execution verb
		"execute_step",
		"query_step",
	} {
		if !surface[name] {
			t.Errorf("the Builder prompt instructs agents to call %q but it is not registered", name)
		}
	}
}
