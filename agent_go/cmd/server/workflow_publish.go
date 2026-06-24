package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

// Publish is the share-twin of Backup: it deploys the workflow's HTML artifacts
// (Pulse log + report dashboard) to a public URL on any static host. It mirrors
// workflow_backup.go — provider-agnostic, agent-driven, status in publish/status.json.

const (
	workflowPublishStatusVersion = 1

	workflowPublishStateNotConfigured         = "not_configured"
	workflowPublishStateConfiguredNotVerified = "configured_not_verified"
	workflowPublishStatePublishing            = "publishing"
	workflowPublishStatePublished             = "published"
	workflowPublishStateStale                 = "stale"
	workflowPublishStateFailed                = "failed"
)

type WorkflowPublishStatus struct {
	Version            int                                `json:"version"`
	State              string                             `json:"state"`
	URL                string                             `json:"url,omitempty"`
	LastPublishedAt    string                             `json:"last_published_at,omitempty"`
	LastAttemptAt      string                             `json:"last_attempt_at,omitempty"`
	LastAgentSessionID string                             `json:"last_agent_session_id,omitempty"`
	LastSourceHash     string                             `json:"last_source_hash,omitempty"`
	Summary            string                             `json:"summary,omitempty"`
	Destinations       []WorkflowPublishDestinationStatus `json:"destinations,omitempty"`
	LastError          string                             `json:"last_error,omitempty"`
	UpdatedAt          string                             `json:"updated_at,omitempty"`
}

