package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

// gmailOAuthRedirectURIFromEnv mirrors deriveOAuthRedirectURIFromEnv
// (oauth_routes.go)'s pattern for the generic MCP OAuth flow, but for
// Gmail's own fixed callback path. Needed because a tool call (unlike the
// browser-driven /auth/start route in gmail_oauth_routes.go) has no
// *http.Request to derive the host from -- PUBLIC_URL is this deployment's
// own known public base URL instead.
func gmailOAuthRedirectURIFromEnv() (string, error) {
	publicURL := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	if publicURL == "" {
		return "", fmt.Errorf("PUBLIC_URL is not configured on this server; cannot generate a Gmail reconnect link from chat")
	}
	return strings.TrimRight(publicURL, "/") + gmailOAuthCallbackPath, nil
}

// registerGmailConnectionManagementTools gives the workflow/builder agent a
// chat-driven path to the same grant change the "Sending accounts" settings
// panel offers, so a user can say "increase my scope to include Drive"
// instead of opening the panel and clicking checkboxes themselves.
//
// This only ever changes STORED intent (GmailConnection.AllowReadAccess /
// Services) and returns a reconnect link -- it never talks to Google or
// changes what the account can actually do. Google fixes a token's scope at
// consent time; there is no API to widen it after the fact. The agent must
// tell the user to open the link and reconnect, and should also open the
// Bots settings panel (workflowWorkspaceViews "bots") so they can see the
// updated request and click Reconnect there too if the link doesn't suit.
func (api *StreamingAPI) registerGmailConnectionManagementTools(registrar definitionToolRegistrar, sessionID, workspacePath string) error {
	serviceNames := services.GoogleServiceCatalog()
	serviceKeys := make([]string, 0, len(serviceNames))
	for key := range serviceNames {
		serviceKeys = append(serviceKeys, key)
	}
	description := fmt.Sprintf(
		"Change what a Gmail connection is authorized for -- Gmail read access and/or Google Workspace services (%s) -- and get back a reconnect link for the user. "+
			"This only updates the STORED request; it does NOT change what Google has already granted. Google fixes a token's scope at the moment the user consents, "+
			"so after this call succeeds you MUST tell the user to open the returned reconnect_url and complete Google's consent screen -- the change has no effect until they do. "+
			"Also call open_workspace_view(view=\"bots\") right after this so the Sending accounts panel is visible with the updated request. "+
			"Pass connection_id to target a specific account; omitted, the account's default connection is used. services replaces the full existing service list for this connection "+
			"(pass every service that should remain authorized, not just the one being added) -- omit it entirely to leave services unchanged and only touch allow_read_access.",
		strings.Join(serviceKeys, ", "),
	)
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"connection_id": map[string]interface{}{"type": "string", "description": "Which Gmail connection to change. Omit to use the account's default connection."},
			"allow_read_access": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether this connection may read/search the mailbox (gmail.readonly), in addition to the always-granted send. Omit to leave unchanged.",
			},
			"services": map[string]interface{}{
				"type": "array",
				"description": fmt.Sprintf(
					"Complete replacement list of Google Workspace services this connection should be authorized for, beyond Gmail. Omit entirely to leave services unchanged. Pass an empty array to remove every service grant, going back to Gmail-only. Each service is one of: %s.",
					strings.Join(serviceKeys, ", "),
				),
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"service": map[string]interface{}{"type": "string", "enum": serviceKeys},
						"write":   map[string]interface{}{"type": "boolean", "description": "true for read+write, false/omitted for read-only."},
					},
					"required": []string{"service"},
				},
			},
		},
	}
	return registrar.RegisterCustomTool("update_gmail_connection_grants", description, params, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return api.updateGmailConnectionGrantsFromTool(ctx, sessionID, workspacePath, args)
	}, "gmail_connection_management")
}

func (api *StreamingAPI) updateGmailConnectionGrantsFromTool(ctx context.Context, sessionID, workspacePath string, args map[string]interface{}) (string, error) {
	svc := services.GetGmailService()
	if svc == nil {
		return "", fmt.Errorf("gmail is not configured on this deployment")
	}

	connectionID, _ := args["connection_id"].(string)
	connectionID = strings.TrimSpace(connectionID)
	var conn services.GmailConnection
	var found bool
	if connectionID != "" {
		conn, found = svc.GetConnection(connectionID)
	} else {
		conn, found = svc.DefaultConnection()
	}
	if !found {
		return "", fmt.Errorf("gmail connection %q not found; call list_gmail_connections or check the Sending accounts panel for valid IDs", connectionID)
	}

	input := services.GmailConnectionInput{}
	if raw, ok := args["allow_read_access"]; ok {
		v, ok := raw.(bool)
		if !ok {
			return "", fmt.Errorf("allow_read_access must be a boolean")
		}
		input.AllowReadAccess = &v
	}
	if raw, ok := args["services"]; ok {
		list, ok := raw.([]interface{})
		if !ok {
			return "", fmt.Errorf("services must be an array")
		}
		grants := make([]services.GoogleServiceGrant, 0, len(list))
		for _, item := range list {
			obj, ok := item.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("each services entry must be an object with a service field")
			}
			name, _ := obj["service"].(string)
			name = strings.TrimSpace(name)
			if name == "" {
				return "", fmt.Errorf("each services entry requires service")
			}
			write, _ := obj["write"].(bool)
			grants = append(grants, services.GoogleServiceGrant{Service: name, Write: write})
		}
		input.Services = grants
		input.ServicesSet = true
	}
	if input.AllowReadAccess == nil && !input.ServicesSet {
		return "", fmt.Errorf("pass allow_read_access and/or services -- nothing to change")
	}

	updated, err := svc.UpdateConnection(ctx, conn.ID, input)
	if err != nil {
		return "", err
	}

	redirectURI, err := gmailOAuthRedirectURIFromEnv()
	if err != nil {
		return "", fmt.Errorf("saved the new request, but could not build a reconnect link: %w. Tell the user to open the Sending accounts panel and click Reconnect themselves", err)
	}
	extraScopes := services.GoogleServiceScopeURIs(updated.Services)
	authURL, err := services.BeginGmailOAuth(updated.ID, updated.ClientName, redirectURI, updated.AllowReadAccess, extraScopes)
	if err != nil {
		return "", fmt.Errorf("saved the new request, but could not start the reconnect flow: %w. Tell the user to open the Sending accounts panel and click Reconnect themselves", err)
	}

	if event, viewErr := workspaceViewPresentation("bots", workspacePath); viewErr == nil {
		api.emitAgentProfileEvent(sessionID, event)
	}

	response := map[string]interface{}{
		"connection_id":     updated.ID,
		"display_name":      updated.DisplayName,
		"allow_read_access": updated.AllowReadAccess,
		"services":          updated.Services,
		"reconnect_url":     authURL,
		"note":              "The stored request is saved, but Google has not granted anything new yet. The user must open reconnect_url and complete Google's consent screen for this to take effect.",
	}
	encoded, encodeErr := json.Marshal(response)
	if encodeErr != nil {
		return "", fmt.Errorf("encode result: %w", encodeErr)
	}
	return string(encoded), nil
}
