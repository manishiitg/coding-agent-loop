package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
)

const (
	workflowNotificationStateNotConfigured = "not_configured"
	workflowNotificationStateMissingSecret = "missing_secret"
	workflowNotificationStateInvalidSecret = "invalid_secret"
	workflowNotificationStateReady         = "ready"
)

type WorkflowNotificationDestinationInfo struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Label      string `json:"label"`
	State      string `json:"state"`
	SecretName string `json:"secret_name,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

type WorkflowNotificationAccountChannelInfo struct {
	ID               string `json:"id"`
	Label            string `json:"label"`
	State            string `json:"state"`
	DefaultRecipient string `json:"default_recipient,omitempty"`
	Summary          string `json:"summary,omitempty"`
}

type WorkflowNotificationInfoResponse struct {
	Success         bool                                     `json:"success"`
	Agentic         bool                                     `json:"agentic"`
	WorkflowLabel   string                                   `json:"workflow_label"`
	EffectiveState  string                                   `json:"effective_state"`
	Destinations    []WorkflowNotificationDestinationInfo    `json:"destinations"`
	AccountChannels []WorkflowNotificationAccountChannelInfo `json:"account_channels"`
}

func resolveWorkflowSlackNotificationState(manifest *WorkflowManifest, secretValue string, secretResolved bool) WorkflowNotificationDestinationInfo {
	destination := WorkflowNotificationDestinationInfo{
		ID:    "workflow-slack-webhook",
		Type:  "slack_webhook",
		Label: "Workflow Slack webhook",
		State: workflowNotificationStateNotConfigured,
	}
	if manifest == nil || manifest.Capabilities.Notifications == nil {
		destination.Summary = "No workflow-specific webhook selected."
		return destination
	}

	secretName := strings.TrimSpace(manifest.Capabilities.Notifications.SlackWebhookSecretName)
	destination.SecretName = secretName
	if secretName == "" {
		destination.Summary = "No workflow-specific webhook selected."
		return destination
	}

	selected := false
	for _, name := range manifest.Capabilities.SelectedSecrets {
		if strings.TrimSpace(name) == secretName {
			selected = true
			break
		}
	}
	if !selected || !secretResolved || strings.TrimSpace(secretValue) == "" {
		destination.State = workflowNotificationStateMissingSecret
		destination.Summary = "The selected encrypted secret is missing or detached."
		return destination
	}
	if err := services.ValidateSlackIncomingWebhookURL(secretValue); err != nil {
		destination.State = workflowNotificationStateInvalidSecret
		destination.Summary = "The encrypted secret is not a valid official Slack Incoming Webhook URL."
		return destination
	}

	destination.State = workflowNotificationStateReady
	destination.Summary = "notify_user calls are delivered here automatically by the backend."
	return destination
}

func (api *StreamingAPI) handleGetWorkflowNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	manifest, found, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	secretName := ""
	if manifest.Capabilities.Notifications != nil {
		secretName = strings.TrimSpace(manifest.Capabilities.Notifications.SlackWebhookSecretName)
	}
	secretValue := ""
	secretResolved := false
	if secretName != "" {
		userID := GetUserIDFromContext(r.Context())
		resolved := api.loadSelectedSecrets(r.Context(), userID, workspacePath, []string{secretName})
		for _, secret := range mergeGlobalSecrets(resolved, manifest.Capabilities.SelectedGlobalSecretNames) {
			if secret.Name == secretName {
				secretValue = secret.Value
				secretResolved = true
				break
			}
		}
	}
	slack := resolveWorkflowSlackNotificationState(manifest, secretValue, secretResolved)

	accountChannels := []WorkflowNotificationAccountChannelInfo{}
	if gmail, gmailErr := ensureGmailService(); gmailErr == nil {
		config := gmail.GetConfig()
		auth := gmail.AuthStatus(r.Context())
		gmailState := "not_ready"
		gmailSummary := "Gmail is not ready at account level."
		if config.Enabled && strings.TrimSpace(config.DefaultTo) != "" && auth.Authenticated && auth.HasGmailScope {
			gmailState = "ready"
			gmailSummary = "Available to this workflow as an inherited account-level channel."
		}
		accountChannels = append(accountChannels, WorkflowNotificationAccountChannelInfo{
			ID:               "gmail",
			Label:            "Gmail account channel",
			State:            gmailState,
			DefaultRecipient: config.DefaultTo,
			Summary:          gmailSummary,
		})
	}

	response := WorkflowNotificationInfoResponse{
		Success:         true,
		Agentic:         true,
		WorkflowLabel:   manifest.Label,
		EffectiveState:  slack.State,
		Destinations:    []WorkflowNotificationDestinationInfo{slack},
		AccountChannels: accountChannels,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
