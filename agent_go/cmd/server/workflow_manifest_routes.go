package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	stepworkflow "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowkb"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
)

// setCORS sets permissive CORS headers shared across workflow/manifest/config route handlers.
func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// --- List / Discovery ---

// handleListWorkflowManifests returns all workflows discovered from workspace manifests.
func (api *StreamingAPI) handleListWorkflowManifests(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	discovered, err := DiscoverWorkflowManifests(r.Context())
	if err != nil {
		log.Printf("[MANIFEST] Error discovering workflows: %v", err)
		http.Error(w, fmt.Sprintf("Failed to discover workflows: %v", err), http.StatusInternalServerError)
		return
	}
	discovered = filterWorkflowManifestsForUser(GetUserFromContext(r.Context()), discovered)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"workflows": discovered,
		"total":     len(discovered),
	})
}

// --- Get single manifest ---

// handleGetWorkflowManifest returns the manifest for a specific workspace.
func (api *StreamingAPI) handleGetWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace", http.StatusNotFound)
		return
	}
	if workflowAccessForManifest(GetUserFromContext(r.Context()), manifest) == WorkflowAccessNone {
		writeWorkflowPermissionDenied(w, "read")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": workspacePath,
	})
}

// --- Create workflow with manifest ---

type CreateWorkflowManifestRequest struct {
	Label                     string                     `json:"label"`
	WorkspacePath             string                     `json:"workspace_path"`
	Capabilities              *WorkflowCapabilities      `json:"capabilities,omitempty"`
	ExecutionDefaults         *WorkflowExecutionDefaults `json:"execution_defaults,omitempty"`
	HumanVerificationRequired bool                       `json:"human_verification_required"`
}

func enforceDeploymentBrowserCapability(capabilities *WorkflowCapabilities) error {
	if capabilities == nil || browser.CDPEnabled() {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(capabilities.BrowserMode), "cdp") {
		return fmt.Errorf("CDP is disabled for this server deployment; use browser_mode auto, headless, or none")
	}
	// CDP ports have no meaning on a deployment that cannot use CDP. Clearing
	// legacy candidates lets a user move an old workflow to auto/headless in a
	// single save instead of being trapped by stale configuration.
	capabilities.CDPPorts = nil
	return nil
}

func (api *StreamingAPI) handleCreateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req CreateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.Label == "" {
		http.Error(w, "label is required", http.StatusBadRequest)
		return
	}
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if err := enforceDeploymentBrowserCapability(req.Capabilities); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if manifest already exists
	_, exists, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check existing manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if exists {
		http.Error(w, "workflow.json already exists at this workspace path", http.StatusConflict)
		return
	}

	// Build manifest
	manifest := NewWorkflowManifest(req.Label)
	manifest.CreatedBy = GetUserIDFromContext(r.Context())
	if manifest.CreatedBy != "" {
		manifest.Access = &WorkflowAccess{Owners: []string{manifest.CreatedBy}, Readers: []string{}}
	}
	if req.Capabilities != nil {
		manifest.Capabilities = *req.Capabilities
	}
	if req.ExecutionDefaults != nil {
		manifest.ExecutionDefs = *req.ExecutionDefaults
	}

	// Write manifest
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": req.WorkspacePath,
	})
}

// --- Update manifest ---

