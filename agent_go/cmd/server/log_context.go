package server

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

type serverLogContext struct {
	Workflow string
	Group    string
	Mode     string
	UserID   string
	Username string
	Session  string
}

func normalizeServerLogMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "workflow", "workflow_phase", "workshop":
		return "workflow"
	case "simple", "multi-agent":
		return "multi-agent"
	default:
		return strings.TrimSpace(mode)
	}
}

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

func singleSelectedGroupName(groupNames []string) string {
	normalized := normalizeScheduleGroupNames(groupNames)
	if len(normalized) != 1 {
		return ""
	}
	return normalized[0]
}

func newServerLogContext(workspacePath, groupName, mode, userID, username, sessionID string) serverLogContext {
	return serverLogContext{
		Workflow: workflowLogName(workspacePath),
		Group:    strings.TrimSpace(groupName),
		Mode:     normalizeServerLogMode(mode),
		UserID:   strings.TrimSpace(userID),
		Username: strings.TrimSpace(username),
		Session:  strings.TrimSpace(sessionID),
	}
}

func requestLogContext(ctx context.Context, req QueryRequest, sessionID string) serverLogContext {
	user := GetUserFromContext(ctx)
	userID := ""
	username := ""
	if user != nil {
		userID = user.UserID
		username = user.Username
	}

	groupName := ""
	if req.ExecutionOptions != nil {
		groupName = singleSelectedGroupName(req.ExecutionOptions.EnabledGroupNames)
	}

	return newServerLogContext(req.SelectedFolder, groupName, req.AgentMode, userID, username, sessionID)
}

func scheduleLogContext(sctx *ScheduleContext) serverLogContext {
	if sctx == nil {
		return serverLogContext{}
	}

	mode := "workflow"
	if sctx.SourceType == "multi-agent" || strings.TrimSpace(sctx.Schedule.Mode) == "multi-agent" {
		mode = "multi-agent"
	}

	return newServerLogContext(
		sctx.WorkspacePath,
		singleSelectedGroupName(sctx.Schedule.GroupNames),
		mode,
		sctx.UserID,
		"",
		"",
	)
}

func (lc serverLogContext) WithSession(sessionID string) serverLogContext {
	lc.Session = strings.TrimSpace(sessionID)
	return lc
}

func (lc serverLogContext) WithWorkflow(workspacePath string) serverLogContext {
	lc.Workflow = workflowLogName(workspacePath)
	return lc
}

func (lc serverLogContext) WithGroup(groupName string) serverLogContext {
	lc.Group = strings.TrimSpace(groupName)
	return lc
}

func (lc serverLogContext) WithUser(userID, username string) serverLogContext {
	lc.UserID = strings.TrimSpace(userID)
	lc.Username = strings.TrimSpace(username)
	return lc
}

func (lc serverLogContext) Fields() []loggerv2.Field {
	fields := make([]loggerv2.Field, 0, 6)
	if lc.Workflow != "" {
		fields = append(fields, loggerv2.String("workflow", lc.Workflow))
	}
	if lc.Group != "" {
		fields = append(fields, loggerv2.String("group", lc.Group))
	}
	if lc.Mode != "" {
		fields = append(fields, loggerv2.String("mode", lc.Mode))
	}
	if lc.Username != "" {
		fields = append(fields, loggerv2.String("user", lc.Username))
	}
	if lc.UserID != "" {
		fields = append(fields, loggerv2.String("user_id", lc.UserID))
	}
	if lc.Session != "" {
		fields = append(fields, loggerv2.String("session", lc.Session))
	}
	return fields
}

func (lc serverLogContext) Prefix() string {
	parts := make([]string, 0, 6)
	if lc.Workflow != "" {
		parts = append(parts, fmt.Sprintf("workflow=%s", lc.Workflow))
	}
	if lc.Group != "" {
		parts = append(parts, fmt.Sprintf("group=%s", lc.Group))
	}
	if lc.Mode != "" {
		parts = append(parts, fmt.Sprintf("mode=%s", lc.Mode))
	}
	if lc.Username != "" {
		parts = append(parts, fmt.Sprintf("user=%s", lc.Username))
	}
	if lc.UserID != "" {
		parts = append(parts, fmt.Sprintf("user_id=%s", lc.UserID))
	}
	if lc.Session != "" {
		parts = append(parts, fmt.Sprintf("session=%s", lc.Session))
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func formatContextLog(logCtx serverLogContext, format string, args ...interface{}) string {
	message := fmt.Sprintf(format, args...)
	if prefix := logCtx.Prefix(); prefix != "" {
		return prefix + " " + message
	}
	return message
}

func logfWithContext(logCtx serverLogContext, format string, args ...interface{}) {
	log.Print(formatContextLog(logCtx, format, args...))
}

func (api *StreamingAPI) loggerWithContext(logCtx serverLogContext) loggerv2.Logger {
	if api == nil || api.logger == nil {
		return nil
	}
	fields := logCtx.Fields()
	if len(fields) == 0 {
		return api.logger
	}
	return api.logger.With(fields...)
}
