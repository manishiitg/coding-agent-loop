package step_based_workflow

import (
	"context"
	"path/filepath"

	workspacepkg "github.com/manishiitg/coding-agent-loop/agent_go/pkg/workspace"
)

// withPlanningFileMutationWriteAccess grants a trusted Go-side writer access
// to one exact file under planning/. Raw shell and general workspace tools stay
// blocked from the file and from the rest of planning/.
func withPlanningFileMutationWriteAccess(ctx context.Context, workspacePath, planningRelativePath string) context.Context {
	managedPath := normalizePathForWorkspaceAPI(filepath.Join("planning", planningRelativePath), workspacePath)
	return workspacepkg.WithSystemManagedWritePaths(ctx, managedPath)
}

func (hcpo *StepBasedWorkflowOrchestrator) writeManagedPlanningFile(ctx context.Context, planningRelativePath, content string) error {
	writeCtx := withPlanningFileMutationWriteAccess(ctx, hcpo.GetWorkspacePath(), planningRelativePath)
	return hcpo.WriteWorkspaceFile(writeCtx, filepath.Join("planning", planningRelativePath), content)
}
