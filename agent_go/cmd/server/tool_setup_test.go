package server

import (
	"context"
	"reflect"
	"strings"
	"testing"

	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
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
			input: []string{"", ".", "/", "/abs/path", "../Workflow/Bad", "Chats/test", "Workflow/Good"},
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

func TestWorkspaceAdvancedToolBundleIncludesProviderMediaTools(t *testing.T) {
	tools, executors, categories := createCustomTools(false, "default", "tool-bundle-test-session")

	toolDefs := map[string]bool{}
	for _, tool := range tools {
		if tool.Function != nil {
			toolDefs[tool.Function.Name] = true
		}
	}

	for _, name := range []string{
		"read_image",
		"read_video",
		"search_web_llm",
		"image_gen",
		"image_edit",
		"generate_video",
		"text_to_speech",
		"speech_to_text",
		"generate_music",
	} {
		if !toolDefs[name] {
			t.Fatalf("workspace tool definitions missing %q", name)
		}
		if _, ok := executors[name]; !ok {
			t.Fatalf("workspace tool executors missing %q", name)
		}
		if got := categories[name]; got != "workspace_advanced" {
			t.Fatalf("tool %q category = %q, want workspace_advanced", name, got)
		}
	}
}

// TestChatModeFolderGuardBlockedWrite verifies that wrapExecutorsWithChatModeFolderGuard
// denies writes to paths under blockedWriteFolders even when the path is under an allowed
// write prefix. Regression guard for the "option 2" design — this is the prefix+blocklist
// pattern that replaced the enumerated subfolder allow-list, so drift between subfolders
// and allow-list entries becomes structurally impossible.
func TestChatModeFolderGuardBlockedWrite(t *testing.T) {
	const workflowRoot = "Workflow/test-ops"
	const planningBlocked = workflowRoot + "/planning/"

	// Fake executor: succeeds trivially, returning "OK". We care about whether the
	// wrapper blocks the call before the executor runs, not what the executor does.
	noop := func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "OK", nil
	}

	cases := []struct {
		name      string
		tool      string
		filepath  string
		wantError string // substring match; empty = expect success
	}{
		{
			name:      "write under blocked prefix is denied",
			tool:      "diff_patch_workspace_file",
			filepath:  workflowRoot + "/planning/plan.json",
			wantError: "blocked-write prefix",
		},
		{
			name:      "write deeper under blocked prefix is denied",
			tool:      "diff_patch_workspace_file",
			filepath:  workflowRoot + "/planning/nested/deep.json",
			wantError: "blocked-write prefix",
		},
		{
			name:     "write to allowed sibling (reports/) succeeds",
			tool:     "diff_patch_workspace_file",
			filepath: workflowRoot + "/reports/report_plan.md",
		},
		{
			name:     "write to allowed sibling (db/) succeeds",
			tool:     "diff_patch_workspace_file",
			filepath: workflowRoot + "/db/cost_history.json",
		},
		{
			name:     "write to allowed sibling (soul/) succeeds",
			tool:     "diff_patch_workspace_file",
			filepath: workflowRoot + "/soul/soul.md",
		},
		{
			name:      "write to folder outside workflow root is denied",
			tool:      "diff_patch_workspace_file",
			filepath:  "Workflow/other-workflow/reports/x.md",
			wantError: "allowed write folders",
		},
		{
			name:     "read of blocked prefix is allowed (blockedWrite does not affect reads)",
			tool:     "read_workspace_file",
			filepath: workflowRoot + "/planning/plan.json",
		},
	}

	executors := map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"diff_patch_workspace_file": noop,
		"read_workspace_file":       noop,
	}

	// Grant writes to the whole workflow root; block writes to planning/ subtree.
	// Matches the pattern used by server.go for chat-agent #workflow sessions.
	wrapped := wrapExecutorsWithChatModeFolderGuard(
		executors,
		nil,
		[]string{planningBlocked},
		workflowRoot+"/",
	)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor, ok := wrapped[tc.tool]
			if !ok {
				t.Fatalf("wrapped executor missing tool %q", tc.tool)
			}
			_, err := executor(context.Background(), map[string]interface{}{"filepath": tc.filepath})
			switch {
			case tc.wantError == "" && err != nil:
				t.Fatalf("expected success for %q, got error: %v", tc.filepath, err)
			case tc.wantError != "" && err == nil:
				t.Fatalf("expected error containing %q for %q, got nil", tc.wantError, tc.filepath)
			case tc.wantError != "" && err != nil && !strings.Contains(err.Error(), tc.wantError):
				t.Fatalf("expected error containing %q, got: %v", tc.wantError, err)
			}
		})
	}
}

// TestWorkflowWritableSubfoldersConsistency is a drift guard: it fails if
// WorkflowWritableSubfolders is missing one of the canonical workflow subfolders
// or accidentally includes planning/. The list feeds folder-guard construction
// for workflow-scoped sessions (server.go:3318 + phase-chat at server.go:4016);
// a silent omission is exactly how reports/, db/, soul/ previously fell out of
// sync and denied legitimate writes.
func TestWorkflowWritableSubfoldersConsistency(t *testing.T) {
	required := map[string]string{
		todo_creation_human.KnowledgebaseFolderName: "knowledgebase facts",
		todo_creation_human.DBFolderName:            "per-run JSON state",
		todo_creation_human.SoulFolderName:          "objective + success criteria (post-migration canonical source)",
		todo_creation_human.ReportsFolderName:       "report_plan.md and widgets",
		todo_creation_human.ExecutionFolderName:     "per-step execution outputs",
		todo_creation_human.LearningsFolderName:     "learnings/_global and per-step learnings",
		todo_creation_human.ScriptsFolderName:       "skill support scripts",
		todo_creation_human.RunsFolderName:          "iteration snapshots",
	}

	have := make(map[string]bool, len(todo_creation_human.WorkflowWritableSubfolders))
	for _, entry := range todo_creation_human.WorkflowWritableSubfolders {
		if !strings.HasSuffix(entry, "/") {
			t.Errorf("WorkflowWritableSubfolders entry %q should end with '/' (consumers use prefix match with trailing slash)", entry)
		}
		have[strings.TrimSuffix(entry, "/")] = true
	}

	for name, purpose := range required {
		if !have[name] {
			t.Errorf("WorkflowWritableSubfolders is missing %q (%s) — adding a *FolderName constant without adding it here causes silent folder-guard drift", name, purpose)
		}
	}

	if have[todo_creation_human.PlanningFolderName] {
		t.Errorf("WorkflowWritableSubfolders must NOT include %q — planning files are managed by typed plan-mod tools, not raw writes", todo_creation_human.PlanningFolderName)
	}
}
