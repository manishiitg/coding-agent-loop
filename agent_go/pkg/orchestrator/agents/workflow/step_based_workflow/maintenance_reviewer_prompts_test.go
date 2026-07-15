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
