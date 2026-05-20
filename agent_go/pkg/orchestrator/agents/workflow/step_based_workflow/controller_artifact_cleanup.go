package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
)

func isSubAgentArtifactFolderForStep(folderName string, stepNumber int) bool {
	return strings.HasPrefix(folderName, fmt.Sprintf("step-%d-sub-", stepNumber))
}

func isGenericAgentArtifactFolderForStep(folderName string, stepNumber int) bool {
	prefixes := []string{
		fmt.Sprintf("step-%d-generic-", stepNumber),
		fmt.Sprintf("generic-step-%d-", stepNumber),
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(folderName, prefix) {
			return true
		}
	}
	return false
}

func isMessageSequenceStepPathForStep(stepPath string, stepNumber int) bool {
	return stepPath == fmt.Sprintf("step-%d", stepNumber) ||
		strings.HasPrefix(stepPath, fmt.Sprintf("step-%d-sub-", stepNumber)) ||
		strings.HasPrefix(stepPath, fmt.Sprintf("step-%d-generic-", stepNumber))
}

func workflowSafeIDPart(s string, fallback string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return fallback
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		isSafe := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isSafe {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	if len(out) > 48 {
		out = strings.Trim(out[:48], "-")
		if out == "" {
			return fallback
		}
	}
	return out
}

func todoSubAgentArtifactFolderName(stepPath string, routeID string, todoID string) string {
	routePart := workflowSafeIDPart(routeID, "route")
	todoPart := workflowSafeIDPart(todoID, "")
	if todoPart == "" {
		return fmt.Sprintf("%s-sub-%s", stepPath, routePart)
	}
	return fmt.Sprintf("%s-sub-%s-%s", stepPath, routePart, todoPart)
}

func (hcpo *StepBasedWorkflowOrchestrator) cleanupExecutionArtifactsForStepPath(ctx context.Context, stepPath string, stepID string, includeMessageSequence bool) error {
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf("selectedRunFolder not set - cannot cleanup execution artifacts")
	}
	if strings.TrimSpace(stepPath) == "" {
		return fmt.Errorf("stepPath not set - cannot cleanup execution artifacts")
	}

	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	logsWorkspacePath := fmt.Sprintf("%s/logs", runWorkspacePath)

	folderNames := make([]string, 0, 2)
	seen := map[string]struct{}{}
	addFolderName := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		folderNames = append(folderNames, name)
	}
	addFolderName(getArtifactFolderName(stepID, stepPath))
	addFolderName(stepPath)

	for _, folderName := range folderNames {
		executionFolderPath := fmt.Sprintf("%s/%s", executionWorkspacePath, folderName)
		hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaning execution artifact folder: %s", executionFolderPath))
		if err := hcpo.CleanupDirectory(ctx, executionFolderPath, fmt.Sprintf("execution/%s", folderName)); err != nil {
			return fmt.Errorf("failed to cleanup execution artifact folder %s: %w", folderName, err)
		}

		logsFolderPath := fmt.Sprintf("%s/%s", logsWorkspacePath, folderName)
		if err := hcpo.archiveLogsFolder(ctx, logsFolderPath, folderName); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive logs for execution artifact folder %s: %v", folderName, err))
		}
	}

	if includeMessageSequence {
		if err := hcpo.cleanupMessageSequenceStepPath(ctx, stepPath); err != nil {
			return err
		}
	}

	return nil
}
