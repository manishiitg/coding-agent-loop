package step_based_workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	workspacepkg "github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

func TestPlanMutationWriteAccessDoesNotUnblockGeneralFileWrites(t *testing.T) {
	const sessionID = "plan-mutation-write-access"
	const planPath = "Workflow/demo/planning/plan.json"
	workspacepkg.SetSessionFolderGuard(sessionID, []string{"Workflow/demo"}, []string{"Workflow/demo"})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{"Workflow/demo/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	if err := client.ValidatePathWithContext(ctx, planPath, true); err == nil {
		t.Fatal("ordinary file write unexpectedly reached the guarded plan")
	}

	called := false
	writeFile := withPlanMutationWriteAccess("Workflow/demo", func(callCtx context.Context, path, _ string) error {
		called = true
		return client.ValidatePathWithContext(callCtx, path, true)
	})
	if err := writeFile(ctx, planPath, `{}`); err != nil {
		t.Fatalf("dedicated plan mutation write was blocked: %v", err)
	}
	if !called {
		t.Fatal("plan mutation write callback was not called")
	}

	if err := client.ValidatePathWithContext(ctx, planPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("plan capability leaked back into the caller context: %v", err)
	}
}
