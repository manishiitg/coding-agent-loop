package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// ExecutionPrincipal separates execution authority from the resource namespace
// and the external audit actor. Only application adapters create this value.
// It is never accepted as part of the public query/JWT contract.
type ExecutionPrincipal struct {
	Kind            string
	ID              string
	ResourceOwnerID string
	Access          WorkflowAccessLevel
	Target          services.ChannelRoute
	AuditActor      string
}

// Revalidate at the shared query boundary, including resumed and live-input
// turns. Listener validation alone is insufficient for a queued turn.
func (api *StreamingAPI) revalidateExecutionPrincipal(ctx context.Context, req QueryRequest) (context.Context, error) {
	claims := GetUserFromContext(ctx)
	if claims == nil || claims.Provider != "bot_route" {
		return ctx, nil
	}
	if req.BotPlatform != "slack" || req.BotChannelID == "" {
		return ctx, fmt.Errorf("bot route has no trusted channel binding")
	}
	cfg, routes, err := api.slackRoutes(ctx)
	if err != nil {
		return ctx, err
	}
	if cfg == nil || !cfg.Enabled || !cfg.BotMode {
		return ctx, fmt.Errorf("Slack bot route is disabled")
	}
	route, found := routes[req.BotChannelID]
	if !found {
		return ctx, fmt.Errorf("Slack bot route was revoked")
	}
	if route.BotGrant != "run" && route.BotGrant != "owner" {
		return ctx, fmt.Errorf("Slack bot route has no explicit grant; save it in Setup > Bots")
	}
	route.BotGrant = "run"
	route.WorkshopMode = "run"
	if !claims.SlackTrustedApp && !services.SlackRouteAllowsEmail(route, req.BotUserEmail) {
		return ctx, fmt.Errorf("Slack email is blocked or unverifiable")
	}
	expected := services.ChannelRoute{WorkflowID: claims.BotRouteWorkflowID, ProfileID: claims.BotRouteProfileID, ConversationKey: claims.BotRouteConversationKey, WorkspacePath: claims.BotRouteWorkspacePath}
	if !sameSlackRouteDestination(route, expected) {
		return ctx, fmt.Errorf("bot route target changed; start a new conversation")
	}
	if filepath.Clean(req.SelectedFolder) != filepath.Clean(route.WorkspacePath) || req.AgentProfileID != route.ProfileID || req.PresetQueryID != route.WorkflowID || req.AgentProfileConversationKey != route.ConversationKey {
		return ctx, fmt.Errorf("query does not match the bot route target")
	}
	principal := &ExecutionPrincipal{Kind: "bot_route", ID: services.BotPrincipalIDForRoute("slack", route), Target: route, Access: WorkflowAccessRead, AuditActor: req.BotUserID}
	copy := *claims
	copy.BotRouteGrant = route.BotGrant
	if route.ProfileID != "" {
		if route.WorkspaceUserID == "" {
			return ctx, fmt.Errorf("product route has no resource owner")
		}
		principal.ResourceOwnerID = route.WorkspaceUserID
		copy.UserID = route.WorkspaceUserID
	} else {
		copy.UserID = principal.ID
	}
	copy.ExecutionPrincipal = principal
	return context.WithValue(ctx, UserContextKey, &copy), nil
}

// conversationTargetAccess is shared by all adapters. The account tier is
// narrowed by the concrete workflow/profile target before constructing tools.
func conversationTargetAccess(ctx context.Context, req QueryRequest) (WorkflowAccessLevel, error) {
	claims := GetUserFromContext(ctx)
	if level, scoped := botRouteProfileAccessForRequest(claims, req); scoped {
		if level == WorkflowAccessNone {
			return level, fmt.Errorf("profile route access denied")
		}
		return level, nil
	}
	if strings.HasPrefix(req.SelectedFolder, "Workflow/") {
		manifest, exists, err := ReadWorkflowManifest(ctx, req.SelectedFolder)
		if err != nil {
			return WorkflowAccessNone, err
		}
		if exists {
			level := workflowAccessForManifest(claims, manifest)
			if !userAllowedWorkflowID(claims, manifest.ID) || level == WorkflowAccessNone {
				return WorkflowAccessNone, fmt.Errorf("workflow access denied")
			}
			return level, nil
		}
		if claims != nil && claims.Provider == "bot_route" {
			return WorkflowAccessNone, fmt.Errorf("workflow route manifest is missing")
		}
	}
	return workflowAccessForClaims(claims), nil
}
