package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// listSecretsTool lets the parent agent see WHICH credentials are stored —
// names only, never values (mirrors AgentWorks' own secrets convention: the
// model references a secret by name in a shell command via its injected
// $SECRET_<NAME> env var, but must never see or print the raw value itself).
func listSecretsTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "list_secrets",
		Description: "List the names of credentials the parent has saved (e.g. a school portal login) — names only, never " +
			"values. Use a listed name's env var form ($SECRET_<NAME>, given back as `env`) inside execute_shell_command to " +
			"actually use it — never ask the parent to repeat a value that's already saved, and never print, cat, or echo " +
			"that env var's value anywhere a reply or file could show it.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: func(_ context.Context, _ map[string]interface{}) (string, error) {
			names := listSecretNames()
			out := make([]string, 0, len(names))
			for _, n := range names {
				out = append(out, fmt.Sprintf(`{"name":%q,"env":%q}`, n, secretEnvName(n)))
			}
			return fmt.Sprintf(`{"secrets":[%s]}`, strings.Join(out, ",")), nil
		},
	}
}

// setSecretTool saves a credential the parent stated directly in chat (e.g.
// "remember my school portal password is X"). The value is encrypted at rest
// immediately; onSet lets the caller (handleParentMessage) track it so it can
// be redacted from the persisted chat transcript right after this turn — and
// it is never echoed back in this tool's own result either, so it never
// re-enters the model's context after the moment the parent typed it.
func setSecretTool(onSet func(name, value string)) agentsession.Tool {
	return agentsession.Tool{
		Name: "set_secret",
		Description: "Save a credential the parent just told you directly (e.g. a school portal username/password) so your " +
			"tools can use it later without them repeating it. Only call this when the parent explicitly states a value to " +
			"remember — never invent or guess one. Give it a short, clear name (e.g. \"school portal password\"); save " +
			"multiple related values (username + password) as separate named secrets, not one combined value.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":  map[string]interface{}{"type": "string", "description": "short label, e.g. \"school portal password\""},
				"value": map[string]interface{}{"type": "string", "description": "the actual credential value"},
			},
			"required": []string{"name", "value"},
		},
		Handler: func(_ context.Context, args map[string]interface{}) (string, error) {
			name, _ := args["name"].(string)
			value, _ := args["value"].(string)
			name, value = strings.TrimSpace(name), strings.TrimSpace(value)
			if name == "" || value == "" {
				return "", fmt.Errorf("name and value are required")
			}
			if err := setSecretValue(name, value); err != nil {
				return "", fmt.Errorf("failed to save secret: %w", err)
			}
			if onSet != nil {
				onSet(name, value)
			}
			return fmt.Sprintf(`{"status":"ok","name":%q,"env":%q}`, name, secretEnvName(name)), nil
		},
	}
}

// retroactivelyRedactStoredConversation reloads a persisted conversation and
// rewrites it with the given (just-learned) secret values redacted from every
// message. Needed because a NEW secret set mid-turn via set_secret couldn't
// have been redacted by the turn's own early "kickoff" persist (see
// persistNewMessages in chat.go), which necessarily ran before the tool call
// happened — this closes that brief window immediately after the turn ends,
// rather than leaving the raw value on disk indefinitely.
func retroactivelyRedactStoredConversation(scope, id string, values []string) {
	if len(values) == 0 {
		return
	}
	convFileMu.Lock()
	defer convFileMu.Unlock()
	existing, ok := loadStoredConversation(scope, id)
	if !ok {
		return
	}
	changed := false
	for i, m := range existing.Messages {
		red := redactSecrets(m.Text, values)
		if red != m.Text {
			existing.Messages[i].Text = red
			changed = true
		}
	}
	if changed {
		persistConversation(scope, id, existing.Messages)
	}
}