type UpdateWorkflowManifestRequest struct {
	KnowledgebaseSources *[]workflowtypes.KnowledgebaseSource         `json:"knowledgebase_sources,omitempty"`
	WorkspacePath        string                                       `json:"workspace_path"`
	Label                *string                                      `json:"label,omitempty"`
	Capabilities         *WorkflowCapabilities                        `json:"capabilities,omitempty"`
	ExecutionDefaults    *WorkflowExecutionDefaults                   `json:"execution_defaults,omitempty"`
	Schedules            *[]WorkflowSchedule                          `json:"schedules,omitempty"`
	WorkshopMode         *string                                      `json:"workshop_mode,omitempty"` // Standalone patch — avoids zeroing out other ExecutionDefaults fields
	RunRetentionCount    *int                                         `json:"run_retention_count,omitempty"`
	FolderAccess         *[]workflowtypes.WorkflowFolderGrant         `json:"folder_access,omitempty"`
	FolderAccessRequests *[]workflowtypes.WorkflowFolderAccessRequest `json:"folder_access_requests,omitempty"`
	PulseEnabled         *bool                                        `json:"pulse_enabled,omitempty"`
	// Notification instruction fields are standalone patches so the Notify
	// popup can update content guidance without replacing workflow capabilities.
	RunNotificationInstructions   *string   `json:"run_notification_instructions,omitempty"`
	PulseNotificationInstructions *string   `json:"pulse_notification_instructions,omitempty"`
	RunNotificationChannels       *[]string `json:"run_notification_channels,omitempty"`
	PulseNotificationChannels     *[]string `json:"pulse_notification_channels,omitempty"`
	// Recipient lists are pointers so an explicitly sent empty array clears them
	// (back to the account default) while omission leaves them untouched.
	RunNotificationRecipients   *[]string `json:"run_notification_recipients,omitempty"`
	PulseNotificationRecipients *[]string `json:"pulse_notification_recipients,omitempty"`
	// Per-summary Gmail senders. Pointers so an omitted field means "leave
	// unchanged" while an explicit "" means "clear it and inherit the default".
	RunNotificationGmailConnectionIDs   *[]string `json:"run_notification_gmail_connection_ids,omitempty"`
	PulseNotificationGmailConnectionIDs *[]string `json:"pulse_notification_gmail_connection_ids,omitempty"`
	// NotificationInstructions is retained for older clients that still send a
	// single preference. New clients should use the two scoped fields above.
	NotificationInstructions *string `json:"notification_instructions,omitempty"`
}

func mergeWorkflowCapabilitiesUpdate(existing WorkflowCapabilities, incoming *WorkflowCapabilities) WorkflowCapabilities {
	if incoming == nil {
		return existing
	}
	updated := *incoming
	// Older frontend builds replace the full capabilities object and do not
	// know about workflow notifications. Preserve the existing notification
	// reference unless the caller explicitly sends that newer field.
	if updated.Notifications == nil {
		updated.Notifications = existing.Notifications
	} else if strings.TrimSpace(updated.Notifications.SlackWebhookSecretName) == "" &&
		strings.TrimSpace(updated.Notifications.RunSummaryInstructions) == "" &&
		strings.TrimSpace(updated.Notifications.PulseSummaryInstructions) == "" &&
		strings.TrimSpace(updated.Notifications.Instructions) == "" &&
		len(updated.Notifications.RunSummaryChannels) == 0 &&
		len(updated.Notifications.PulseSummaryChannels) == 0 &&
		len(updated.Notifications.ExcludeChannels) == 0 &&
		len(updated.Notifications.BlockRecipients) == 0 &&
		strings.TrimSpace(updated.Notifications.GmailConnectionID) == "" &&
		len(updated.Notifications.RunSummaryGmailConnectionIDs) == 0 &&
		len(updated.Notifications.PulseSummaryGmailConnectionIDs) == 0 &&
		len(updated.Notifications.RunSummaryRecipients) == 0 &&
		len(updated.Notifications.PulseSummaryRecipients) == 0 &&
		len(updated.Notifications.RunSummarySlackWebhookSecretNames) == 0 &&
		len(updated.Notifications.PulseSummarySlackWebhookSecretNames) == 0 {
		// An explicitly supplied empty object (no webhook, no exclude/block
		// preferences) disables the workflow-specific notification config.
		updated.Notifications = nil
	}
	if updated.Notifications != nil {
		updated.SelectedSecrets = removeString(updated.SelectedSecrets, updated.Notifications.SlackWebhookSecretName)
		if updated.SelectedGlobalSecretNames != nil {
			filtered := removeString(*updated.SelectedGlobalSecretNames, updated.Notifications.SlackWebhookSecretName)
			updated.SelectedGlobalSecretNames = &filtered
		}
	}
	return updated
}

func setWorkflowPulseEnabled(manifest *WorkflowManifest, enabled bool) {
	if manifest.Pulse == nil {
		manifest.Pulse = &WorkflowPulseConfig{}
	}
	manifest.Pulse.Enabled = enabled
	// The old model stored Pulse as an independent recurring schedule. Once the
	// explicit flag is saved, remove those obsolete cron entries so only normal
	// scheduled runs can trigger recurring Pulse.
	schedules := manifest.Schedules[:0]
	for _, schedule := range manifest.Schedules {
		if !schedule.PulseReviewOnly {
			schedules = append(schedules, schedule)
		}
	}
	manifest.Schedules = schedules
}

