package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/google/uuid"
)

// Current manifest schema version
const WorkflowManifestSchemaVersion = 1

// WorkflowManifest is the top-level workflow.json structure that lives in each workspace.
type WorkflowManifest struct {
	SchemaVersion   int                       `json:"schema_version"`
	ID              string                    `json:"id"`
	Label           string                    `json:"label"`
	Objective       string                    `json:"objective,omitempty"`
	SuccessCriteria string                    `json:"success_criteria,omitempty"`
	Capabilities    WorkflowCapabilities      `json:"capabilities"`
	ExecutionDefs   WorkflowExecutionDefaults `json:"execution_defaults"`
	Ownership       WorkflowOwnership         `json:"ownership"`
	Schedules       []WorkflowSchedule        `json:"schedules"`
	CreatedAt       string                    `json:"created_at,omitempty"`
	UpdatedAt       string                    `json:"updated_at,omitempty"`
}

// WorkflowCapabilities stores workflow-wide agent and tool configuration.
type WorkflowCapabilities struct {
	SelectedServers          []string                  `json:"selected_servers"`
	SelectedTools            []string                  `json:"selected_tools"`
	SelectedSkills           []string                  `json:"selected_skills"`
	SelectedSecrets          []string                  `json:"selected_secrets"`
	SelectedGlobalSecretNames *[]string                `json:"selected_global_secret_names"` // nil = all, [] = none
	BrowserMode              string                    `json:"browser_mode"`
	UseCodeExecutionMode     bool                      `json:"use_code_execution_mode"`
	LLMConfig                *database.PresetLLMConfig `json:"llm_config,omitempty"`
}

// WorkflowExecutionDefaults stores toolbar-level defaults for workflow execution.
type WorkflowExecutionDefaults struct {
	AlwaysUseSameRun            bool     `json:"always_use_same_run"`
	SkipExecutionCleanup        bool     `json:"skip_execution_cleanup"`
	ExecutionMode               string   `json:"execution_mode,omitempty"` // "stateless" | "stateful"
	// Global step overrides (replaces step_override.json)
	DisableLearning              *bool    `json:"disable_learning,omitempty"`
	GlobalSkillObjective         string   `json:"global_skill_objective,omitempty"`
	DisableParallelToolExecution *bool    `json:"disable_parallel_tool_execution,omitempty"`
	ExecutionMaxTurns            *int     `json:"execution_max_turns,omitempty"`
	EnabledCustomTools           []string `json:"enabled_custom_tools,omitempty"`
}

// WorkflowOwnership tracks workflow assignment.
type WorkflowOwnership struct {
	EmployeeID *string `json:"employee_id"`
}

