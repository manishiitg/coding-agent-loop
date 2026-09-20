package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	mcpexecutor "github.com/manishiitg/mcpagent/executor"
)

var slackReadParameters = map[string][]string{
	"conversations.history": {"cursor", "limit", "oldest", "latest", "inclusive"},
	"conversations.replies": {"ts", "cursor", "limit", "oldest", "latest", "inclusive"},
	"conversations.info":    {"include_locale", "include_num_members"},
	"reactions.get":         {"timestamp", "full"}, "pins.list": {}, "bookmarks.list": {},
}

func validateSlackCLIRead(method, channel string, parameters map[string]interface{}) (map[string]interface{}, error) {
	keys, ok := slackReadParameters[method]
	if !ok {
		return nil, fmt.Errorf("Slack method %s is not supported by this tool; search requires separate search permissions and token support", method)
	}
	allowed := map[string]bool{"channel": true}
	for _, key := range keys {
		allowed[key] = true
	}
	args := map[string]interface{}{"channel": channel}
	for key, value := range parameters {
		if !allowed[key] {
			return nil, fmt.Errorf("unsupported Slack parameter %s", key)
		}
		if key == "channel" {
			if value != channel {
				return nil, fmt.Errorf("cross-channel Slack access denied")
			}
			continue
		}
		switch value.(type) {
		case string, bool, float64, int, json.Number:
		default:
			return nil, fmt.Errorf("Slack parameters must be scalar values")
		}
		args[key] = value
	}
	if required := map[string]string{"conversations.replies": "ts", "reactions.get": "timestamp"}[method]; required != "" {
		if value, ok := args[required].(string); !ok || value == "" {
			return nil, fmt.Errorf("%s requires %s", method, required)
		}
	}
	if method == "conversations.history" || method == "conversations.replies" {
		limit := 100.
		if raw, ok := args["limit"]; ok {
			switch v := raw.(type) {
			case float64:
				limit = v
			case int:
				limit = float64(v)
			default:
				return nil, fmt.Errorf("limit must be an integer")
			}
		}
		if limit < 1 || limit > 100 || limit != float64(int(limit)) {
			return nil, fmt.Errorf("limit must be an integer from 1 to 100")
		}
		args["limit"] = int(limit)
	}
	return args, nil
}
func (api *StreamingAPI) slackCLIFromTool(ctx context.Context, args map[string]interface{}) (string, error) {
	channel := stringFromRequestMap(args, "route_id")
	method := stringFromRequestMap(args, "method")
	if !slackChannelIDPattern.MatchString(channel) {
		return "", fmt.Errorf("an exact configured route_id is required")
	}
	parameters := map[string]interface{}{}
	if raw, ok := args["parameters"]; ok {
		var valid bool
		parameters, valid = raw.(map[string]interface{})
		if !valid {
			return "", fmt.Errorf("parameters must be a JSON object")
		}
	}
	if method == "chat.postMessage" {
		for key := range parameters {
			if key != "channel" && key != "text" {
				return "", fmt.Errorf("tracked sends support text only; use thread_ref for a thread")
			}
		}
		if supplied, ok := parameters["channel"]; ok && supplied != channel {
			return "", fmt.Errorf("cross-channel Slack send denied")
		}
		return api.sendSlackMessageFromTool(ctx, map[string]interface{}{"route_id": channel, "message": parameters["text"], "thread_ref": args["thread_ref"], "idempotency_key": args["idempotency_key"]})
	}
	parameters, err := validateSlackCLIRead(method, channel, parameters)
	if err != nil {
		return "", err
	}
	session, _ := ctx.Value(common.ChatSessionIDKey).(string)
	if session == "" {
		session = mcpexecutor.SessionIDFromContext(ctx)
	}
	if session == "" {
		return "", fmt.Errorf("authenticated session required")
	}
	cfg, routes, err := api.slackRoutes(ctx)
	if err != nil {
		return "", err
	}
	route, found := routes[channel]
	if cfg == nil || !cfg.Enabled || !cfg.BotMode || !found || (route.BotGrant != "run" && route.BotGrant != "owner") {
		return "", fmt.Errorf("Slack route is inactive or revoked")
	}
	if _, err = api.authorizeSlackToolRoute(ctx, session, channel, route); err != nil {
		return "", err
	}
	return services.RunSlackCLIOnConnection(ctx, slackToolConnectionID(ctx, api, session, route), method, parameters)
}