func (api *StreamingAPI) handleUpdateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req UpdateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if err := enforceDeploymentBrowserCapability(req.Capabilities); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Read existing manifest
	manifest, exists, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}
	if level := workflowAccessForManifest(GetUserFromContext(r.Context()), manifest); level != WorkflowAccessOwner && level != WorkflowAccessWrite {
		writeWorkflowPermissionDenied(w, "owner")
		return
	}
	if req.KnowledgebaseSources != nil {
		if err := workflowkb.Validate(stepworkflow.GetPromptDocsRoot(), req.WorkspacePath, *req.KnowledgebaseSources); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		manifest.KnowledgebaseSources = append([]workflowtypes.KnowledgebaseSource{}, (*req.KnowledgebaseSources)...)
	}
	previousFolderAccess := append([]workflowtypes.WorkflowFolderGrant(nil), manifest.FolderAccess...)

	// Apply partial updates
	if req.Label != nil {
		manifest.Label = *req.Label
	}
	if req.Capabilities != nil {
		manifest.Capabilities = mergeWorkflowCapabilitiesUpdate(manifest.Capabilities, req.Capabilities)
	}
	if req.ExecutionDefaults != nil {
		manifest.ExecutionDefs = *req.ExecutionDefaults
	}
	if req.Schedules != nil {
		manifest.Schedules = *req.Schedules
	}
	if req.WorkshopMode != nil {
		manifest.ExecutionDefs.WorkshopMode = *req.WorkshopMode
	}
	if req.RunRetentionCount != nil {
		manifest.RunRetentionCount = req.RunRetentionCount
	}
	if req.FolderAccess != nil {
		normalized, normalizeErr := normalizeWorkflowFolderGrants(*req.FolderAccess, manifest.FolderAccess)
		if normalizeErr != nil {
			http.Error(w, normalizeErr.Error(), http.StatusBadRequest)
			return
		}
		manifest.FolderAccess = normalized
	}
	if req.FolderAccessRequests != nil {
		manifest.FolderAccessRequests = append([]workflowtypes.WorkflowFolderAccessRequest(nil), (*req.FolderAccessRequests)...)
	}
	if req.PulseEnabled != nil {
		setWorkflowPulseEnabled(manifest, *req.PulseEnabled)
	}
	if req.RunNotificationInstructions != nil || req.PulseNotificationInstructions != nil ||
		req.RunNotificationChannels != nil || req.PulseNotificationChannels != nil ||
		req.RunNotificationRecipients != nil || req.PulseNotificationRecipients != nil ||
		req.RunNotificationGmailConnectionIDs != nil || req.PulseNotificationGmailConnectionIDs != nil {
		runInstructions := ""
		pulseInstructions := ""
		if manifest.Capabilities.Notifications != nil {
			runInstructions = manifest.Capabilities.Notifications.EffectiveRunSummaryInstructions()
			pulseInstructions = manifest.Capabilities.Notifications.EffectivePulseSummaryInstructions()
		}
		if req.RunNotificationInstructions != nil {
			runInstructions = strings.TrimSpace(*req.RunNotificationInstructions)
		}
		if req.PulseNotificationInstructions != nil {
			pulseInstructions = strings.TrimSpace(*req.PulseNotificationInstructions)
		}
		if manifest.Capabilities.Notifications == nil && (runInstructions != "" || pulseInstructions != "" ||
			req.RunNotificationChannels != nil || req.PulseNotificationChannels != nil ||
			req.RunNotificationRecipients != nil || req.PulseNotificationRecipients != nil ||
			req.RunNotificationGmailConnectionIDs != nil || req.PulseNotificationGmailConnectionIDs != nil) {
			manifest.Capabilities.Notifications = &WorkflowNotificationConfig{}
		}
		if manifest.Capabilities.Notifications != nil {
			manifest.Capabilities.Notifications.RunSummaryInstructions = runInstructions
			manifest.Capabilities.Notifications.PulseSummaryInstructions = pulseInstructions
			manifest.Capabilities.Notifications.Instructions = ""
			if req.RunNotificationChannels != nil {
				manifest.Capabilities.Notifications.RunSummaryChannels = normalizeNotificationChannels(*req.RunNotificationChannels)
			}
			if req.PulseNotificationChannels != nil {
				manifest.Capabilities.Notifications.PulseSummaryChannels = normalizeNotificationChannels(*req.PulseNotificationChannels)
			}
			if req.RunNotificationRecipients != nil {
				manifest.Capabilities.Notifications.RunSummaryRecipients = normalizeNotificationRecipients(*req.RunNotificationRecipients)
			}
			if req.PulseNotificationRecipients != nil {
				manifest.Capabilities.Notifications.PulseSummaryRecipients = normalizeNotificationRecipients(*req.PulseNotificationRecipients)
			}
			if req.RunNotificationGmailConnectionIDs != nil {
				manifest.Capabilities.Notifications.RunSummaryGmailConnectionIDs = normalizeGmailConnectionIDs(*req.RunNotificationGmailConnectionIDs)
			}
			if req.PulseNotificationGmailConnectionIDs != nil {
				manifest.Capabilities.Notifications.PulseSummaryGmailConnectionIDs = normalizeGmailConnectionIDs(*req.PulseNotificationGmailConnectionIDs)
			}
		}
	} else if req.NotificationInstructions != nil {
		instructions := strings.TrimSpace(*req.NotificationInstructions)
		if manifest.Capabilities.Notifications == nil && instructions != "" {
			manifest.Capabilities.Notifications = &WorkflowNotificationConfig{}
		}
		if manifest.Capabilities.Notifications != nil {
			manifest.Capabilities.Notifications.Instructions = instructions
		}
	}

	// Write updated manifest
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if req.FolderAccess != nil {
		previousRoots := make([]string, 0, len(previousFolderAccess))
		for _, grant := range previousFolderAccess {
			previousRoots = append(previousRoots, grant.Path)
		}
		readRoots := make([]string, 0, len(manifest.FolderAccess))
		writeRoots := make([]string, 0, len(manifest.FolderAccess))
		readOnlyRoots := make([]string, 0, len(manifest.FolderAccess))
		folderEnv := make(map[string]string, len(manifest.FolderAccess))
		for _, grant := range manifest.FolderAccess {
			readRoots = append(readRoots, grant.Path)
			if grant.CanWrite() {
				writeRoots = append(writeRoots, grant.Path)
			} else {
				readOnlyRoots = append(readOnlyRoots, grant.Path)
			}
			folderEnv["WORKFLOW_FOLDER_"+workflowFolderAliasEnvKey(grant.Alias)] = grant.Path
		}
		common.ReconcileSessionWorkflowFolderAccess(req.WorkspacePath, previousRoots, readRoots, writeRoots, readOnlyRoots, folderEnv)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       manifest,
		"workspace_path": req.WorkspacePath,
	})
}

