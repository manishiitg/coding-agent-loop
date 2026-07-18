package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	mcpagent "github.com/manishiitg/mcpagent/agent"
)

const chiefOfStaffNotificationLabel = "Chief of Staff"

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.TrimSpace(existing) == value {
			return values
		}
	}
	return append(values, value)
}

func withChiefNotificationConfig(caps WorkflowCapabilities, secretName string) WorkflowCapabilities {
	updated := caps
	updated.SelectedSecrets = append([]string(nil), caps.SelectedSecrets...)
	secretName = strings.TrimSpace(secretName)
	if secretName == "" {
		updated.Notifications = nil
		return updated
	}
	updated.SelectedSecrets = appendUniqueString(updated.SelectedSecrets, secretName)
	updated.Notifications = &WorkflowNotificationConfig{SlackWebhookSecretName: secretName}
	return updated
}

func (api *StreamingAPI) resolveChiefNotificationSecret(ctx context.Context, userID string, caps WorkflowCapabilities) (string, bool) {
	if caps.Notifications == nil {
		return "", false
	}
	secretName := strings.TrimSpace(caps.Notifications.SlackWebhookSecretName)
	if secretName == "" {
		return "", false
	}
	selected := api.loadSelectedSecrets(ctx, userID, "", []string{secretName})
	for _, secret := range mergeGlobalSecrets(selected, caps.SelectedGlobalSecretNames) {
		if secret.Name == secretName {
			return secret.Value, true
		}
	}
	return "", false
}

func (api *StreamingAPI) handleGetOrgNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	userID := resolveChatConfigUserID(r)
	cfg, _, err := ReadMultiAgentChatConfig(r.Context(), userID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	secretValue, secretResolved := api.resolveChiefNotificationSecret(r.Context(), userID, cfg.Capabilities)
	slack := resolveSlackNotificationState(
		"chief-of-staff-slack-webhook",
		"Chief of Staff Slack webhook",
		cfg.Capabilities,
		secretValue,
		secretResolved,
	)
	accountChannels := notificationAccountChannels(r.Context())

	response := WorkflowNotificationInfoResponse{
		Success:         true,
		Agentic:         true,
		ScopeLabel:      chiefOfStaffNotificationLabel,
		EffectiveState:  effectiveNotificationState(slack, accountChannels),
		Destinations:    []WorkflowNotificationDestinationInfo{slack},
		AccountChannels: accountChannels,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (api *StreamingAPI) persistChiefNotificationConfig(ctx context.Context, userID, secretName string) (WorkflowCapabilities, string, error) {
	if strings.TrimSpace(userID) == "" {
		userID = "default"
	}
	chatCfg, _, err := ReadMultiAgentChatConfig(ctx, userID)
	if err != nil {
		return WorkflowCapabilities{}, "", err
	}

	secretName = strings.TrimSpace(secretName)
	secretValue := ""
	if secretName != "" {
		candidateCaps := withChiefNotificationConfig(chatCfg.Capabilities, secretName)
		var found bool
		secretValue, found = api.resolveChiefNotificationSecret(ctx, userID, candidateCaps)
		if !found || strings.TrimSpace(secretValue) == "" {
			return WorkflowCapabilities{}, "", fmt.Errorf("encrypted secret %q does not exist or is not available to this Chief of Staff", secretName)
		}
		if err := services.ValidateSlackIncomingWebhookURL(secretValue); err != nil {
			return WorkflowCapabilities{}, "", fmt.Errorf("encrypted secret %q is not a valid official Slack Incoming Webhook URL", secretName)
		}
	}

	updatedChatCaps := withChiefNotificationConfig(chatCfg.Capabilities, secretName)
	if err := WriteMultiAgentChatConfig(ctx, userID, updatedChatCaps); err != nil {
		return WorkflowCapabilities{}, "", err
	}

	// Scheduled Chief-of-Staff and Org Pulse runs use the capabilities stored
	// beside their schedules, not the interactive chat defaults. Mirror only the
	// notification reference and its selected secret so unrelated schedule
	// capability choices remain untouched.
	scheduleFile, _, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil {
		return WorkflowCapabilities{}, "", fmt.Errorf("Chief of Staff chat notifications were saved, but scheduled-run config could not be read: %w", err)
	}
	scheduleFile.Capabilities = withChiefNotificationConfig(scheduleFile.Capabilities, secretName)
	if err := WriteMultiAgentSchedules(ctx, userID, scheduleFile); err != nil {
		return WorkflowCapabilities{}, "", fmt.Errorf("Chief of Staff chat notifications were saved, but scheduled-run config could not be updated: %w", err)
	}
	if api.scheduler != nil {
		for _, schedule := range MergeBuiltinSchedules(scheduleFile.Schedules) {
			if err := api.scheduler.ReloadMultiAgentSchedule(ctx, userID, schedule.ID); err != nil {
				return WorkflowCapabilities{}, "", fmt.Errorf("notification config was saved, but schedule %q could not be reloaded: %w", schedule.ID, err)
			}
		}
	}
	return updatedChatCaps, secretValue, nil
}

func (api *StreamingAPI) registerMultiAgentNotificationTool(agent *mcpagent.Agent, userID string) error {
	if agent == nil {
		return fmt.Errorf("agent is nil")
	}
	if strings.TrimSpace(userID) == "" {
		userID = "default"
	}
	return agent.RegisterCustomTool(
		"update_chief_of_staff_notifications",
		"Configure or disable the Chief of Staff Slack Incoming Webhook destination. Pass the name of an existing encrypted user secret containing an official Slack Incoming Webhook URL; never pass or expose the URL itself. This updates both interactive Chief of Staff chat and scheduled Chief/Org Pulse runs. Pass an empty secret name to disable the dedicated webhook.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"slack_webhook_secret_name": map[string]interface{}{
					"type":        "string",
					"description": "Existing encrypted secret name, or an empty string to disable the Chief of Staff Slack webhook.",
				},
			},
			"required": []string{"slack_webhook_secret_name"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			secretName, _ := args["slack_webhook_secret_name"].(string)
			_, secretValue, err := api.persistChiefNotificationConfig(ctx, userID, secretName)
			if err != nil {
				return "", err
			}

			// notificationDestinationFromQuery stores an explicit mutable pointer in
			// the tool context. Refresh it so a requested notify_user test later in
			// this same turn uses the newly saved destination immediately.
			if destination, ok := ctx.Value(virtualtools.BotNotificationDestinationKey).(*services.NotificationDestination); ok && destination != nil {
				if strings.TrimSpace(secretName) == "" {
					destination.SlackWebhook = nil
				} else {
					destination.SlackWebhook = &services.SlackWebhookDest{
						SecretName: strings.TrimSpace(secretName),
						URL:        secretValue,
					}
				}
			}

			if strings.TrimSpace(secretName) == "" {
				return "Chief of Staff Slack webhook notifications are disabled for interactive and scheduled runs.", nil
			}
			return fmt.Sprintf("Chief of Staff Slack webhook notifications are ready for interactive and scheduled runs using encrypted secret %q. The webhook value was not exposed.", strings.TrimSpace(secretName)), nil
		},
		"notification_tools",
	)
}
