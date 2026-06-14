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

const (
	workflowBackupStatusVersion = 1

	workflowBackupStateNotConfigured         = "not_configured"
	workflowBackupStateConfiguredNotVerified = "configured_not_verified"
	workflowBackupStateRunning               = "running"
	workflowBackupStateHealthy               = "healthy"
	workflowBackupStateStale                 = "stale"
	workflowBackupStatePartial               = "partial"
	workflowBackupStateFailed                = "failed"
)

type WorkflowBackupStatus struct {
	Version            int                               `json:"version"`
	State              string                            `json:"state"`
	LastAttemptAt      string                            `json:"last_attempt_at,omitempty"`
	LastSuccessAt      string                            `json:"last_success_at,omitempty"`
	LastAgentSessionID string                            `json:"last_agent_session_id,omitempty"`
	LastSourceHash     string                            `json:"last_source_hash,omitempty"`
	Summary            string                            `json:"summary,omitempty"`
	Destinations       []WorkflowBackupDestinationStatus `json:"destinations,omitempty"`
	LastError          string                            `json:"last_error,omitempty"`
	UpdatedAt          string                            `json:"updated_at,omitempty"`
}

type WorkflowBackupDestinationStatus struct {
	ID            string `json:"id"`
	Type          string `json:"type,omitempty"`
	Provider      string `json:"provider,omitempty"`
	State         string `json:"state"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	Commit        string `json:"commit,omitempty"`
	ObjectsSynced int    `json:"objects_synced,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Error         string `json:"error,omitempty"`
}

type WorkflowBackupStrategyInfo struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	BestFor     []string `json:"best_for"`
}

type WorkflowBackupInfoResponse struct {
	Success           bool                         `json:"success"`
	Config            *WorkflowBackupConfig        `json:"config,omitempty"`
	Status            *WorkflowBackupStatus        `json:"status,omitempty"`
	EffectiveState    string                       `json:"effective_state"`
	CurrentSourceHash string                       `json:"current_source_hash,omitempty"`
	TrackedFilesCount int                          `json:"tracked_files_count,omitempty"`
	Supported         []WorkflowBackupStrategyInfo `json:"supported"`
	StatusPath        string                       `json:"status_path"`
}

type workflowBackupRunRequest struct {
	WorkspacePath string `json:"workspace_path"`
	Action        string `json:"action,omitempty"` // "backup" or "configure"
}

func workflowBackupStatusPath(workspacePath string) string {
	return strings.TrimRight(workspacePath, "/") + "/backup/status.json"
}

func supportedWorkflowBackupStrategies() []WorkflowBackupStrategyInfo {
	return []WorkflowBackupStrategyInfo{
		{
			ID:          "git",
			Label:       "Git / GitHub",
			Description: "Best for text, workflow config, planning, knowledgebase, learnings, scripts, and small JSON data.",
			BestFor:     []string{"workflow", "planning", "knowledgebase", "learnings", "small-db"},
		},
		{
			ID:          "object_store",
			Label:       "R2 / S3 / B2",
			Description: "Best for run folders, generated media, large artifacts, and files that should not live in git.",
			BestFor:     []string{"runs", "large-artifacts", "media", "archives"},
		},
		{
			ID:          "huggingface",
			Label:       "HuggingFace Hub",
			Description: "Best for dataset/model-style backups, generated media, and revisioned ML artifacts.",
			BestFor:     []string{"datasets", "models", "media", "large-artifacts"},
		},
		{
			ID:          "local_versions",
			Label:       "Local workflow versions",
			Description: "App-managed local snapshots for quick config rollback. This is not an off-box backup.",
			BestFor:     []string{"workflow", "planning", "learnings"},
		},
		{
			ID:          "local_zip",
			Label:       "Local ZIP export",
			Description: "Manual full-folder export/import for recovery or transfer. This is not automatic remote backup.",
			BestFor:     []string{"manual-export", "restore", "transfer"},
		},
	}
}

func readWorkflowBackupStatus(ctx context.Context, workspacePath string) (*WorkflowBackupStatus, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, workflowBackupStatusPath(workspacePath))
	if err != nil || !exists {
		return nil, exists, err
	}
	var status WorkflowBackupStatus
	if err := json.Unmarshal([]byte(content), &status); err != nil {
		return nil, true, fmt.Errorf("failed to parse backup status: %w", err)
	}
	if status.Version == 0 {
		status.Version = workflowBackupStatusVersion
	}
	return &status, true, nil
}

func writeWorkflowBackupStatus(ctx context.Context, workspacePath string, status WorkflowBackupStatus) error {
	if status.Version == 0 {
		status.Version = workflowBackupStatusVersion
	}
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup status: %w", err)
	}
	if err := createWorkspaceFolder(ctx, strings.TrimRight(workspacePath, "/")+"/backup"); err != nil {
		return fmt.Errorf("failed to create backup status folder: %w", err)
	}
	return writeRawFileToWorkspace(ctx, workflowBackupStatusPath(workspacePath), string(data))
}

