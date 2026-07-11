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

	var artifactBuilder strings.Builder
	if err := reviewArtifactSyncAgentSystemTemplate.Execute(&artifactBuilder, map[string]string{
		"WorkspacePath":           "Workflow/test",
		"AbsWorkspacePath":        "/workspace/Workflow/test",
		"WorkflowObjective":       "Test objective",
		"WorkflowSuccessCriteria": "Test success",
		"StepID":                  "",
		"Focus":                   "",
		"PlanJSON":                "",
		"StepConfigSummary":       "",
	}); err != nil {
		t.Fatalf("render artifact reviewer prompt: %v", err)
	}

	cases := map[string]string{
		"kb_reorganize":  kbReorganize,
		"kb_consolidate": kbConsolidate,
		"artifact_sync":  artifactBuilder.String(),
	}
	for name, prompt := range cases {
		for _, want := range []string{"READ-ONLY REVIEW OVERRIDE", "Pulse Fixer", "Do not"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s reviewer prompt missing %q:\n%s", name, want, prompt)
			}
		}
	}

	for _, forbidden := range []string{
		"Use `diff_patch_workspace_file` to update",
		"After writing the report, call `mark_changelog_artifact_reviewed`",
	} {
		if strings.Contains(artifactBuilder.String(), forbidden) {
			t.Fatalf("artifact reviewer prompt still grants mutation through %q", forbidden)
		}
	}
}
