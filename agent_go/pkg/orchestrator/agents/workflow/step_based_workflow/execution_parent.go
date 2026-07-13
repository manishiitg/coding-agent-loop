package step_based_workflow

import (
	"context"
	"strings"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
)

func currentWorkshopParentExecutionID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	parentExecutionID := virtualtools.SubAgentSpecFromContext(ctx).BackgroundAgentID
	return strings.TrimSpace(parentExecutionID)
}
