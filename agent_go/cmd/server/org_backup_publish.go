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
)

const (
	orgBackupConfigPath  = "pulse/backup.json"
	orgBackupStatusPath  = "pulse/backup/status.json"
	orgPublishConfigPath = "pulse/publish.json"
	orgPublishStatusPath = "pulse/publish/status.json"
)

func readOrgBackupConfig(ctx context.Context) (*WorkflowBackupConfig, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, orgBackupConfigPath)
	if err != nil || !exists {
		return nil, exists, err
	}
	var config WorkflowBackupConfig
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return nil, true, fmt.Errorf("failed to parse org backup config: %w", err)
	}
	return &config, true, nil
}

func readOrgBackupStatus(ctx context.Context) (*WorkflowBackupStatus, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, orgBackupStatusPath)
	if err != nil || !exists {
		return nil, exists, err
	}
	var status WorkflowBackupStatus
	if err := json.Unmarshal([]byte(content), &status); err != nil {
		return nil, true, fmt.Errorf("failed to parse org backup status: %w", err)
	}
	if status.Version == 0 {
		status.Version = workflowBackupStatusVersion
	}
	return &status, true, nil
}

func readOrgPublishConfig(ctx context.Context) (*WorkflowPublishConfig, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, orgPublishConfigPath)
	if err != nil || !exists {
		return nil, exists, err
	}
	var config WorkflowPublishConfig
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return nil, true, fmt.Errorf("failed to parse org publish config: %w", err)
	}
	return &config, true, nil
}

func readOrgPublishStatus(ctx context.Context) (*WorkflowPublishStatus, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, orgPublishStatusPath)
	if err != nil || !exists {
		return nil, exists, err
	}
	var status WorkflowPublishStatus
	if err := json.Unmarshal([]byte(content), &status); err != nil {
		return nil, true, fmt.Errorf("failed to parse org publish status: %w", err)
	}
	if status.Version == 0 {
		status.Version = workflowPublishStatusVersion
	}
	return &status, true, nil
}

var orgBackupHashFiles = []string{
	"pulse/goals.html",
	"pulse/org-pulse.html",
	"pulse/task.html",
	"pulse/backup.json",
	"pulse/publish.json",
	"org.json",
}

var orgBackupHashFolders = []string{
	"_users",
	"memories",
}

func computeOrgBackupSourceHash(ctx context.Context) (string, int) {
	files := make(map[string]string)
	for _, relPath := range orgBackupHashFiles {
		files[relPath] = relPath
	}

	for _, folder := range orgBackupHashFolders {
		listing, exists, err := listWorkspaceFolder(ctx, folder, 100)
		if err != nil || !exists {
			continue
		}
		var fullPaths []string
		collectWorkspaceFilePaths(listing, &fullPaths)
		for _, fullPath := range fullPaths {
			relPath := strings.TrimPrefix(fullPath, "/")
			if shouldSkipOrgHashFile(relPath) {
				continue
			}
			files[relPath] = relPath
		}
	}

	return hashWorkspaceFiles(ctx, files)
}

func computeOrgPublishSourceHash(ctx context.Context) string {
	files := map[string]string{
		"pulse/goals.html":     "pulse/goals.html",
		"pulse/org-pulse.html": "pulse/org-pulse.html",
	}
	hash, _ := hashWorkspaceFiles(ctx, files)
	return hash
}

func hashWorkspaceFiles(ctx context.Context, files map[string]string) (string, int) {
	relPaths := make([]string, 0, len(files))
	for relPath := range files {
		if shouldSkipOrgHashFile(relPath) {
			continue
		}
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

func shouldSkipOrgHashFile(relPath string) bool {
	lower := strings.ToLower(strings.TrimPrefix(relPath, "/"))
	if lower == orgBackupStatusPath || lower == orgPublishStatusPath {
		return true
	}
	if strings.Contains(lower, "/.git/") || strings.HasPrefix(lower, ".git/") {
		return true
	}
	if strings.Contains(lower, "/node_modules/") || strings.HasPrefix(lower, "node_modules/") {
		return true
	}
	if strings.Contains(lower, "/secrets") || strings.Contains(lower, "secret") {
		return true
	}
	if strings.HasSuffix(lower, ".sqlite") || strings.HasSuffix(lower, ".db") {
		return true
	}
	return false
}

func (api *StreamingAPI) handleGetOrgBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	config, _, configErr := readOrgBackupConfig(r.Context())
	if configErr != nil {
		log.Printf("[ORG_BACKUP] Failed to read org backup config: %v", configErr)
	}
	status, _, statusErr := readOrgBackupStatus(r.Context())
	if statusErr != nil {
		log.Printf("[ORG_BACKUP] Failed to read org backup status: %v", statusErr)
	}
	sourceHash, trackedFiles := computeOrgBackupSourceHash(r.Context())

	resp := WorkflowBackupInfoResponse{
		Success:           true,
		Config:            config,
		Status:            status,
		EffectiveState:    workflowBackupEffectiveState(config, status, sourceHash),
		CurrentSourceHash: sourceHash,
		TrackedFilesCount: trackedFiles,
		Supported:         supportedWorkflowBackupStrategies(),
		StatusPath:        orgBackupStatusPath,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (api *StreamingAPI) handleGetOrgPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	config, _, configErr := readOrgPublishConfig(r.Context())
	if configErr != nil {
		log.Printf("[ORG_PUBLISH] Failed to read org publish config: %v", configErr)
	}
	status, _, statusErr := readOrgPublishStatus(r.Context())
	if statusErr != nil {
		log.Printf("[ORG_PUBLISH] Failed to read org publish status: %v", statusErr)
	}
	sourceHash := computeOrgPublishSourceHash(r.Context())
	url := ""
	if status != nil {
		url = status.URL
	}

	resp := WorkflowPublishInfoResponse{
		Success:           true,
		Config:            config,
		Status:            status,
		EffectiveState:    workflowPublishEffectiveState(config, status, sourceHash),
		URL:               url,
		CurrentSourceHash: sourceHash,
		Supported:         supportedWorkflowPublishStrategies(),
		StatusPath:        orgPublishStatusPath,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
