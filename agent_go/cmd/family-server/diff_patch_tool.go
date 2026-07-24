package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/workspace/handlers"
)

// diffPatchWorkspaceFileTool applies a unified diff to an existing workspace
// file. "diff_patch_workspace_file" is already advertised to the model via
// mcpagent's bridgeTools default definition (a fallback description used when
// no app registers a real handler for it) — without registering an actual
// handler here, the model sees the tool, tries to call it, and gets a
// confusing "not registered for this session" error instead of a clean
// fallback.
//
// Reuses AgentWorks' own diff-apply logic directly: handlers.ApplyDiffPatchDirect
// (workspace/handlers/diff_patch.go) is the exact function AgentWorks' real
// /api/documents/*/diff endpoint calls, exported specifically for in-process
// reuse — same malformed-diff repair, fuzzy-match fallback, and JSON re-format
// behavior, no reimplementation and no dependency on a separate service.
func diffPatchWorkspaceFileTool() agentsession.Tool {
	return agentsession.Tool{
		Name:        "diff_patch_workspace_file",
		Description: "Apply a unified diff patch to a workspace file — faster than rewriting the whole file for a small change.",
		Category:    "family_tools",
		Params:      diffPatchParams,
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			return applyWorkspaceDiffPatch(args, func(string) bool { return true })
		},
	}
}

// childDiffPatchWorkspaceFileTool is the SAME diff_patch_workspace_file tool,
// restricted via childCanWrite to exactly what the child may write: the
// CURRENT activity's files (annotated in place — this is how "✓ Answered"
// progress notes get recorded) and the child's own child/attempts/ scratch
// space. Unlike childShellTool(), this tool applies the patch directly
// in-process with no security.Isolator sandbox underneath it, so the
// childCanWrite check here IS the only boundary — it must stay in sync with
// childShellTool()'s WritePaths (both derive from currentActivityItems).
func childDiffPatchWorkspaceFileTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "diff_patch_workspace_file",
		Description: "Apply a small unified diff patch to the current lesson/test or your own work — " +
			"faster and more reliable than rewriting the whole file for a small insertion like an answered-note line.",
		Category: "family_tools",
		Params:   diffPatchParams,
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			return applyWorkspaceDiffPatch(args, childCanWrite)
		},
	}
}

var diffPatchParams = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"filepath": map[string]interface{}{"type": "string", "description": "Workspace-relative path to the file to patch"},
		"diff":     map[string]interface{}{"type": "string", "description": "Unified diff content to apply"},
	},
	"required": []string{"filepath", "diff"},
}

func applyWorkspaceDiffPatch(args map[string]interface{}, allowed func(rel string) bool) (string, error) {
	rel, _ := args["filepath"].(string)
	diff, _ := args["diff"].(string)
	rel = strings.TrimSpace(rel)
	diff = strings.TrimSpace(diff)
	if rel == "" || diff == "" {
		return "", fmt.Errorf("filepath and diff are required")
	}
	if !allowed(rel) {
		return "", fmt.Errorf("not permitted to patch this path")
	}
	abs, ok := resolveWorkspacePath(rel)
	if !ok {
		return "", fmt.Errorf("invalid path")
	}
	current, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", rel)
	}
	patched, err := handlers.ApplyDiffPatchDirect(string(current), diff)
	if err != nil {
		return "", fmt.Errorf("failed to apply diff: %w", err)
	}
	if err := os.WriteFile(abs, []byte(patched), 0o600); err != nil {
		return "", fmt.Errorf("failed to write patched file: %w", err)
	}
	return fmt.Sprintf(`{"status":"ok","patched":%q}`, rel), nil
}