func normalizeWorkflowFolderGrants(requested, previous []workflowtypes.WorkflowFolderGrant) ([]workflowtypes.WorkflowFolderGrant, error) {
	previousByID := make(map[string]workflowtypes.WorkflowFolderGrant, len(previous))
	for _, grant := range previous {
		previousByID[grant.ID] = grant
	}
	now := time.Now().UTC().Format(time.RFC3339)
	normalized := make([]workflowtypes.WorkflowFolderGrant, 0, len(requested))
	for i, grant := range requested {
		canonical, err := filepath.EvalSymlinks(filepath.Clean(strings.TrimSpace(grant.Path)))
		if err != nil {
			return nil, fmt.Errorf("folder_access[%d] is unavailable: %w", i, err)
		}
		info, err := os.Stat(canonical)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("folder_access[%d] must reference an existing directory", i)
		}
		grant.Path = filepath.Clean(canonical)
		grant.ID = strings.TrimSpace(grant.ID)
		grant.Alias = strings.TrimSpace(grant.Alias)
		grant.Access = strings.TrimSpace(grant.Access)
		grant.Reason = strings.TrimSpace(grant.Reason)
		if prior, exists := previousByID[grant.ID]; exists && strings.TrimSpace(prior.CreatedAt) != "" {
			grant.CreatedAt = prior.CreatedAt
		} else {
			grant.CreatedAt = now
		}
		grant.UpdatedAt = now
		normalized = append(normalized, grant)
	}
	return normalized, nil
}

func normalizeNotificationChannels(channels []string) []string {
	allowed := map[string]bool{"gmail": true, "slack": true, "whatsapp": true}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(channels))
	for _, raw := range channels {
		channel := strings.ToLower(strings.TrimSpace(raw))
		if !allowed[channel] || seen[channel] {
			continue
		}
		seen[channel] = true
		normalized = append(normalized, channel)
	}
	return normalized
}

