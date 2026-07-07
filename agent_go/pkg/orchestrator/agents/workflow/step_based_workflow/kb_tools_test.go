package step_based_workflow

import (
	"strings"
	"testing"
)

func TestBuildStepKBGuidanceWithTargetRequiresPatchToolForAllWrites(t *testing.T) {
	target := "/app/workspace-docs/Workflow/social-media/knowledgebase/notes"
	prompt := BuildStepKBGuidanceWithTarget(KBAccessReadWrite, KBWriteMethodDirect, "Capture durable audience facts.", target)

	required := []string{
		"Knowledgebase contribution",
		"**Target:** `" + target + "/`",
		"Use these exact paths; do not rely on your shell working directory",
		"diff_patch_workspace_file",
		"including new topic files",
		"Do not use shell redirection, heredocs, tee, Python",
	}
	for _, snippet := range required {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}

	forbidden := []string{
		"Write with shell heredoc",
		"cat > file <<EOF",
		"Use heredoc",
	}
	for _, snippet := range forbidden {
		if strings.Contains(prompt, snippet) {
			t.Fatalf("prompt still contains forbidden write guidance %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}

func TestBuildKBContributionReviewMessageWithTargetRequiresPatchTool(t *testing.T) {
	target := "/app/workspace-docs/Workflow/social-media/knowledgebase/notes"
	prompt := BuildKBContributionReviewMessageWithTarget(KBAccessReadWrite, KBWriteMethodDirect, "Capture durable audience facts.", target)

	required := []string{
		"**Target:** `" + target + "/`",
		"Use these exact paths; do not rely on cwd",
		"diff_patch_workspace_file",
		"Do not use shell redirection, heredocs, tee, Python",
	}
	for _, snippet := range required {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("expected prompt to contain %q\n\nPrompt:\n%s", snippet, prompt)
		}
	}
}
