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

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
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

func (api *StreamingAPI) mutateSlackRoute(ctx context.Context, target ChannelRoute, operation, channel, grant string) error {
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
		}
		raw, err := json.Marshal(result)
		return string(raw), err
	}); err != nil {
		return err
	}
	if err := register("test_slack_bot_connection", "Check Slack bot/app tokens and granted bot scopes. Returns passed, missing, failed, or manual checks. Event subscriptions and mention delivery require manual verification; token success alone does not verify incoming events. Credentials are never returned.", map[string]interface{}{}, nil, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack connector unavailable; check Setup > Bots")
		}
		raw, err := json.Marshal(svc.DiagnoseConnection(ctx))
		return string(raw), err
	}); err != nil {
		return err
	}
	if !canMutate {
		return nil
	}

	// Reuse the UI handler so credential encryption, operator authorization,
	// route preservation, session revocation and connector activation stay shared.
	if err := register("configure_slack_bot", "Save shared Slack bot credentials and enable/disable the bot. Interactive operators only. Omit either token to keep it unchanged. Tokens are encrypted and never returned. Channel routes are preserved. Use test_slack_bot_connection afterward.", map[string]interface{}{
		"enabled":   map[string]interface{}{"type": "boolean"},
		"bot_token": map[string]interface{}{"type": "string", "description": "New Bot User OAuth Token (xoxb-); omit to preserve"},
		"app_token": map[string]interface{}{"type": "string", "description": "New App-Level Token (xapp-); omit to preserve"},
	}, []string{"enabled"}, func(ctx context.Context, args map[string]interface{}) (string, error) {
		claims := GetUserFromContext(ctx)
		operatorRequest, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/api/slack/config", nil)
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" || !currentUserIsAdmin(operatorRequest) {
			return "", fmt.Errorf("only an authenticated interactive operator may configure Slack credentials")
		}
		enabled, ok := args["enabled"].(bool)
		if !ok {
			return "", fmt.Errorf("enabled is required")
		}
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack service unavailable")
		}
		current := svc.GetConfig()
		request := SlackConfigRequest{Enabled: enabled, BotMode: enabled, BotToken: current.BotToken, AppToken: current.AppToken}
		for key, target := range map[string]*string{"bot_token": &request.BotToken, "app_token": &request.AppToken} {
			if raw, present := args[key]; present {
				token, ok := raw.(string)
				if !ok {
					return "", fmt.Errorf("%s must be a string", key)
				}
				*target = strings.TrimSpace(token)
				prefix := "xoxb-"
				if key == "app_token" {
					prefix = "xapp-"
				}
				if *target != "" && !strings.HasPrefix(*target, prefix) {
					return "", fmt.Errorf("%s has the wrong token type", key)
				}
			}
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
	if err := register("get_slack_bot_credentials", "Read masked Slack bot/app credentials and enablement. Interactive operators only; full token values are never exposed.", map[string]interface{}{}, nil, func(ctx context.Context, _ map[string]interface{}) (string, error) {
		claims := GetUserFromContext(ctx)
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/slack/config", nil)
		if claims == nil || claims.Provider == "bot_route" || claims.BotRouteGrant != "" || !currentUserIsAdmin(request) {
			return "", fmt.Errorf("only an authenticated interactive operator may inspect Slack credentials")
		}
		svc, err := ensureSlackService()
		if err != nil {
			return "", fmt.Errorf("Slack service unavailable")
		}
		raw, err := json.Marshal(svc.GetConfig())
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
		if name == "create_slack_bot_route" {
			properties["trigger"] = map[string]interface{}{"type": "object", "additionalProperties": false, "required": []string{"type"}, "properties": map[string]interface{}{
				"type":   map[string]interface{}{"type": "string", "enum": []string{"human_message", "trusted_app"}},
				"app_id": map[string]interface{}{"type": "string"}, "bot_id": map[string]interface{}{"type": "string"}, "contains": map[string]interface{}{"type": "string"},
				"step_id": map[string]interface{}{"type": "string"}, "route_selections": map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}}, "group_names": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			}}
		}

		if name != "remove_slack_bot_route" {
			properties["bot_grant"] = map[string]interface{}{"type": "string", "enum": []string{"run"}}
			// Run is the default; Builder must not ask users to choose a grant.
		}
		if err := register(name, "Manage only this workflow/project's Slack route. Requires an authenticated interactive owner. Routes use run authority only; do not ask for a grant. Read get_slack_bot_settings before and after. Slack-origin sessions cannot change grants.", properties, required, func(ctx context.Context, args map[string]interface{}) (string, error) {
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
			if name == "create_slack_bot_route" && args["trigger"] != nil {
				raw, err := json.Marshal(args["trigger"])
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal(raw, &target.Trigger); err != nil {
					return "", err
				}
			}
			if err := api.mutateSlackRoute(ctx, target, name, stringFromRequestMap(args, "channel_id"), "run"); err != nil {
				return "", err
			}
			return `{"success":true}`, nil
		}); err != nil {
			return err
		}
	}
	return nil
}