// normalizeNotificationRecipients cleans a saved recipient list: one address
// per entry, lowercased and de-duplicated. Callers may paste "a@x.com, b@x.com"
// into a single entry, so separators are split the same way the Gmail connector
// splits them at send time — otherwise a pasted list would be stored as one
// malformed address and silently fail to deliver.
func normalizeNotificationRecipients(recipients []string) []string {
	seen := map[string]bool{}
	normalized := make([]string, 0, len(recipients))
	for _, raw := range recipients {
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			email := strings.ToLower(strings.TrimSpace(part))
			// A bare token with no "@" is not an address. Dropping it keeps a
			// typo from becoming a recipient that never receives anything.
			if email == "" || seen[email] || !strings.Contains(email, "@") {
				continue
			}
			seen[email] = true
			normalized = append(normalized, email)
		}
	}
	return normalized
}

// --- Delete manifest ---

func (api *StreamingAPI) handleDeleteWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := r.URL.Query().Get("workspace_path")
	if workspacePath == "" {
		http.Error(w, "workspace_path parameter is required", http.StatusBadRequest)
		return
	}

	// Check that manifest exists first
	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}
	if level := workflowAccessForManifest(GetUserFromContext(r.Context()), manifest); level != WorkflowAccessOwner && level != WorkflowAccessWrite {
		writeWorkflowPermissionDenied(w, "owner")
		return
	}

	// Delete workflow.json
	if err := deleteWorkspaceFile(r.Context(), manifestPath(workspacePath)); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete manifest: %v", err), http.StatusInternalServerError)
		return
	}

	// Clean up in-memory runtime state
	deleteWorkflowRuntime(manifest.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Deleted workflow manifest for %s", workspacePath),
	})
}

// --- Delete workflow folder ---

type DeleteWorkflowFolderRequest struct {
	WorkspacePath string `json:"workspace_path"`
}

func (api *StreamingAPI) handleDeleteWorkflowFolder(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	workspacePath := strings.TrimSpace(r.URL.Query().Get("workspace_path"))
	if workspacePath == "" {
		var req DeleteWorkflowFolderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			workspacePath = strings.TrimSpace(req.WorkspacePath)
		}
	}
	if workspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	manifest, exists, err := ReadWorkflowManifest(r.Context(), workspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at this workspace path", http.StatusNotFound)
		return
	}

	if err := deleteWorkspaceFolder(r.Context(), workspacePath); err != nil {
		http.Error(w, fmt.Sprintf("Failed to delete workflow folder: %v", err), http.StatusInternalServerError)
		return
	}

	deleteWorkflowRuntime(manifest.ID)

	api.workflowOrchestratorContextMux.Lock()
	if cancelFunc, ok := api.workflowOrchestratorContexts[manifest.ID]; ok {
		cancelFunc()
		delete(api.workflowOrchestratorContexts, manifest.ID)
	}
	api.workflowOrchestratorContextMux.Unlock()

	api.workflowStepIDMux.Lock()
	delete(api.workflowStepIDs, manifest.ID)
	api.workflowStepIDMux.Unlock()

	api.workflowObjectiveMux.Lock()
	delete(api.workflowObjectives, manifest.ID)
	api.workflowObjectiveMux.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"workspace_path": workspacePath,
		"message":        fmt.Sprintf("Deleted workflow folder %s", workspacePath),
	})
}

// --- Duplicate workflow ---

type DuplicateWorkflowManifestRequest struct {
	SourceWorkspacePath string `json:"source_workspace_path"`
	TargetWorkspacePath string `json:"target_workspace_path"`
	NewLabel            string `json:"new_label,omitempty"`
}

