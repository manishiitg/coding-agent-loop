package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// The UI and tools use the same ChannelRoute contract and owner checks. Tools
// capture their target at registration; a model cannot select another project.
func (api *StreamingAPI) slackToolTarget(ctx context.Context, workspace, profile string) (ChannelRoute, error) {
	target := ChannelRoute{WorkspacePath: workspace, ProfileID: profile}
	if profile != "" {
		raw, found, err := readFileFromWorkspace(ctx, filepath.ToSlash(filepath.Join(workspace, "product.json")))
		if err != nil || !found {
			return target, fmt.Errorf("product project manifest unavailable")
		}
		var manifest productProjectManifest
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
			return target, err
		}
		if manifest.Product != profile || manifest.ID == "" {
			return target, fmt.Errorf("product target mismatch")
		}
		target.ConversationKey = manifest.ID
	} else {
		manifest, found, err := ReadWorkflowManifest(ctx, workspace)
		if err != nil || !found {
			return target, fmt.Errorf("workflow manifest unavailable")
		}
		target.WorkflowID = manifest.ID
	}
	return target, nil
}

// toolSlackConnection resolves the bound target's effective Slack
// connection: the workflow manifest selection, or the platform default for
// anything without a selection. The returned connection is masked.
func (api *StreamingAPI) toolSlackConnection(ctx context.Context, svc *services.SlackService, workspace, profile string) (services.SlackConnection, bool) {
	if profile == "" && workspace != "" {
		if manifest, found, err := ReadWorkflowManifest(ctx, workspace); err == nil && found && manifest != nil {
			if id := strings.TrimSpace(manifest.Capabilities.SlackConnectionID); id != "" {
				return svc.GetConnection(id)
			}
		}
	}
	if id := svc.DefaultConnectionID(); id != "" {
		return svc.GetConnection(id)
	}
	return services.SlackConnection{}, false
}

// parseSlackToolTokens extracts and type-checks the optional token args.
// Empty means "not supplied" (preserve on save).
func parseSlackToolTokens(args map[string]interface{}) (botToken, appToken string, err error) {
	for key, prefix := range map[string]string{"bot_token": "xoxb-", "app_token": "xapp-"} {
		raw, present := args[key]
		if !present {
			continue
		}
		token, ok := raw.(string)
		if !ok {
			return "", "", fmt.Errorf("%s must be a string", key)
		}
		token = strings.TrimSpace(token)
		if token != "" && !strings.HasPrefix(token, prefix) {
			return "", "", fmt.Errorf("%s has the wrong token type", key)
		}
		if key == "bot_token" {
			botToken = token
		} else {
			appToken = token
		}
	}
	return botToken, appToken, nil
}