type WorkflowPublishDestinationStatus struct {
	ID            string `json:"id"`
	Provider      string `json:"provider,omitempty"`
	Method        string `json:"method,omitempty"`
	State         string `json:"state"`
	URL           string `json:"url,omitempty"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Error         string `json:"error,omitempty"`
}

type WorkflowPublishStrategyInfo struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Method      string `json:"method"`
	Description string `json:"description"`
}

type WorkflowPublishInfoResponse struct {
	Success           bool                          `json:"success"`
	Config            *WorkflowPublishConfig        `json:"config,omitempty"`
	Status            *WorkflowPublishStatus        `json:"status,omitempty"`
	EffectiveState    string                        `json:"effective_state"`
	URL               string                        `json:"url,omitempty"`
	CurrentSourceHash string                        `json:"current_source_hash,omitempty"`
	Supported         []WorkflowPublishStrategyInfo `json:"supported"`
	StatusPath        string                        `json:"status_path"`
}

type workflowPublishRunRequest struct {
	WorkspacePath string `json:"workspace_path"`
	Action        string `json:"action,omitempty"` // "publish" or "configure"
}

func workflowPublishStatusPath(workspacePath string) string {
	return strings.TrimRight(workspacePath, "/") + "/publish/status.json"
}

// supportedWorkflowPublishStrategies is an illustrative hint list for the UI —
// NOT an enum. Any static host works; the deploy logic lives in publish-strategy.md.
func supportedWorkflowPublishStrategies() []WorkflowPublishStrategyInfo {
	return []WorkflowPublishStrategyInfo{
		{ID: "netlify", Label: "Netlify", Method: "cli", Description: "netlify deploy --prod; default URL *.netlify.app."},
		{ID: "vercel", Label: "Vercel", Method: "cli", Description: "vercel deploy --prod; default URL *.vercel.app."},
		{ID: "cloudflare-pages", Label: "Cloudflare Pages", Method: "cli", Description: "wrangler pages deploy; default URL *.pages.dev."},
		{ID: "github-pages", Label: "GitHub Pages", Method: "git", Description: "Push static files to the gh-pages branch."},
		{ID: "s3", Label: "S3 / object store", Method: "sync", Description: "aws s3 sync / rclone to a static bucket (the any-host catch-all)."},
	}
}

func readWorkflowPublishStatus(ctx context.Context, workspacePath string) (*WorkflowPublishStatus, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, workflowPublishStatusPath(workspacePath))
	if err != nil || !exists {
		return nil, exists, err
	}
	var status WorkflowPublishStatus
	if err := json.Unmarshal([]byte(content), &status); err != nil {
		return nil, true, fmt.Errorf("failed to parse publish status: %w", err)
	}
	if status.Version == 0 {
		status.Version = workflowPublishStatusVersion
	}
	return &status, true, nil
}

func writeWorkflowPublishStatus(ctx context.Context, workspacePath string, status WorkflowPublishStatus) error {
	if status.Version == 0 {
		status.Version = workflowPublishStatusVersion
	}
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal publish status: %w", err)
	}
	if err := createWorkspaceFolder(ctx, strings.TrimRight(workspacePath, "/")+"/publish"); err != nil {
		return fmt.Errorf("failed to create publish status folder: %w", err)
	}
	return writeRawFileToWorkspace(ctx, workflowPublishStatusPath(workspacePath), string(data))
}

func workflowPublishEffectiveState(config *WorkflowPublishConfig, status *WorkflowPublishStatus, currentSourceHash string) string {
	if config == nil || !config.Enabled {
		return workflowPublishStateNotConfigured
	}
	if status == nil || strings.TrimSpace(status.State) == "" {
		return workflowPublishStateConfiguredNotVerified
	}
	state := strings.TrimSpace(status.State)
	if state == workflowPublishStatePublished && status.LastSourceHash != "" && currentSourceHash != "" && status.LastSourceHash != currentSourceHash {
		return workflowPublishStateStale
	}
	return state
}

// The artifacts whose change should trigger a re-publish: the Pulse log, the
// report HTML, and db.sqlite (the dashboard snapshot is baked from it).
var publishHashFiles = []string{
	"builder/improve.html",
	"db/db.sqlite",
}

var publishHashFolders = []string{
	"reports",
}

func computeWorkflowPublishSourceHash(ctx context.Context, workspacePath string) string {
	workspacePath = strings.TrimRight(workspacePath, "/")
	files := make(map[string]string)
	for _, relPath := range publishHashFiles {
		files[relPath] = workspacePath + "/" + relPath
	}
	for _, folder := range publishHashFolders {
		listing, exists, err := listWorkspaceFolder(ctx, workspacePath+"/"+folder, 100)
		if err != nil || !exists {
			continue
		}
		var fullPaths []string
		collectWorkspaceFilePaths(listing, &fullPaths)
		for _, fullPath := range fullPaths {
			relPath := strings.TrimPrefix(fullPath, workspacePath+"/")
			if relPath == fullPath {
				continue
			}
			files[relPath] = fullPath
		}
	}

	relPaths := make([]string, 0, len(files))
	for relPath := range files {
		relPaths = append(relPaths, relPath)
	}
	sort.Strings(relPaths)

	hasher := sha256.New()
	for _, relPath := range relPaths {
		content, exists, err := readFileFromWorkspace(ctx, files[relPath])
		if err != nil || !exists {
			continue
		}
		hasher.Write([]byte(relPath))
		hasher.Write([]byte(content))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func normalizeWorkflowPublishConfig(config WorkflowPublishConfig) WorkflowPublishConfig {
	if config.Mode == "" {
		config.Mode = "agent"
	}
	if config.DashboardMode == "" {
		config.DashboardMode = "snapshot"
	}
	if config.Targets == nil {
		config.Targets = []string{}
	}
	if config.Destinations == nil {
		config.Destinations = []WorkflowPublishDestination{}
	}
	for i := range config.Destinations {
		config.Destinations[i].ID = strings.TrimSpace(config.Destinations[i].ID)
		config.Destinations[i].Provider = strings.TrimSpace(config.Destinations[i].Provider)
		config.Destinations[i].Method = strings.TrimSpace(config.Destinations[i].Method)
		if config.Destinations[i].Covers == nil {
			config.Destinations[i].Covers = []string{}
		}
	}
	return config
}

func (api *StreamingAPI) handleGetWorkflowPublish(w http.ResponseWriter, r *http.Request) {
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

	status, _, statusErr := readWorkflowPublishStatus(r.Context(), workspacePath)
	if statusErr != nil {
		log.Printf("[PUBLISH] Failed to read publish status for %s: %v", workspacePath, statusErr)
	}
	sourceHash := computeWorkflowPublishSourceHash(r.Context(), workspacePath)
	url := ""
	if status != nil {
		url = status.URL
	}
	resp := WorkflowPublishInfoResponse{
		Success:           true,
		Config:            manifest.Publish,
		Status:            status,
		EffectiveState:    workflowPublishEffectiveState(manifest.Publish, status, sourceHash),
		URL:               url,
		CurrentSourceHash: sourceHash,
		Supported:         supportedWorkflowPublishStrategies(),
		StatusPath:        workflowPublishStatusPath(workspacePath),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (api *StreamingAPI) handleUpdateWorkflowPublishConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req struct {
		WorkspacePath string                `json:"workspace_path"`
		Publish       WorkflowPublishConfig `json:"publish"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspacePath = strings.TrimSpace(req.WorkspacePath)
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}

	manifest, found, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}

	publish := normalizeWorkflowPublishConfig(req.Publish)
	manifest.Publish = &publish
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"publish": publish,
	})
}

