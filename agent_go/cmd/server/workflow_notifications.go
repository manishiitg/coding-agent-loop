package server

import (
	"context"
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
	ScopeLabel      string                                   `json:"scope_label"`
	WorkflowLabel   string                                   `json:"workflow_label,omitempty"`
	EffectiveState  string                                   `json:"effective_state"`
	Destinations    []WorkflowNotificationDestinationInfo    `json:"destinations"`
	AccountChannels []WorkflowNotificationAccountChannelInfo `json:"account_channels"`
}

func resolveSlackNotificationState(id, label string, capabilities WorkflowCapabilities, secretValue string, secretResolved bool) WorkflowNotificationDestinationInfo {
	destination := WorkflowNotificationDestinationInfo{
		ID:    id,
		Type:  "slack_webhook",
		Label: label,
		State: workflowNotificationStateNotConfigured,
	}
	if capabilities.Notifications == nil {
		destination.Summary = "No Slack webhook selected for this scope."
		return destination
	}

	secretName := strings.TrimSpace(capabilities.Notifications.SlackWebhookSecretName)
	destination.SecretName = secretName
	if secretName == "" {
		destination.Summary = "No Slack webhook selected for this scope."
		return destination
	}

	if !secretResolved || strings.TrimSpace(secretValue) == "" {
		destination.State = workflowNotificationStateMissingSecret
		destination.Summary = "The encrypted notification secret is missing."
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

func resolveWorkflowSlackNotificationState(manifest *WorkflowManifest, secretValue string, secretResolved bool) WorkflowNotificationDestinationInfo {
	if manifest == nil {
		return resolveSlackNotificationState("workflow-slack-webhook", "Workflow Slack webhook", WorkflowCapabilities{}, secretValue, secretResolved)
	}
	return resolveSlackNotificationState("workflow-slack-webhook", "Workflow Slack webhook", manifest.Capabilities, secretValue, secretResolved)
}

func notificationAccountChannels(ctx context.Context) []WorkflowNotificationAccountChannelInfo {
	accountChannels := []WorkflowNotificationAccountChannelInfo{}
	if gmail, gmailErr := ensureGmailService(); gmailErr == nil {
		config := gmail.GetConfig()
		auth := gmail.AuthStatus(ctx)
		gmailState := "not_ready"
		gmailSummary := "Gmail is not ready at account level."
		if config.Enabled && strings.TrimSpace(config.DefaultTo) != "" && auth.Authenticated && auth.HasGmailScope {
			gmailState = "ready"
			gmailSummary = "Available as an inherited account-level channel."
		}
		accountChannels = append(accountChannels, WorkflowNotificationAccountChannelInfo{
			ID:               "gmail",
			Label:            "Gmail account channel",
			State:            gmailState,
			DefaultRecipient: config.DefaultTo,
			Summary:          gmailSummary,
		})
	}
	return accountChannels
}

// effectiveNotificationState reports whether notify_user has at least one
// usable delivery path for this scope. Account-level channels such as Gmail
// are inherited automatically; a workflow does not need a redundant Gmail
// field in workflow.json. A broken explicitly configured Slack destination
// still wins so the UI does not hide a configuration problem merely because
// an inherited fallback is available.
func effectiveNotificationState(scopeDestination WorkflowNotificationDestinationInfo, accountChannels []WorkflowNotificationAccountChannelInfo) string {
	switch scopeDestination.State {
	case workflowNotificationStateMissingSecret, workflowNotificationStateInvalidSecret:
		return scopeDestination.State
	case workflowNotificationStateReady:
		return workflowNotificationStateReady
	}
	for _, channel := range accountChannels {
		if strings.EqualFold(strings.TrimSpace(channel.State), workflowNotificationStateReady) {
			return workflowNotificationStateReady
		}
	}
	return workflowNotificationStateNotConfigured
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
		secretValue, secretResolved = api.resolveBackendNotificationSecret(r.Context(), userID, workspacePath, secretName)
	}
	slack := resolveWorkflowSlackNotificationState(manifest, secretValue, secretResolved)
	accountChannels := notificationAccountChannels(r.Context())

	response := WorkflowNotificationInfoResponse{
		Success:         true,
		Agentic:         true,
		ScopeLabel:      manifest.Label,
		WorkflowLabel:   manifest.Label,
		EffectiveState:  effectiveNotificationState(slack, accountChannels),
		Destinations:    []WorkflowNotificationDestinationInfo{slack},
		AccountChannels: accountChannels,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