// WorkflowSchedule represents a cron schedule stored in the manifest.
type WorkflowSchedule struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	CronExpression string          `json:"cron_expression"`
	Timezone       string          `json:"timezone"`
	Enabled        bool            `json:"enabled"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs       []string        `json:"group_ids,omitempty"`
	Mode           string          `json:"mode,omitempty"`          // "workflow" (default/orchestrator) or "workshop" (LLM-driven via workshop builder)
	Messages       []string        `json:"messages,omitempty"`      // Predefined message queue for workshop mode (sent one-by-one)
	WorkshopMode   string          `json:"workshop_mode,omitempty"` // Workshop builder mode: "builder", "optimizer", "runner" (default), "debugger"
}

// --- Validation ---

// ValidateManifest checks that a WorkflowManifest has required fields and valid values.
func ValidateManifest(m *WorkflowManifest) error {
	if m.SchemaVersion < 1 {
		return fmt.Errorf("schema_version must be >= 1")
	}
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if m.Label == "" {
		return fmt.Errorf("label is required")
	}

	// Validate browser mode if set
	if m.Capabilities.BrowserMode != "" {
		validModes := map[string]bool{
			"none": true, "headless": true, "cdp": true, "playwright": true, "stealth": true,
		}
		if !validModes[m.Capabilities.BrowserMode] {
			return fmt.Errorf("invalid browser_mode: %s", m.Capabilities.BrowserMode)
		}
	}

	// Validate LLM config if present
	if m.Capabilities.LLMConfig != nil {
		if err := database.ValidatePresetLLMConfigPublic(m.Capabilities.LLMConfig); err != nil {
			return fmt.Errorf("invalid llm_config: %w", err)
		}
	}

	// Validate schedules
	for i, sched := range m.Schedules {
		if sched.ID == "" {
			return fmt.Errorf("schedules[%d].id is required", i)
		}
		if sched.CronExpression == "" {
			return fmt.Errorf("schedules[%d].cron_expression is required", i)
		}
		if len(normalizeScheduleGroupIDs(sched.GroupIDs)) == 0 {
			return fmt.Errorf("schedules[%d].group_ids is required", i)
		}
	}

	return nil
}

func normalizeScheduleGroupIDs(groupIDs []string) []string {
	seen := make(map[string]struct{}, len(groupIDs))
	normalized := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		trimmed := strings.TrimSpace(groupID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func validateScheduleGroupIDsForWorkspace(ctx context.Context, workspacePath string, groupIDs []string) ([]string, error) {
	normalized := normalizeScheduleGroupIDs(groupIDs)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("group_ids is required and must contain at least one group ID")
	}

	content, exists, err := readFileFromWorkspace(ctx, workspacePath+"/variables/variables.json")
	if err != nil {
		return nil, fmt.Errorf("failed to read variables.json: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("variables/variables.json not found for %s", workspacePath)
	}

	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}
	if len(manifest.Groups) == 0 {
		return nil, fmt.Errorf("workflow has no variable groups; schedules must specify at least one valid group_id")
	}

	validGroups := make(map[string]struct{}, len(manifest.Groups))
	available := make([]string, 0, len(manifest.Groups))
	for _, group := range manifest.Groups {
		groupID := strings.TrimSpace(group.GroupID)
		if groupID == "" {
			continue
		}
		if _, exists := validGroups[groupID]; exists {
			continue
		}
		validGroups[groupID] = struct{}{}
		available = append(available, groupID)
	}
	sort.Strings(available)

	for _, groupID := range normalized {
		if _, ok := validGroups[groupID]; !ok {
			return nil, fmt.Errorf("unknown group_id %q; available groups: %s", groupID, strings.Join(available, ", "))
		}
	}

	return normalized, nil
}

// --- Default factory ---

// NewWorkflowManifest creates a manifest with defaults.
func NewWorkflowManifest(label string) *WorkflowManifest {
	now := time.Now().UTC().Format(time.RFC3339)
	return &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "wf_" + uuid.New().String()[:8],
		Label:         label,
		Capabilities: WorkflowCapabilities{
			SelectedServers:    []string{},
			SelectedTools:      []string{},
			SelectedSkills:     []string{},
			SelectedSecrets:    []string{},
			BrowserMode:        "none",
		},
		ExecutionDefs: WorkflowExecutionDefaults{},
		Ownership:     WorkflowOwnership{},
		Schedules:     []WorkflowSchedule{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// --- Workspace I/O ---

// manifestPath returns the workspace-relative path to workflow.json for a given workspace.
func manifestPath(workspacePath string) string {
	return workspacePath + "/workflow.json"
}

// ReadWorkflowManifest reads and parses workflow.json from a workspace.
// Returns (manifest, true, nil) if found, (nil, false, nil) if not found, (nil, false, error) on error.
func ReadWorkflowManifest(ctx context.Context, workspacePath string) (*WorkflowManifest, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, manifestPath(workspacePath))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read workflow.json: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	var m WorkflowManifest
	if err := json.Unmarshal([]byte(content), &m); err != nil {
		return nil, false, fmt.Errorf("failed to parse workflow.json: %w", err)
	}

	// Track whether any schedule IDs need auto-assignment before applying defaults.
	hadEmptyScheduleID := false
	for _, s := range m.Schedules {
		if s.ID == "" {
			hadEmptyScheduleID = true
			break
		}
	}

	// Apply defaults for missing fields from older schema versions
	applyManifestDefaults(&m)

	// Persist auto-assigned schedule IDs so subsequent lookups find the same UUID.
	if hadEmptyScheduleID {
		if err := WriteWorkflowManifest(ctx, workspacePath, &m); err != nil {
			log.Printf("[WARN] ReadWorkflowManifest: failed to persist auto-assigned schedule IDs for %s: %v", workspacePath, err)
		}
	}

	return &m, true, nil
}

// WriteWorkflowManifest validates and writes workflow.json to a workspace.
func WriteWorkflowManifest(ctx context.Context, workspacePath string, m *WorkflowManifest) error {
	// Ensure nil slices become empty arrays in JSON
	ensureManifestSlices(m)

	// Set updated timestamp
	m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := ValidateManifest(m); err != nil {
		return fmt.Errorf("manifest validation failed: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal workflow.json: %w", err)
	}

	if err := writeFileToWorkspace(ctx, manifestPath(workspacePath), string(data)); err != nil {
		return fmt.Errorf("failed to write workflow.json: %w", err)
	}

	return nil
}

// --- Conversion helpers: preset ↔ manifest ---

// ManifestFromPreset creates a WorkflowManifest from a preset query row.
// Used during migration (Phase 4) and when creating new workflows.
func ManifestFromPreset(preset *database.PresetQuery) (*WorkflowManifest, error) {
	m := NewWorkflowManifest(preset.Label)

	// Parse JSON array fields from preset
	if preset.SelectedServers != "" {
		if err := json.Unmarshal([]byte(preset.SelectedServers), &m.Capabilities.SelectedServers); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse selected_servers: %v", err)
		}
	}
	if preset.SelectedTools != "" {
		if err := json.Unmarshal([]byte(preset.SelectedTools), &m.Capabilities.SelectedTools); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse selected_tools: %v", err)
		}
	}
	if preset.SelectedSkills != "" {
		if err := json.Unmarshal([]byte(preset.SelectedSkills), &m.Capabilities.SelectedSkills); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse selected_skills: %v", err)
		}
	}
	if preset.SelectedSecrets != "" {
		if err := json.Unmarshal([]byte(preset.SelectedSecrets), &m.Capabilities.SelectedSecrets); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse selected_secrets: %v", err)
		}
	}
	// Global secret names: NULL in DB = all selected (nil pointer); empty JSON array = none
	if preset.SelectedGlobalSecretNames != "" && preset.SelectedGlobalSecretNames != "null" {
		var names []string
		if err := json.Unmarshal([]byte(preset.SelectedGlobalSecretNames), &names); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse selected_global_secret_names: %v", err)
		} else {
			m.Capabilities.SelectedGlobalSecretNames = &names
		}
	}
	// nil means "all secrets selected" — leave as nil

	// Scalar fields
	m.Capabilities.BrowserMode = preset.BrowserMode
	if m.Capabilities.BrowserMode == "" {
		m.Capabilities.BrowserMode = "none"
	}
	m.Capabilities.UseCodeExecutionMode = preset.UseCodeExecutionMode

	// LLM config — clean up legacy fields when tiered mode is active
	if len(preset.LLMConfig) > 0 {
		var llmConfig database.PresetLLMConfig
		if err := json.Unmarshal(preset.LLMConfig, &llmConfig); err != nil {
			log.Printf("[WARN] ManifestFromPreset: failed to parse llm_config: %v", err)
		} else {
			cleanLLMConfigForManifest(&llmConfig)
			m.Capabilities.LLMConfig = &llmConfig
		}
	}

	// Ownership
	if preset.EmployeeID.Valid && preset.EmployeeID.String != "" {
		empID := preset.EmployeeID.String
		m.Ownership.EmployeeID = &empID
	}

	// Timestamps from preset
	m.CreatedAt = preset.CreatedAt.UTC().Format(time.RFC3339)
	m.UpdatedAt = preset.UpdatedAt.UTC().Format(time.RFC3339)

	return m, nil
}

// --- Internal helpers ---

// cleanLLMConfigForManifest strips legacy fields from LLM config when tiered mode is active.
// In tiered mode, execution uses tier_1/tier_2/tier_3 — the legacy provider/model_id,
// execution_llm, and learning_llm fields are unused noise. Only phase_llm is kept
// (used by the workflow builder phase chat).
func cleanLLMConfigForManifest(cfg *database.PresetLLMConfig) {
	if cfg == nil {
		return
	}
	if cfg.LLMAllocationMode == "tiered" && cfg.TieredConfig != nil {
		cfg.Provider = ""
		cfg.ModelID = ""
		cfg.ExecutionLLM = nil
		cfg.LearningLLM = nil
		// Keep PhaseLLM — used by workflow_phase builder chat
		// Keep TieredConfig — the actual execution config
		// Keep feature toggles (UseKnowledgebase, etc.)
	}
}

// applyManifestDefaults fills in defaults for fields that may be missing from older schema versions.
func applyManifestDefaults(m *WorkflowManifest) {
	if m.SchemaVersion == 0 {
		m.SchemaVersion = 1
	}
	if m.Capabilities.BrowserMode == "" {
		m.Capabilities.BrowserMode = "none"
	}
	if m.Capabilities.SelectedServers == nil {
		m.Capabilities.SelectedServers = []string{}
	}
	if m.Capabilities.SelectedTools == nil {
		m.Capabilities.SelectedTools = []string{}
	}
	if m.Capabilities.SelectedSkills == nil {
		m.Capabilities.SelectedSkills = []string{}
	}
	if m.Capabilities.SelectedSecrets == nil {
		m.Capabilities.SelectedSecrets = []string{}
	}
	if m.Schedules == nil {
		m.Schedules = []WorkflowSchedule{}
	}
	// Auto-assign IDs to schedules that pre-date the ID field.
	for i := range m.Schedules {
		if m.Schedules[i].ID == "" {
			m.Schedules[i].ID = uuid.New().String()
		}
	}
}

// ensureManifestSlices ensures all slice fields are non-nil so they serialize as [] not null.
func ensureManifestSlices(m *WorkflowManifest) {
	applyManifestDefaults(m) // reuses the same logic
}

// --- Workspace discovery ---

// DiscoverWorkflowManifests scans all workspace folders to find those with workflow.json.
// It calls the workspace API to list top-level folders, then checks each for a manifest.
func DiscoverWorkflowManifests(ctx context.Context) ([]DiscoveredWorkflow, error) {
	// List all workspace folders via the workspace API
	folders, err := listWorkspaceFolders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workspace folders: %w", err)
	}

	var results []DiscoveredWorkflow
	for _, folder := range folders {
		manifest, exists, err := ReadWorkflowManifest(ctx, folder)
		if err != nil {
			log.Printf("[WARN] DiscoverWorkflowManifests: error reading manifest from %s: %v", folder, err)
			continue
		}
		if !exists {
			continue
		}

		results = append(results, DiscoveredWorkflow{
			WorkspacePath: folder,
			Manifest:      manifest,
		})
	}

	return results, nil
}

// DiscoveredWorkflow pairs a manifest with its workspace path.
type DiscoveredWorkflow struct {
	WorkspacePath string            `json:"workspace_path"`
	Manifest      *WorkflowManifest `json:"manifest"`
}

// listWorkspaceFolders returns all top-level folders under the "Workflow" namespace.
// Uses the workspace API's /api/documents?folder=Workflow&max_depth=1 endpoint.
func listWorkspaceFolders(ctx context.Context) ([]string, error) {
	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("folder", "Workflow")
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse workspace API response: { success: true, data: [ { filepath, type, children } ] }
	var apiResp struct {
		Success bool `json:"success"`
		Data    []struct {
			Filepath string `json:"filepath"`
			Type     string `json:"type"`
			Children []struct {
				Filepath string `json:"filepath"`
				Type     string `json:"type"`
			} `json:"children"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse folder listing: %w", err)
	}

	var folders []string
	// The root "Workflow" folder is data[0], its children are the workflow workspaces
	for _, root := range apiResp.Data {
		for _, child := range root.Children {
			if child.Type == "folder" && child.Filepath != "" {
				folders = append(folders, child.Filepath)
			}
		}
	}

	return folders, nil
}
