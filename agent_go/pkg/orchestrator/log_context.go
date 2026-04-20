package orchestrator

import (
	"path/filepath"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func workflowLogName(workspacePath string) string {
	trimmed := strings.TrimSpace(workspacePath)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == "." || cleaned == string(filepath.Separator) {
		return ""
	}
	return filepath.Base(cleaned)
}

func workflowLogFields(workspacePath, groupName string) []loggerv2.Field {
	fields := make([]loggerv2.Field, 0, 2)
	if workflowName := workflowLogName(workspacePath); workflowName != "" {
		fields = append(fields, loggerv2.String("workflow", workflowName))
	}
	if trimmedGroup := strings.TrimSpace(groupName); trimmedGroup != "" {
		fields = append(fields, loggerv2.String("group", trimmedGroup))
	}
	return fields
}

// SingleSelectedGroupName returns the selected group only when execution is
// clearly scoped to exactly one group. Multi-group runs return empty so callers
// can later bind a per-group logger inside the batch execution loop.
func SingleSelectedGroupName(groupNames []string) string {
	if len(groupNames) != 1 {
		return ""
	}
	return strings.TrimSpace(groupNames[0])
}

// ApplyWorkflowLogContext scopes the orchestrator logger to a workflow and,
// optionally, a specific group. This keeps the same base logger while allowing
// callers to rebind the current logger as execution moves across groups.
func (bo *BaseOrchestrator) ApplyWorkflowLogContext(workspacePath, groupName string) {
	if bo == nil || bo.baseLogger == nil {
		return
	}

	scopedLogger := bo.baseLogger
	if fields := workflowLogFields(workspacePath, groupName); len(fields) > 0 {
		scopedLogger = bo.baseLogger.With(fields...)
	}

	bo.logger = scopedLogger
	if bridge, ok := bo.contextAwareBridge.(*ContextAwareEventBridge); ok {
		bridge.SetLogger(scopedLogger)
	}
}
