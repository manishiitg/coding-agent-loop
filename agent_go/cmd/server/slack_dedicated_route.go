package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/agentprofiles"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

// dedicatedSlackRoute resolves the destination a scoped Slack app serves
// in a channel (see services.DedicatedSlackRouteFunc). A workflow-scoped app
// runs its workflow and a project-scoped app runs its crew project, in Run
// mode, in any channel it is invited to. The connection's scope is the
// grant: only the destination's owner (or an admin) can create a scoped
// connection. A channel the owner routed on the connection itself
// (ChannelRoutes, granted by someone who manages the app and can write the
// destination) answers for that route's destination instead. A route whose
// destination no longer resolves is revoked, never the app's own
// destination: the channel was handed to someone else.
func (api *StreamingAPI) dedicatedSlackRoute(ctx context.Context, connectionID, channelID string) (*services.ChannelRoute, bool) {
	svc := services.GetSlackService()
	if svc == nil {
		return nil, false
	}
	if connectionID == "" {
		connectionID = svc.DefaultConnectionID()
		if connectionID == "" {
			return nil, false
		}
	}
	conn, ok := svc.GetConnection(connectionID)
	if !ok {
		return nil, false
	}
	workspacePath := strings.TrimSpace(conn.WorkspacePath)
	if workspacePath == "" {
		return nil, false
	}
	if !conn.Enabled {
		return nil, true
	}
	destinationPath, profileID := workspacePath, strings.TrimSpace(conn.ProfileID)
	if routed, found := conn.ChannelRoutes[services.NormalizeSlackChannelID(channelID)]; found && channelID != "" {
		destinationPath, profileID = strings.TrimSpace(routed.WorkspacePath), strings.TrimSpace(routed.ProfileID)
	}
	route, err := api.slackDestinationRoute(ctx, destinationPath, profileID)
	if err != nil {
		log.Printf("[SLACK] Dedicated app %s: destination %s unavailable in channel %s: %v", connectionID, destinationPath, channelID, err)
		return nil, true
	}
	return route, true
}

// slackDestinationRoute builds the Run-mode route for a workflow folder or,
// with profileID, a crew project.
func (api *StreamingAPI) slackDestinationRoute(ctx context.Context, workspacePath, profileID string) (*services.ChannelRoute, error) {
	if profileID != "" {
		return api.dedicatedSlackProfileRoute(ctx, profileID, workspacePath)
	}
	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil {
		return nil, err
	}
	if !found || manifest == nil || strings.TrimSpace(manifest.ID) == "" {
		return nil, fmt.Errorf("workflow %s has no manifest", workspacePath)
	}
	return &services.ChannelRoute{
		WorkflowID:    strings.TrimSpace(manifest.ID),
		WorkspacePath: workspacePath,
		BotGrant:      "run",
		WorkshopMode:  "run",
	}, nil
}

// dedicatedSlackProfileRoute builds a crew project's route: the owner comes
// from the project's "_users/<id>/" path and the conversation from its
// product manifest, re-checked through the same binding a saved channel
// route must match.
func (api *StreamingAPI) dedicatedSlackProfileRoute(ctx context.Context, profileID, workspacePath string) (*services.ChannelRoute, error) {
	route := services.ChannelRoute{ProfileID: profileID, WorkspacePath: workspacePath, BotGrant: "run", WorkshopMode: "run"}
	ownerID := services.RouteWorkspaceUserID(route, "")
	if ownerID == "" {
		return nil, fmt.Errorf("project path has no owner")
	}
	if api == nil || api.agentProfiles == nil {
		return nil, fmt.Errorf("agent profiles are unavailable")
	}
	profile, err := api.agentProfiles.Resolve(profileID, 0, ownerID)
	if err != nil {
		return nil, err
	}
	key := ""
	if strings.EqualFold(strings.TrimSpace(profile.Runtime.Conversation.Mode), agentprofiles.ConversationModeKeyed) {
		raw, found, err := readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspacePath, "product.json")))
		if err != nil {
			return nil, err
		}
		var manifest productProjectManifest
		if !found || json.Unmarshal([]byte(raw), &manifest) != nil || strings.TrimSpace(manifest.ID) == "" {
			return nil, fmt.Errorf("project has no product manifest")
		}
		key = strings.TrimSpace(manifest.ID)
	}
	binding, err := resolveProductConversationBinding(internalBotRequestContext(ctx, ownerID), ownerID, profile, key)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(binding.WorkspacePath) != filepath.Clean(workspacePath) {
		return nil, fmt.Errorf("project conversation resolves to another workspace")
	}
	route.ConversationKey = binding.ConversationKey
	route.WorkspaceUserID = ownerID
	route.ProfileLabel = strings.TrimSpace(binding.Title)
	if route.ProfileLabel == "" {
		route.ProfileLabel = firstNonBlank(strings.TrimSpace(profile.Name), profileID)
	}
	return &route, nil
}

// slackRouteForConnection is the tool-side twin of the inbound rule: for a
// dedicated arrival app, the destination its owner routed this channel to,
// else its own destination; for a shared app, the platform channel route. found=false means no route (or a revoked dedicated one). A
// dedicated app is its destination's own enablement, like a saved route,
// so it never depends on the platform switch or a saved connector config.
func (api *StreamingAPI) slackRouteForConnection(ctx context.Context, connectionID, channel string, routes map[string]ChannelRoute) (route ChannelRoute, found, dedicated bool) {
	if own, isDedicated := services.DedicatedSlackRoute(ctx, connectionID, channel); isDedicated {
		if own == nil || services.IsRevokedSlackRoute(*own) {
			return ChannelRoute{}, false, true
		}
		return *own, true, true
	}
	route, found = routes[channel]
	return route, found, false
}

// slackTrafficAllowed gates Slack bot traffic: a dedicated app or saved
// route authorizes itself; anything else needs the platform switch.
func slackTrafficAllowed(cfg *chathistory.BotConnectorConfig, found, dedicated bool) bool {
	return (dedicated && found) || services.SlackBotTrafficAllowed(cfg, found)
}

// slackToolRoute resolves the route a Slack tool call acts on. A bot turn
// uses its arrival app's rule; any other session (a workflow run posting to
// a channel) only ever acts through channel routes.
func (api *StreamingAPI) slackToolRoute(ctx context.Context, session, channel string, routes map[string]ChannelRoute) (route ChannelRoute, found, dedicated bool) {
	if execution, ok := api.botExecutionForSession(session); ok {
		return api.slackRouteForConnection(ctx, execution.Request.BotConnectionID, channel, routes)
	}
	route, found = routes[channel]
	return route, found, false
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