func workflowBackupEffectiveState(config *WorkflowBackupConfig, status *WorkflowBackupStatus, currentSourceHash string) string {
	if config == nil || !config.Enabled {
		return workflowBackupStateNotConfigured
	}
	if status == nil || strings.TrimSpace(status.State) == "" {
		return workflowBackupStateConfiguredNotVerified
	}
	state := strings.TrimSpace(status.State)
	if state == workflowBackupStateHealthy && status.LastSourceHash != "" && currentSourceHash != "" && status.LastSourceHash != currentSourceHash {
		return workflowBackupStateStale
	}
	return state
}

var backupHashFiles = []string{
	"workflow.json",
	"planning/plan.json",
	"planning/step_config.json",
	"planning/workflow_layout.json",
	"planning/step_override.json",
	"reports/report_plan.json",
	"variables/variables.json",
	"evaluation/evaluation_plan.json",
}

var backupHashFolders = []string{
	"knowledgebase",
	"learnings",
}

func computeWorkflowBackupSourceHash(ctx context.Context, workspacePath string) (string, int) {
	workspacePath = strings.TrimRight(workspacePath, "/")
	files := make(map[string]string)
	for _, relPath := range backupHashFiles {
		files[relPath] = workspacePath + "/" + relPath
	}

	for _, folder := range backupHashFolders {
		listing, exists, err := listWorkspaceFolder(ctx, workspacePath+"/"+folder, 100)
		if err != nil || !exists {
			continue
		}
		var fullPaths []string
		collectWorkspaceFilePaths(listing, &fullPaths)
		for _, fullPath := range fullPaths {
			relPath := strings.TrimPrefix(fullPath, workspacePath+"/")
			if relPath == fullPath || shouldSkipBackupHashFile(relPath) {
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
	tracked := 0
	for _, relPath := range relPaths {
		content, exists, err := readFileFromWorkspace(ctx, files[relPath])
		if err != nil || !exists {
			continue
		}
		tracked++
		hasher.Write([]byte(relPath))
		hasher.Write([]byte{0})
		hasher.Write([]byte(content))
		hasher.Write([]byte{0})
	}
	if tracked == 0 {
		return "", 0
	}
	return hex.EncodeToString(hasher.Sum(nil)), tracked
}

func shouldSkipBackupHashFile(relPath string) bool {
	lower := strings.ToLower(relPath)
	if strings.Contains(lower, "/.git/") || strings.HasPrefix(lower, ".git/") {
		return true
	}
	if strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".db") {
		return true
	}
	if strings.Contains(lower, "/runs/") || strings.HasPrefix(lower, "runs/") {
		return true
	}
	return false
}

func normalizeWorkflowBackupConfig(config WorkflowBackupConfig) WorkflowBackupConfig {
	if config.Mode == "" {
		config.Mode = "agent"
	}
	if config.Destinations == nil {
		config.Destinations = []WorkflowBackupDestination{}
	}
	for i := range config.Destinations {
		config.Destinations[i].ID = strings.TrimSpace(config.Destinations[i].ID)
		config.Destinations[i].Type = strings.TrimSpace(config.Destinations[i].Type)
		config.Destinations[i].Provider = strings.TrimSpace(config.Destinations[i].Provider)
		if config.Destinations[i].Covers == nil {
			config.Destinations[i].Covers = []string{}
		}
		if config.Destinations[i].SecretRefs == nil {
			config.Destinations[i].SecretRefs = []string{}
		}
	}
	return config
}

func (api *StreamingAPI) handleGetWorkflowBackup(w http.ResponseWriter, r *http.Request) {
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

	status, _, statusErr := readWorkflowBackupStatus(r.Context(), workspacePath)
	if statusErr != nil {
		log.Printf("[BACKUP] Failed to read backup status for %s: %v", workspacePath, statusErr)
	}
	sourceHash, trackedFiles := computeWorkflowBackupSourceHash(r.Context(), workspacePath)
	resp := WorkflowBackupInfoResponse{
		Success:           true,
		Config:            manifest.Backup,
		Status:            status,
		EffectiveState:    workflowBackupEffectiveState(manifest.Backup, status, sourceHash),
		CurrentSourceHash: sourceHash,
		TrackedFilesCount: trackedFiles,
		Supported:         supportedWorkflowBackupStrategies(),
		StatusPath:        workflowBackupStatusPath(workspacePath),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (api *StreamingAPI) handleUpdateWorkflowBackupConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req struct {
		WorkspacePath string               `json:"workspace_path"`
		Backup        WorkflowBackupConfig `json:"backup"`
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

	backup := normalizeWorkflowBackupConfig(req.Backup)
	manifest.Backup = &backup
	if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"backup":  backup,
	})
}

func (api *StreamingAPI) handleRunWorkflowBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var req workflowBackupRunRequest
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
		req.Action = "backup"
	}
	if req.Action != "backup" && req.Action != "configure" {
		http.Error(w, "action must be backup or configure", http.StatusBadRequest)
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
	if req.Action == "backup" && (manifest.Backup == nil || !manifest.Backup.Enabled) {
		http.Error(w, "backup is not enabled for this workflow; configure backup first", http.StatusBadRequest)
		return
	}

	sourceHash, trackedFiles := computeWorkflowBackupSourceHash(r.Context(), req.WorkspacePath)
	sessionID := fmt.Sprintf("workflow-backup-%d", time.Now().UnixNano())
	query := buildWorkflowBackupAgentPrompt(req, manifest, sourceHash, trackedFiles, sessionID)
	reqMap := map[string]interface{}{
		"query":                   query,
		"agent_mode":              "workflow_phase",
		"phase_id":                workflowtypes.WorkflowStatusWorkflowBuilder,
		"preset_query_id":         manifest.ID,
		"selected_folder":         req.WorkspacePath,
		"triggered_by":            "workflow_backup",
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
	if err := writeWorkflowBackupStatus(context.Background(), req.WorkspacePath, WorkflowBackupStatus{
		Version:            workflowBackupStatusVersion,
		State:              workflowBackupStateRunning,
		LastAttemptAt:      now,
		LastAgentSessionID: sessionID,
		LastSourceHash:     sourceHash,
		Summary:            "Builder backup task started.",
	}); err != nil {
		log.Printf("[BACKUP] Failed to write running backup status for %s: %v", req.WorkspacePath, err)
	}

	userID := GetUserIDFromContext(r.Context())
	if err := api.startSessionInternal(context.Background(), reqMap, sessionID, userID, nil); err != nil {
		if statusErr := writeWorkflowBackupStatus(context.Background(), req.WorkspacePath, WorkflowBackupStatus{
			Version:            workflowBackupStatusVersion,
			State:              workflowBackupStateFailed,
			LastAttemptAt:      now,
			LastAgentSessionID: sessionID,
			LastSourceHash:     sourceHash,
			Summary:            "Failed to start builder backup task.",
			LastError:          err.Error(),
		}); statusErr != nil {
			log.Printf("[BACKUP] Failed to write failed backup status for %s: %v", req.WorkspacePath, statusErr)
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"message":    "Builder backup task started. The builder will update backup/status.json when it finishes.",
	})
}

func buildWorkflowBackupAgentPrompt(req workflowBackupRunRequest, manifest *WorkflowManifest, sourceHash string, trackedFiles int, sessionID string) string {
	backupJSON, _ := json.MarshalIndent(manifest.Backup, "", "  ")
	actionText := "perform the configured backup now"
	if req.Action == "configure" {
		actionText = "configure or update this workflow's backup strategy"
	}
	return fmt.Sprintf(`You are the workflow backup operator for %s.

Task: %s.

Rules:
1. Call get_reference_doc(kind="backup-strategy") and follow it.
2. Read workflow.json, especially the backup field below.
3. If configuring backup, update workflow.json.backup with enabled=true, mode="agent", triggers, and destinations once the strategy is clear. If critical destination details are missing, ask the user in this builder chat and write backup/status.json with state "configured_not_verified".
4. If running backup, use workflow.json.backup as the contract. Do not invent destinations. If a destination is missing credentials or setup, mark that destination failed and continue with any other configured destinations.
5. Always write backup/status.json before you finish, even on failure. Do not write changing backup status into workflow.json.
6. Use this exact source hash for the status file when the backup covers current config/text state: %s. Tracked files counted by the app: %d.
7. Set last_agent_session_id to %q.

Supported strategy summary:
- git/github: config, planning, knowledgebase, learnings, scripts, small JSON.
- object_store: Cloudflare R2, S3, Backblaze B2 for runs, media, large artifacts.
- huggingface: dataset/model-style backups and generated media.
- local_zip and local workflow versions are manual/local recovery, not remote automatic backup.

Current workflow.json.backup:
%s

Required backup/status.json schema:
{
  "version": 1,
  "state": "healthy | partial | failed | configured_not_verified",
  "last_attempt_at": "<ISO timestamp>",
  "last_success_at": "<ISO timestamp if all required backup destinations succeeded>",
  "last_agent_session_id": "%s",
  "last_source_hash": "%s",
  "summary": "<short human-readable result>",
  "destinations": [
    {
      "id": "<destination id>",
      "type": "git | object_store | huggingface | local_zip",
      "provider": "<github|git|r2|s3|b2|huggingface|local>",
      "state": "healthy | failed | skipped",
      "last_success_at": "<ISO timestamp when this destination succeeded>",
      "commit": "<git commit if applicable>",
      "objects_synced": 0,
      "summary": "<what happened>",
      "error": "<error if failed>"
    }
  ],
  "last_error": "<empty on healthy; concise error on failed/partial>",
  "updated_at": "<ISO timestamp>"
}
`, req.WorkspacePath, actionText, sourceHash, trackedFiles, sessionID, string(backupJSON), sessionID, sourceHash)
}