// configureWorkflowSlackConnection implements the non-admin branch of
// configure_slack_bot: a workflow owner creates or updates the connection
// scoped to their workflow, and the workflow selects it.
func (api *StreamingAPI) configureWorkflowSlackConnection(ctx context.Context, workspace, profile string, enabled bool, botToken, appToken, appName string) (string, error) {
	if profile != "" {
		return "", fmt.Errorf("profile projects use the platform default Slack connection; ask an admin to configure it")
	}
	manifest, found, err := ReadWorkflowManifest(ctx, workspace)
	if err != nil || !found || manifest == nil {
		return "", fmt.Errorf("workflow manifest unavailable")
	}
	if workflowAccessForManifest(GetUserFromContext(ctx), manifest) != WorkflowAccessOwner {
		return "", fmt.Errorf("only a workflow owner may configure this workflow's Slack connection")
	}
	svc, err := ensureSlackService()
	if err != nil {
		return "", fmt.Errorf("Slack service unavailable")
	}
	connID := strings.TrimSpace(manifest.Capabilities.SlackConnectionID)
	if connID == "" {
		// Adopt a connection previously scoped to this workflow (created
		// via the UI or an earlier call) instead of minting duplicates.
		for _, conn := range svc.ListConnections() {
			if normalizeConversationWorkspace(conn.WorkspacePath) == normalizeConversationWorkspace(workspace) {
				connID = conn.ID
				break
			}
		}
	}
	if connID == "" {
		displayName := strings.TrimSpace(appName)
		if displayName == "" {
			displayName = strings.TrimSpace(manifest.Label)
		}
		if displayName == "" {
			displayName = "Workflow Slack"
		}
		conn, err := svc.CreateSlackConnection(ctx, services.SlackConnectionInput{
			DisplayName:   displayName,
			BotToken:      botToken,
			AppToken:      appToken,
			Enabled:       enabled,
			WorkspacePath: workspace,
		})
		if err != nil {
			return "", err
		}
		connID = conn.ID
	} else {
		existing, ok := svc.GetConnection(connID)
		if !ok {
			return "", fmt.Errorf("the workflow's Slack connection is no longer registered; clear the selection in Setup > Bots and retry")
		}
		if normalizeConversationWorkspace(existing.WorkspacePath) != normalizeConversationWorkspace(workspace) {
			return "", fmt.Errorf("the workflow uses a platform-managed Slack connection; ask an admin to change it")
		}
		if _, err := svc.UpdateSlackConnection(ctx, connID, services.SlackConnectionInput{
			DisplayName:   appName,
			BotToken:      botToken,
			AppToken:      appToken,
			Enabled:       enabled,
			WorkspacePath: workspace,
		}); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(manifest.Capabilities.SlackConnectionID) != connID {
		manifest.Capabilities.SlackConnectionID = connID
		if err := WriteWorkflowManifest(ctx, workspace, manifest); err != nil {
			return "", fmt.Errorf("Slack connection saved but the workflow selection failed: %w", err)
		}
	}
	raw, _ := json.Marshal(map[string]interface{}{"saved": true, "connection_id": connID, "credentials_redacted": true})
	return string(raw), nil
}

func (api *StreamingAPI) slackRoutes(ctx context.Context) (*chathistory.BotConnectorConfig, map[string]ChannelRoute, error) {
	if api.chatStore == nil {
		return nil, nil, fmt.Errorf("bot store unavailable")
	}
	cfg, err := api.chatStore.GetBotConnectorConfig(ctx, "slack")
	if err != nil {
		return nil, nil, err
	}
	routes := map[string]ChannelRoute{}
	if cfg != nil && cfg.AllowedChannels != "" && cfg.AllowedChannels != "[]" {
		if err := json.Unmarshal([]byte(cfg.AllowedChannels), &routes); err != nil {
			return nil, nil, err
		}
	}
	return cfg, routes, nil
}

func (api *StreamingAPI) mutateSlackRoute(ctx context.Context, target ChannelRoute, operation, channel, grant string, triggerProvided ...bool) error {
	slackRouteMutationMu.Lock()
	defer slackRouteMutationMu.Unlock()
	channel = strings.TrimSpace(channel)
	if !slackChannelIDPattern.MatchString(channel) {
		return fmt.Errorf("an exact Slack channel ID is required")
	}
	if operation != "remove_slack_bot_route" && grant != "run" && grant != "owner" {
		return fmt.Errorf("bot_grant must be run or owner")
	}
	if _, err := requireSlackRouteDestinationOwner(ctx, api, target); err != nil {
		return err
	}
	cfg, routes, err := api.slackRoutes(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("configure the connector in Setup > Bots first")
	}
	old := make(map[string]ChannelRoute, len(routes))
	for k, v := range routes {
		old[k] = v
	}
	existing, found := routes[channel]
	if found && !sameSlackRouteDestination(existing, target) {
		return fmt.Errorf("channel is assigned to another target")
	}
	switch operation {
	case "create_slack_bot_route":
		if found {
			return fmt.Errorf("route already exists; use update_slack_bot_route_permission")
		}
		target.BotGrant = grant
		routes[channel] = target
	case "update_slack_bot_route_permission":
		if !found {
			return fmt.Errorf("route not found")
		}
		if target.BlockedEmails != nil {
			existing.BlockedEmails = target.BlockedEmails
		}
		if target.Trigger != nil || len(triggerProvided) > 0 && triggerProvided[0] {
			existing.Trigger = target.Trigger
		}
		existing.BotGrant = grant
		routes[channel] = existing
	case "remove_slack_bot_route":
		if !found {
			return fmt.Errorf("route not found")
		}
		delete(routes, channel)
	default:
		return fmt.Errorf("unknown route operation")
	}
	routes, err = normalizeSlackChannelRouting(routes)
	if err != nil {
		return err
	}
	if err := validateSlackRouteMutationPermissions(ctx, api, routes, old); err != nil {
		return err
	}
	encoded, err := json.Marshal(routes)
	if err != nil {
		return err
	}
	_, err = api.chatStore.UpsertBotConnectorConfig(ctx, &chathistory.CreateBotConnectorConfigRequest{
		ID: "slack", Enabled: cfg.Enabled, BotMode: cfg.BotMode, ConfigJSON: cfg.ConfigJSON, DefaultPresetID: cfg.DefaultPresetID, AutoConfirm: cfg.AutoConfirm, AllowedChannels: string(encoded),
	})
	if err == nil {
		api.revokeChangedBotSessions(routes)
	}
	return err
}

func (api *StreamingAPI) registerSlackBotTools(registrar definitionToolRegistrar, session, workspace, profile string, canMutate bool) error {
	tool := virtualtools.SlackCLIToolDefinition().Function
	raw, err := json.Marshal(tool.Parameters)
	if err != nil {
		return err
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	if err := registrar.RegisterCustomTool(tool.Name, tool.Description, schema, func(ctx context.Context, args map[string]interface{}) (string, error) {
		return api.slackCLIFromTool(context.WithValue(ctx, common.ChatSessionIDKey, session), args)
	}, "slack_bot_management"); err != nil {
		return err
	}

	register := func(name, description string, properties map[string]interface{}, required []string, execute func(context.Context, map[string]interface{}) (string, error)) error {
		return registrar.RegisterCustomTool(name, description, map[string]interface{}{"type": "object", "properties": properties, "required": required, "additionalProperties": false}, execute, "slack_bot_management")
	}
	if err := register("get_slack_bot_settings", "Inspect Slack connector state and routes for this workflow/project. Credentials are never returned. Read before and after any route change.", map[string]interface{}{}, nil, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		target, err := api.slackToolTarget(ctx, workspace, profile)
		if err != nil {
			return "", err
		}
		cfg, routes, err := api.slackRoutes(ctx)
		if err != nil {
			return "", err
		}
		scoped := map[string]ChannelRoute{}
		for channel, route := range routes {
			if sameSlackRouteDestination(route, target) {
				scoped[channel] = route
			}
		}
		svc, err := ensureSlackService()
		if err != nil {
			return "", err
		}
		config := svc.GetConfig()
		result := map[string]interface{}{"channel_routing": scoped, "enabled": false, "bot_mode": false, "bot_token_configured": false, "app_token_configured": false}
		if cfg != nil {
			result["bot_mode"] = cfg.BotMode
		}
		if config != nil {
			result["enabled"] = config.Enabled
			result["bot_token_configured"] = config.BotToken != ""
			result["app_token_configured"] = config.AppToken != ""
			result["default_connection_id"] = config.DefaultConnectionID
		}
		if conn, ok := api.toolSlackConnection(ctx, svc, workspace, profile); ok {
			result["connection"] = map[string]interface{}{
				"id":           conn.ID,
				"display_name": conn.DisplayName,
				"configured":   strings.TrimSpace(conn.BotToken) != "" && strings.TrimSpace(conn.AppToken) != "",
				"enabled":      conn.Enabled,
				"is_default":   conn.ID == svc.DefaultConnectionID(),
			}
		}
		raw, err := json.Marshal(result)
		return string(raw), err
	}); err != nil {
		return err
	}
	if err := register("test_slack_bot_connection", "Check this workflow/project's effective Slack connection: bot/app tokens and granted bot scopes. Returns passed, missing, failed, or manual checks. Event subscriptions and mention delivery require manual verification; token success alone does not verify incoming events. Credentials are never returned.", map[string]interface{}{}, nil, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack connector unavailable; check Setup > Bots")
		}
		connID := ""
		if conn, ok := api.toolSlackConnection(ctx, svc, workspace, profile); ok {
			connID = conn.ID
		}
		raw, err := json.Marshal(svc.DiagnoseConnectionFor(ctx, connID))
		return string(raw), err
	}); err != nil {
		return err
	}
	if !canMutate {
		return nil
	}

	// Reuse the UI handler so credential encryption, operator authorization,
	// route preservation, session revocation and connector activation stay shared.
	if err := register("configure_slack_bot", "Save Slack bot credentials and enable/disable the bot. Admins configure the shared default connection; a workflow owner configures this workflow's own connection (created on first use) and selects it on the workflow. Interactive users only. Omit either token to keep it unchanged. app_name sets the workflow app's display name (App name in Setup > Bots); omit to keep it unchanged. Tokens are encrypted and never returned. Channel routes are preserved. Use test_slack_bot_connection afterward.", map[string]interface{}{
		"enabled":   map[string]interface{}{"type": "boolean"},
		"bot_token": map[string]interface{}{"type": "string", "description": "New Bot User OAuth Token (xoxb-); omit to preserve"},
		"app_token": map[string]interface{}{"type": "string", "description": "New App-Level Token (xapp-); omit to preserve"},
		"app_name":  map[string]interface{}{"type": "string", "description": "Display name for this workflow's Slack app; omit to keep unchanged"},
	}, []string{"enabled"}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		claims := GetUserFromContext(ctx)
		operatorRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/slack/config", nil)
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
			return "", fmt.Errorf("only an authenticated interactive user may configure Slack credentials")
		}
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return "", fmt.Errorf("enabled is required")
		}
		botToken, appToken, err := parseSlackToolTokens(args)
		if err != nil {
			return "", err
		}
		appName := ""
		if raw, present := args["app_name"]; present {
			name, ok := raw.(string)
			if !ok {
				return "", fmt.Errorf("app_name must be a string")
			}
			appName = strings.TrimSpace(name)
		}
		if !currentUserIsAdmin(operatorRequest) {
			return api.configureWorkflowSlackConnection(ctx, workspace, profile, enabled, botToken, appToken, appName)
		}
		if appName != "" {
			return "", fmt.Errorf("app_name applies to workflow connections; the shared default keeps its name")
		}
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack service unavailable")
		}
		current := svc.GetConfig()
		request := SlackConfigRequest{Enabled: enabled, BotMode: enabled, BotToken: current.BotToken, AppToken: current.AppToken}
		if botToken != "" {
			request.BotToken = botToken
		}
		if appToken != "" {
			request.AppToken = appToken
		}
		body, err := json.Marshal(request)
		if err != nil {
			return "", fmt.Errorf("invalid Slack configuration")
		}
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/slack/config", bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("invalid Slack configuration request")
		}
		response := httptest.NewRecorder()
		updateSlackConfigHandler(api)(response, r)
		if response.Code != http.StatusOK {
			return "", fmt.Errorf("Slack configuration was not saved (HTTP %d); inspect operator permissions and Slack settings", response.Code)
		}
		return `{"saved":true,"credentials_redacted":true}`, nil
	}); err != nil {
		return err
	}
	if err := register("get_slack_bot_credentials", "Read masked Slack bot/app credentials and enablement for this workflow/project's effective connection (admins: the shared default). Interactive users only; full token values are never exposed.", map[string]interface{}{}, nil, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		claims := GetUserFromContext(ctx)
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/slack/config", nil)
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" {
			return "", fmt.Errorf("only an authenticated interactive user may inspect Slack credentials")
		}
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack service unavailable")
		}
		if currentUserIsAdmin(request) {
			raw, err := json.Marshal(svc.GetConfig())
			return string(raw), err
		}
		if profile != "" {
			return "", fmt.Errorf("profile projects use the platform default Slack connection; ask an admin to inspect it")
		}
		manifest, found, err := ReadWorkflowManifest(ctx, workspace)
		if err != nil || !found {
			return "", fmt.Errorf("workflow manifest unavailable")
		}
		if workflowAccessForManifest(GetUserFromContext(ctx), manifest) != WorkflowAccessOwner {
			return "", fmt.Errorf("only a workflow owner may inspect this workflow's Slack credentials")
		}
		conn, ok := api.toolSlackConnection(ctx, svc, workspace, profile)
		if !ok {
			return "", fmt.Errorf("no Slack connection is configured for this workflow")
		}
		raw, err := json.Marshal(projectSlackConnection(conn, svc.DefaultConnectionID()))
		return string(raw), err
	}); err != nil {
		return err
	}
	for _, name := range []string{"create_slack_bot_route", "update_slack_bot_route_permission", "remove_slack_bot_route"} {
		properties := map[string]interface{}{"channel_id": map[string]interface{}{"type": "string", "description": "Exact Slack channel ID, e.g. C1234567890"}}
		if name != "remove_slack_bot_route" {
			properties["blocked_emails"] = map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Emails excluded from this channel route. Empty array clears exclusions."}
		}
		required := []string{"channel_id"}
		if name != "remove_slack_bot_route" {
			properties["trigger"] = map[string]interface{}{"type": []string{"object", "null"}, "additionalProperties": false, "required": []string{"type"}, "properties": map[string]interface{}{
				"type":   map[string]interface{}{"type": "string", "enum": []string{"human_message", "trusted_app"}},
				"app_id": map[string]interface{}{"type": "string"}, "bot_id": map[string]interface{}{"type": "string"}, "contains": map[string]interface{}{"type": "string"},
				"match":            slackTriggerMatchSchema(),
				"payload_mappings": webhookPayloadMappingsSchema(),
				"context": map[string]interface{}{"type": "object", "additionalProperties": false, "required": []string{"limit", "lookback_minutes"}, "properties": map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100}, "lookback_minutes": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 1440}, "include_threads": map[string]interface{}{"type": "boolean"},
				}},
				"step_id": map[string]interface{}{"type": "string"}, "route_selections": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}, "group_names": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			}}
		}

		if name != "remove_slack_bot_route" {
			properties["bot_grant"] = map[string]interface{}{"type": "string", "enum": []string{"run"}}
			// Run is the default; Builder must not ask users to choose a grant.
		}
		if err := register(name, "Manage only this workflow/project's Slack route. Requires an authenticated interactive owner. Routes use run authority only; do not ask for a grant. Configure trigger on create/update: trusted app/bot source, rich JSON match, shared payload_mappings, and bounded same-channel context. trigger=null clears automation on update. Read get_slack_bot_settings before and after. Slack-origin sessions cannot change grants.", properties, required, func(ctx context.Context, args map[string]interface{}) (string, error) {
			active, _ := api.getActiveSession(session)
			if active != nil && (active.BotPlatform != "" || strings.HasPrefix(active.TriggeredBy, "bot:")) {
				return "", fmt.Errorf("bot-origin sessions cannot manage grants")
			}
			target, err := api.slackToolTarget(ctx, workspace, profile)
			if err != nil {
				return "", err
			}
			if value, present := args["blocked_emails"]; present {
				raw, err := json.Marshal(value)
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal(raw, &target.BlockedEmails); err != nil {
					return "", err
				}
			}
			_, triggerProvided := args["trigger"]
			if triggerProvided {
				raw, err := json.Marshal(args["trigger"])
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal(raw, &target.Trigger); err != nil {
					return "", err
				}
			}
			if err := api.mutateSlackRoute(ctx, target, name, stringFromRequestMap(args, "channel_id"), "run", triggerProvided); err != nil {
				return "", err
			}
			return `{"success":true}`, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func slackTriggerMatchSchema() map[string]interface{} {
	condition := map[string]interface{}{"type": "object", "additionalProperties": false, "required": []string{"source", "operator", "value"}, "properties": map[string]interface{}{
		"source":   map[string]interface{}{"type": "string", "maxLength": 256, "description": "Fixed dotted path in normalized Slack event JSON, e.g. text or message.attachments.0.title"},
		"operator": map[string]interface{}{"type": "string", "enum": []string{"equals", "contains"}}, "value": map[string]interface{}{"type": "string", "maxLength": 4096}, "case_insensitive": map[string]interface{}{"type": "boolean"},
	}}
	list := map[string]interface{}{"type": "array", "items": condition, "maxItems": 20}
	return map[string]interface{}{"type": "object", "additionalProperties": false, "properties": map[string]interface{}{"all": list, "any": list}, "description": "All conditions must match, plus at least one any condition when supplied. Maximum 20 conditions total."}
}
