package server

import (
	"reflect"
	"testing"
)

func TestExtractWorkflowContextFolders(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "normalizes and deduplicates workflow paths",
			input: []string{"Workflow/Alpha", "Workflow/Alpha/../Alpha", " Workflow/Beta "},
			want:  []string{"Workflow/Alpha", "Workflow/Beta"},
		},
		{
			name:  "drops protected and invalid paths",
			input: []string{"", ".", "/", "/abs/path", "../Workflow/Bad", "_users/private", "Chats/test", "Workflow/Good"},
			want:  []string{"Workflow/Good"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractWorkflowContextFolders(tc.input)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractWorkflowContextFolders(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestCollectAdditionalFolderGuardFolders(t *testing.T) {
	query := "Please inspect this.\n📁 Files in context: Workflow/Main/plan.json, skills/custom/SKILL.md, Chats/ignore.md\n"
	workflowPaths := []string{"Workflow/Referenced", "Workflow/Main"}

	got := collectAdditionalFolderGuardFolders(query, workflowPaths)
	want := []string{"Workflow/Main/plan.json", "skills/custom/SKILL.md", "Workflow/Referenced", "Workflow/Main"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAdditionalFolderGuardFolders() = %v, want %v", got, want)
	}
}