func (api *StreamingAPI) handleDuplicateWorkflowManifest(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req DuplicateWorkflowManifestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	if req.SourceWorkspacePath == "" || req.TargetWorkspacePath == "" {
		http.Error(w, "source_workspace_path and target_workspace_path are required", http.StatusBadRequest)
		return
	}

	// Read source manifest
	srcManifest, exists, err := ReadWorkflowManifest(r.Context(), req.SourceWorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read source manifest: %v", err), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "No workflow.json found at source workspace path", http.StatusNotFound)
		return
	}

	// Check target doesn't already have a manifest
	_, targetExists, err := ReadWorkflowManifest(r.Context(), req.TargetWorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check target: %v", err), http.StatusInternalServerError)
		return
	}
	if targetExists {
		http.Error(w, "workflow.json already exists at target workspace path", http.StatusConflict)
		return
	}

	// Deep-copy and assign new identity
	if workflowAccessForManifest(GetUserFromContext(r.Context()), srcManifest) == WorkflowAccessNone {
		writeWorkflowPermissionDenied(w, "read")
		return
	}
	newManifest := *srcManifest
	newManifest.ID = "wf_" + uuid.New().String()[:8]
	newManifest.CreatedBy = GetUserIDFromContext(r.Context())
	newManifest.Access = nil
	if newManifest.CreatedBy != "" {
		newManifest.Access = &WorkflowAccess{Owners: []string{newManifest.CreatedBy}, Readers: []string{}}
	}
	if req.NewLabel != "" {
		newManifest.Label = req.NewLabel
	} else {
		newManifest.Label = srcManifest.Label + " (copy)"
	}
	newManifest.CreatedAt = "" // Will be set by WriteWorkflowManifest
	newManifest.UpdatedAt = ""

	// API endpoints and their credentials belong to the source workflow. A copy
	// must attach its own API triggers; ordinary time schedules get fresh IDs.
	newManifest.Schedules = nil
	for _, schedule := range srcManifest.Schedules {
		if schedule.ScheduleType == "webhook" {
			continue
		}
		schedule.ID = uuid.New().String()[:8]
		newManifest.Schedules = append(newManifest.Schedules, schedule)
	}

	// Write new manifest
	if err := WriteWorkflowManifest(r.Context(), req.TargetWorkspacePath, &newManifest); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write target manifest: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":        true,
		"manifest":       newManifest,
		"workspace_path": req.TargetWorkspacePath,
	})
}

// --- Resolve manifest for execution ---

// ResolveWorkflowManifest loads a manifest for a given workspace path.
func (api *StreamingAPI) ResolveWorkflowManifest(ctx context.Context, workspacePath string, presetQueryID string) (*WorkflowManifest, error) {
	if workspacePath != "" {
		manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
		if err == nil && exists {
			return manifest, nil
		}
		if err != nil {
			log.Printf("[MANIFEST] Warning: error reading manifest from %s: %v", workspacePath, err)
		}
	}

	return nil, fmt.Errorf("no workflow.json manifest found at workspace path %q", workspacePath)
}

// --- Helper: resolve workspace path from preset/workflow ID ---

func (api *StreamingAPI) resolveWorkspacePathFromPreset(ctx context.Context, presetQueryID string) (string, error) {
	if presetQueryID == "" {
		return "", fmt.Errorf("preset_query_id is empty")
	}

	// Look up workflow manifest by ID (file-backed, no DB dependency)
	workflows, err := DiscoverWorkflowManifests(ctx)
	if err == nil {
		for _, wf := range workflows {
			if wf.Manifest.ID == presetQueryID {
				return wf.WorkspacePath, nil
			}
		}
	}

	return "", fmt.Errorf("workflow %s not found in discovered manifests", presetQueryID)
}

// --- Helper: check if a setCORS-like helper already exists ---
// The existing workflow handlers use inline CORS headers. This centralizes it.
// For backward compatibility we keep inline CORS in existing handlers untouched.

// LoadManifestForExecution loads workflow defaults from manifest for use in execution bootstrap.
// Returns parsed capabilities that can be applied to the agent request.
func LoadManifestForExecution(ctx context.Context, workspacePath string) (*WorkflowCapabilities, bool, error) {
	manifest, exists, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !exists {
		return nil, false, err
	}
	return &manifest.Capabilities, true, nil
}

// normalizeGmailConnectionIDs trims, drops blanks, and deduplicates a sender
// list. Naming the same account twice must not deliver twice.
func normalizeGmailConnectionIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// workflowsReferencingMCPServer returns a display label (workflow Label, or
// its workspace path if Label is empty) for every workflow visible to the
// requesting user whose capabilities.selected_servers includes serverName.
// Used by remove_mcp_server to warn about dangling references before/after
// deleting a server from the account-wide config -- nothing else in that
// path checks this, so without it a removed server silently breaks the next
// run of any workflow that had it attached.
func workflowsReferencingMCPServer(ctx context.Context, serverName string) []string {
	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		log.Printf("[MCP] Failed to discover workflows while checking references to %q: %v", serverName, err)
		return nil
	}
	discovered = filterWorkflowManifestsForUser(GetUserFromContext(ctx), discovered)

	var affected []string
	for _, wf := range discovered {
		if wf.Manifest == nil {
			continue
		}
		for _, selected := range wf.Manifest.Capabilities.SelectedServers {
			if selected != serverName {
				continue
			}
			label := strings.TrimSpace(wf.Manifest.Label)
			if label == "" {
				label = wf.WorkspacePath
			}
			affected = append(affected, label)
			break
		}
	}
	return affected
}
