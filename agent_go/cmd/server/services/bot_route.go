package services

import (
	"encoding/json"
	"strings"
)

// ResolveChannelRoute is the single route decoder for listeners and turns.
// Destination matching never depends on the external sender or display name.
func ResolveChannelRoute(encoded, channelID string) *ChannelRoute {
	var routes map[string]ChannelRoute
	if json.Unmarshal([]byte(encoded), &routes) != nil {
		return nil
	}
	for key, route := range routes {
		if !strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(channelID)) {
			continue
		}
		route.WorkflowID = strings.TrimSpace(route.WorkflowID)
		route.ProfileID = strings.TrimSpace(route.ProfileID)
		route.WorkspacePath = strings.TrimSpace(route.WorkspacePath)
		route.ConversationKey = strings.TrimSpace(route.ConversationKey)
		route.WorkspaceUserID = strings.TrimSpace(route.WorkspaceUserID)
		workflow := route.WorkflowID != "" && route.ProfileID == "" && route.WorkspacePath != ""
		profile := route.ProfileID != "" && route.WorkflowID == "" && route.ConversationKey != "" && route.WorkspacePath != ""
		if !workflow && !profile {
			return nil
		}
		route.WorkshopMode = "run"
		if route.BotGrant == "owner" {
			route.BotGrant = "run"
		}
		return &route
	}
	return nil
}

// Slack email exclusions narrow a deployed route without changing its grant.
// If exclusions exist, unverifiable human identities cannot bypass them.
func SlackRouteAllowsEmail(route ChannelRoute, email string) bool {
	if len(route.BlockedEmails) == 0 {
		return true
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return false
	}
	for _, blocked := range route.BlockedEmails {
		if strings.EqualFold(strings.TrimSpace(blocked), email) {
			return false
		}
	}
	return true
}
