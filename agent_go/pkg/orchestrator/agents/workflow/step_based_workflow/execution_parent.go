package step_based_workflow

import (
	"context"
	"strings"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

func currentWorkshopParentExecutionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	parentExecutionID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)
	return strings.TrimSpace(parentExecutionID)
}
