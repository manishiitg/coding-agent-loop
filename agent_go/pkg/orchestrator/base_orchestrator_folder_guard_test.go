package orchestrator

import (
	"context"
	"testing"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

func TestWorkspacePathFallbackDoesNotInjectEmptyCapabilitySlices(t *testing.T) {
	bo := &BaseOrchestrator{}
	bo.SetWorkspacePath("Workflow/demo")

	called := false
	executors := bo.WrapWorkspaceToolsWithFolderGuard(map[string]interface{}{
		"execute_shell_command": func(ctx context.Context, _ map[string]interface{}) (string, error) {
			called = true
			if _, ok := ctx.Value(virtualtools.FolderGuardReadPathsKey).([]string); ok {
				t.Fatal("workspacePath fallback injected an explicit read capability")
			}
			if _, ok := ctx.Value(virtualtools.FolderGuardWritePathsKey).([]string); ok {
				t.Fatal("workspacePath fallback injected an explicit write capability")
			}
			return "ok", nil
		},
	})

	executor := executors["execute_shell_command"].(func(context.Context, map[string]interface{}) (string, error))
	if _, err := executor(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("fallback executor failed: %v", err)
	}
	if !called {
		t.Fatal("fallback executor was not called")
	}
}

func TestExplicitReadOnlyGuardInjectsEmptyWriteCapability(t *testing.T) {
	bo := &BaseOrchestrator{}
	executors := bo.WrapWorkspaceToolsWithExplicitPaths(
		[]string{"Workflow/demo"},
		[]string{},
		map[string]interface{}{
			"execute_shell_command": func(ctx context.Context, _ map[string]interface{}) (string, error) {
				writes, ok := ctx.Value(virtualtools.FolderGuardWritePathsKey).([]string)
				if !ok || len(writes) != 0 {
					t.Fatalf("explicit read-only guard lost empty write capability: %#v", writes)
				}
				return "ok", nil
			},
		},
	)

	executor := executors["execute_shell_command"].(func(context.Context, map[string]interface{}) (string, error))
	if _, err := executor(context.Background(), map[string]interface{}{}); err != nil {
		t.Fatalf("read-only executor failed: %v", err)
	}
}
