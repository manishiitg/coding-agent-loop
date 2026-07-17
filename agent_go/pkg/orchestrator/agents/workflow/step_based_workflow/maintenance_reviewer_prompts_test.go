package step_based_workflow

import (
	"strings"
	"testing"
)

func TestMaintenanceSpecialistSystemPromptsAreReadOnly(t *testing.T) {
	kbReorganize := renderKBReorganizeSystemPrompt(map[string]string{
		"NotesFolderPath": "/workspace/knowledgebase/notes",
		"NotesIndexPath":  "/workspace/knowledgebase/notes/_index.json",
	})
	kbConsolidate := renderKBConsolidateSystemPrompt(map[string]string{
		"NotesFolderPath": "/workspace/knowledgebase/notes",
		"NotesIndexPath":  "/workspace/knowledgebase/notes/_index.json",
	})

	cases := map[string]string{
		"kb_reorganize":  kbReorganize,
		"kb_consolidate": kbConsolidate,
	}
	for name, prompt := range cases {
		for _, want := range []string{"READ-ONLY REVIEW OVERRIDE", "Pulse Fixer", "Do not"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s reviewer prompt missing %q:\n%s", name, want, prompt)
			}
		}
	}
}

func TestReviewPlanPromptPrefersCoherentAgenticSteps(t *testing.T) {
	prompt, err := ExecuteTemplate("reviewPlanAgentSystem", map[string]string{
		"AbsWorkspacePath":        "/app/workspace-docs/Workflow/example",
		"PlanJSON":                `{}`,
		"StepConfigSummary":       "",
		"TargetRunFolder":         "",
		"WorkflowObjective":       "Complete the workflow outcome.",
		"WorkflowSelectedSkills":  "",
		"WorkflowSuccessCriteria": "The outcome is verified.",
		"WorkspacePath":           "Workflow/example",
		"Focus":                   "",
	})
	if err != nil {
		t.Fatalf("render review plan prompt: %v", err)
	}

	for _, want := range []string{
		"one large `message_sequence` per coherent shared-context span",
		"fewest durable steps",
		"substantial end-to-end outcome",
		"message_sequence",
		"Give the first work turn the whole outcome",
		"tiny sequence item per routine action",
		"Separate deterministic acquisition from agentic processing",
		"scripted regular fetcher steps",
		"one-scripted-step-per-endpoint fragmentation",
		"run-specific proof/provenance",
		"evidence-based double-check and repair turns",
		"Multiple large sequences are correct when their contexts should not be shared",
		"builder should decide this intelligently from workflow semantics",
		"10+ representative-run threshold applies only",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("review plan prompt missing coherent-agentic-step guidance %q", want)
		}
	}
}
