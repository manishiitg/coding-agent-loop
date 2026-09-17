package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/mcpagent/executor"
)

// bindToolExecutionContext captures the authenticated query's identity, not
// model arguments or a fallback local user. Every platform tool registered on
// the query's definition receives these claims, regardless of its transport.
// Action-specific authorization remains in the tool/UI handler.
func (api *StreamingAPI) bindToolExecutionContext(requestCtx context.Context, session string, req QueryRequest, readOnly bool) func(context.Context, string) (context.Context, error) {
	var bound *UserClaims
	if claims := GetUserFromContext(requestCtx); claims != nil {
		copy := *claims
		if claims.ExecutionPrincipal != nil {
			principal := *claims.ExecutionPrincipal
			copy.ExecutionPrincipal = &principal
		}
		bound = &copy
	}
	return func(ctx context.Context, tool string) (context.Context, error) {
		if bound == nil || strings.TrimSpace(bound.UserID) == "" || strings.TrimSpace(session) == "" {
			return nil, fmt.Errorf("%s requires an authenticated session", tool)
		}
		callerSession := executor.SessionIDFromContext(ctx)
		if callerSession == "" {
			callerSession, _ = ctx.Value(common.ChatSessionIDKey).(string)
		}
		if callerSession != "" && callerSession != session {
			return nil, fmt.Errorf("%s caller does not own this tool session", tool)
		}
		if claims := GetUserFromContext(ctx); claims != nil && (claims.UserID != bound.UserID || claims.Provider != bound.Provider || claims.BotRouteGrant != bound.BotRouteGrant) {
			return nil, fmt.Errorf("%s caller identity conflicts with its authenticated session", tool)
		}
		if userID, _ := ctx.Value(common.UserIDKey).(string); userID != "" && userID != bound.UserID {
			return nil, fmt.Errorf("%s caller identity conflicts with its authenticated session", tool)
		}
		if api.eventStore != nil {
			if owner := api.eventStore.GetSessionOwner(session); owner != "" && owner != bound.UserID {
				return nil, fmt.Errorf("%s session ownership changed; start a new turn", tool)
			}
		}
		if bound.Provider != "bot_route" {
			if _, bot := api.botExecutionForSession(session); bot {
				return nil, fmt.Errorf("%s session origin changed; start a new turn", tool)
			}
			if active, _ := api.getActiveSession(session); active != nil && (active.BotPlatform != "" || strings.HasPrefix(active.TriggeredBy, "bot:")) {
				return nil, fmt.Errorf("%s session origin changed; start a new turn", tool)
			}
		}
		copy := *bound
		ctx = context.WithValue(ctx, UserContextKey, &copy)
		ctx = context.WithValue(ctx, common.UserIDKey, copy.UserID)
		ctx = executor.WithSessionID(ctx, session)
		if copy.Provider == "bot_route" {
			validated, err := api.revalidateExecutionPrincipal(ctx, req)
			if err != nil {
				return nil, err
			}
			ctx = validated
		} else if access := userAccessForClaims(&copy); access.Disabled || directoryUserIsUnknown(&copy) {
			return nil, fmt.Errorf("%s account is unavailable", tool)
		}
		access, err := conversationTargetAccess(ctx, req)
		if err != nil {
			return nil, err
		}
		if access == WorkflowAccessNone || (!readOnly && access == WorkflowAccessRead) {
			return nil, fmt.Errorf("%s session permissions changed; start a new turn", tool)
		}
		return ctx, nil
	}
}