func (api *StreamingAPI) handleRunWorkflowPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req workflowPublishRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.WorkspacePath = strings.TrimSpace(req.WorkspacePath)
	if req.WorkspacePath == "" {
		http.Error(w, "workspace_path is required", http.StatusBadRequest)
		return
	}
	if req.Action == "" {
		req.Action = "publish"
	}
	if req.Action != "publish" && req.Action != "configure" {
		http.Error(w, "action must be publish or configure", http.StatusBadRequest)
		return
	}

	manifest, found, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.Error(w, "workflow not found", http.StatusNotFound)
		return
	}
	if req.Action == "publish" && (manifest.Publish == nil || !manifest.Publish.Enabled) {
		http.Error(w, "publish is not enabled for this workflow; configure publish first", http.StatusBadRequest)
		return
	}

	sourceHash := computeWorkflowPublishSourceHash(r.Context(), req.WorkspacePath)
	sessionID := fmt.Sprintf("workflow-publish-%d", time.Now().UnixNano())
	query := buildWorkflowPublishAgentPrompt(req, manifest, sourceHash, sessionID)
	reqMap := map[string]interface{}{
		"query":                   query,
		"agent_mode":              "workflow_phase",
		"phase_id":                workflowtypes.WorkflowStatusWorkflowBuilder,
		"preset_query_id":         manifest.ID,
		"selected_folder":         req.WorkspacePath,
		"triggered_by":            "workflow_publish",
		"servers":                 manifest.Capabilities.SelectedServers,
		"selected_tools":          manifest.Capabilities.SelectedTools,
		"selected_skills":         manifest.Capabilities.SelectedSkills,
		"browser_mode":            manifest.Capabilities.BrowserMode,
		"use_code_execution_mode": manifest.Capabilities.UseCodeExecutionMode,
		"execution_options": map[string]interface{}{
			"run_mode":            "use_same_run",
			"selected_run_folder": "iteration-0",
			"execution_strategy":  "start_from_beginning_no_human",
			"workshop_mode":       "workshop",
		},
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := writeWorkflowPublishStatus(context.Background(), req.WorkspacePath, WorkflowPublishStatus{
		Version:            workflowPublishStatusVersion,
		State:              workflowPublishStatePublishing,
		LastAttemptAt:      now,
		LastAgentSessionID: sessionID,
		LastSourceHash:     sourceHash,
		Summary:            "Builder publish task started.",
	}); err != nil {
		log.Printf("[PUBLISH] Failed to write publishing status for %s: %v", req.WorkspacePath, err)
	}

	userID := GetUserIDFromContext(r.Context())
	if err := api.startSessionInternal(context.Background(), reqMap, sessionID, userID, nil); err != nil {
		if statusErr := writeWorkflowPublishStatus(context.Background(), req.WorkspacePath, WorkflowPublishStatus{
			Version:            workflowPublishStatusVersion,
			State:              workflowPublishStateFailed,
			LastAttemptAt:      now,
			LastAgentSessionID: sessionID,
			LastSourceHash:     sourceHash,
			Summary:            "Failed to start builder " + req.Action + " task.",
			LastError:          err.Error(),
		}); statusErr != nil {
			log.Printf("[PUBLISH] Failed to write failed publish status for %s: %v", req.WorkspacePath, statusErr)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"message":    "Builder publish task started. The builder will update publish/status.json when it finishes.",
	})
}

func buildWorkflowPublishAgentPrompt(req workflowPublishRunRequest, manifest *WorkflowManifest, sourceHash, sessionID string) string {
	publishJSON, _ := json.MarshalIndent(manifest.Publish, "", "  ")
	actionText := "publish this workflow's configured HTML artifacts now"
	if req.Action == "configure" {
		actionText = "configure or update this workflow's publish strategy"
	}
	return fmt.Sprintf(`You are the workflow publish operator for %s.

Task: %s.

Rules:
1. Call get_reference_doc(kind="publish-strategy") and follow it exactly.
2. Read workflow.json, especially the publish field below. The targets list says which artifacts to publish (pulse = builder/improve.html; report = the reports/ dashboard). Publish ONLY what targets lists.
3. The report dashboard is live (it queries db.sqlite via window.report.query). You MUST generate a static snapshot before deploying it (run its queries, inline the results as JSON + a shim) per the reference doc. The Pulse log is self-contained — publish as-is. Never ship db.sqlite or stand up a server.
4. If configuring, update workflow.json.publish with enabled=true, mode="agent", the destination(s) and their secret_name, then write publish/status.json with state "configured_not_verified" and do NOT publish yet. If running publish, use workflow.json.publish as the contract; deploy via each destination's method (cli/git/sync), fetch the returned URL to confirm it loads, and record it.
5. PRIVACY: publishing puts data on a public URL. Before the first publish of a destination, state what becomes public and confirm scope. Never publish secrets or raw sensitive rows.
6. Always write publish/status.json before you finish, even on failure, with the public URL and per-destination results. Use this source hash for last_source_hash when the publish covers current artifacts: %s. Set last_agent_session_id to %q. Do not write operational publish status into workflow.json.

Provider-agnostic: deploy to whatever the destination's provider names, using its CLI, a git push, or an object-store/rsync sync. Auth comes from the named secret.

Current workflow.json.publish:
%s

Required publish/status.json schema:
{
  "version": 1,
  "state": "configured_not_verified | published | stale | failed",
  "url": "<public URL of the latest publish>",
  "last_published_at": "<ISO if a publish succeeded>",
  "last_attempt_at": "<ISO>",
  "last_agent_session_id": "%s",
  "last_source_hash": "%s",
  "summary": "<short human-readable result>",
  "destinations": [
    { "id": "<id>", "provider": "<provider>", "method": "cli|git|sync", "url": "<url>", "state": "published|failed|skipped", "error": "<if failed>" }
  ],
  "last_error": "<empty on success; concise on failure>",
  "updated_at": "<ISO>"
}
`, req.WorkspacePath, actionText, sourceHash, sessionID, string(publishJSON), sessionID, sourceHash)
}
