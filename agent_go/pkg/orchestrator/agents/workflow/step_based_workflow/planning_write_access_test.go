package step_based_workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	workspacepkg "github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
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

func TestPlanningFileMutationWriteAccessIsExact(t *testing.T) {
	const (
		sessionID     = "planning-file-mutation-write-access"
		workspacePath = "Workflow/demo"
		managedPath   = workspacePath + "/planning/changelog/changelog-test.json"
	)
	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	managedCtx := withPlanningFileMutationWriteAccess(ctx, workspacePath, "changelog/changelog-test.json")

	if err := client.ValidatePathWithContext(managedCtx, managedPath, true); err != nil {
		t.Fatalf("exact managed planning file should be writable: %v", err)
	}
	for _, blockedSibling := range []string{
		workspacePath + "/planning/plan.json",
		workspacePath + "/planning/changelog/other.json",
	} {
		if err := client.ValidatePathWithContext(managedCtx, blockedSibling, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
			t.Fatalf("managed file capability unlocked sibling %s: %v", blockedSibling, err)
		}
	}
	if err := client.ValidatePathWithContext(ctx, managedPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("managed file capability leaked into caller context: %v", err)
	}
}

func TestWritePlanChangelogEntryUsesManagedFileAccess(t *testing.T) {
	const (
		sessionID     = "plan-changelog-managed-write-access"
		workspacePath = "Workflow/demo"
		filename      = "changelog-test.json"
		changelogPath = workspacePath + "/planning/changelog/" + filename
	)
	workspacepkg.SetSessionFolderGuard(sessionID, []string{workspacePath}, []string{workspacePath})
	workspacepkg.SetSessionFolderGuardBlockedWritePaths(sessionID, []string{workspacePath + "/planning"})
	defer workspacepkg.ClearSessionShellConfig(sessionID)

	planChangelogSessionMutex.Lock()
	previousFile := planChangelogSessionFile
	previousStart := planChangelogSessionStart
	planChangelogSessionFile = filename
	planChangelogSessionStart = time.Now().UTC()
	planChangelogSessionMutex.Unlock()
	t.Cleanup(func() {
		planChangelogSessionMutex.Lock()
		planChangelogSessionFile = previousFile
		planChangelogSessionStart = previousStart
		planChangelogSessionMutex.Unlock()
	})

	client := workspacepkg.NewClient("http://unused")
	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, sessionID)
	wrote := false
	err := writePlanChangelogEntry(
		ctx,
		workspacePath,
		PlanChangelogEntry{Tool: "update_step_config", Reason: "test managed changelog write"},
		func(context.Context, string) (string, error) { return "", errors.New("not found") },
		func(writeCtx context.Context, path, _ string) error {
			wrote = true
			if path != changelogPath {
				t.Fatalf("unexpected changelog path: %s", path)
			}
			return client.ValidatePathWithContext(writeCtx, path, true)
		},
		loggerv2.NewNoop(),
	)
	if err != nil {
		t.Fatalf("typed changelog write was blocked: %v", err)
	}
	if !wrote {
		t.Fatal("typed changelog writer was not called")
	}
	if err := client.ValidatePathWithContext(ctx, changelogPath, true); err == nil || !strings.Contains(err.Error(), "blocked for writes") {
		t.Fatalf("changelog capability leaked into caller context: %v", err)
	}
}
