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

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"

	"github.com/google/uuid"
)

// Current manifest schema version. This is the JSON shape version.
const WorkflowManifestSchemaVersion = 1

// WorkflowContractCurrentVersion is the product-managed workflow behavior
// contract version. Unlike schema_version, this gates agent-run workflow
// upgrades: Pulse can add version-specific messages and stamp this value only
// after the workflow has been checked or migrated.
const WorkflowContractCurrentVersion = "1.0.11"

const workflowContractInitialVersion = "1.0.0"
const workflowContractMessageSequenceCodeVersion = "1.0.10"
const workflowContractPulseHistoryVersion = "1.0.11"

const (
	DefaultRunRetentionCount = 5
	MaxRunRetentionCount     = 50
)

// WorkflowManifest is the top-level workflow.json structure that lives in each workspace.
type WorkflowManifest struct {
	SchemaVersion     int                       `json:"schema_version"`
	ID                string                    `json:"id"`
	Version           string                    `json:"version,omitempty"`
	Label             string                    `json:"label"`
	Capabilities      WorkflowCapabilities      `json:"capabilities"`
	ExecutionDefs     WorkflowExecutionDefaults `json:"execution_defaults"`
	Schedules         []WorkflowSchedule        `json:"schedules"`
	CreatedAt         string                    `json:"created_at,omitempty"`
	UpdatedAt         string                    `json:"updated_at,omitempty"`
	RunRetentionCount *int                      `json:"run_retention_count,omitempty"`

	// Auto-improvement framework fields. See docs/workflow/auto_improvement_framework.md.
	//
	// Only fields that drive HARD behavioral gates live here. Workflow profile
	// (deterministic / exploratory / contextual classification, plan-stability
	// guidance, dual-mode declarations) lives as prose in builder/improve.html
	// — the agent reads improve.html on every improvement turn anyway, and prose
	// captures nuance that enums can't.
	OversightMode OversightMode `json:"oversight_mode,omitempty"`

	// PostRunMonitor opts this workflow into the post-run monitor: a compact
	// evidence scan that runs after each scheduled run and records Bug +
	// Goal verdicts (and any silent-failure / drift finding) into the workflow
	// log. It is a deliberate choice — the user or the builder agent enables it
	// for workflows where silent breakage matters (QA, production, monitoring,
	// compliance). Unset/false = off (no monitor pass, no extra cost).
	PostRunMonitor *bool `json:"post_run_monitor,omitempty"`

	// Backup is declarative configuration for builder-agent managed backup.
	// Operational status is written separately to backup/status.json so normal
	// backup attempts do not churn workflow.json.
	Backup *WorkflowBackupConfig `json:"backup,omitempty"`

	// Publish is declarative config for builder-agent managed publishing of the
	// workflow's HTML artifacts (Pulse log, report dashboard) to a public URL.
	// Operational status (incl. the URL) is written to publish/status.json so
	// publish attempts do not churn workflow.json.
	Publish *WorkflowPublishConfig `json:"publish,omitempty"`

	// MalformedConfig lists optional config blocks (e.g. "backup", "publish") that
	// failed to parse and were dropped so the workflow could still load. Transient
	// (never serialized): set during ReadWorkflowManifest, used to avoid clobbering
	// the on-disk config on write-back and to flag the issue.
	MalformedConfig []string `json:"-"`
}

// MonitorEnabled reports whether the post-run monitor should run for this
// workflow. It is opt-in: only an explicit true enables it.
func (m *WorkflowManifest) MonitorEnabled() bool {
	return m != nil && m.PostRunMonitor != nil && *m.PostRunMonitor
}

type WorkflowBackupConfig struct {
	Enabled      bool                        `json:"enabled"`
	Mode         string                      `json:"mode,omitempty"` // "agent" (default)
	Triggers     WorkflowBackupTriggers      `json:"triggers,omitempty"`
	Destinations []WorkflowBackupDestination `json:"destinations,omitempty"`
	Notes        string                      `json:"notes,omitempty"`
}

type WorkflowBackupTriggers struct {
	AfterScheduledRun bool `json:"after_scheduled_run,omitempty"`
	AfterManualRun    bool `json:"after_manual_run,omitempty"`
}

type WorkflowBackupDestination struct {
	ID         string   `json:"id"`
	Type       string   `json:"type"`     // git, object_store, huggingface, local_zip
	Provider   string   `json:"provider"` // github, git, r2, s3, b2, huggingface, local
	Repo       string   `json:"repo,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Bucket     string   `json:"bucket,omitempty"`
	Prefix     string   `json:"prefix,omitempty"`
	Covers     []string `json:"covers,omitempty"`
	SecretRefs []string `json:"secret_refs,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

// WorkflowPublishConfig is declarative config for publishing the workflow's HTML
// artifacts to a public URL. Provider-agnostic: the destination's provider is a
// free-form string and the per-host deploy logic lives in the publish-strategy
// reference doc, not in Go.
// NOTE: this config is authored by the builder agent, so its sub-fields are kept
// deliberately tolerant. Free-form / variable-shape fields (e.g. targets, which the
// agent may write as plain strings OR rich objects) use json.RawMessage so a shape
// the agent chose can never fail manifest parsing and drop the whole workflow.
type WorkflowPublishConfig struct {
	Enabled       bool                         `json:"enabled"`
	Mode          string                       `json:"mode,omitempty"`           // "agent" (default)
	Targets       []json.RawMessage            `json:"targets,omitempty"`        // strings ("pulse"/"report") or objects — agent's choice
	DashboardMode string                       `json:"dashboard_mode,omitempty"` // "snapshot" (static HTML)
	URL           string                       `json:"url,omitempty"`            // last published URL (agent-written, mirror of status)
	Triggers      WorkflowBackupTriggers       `json:"triggers,omitempty"`
	Destinations  []WorkflowPublishDestination `json:"destinations,omitempty"`
	Notes         string                       `json:"notes,omitempty"`
}

type WorkflowPublishDestination struct {
	ID            string   `json:"id"`
	Provider      string   `json:"provider"`                  // free-form: netlify, vercel, cloudflare-pages, github-pages, s3, ...
	Method        string   `json:"method,omitempty"`          // cli | git | sync
	Site          string   `json:"site,omitempty"`            // project / site / bucket / repo identifier
	SecretName    string   `json:"secret_name,omitempty"`     // global secret holding the deploy token (CI only)
	Visibility    string   `json:"visibility,omitempty"`      // public | private | unguessable-link (agent's choice)
	PublicBaseURL string   `json:"public_base_url,omitempty"` // filled in by the agent after first deploy
	URL           string   `json:"url,omitempty"`             // this destination's published URL
	Covers        []string `json:"covers,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// WorkflowCapabilities stores workflow-wide agent and tool configuration.
type WorkflowCapabilities struct {
	SelectedServers           []string                       `json:"selected_servers"`
	SelectedTools             []string                       `json:"selected_tools"`
	SelectedSkills            []string                       `json:"selected_skills"`
	SelectedSecrets           []string                       `json:"selected_secrets"`
	SelectedGlobalSecretNames *[]string                      `json:"selected_global_secret_names"` // nil = all, [] = none
	BrowserMode               string                         `json:"browser_mode"`
	CDPPorts                  []int                          `json:"cdp_ports,omitempty"`
	UseCodeExecutionMode      bool                           `json:"use_code_execution_mode"`
	LLMConfig                 *workflowtypes.PresetLLMConfig `json:"llm_config,omitempty"`
	Notifications             *WorkflowNotificationConfig    `json:"notifications,omitempty"`
}

// WorkflowNotificationConfig contains only safe references. Credential values
// are kept in the encrypted secret store and resolved immediately before a run.
type WorkflowNotificationConfig struct {
	SlackWebhookSecretName string `json:"slack_webhook_secret_name,omitempty"`
}

// WorkflowExecutionDefaults stores toolbar-level defaults for workflow execution.
type WorkflowExecutionDefaults struct {
	AlwaysUseSameRun bool `json:"always_use_same_run"`
	// Global step overrides (replaces step_override.json)
	DisableLearning              *bool    `json:"disable_learning,omitempty"`
	GlobalSkillObjective         string   `json:"global_skill_objective,omitempty"`
	DisableParallelToolExecution *bool    `json:"disable_parallel_tool_execution,omitempty"`
	ExecutionMaxTurns            *int     `json:"execution_max_turns,omitempty"`
	EnabledCustomTools           []string `json:"enabled_custom_tools,omitempty"`
	WorkshopMode                 string   `json:"workshop_mode,omitempty"` // Workshop builder mode: "builder", "optimizer", "run" (legacy values "ask"/"debugger"/"runner"/"eval"/"output" auto-migrated by server)
}

// WorkflowSchedule represents a cron or calendar schedule stored in the manifest.
type WorkflowSchedule struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	ScheduleType   string                 `json:"schedule_type,omitempty"` // "cron" (default) or "calendar"
	CronExpression string                 `json:"cron_expression"`
	Timezone       string                 `json:"timezone"`
	Enabled        bool                   `json:"enabled"`
	TriggerPayload json.RawMessage        `json:"trigger_payload,omitempty"`
	CalendarItems  []CalendarScheduleItem `json:"calendar_items,omitempty"`
	GroupNames     []string               `json:"group_names,omitempty"`
	Mode           string                 `json:"mode,omitempty"`            // "workshop" for workflow schedules; legacy "workflow" is normalized at runtime
	Messages       []string               `json:"messages,omitempty"`        // Predefined message queue for workshop schedules (sent one-by-one)
	WorkshopMode   string                 `json:"workshop_mode,omitempty"`   // Workshop builder mode for scheduled runs: "run" (default) or "optimizer" (legacy "ask"/"runner"/"debugger" auto-migrated to "run")
	Query          string                 `json:"query,omitempty"`           // Message to execute (multi-agent mode)
	ResumePrevious *bool                  `json:"resume_previous,omitempty"` // Coding-agent CLI only: resume the latest prior thread (same provider) instead of a fresh session each run. nil = default (fresh session); explicit true opts in.
}

// ShouldResumePrevious reports whether a scheduled run should resume the
// workflow's latest coding-agent thread. Resume is opt-in: omitted/null means
// each scheduled run starts fresh.
func (s WorkflowSchedule) ShouldResumePrevious() bool {
	return s.ResumePrevious != nil && *s.ResumePrevious
}

type CalendarScheduleItem struct {
	ID             string          `json:"id,omitempty"`
	Date           string          `json:"date"` // YYYY-MM-DD in schedule timezone
	Time           string          `json:"time"` // HH:MM in schedule timezone
	Description    string          `json:"description,omitempty"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	Messages       []string        `json:"messages,omitempty"`
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
	if m.RunRetentionCount != nil {
		if *m.RunRetentionCount < 1 || *m.RunRetentionCount > MaxRunRetentionCount {
			return fmt.Errorf("run_retention_count must be between 1 and %d", MaxRunRetentionCount)
		}
	}

	// Validate browser mode if set
	if m.Capabilities.BrowserMode != "" {
		validModes := map[string]bool{
			"none": true, "auto": true, "headless": true, "cdp": true,
		}
		if !validModes[m.Capabilities.BrowserMode] {
			return fmt.Errorf("invalid browser_mode: %s", m.Capabilities.BrowserMode)
		}
	}
	if len(m.Capabilities.CDPPorts) > maxCDPPortsPerRun {
		return fmt.Errorf("capabilities.cdp_ports supports at most %d ports", maxCDPPortsPerRun)
	}
	if len(m.Capabilities.CDPPorts) > 0 && m.Capabilities.BrowserMode != "cdp" && m.Capabilities.BrowserMode != "auto" {
		return fmt.Errorf("capabilities.cdp_ports requires browser_mode %q or %q", "cdp", "auto")
	}
	seenCDPPorts := make(map[int]bool, len(m.Capabilities.CDPPorts))
	for _, port := range m.Capabilities.CDPPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid capabilities.cdp_ports entry %d: port must be between 1 and 65535", port)
		}
		if seenCDPPorts[port] {
			return fmt.Errorf("duplicate capabilities.cdp_ports entry: %d", port)
		}
		seenCDPPorts[port] = true
	}

	// Validate LLM config if present
	if m.Capabilities.LLMConfig != nil {
		if err := workflowtypes.ValidatePresetLLMConfigPublic(m.Capabilities.LLMConfig); err != nil {
			return fmt.Errorf("invalid llm_config: %w", err)
		}
	}

	// Validate schedules
	for i, sched := range m.Schedules {
		if sched.ID == "" {
			return fmt.Errorf("schedules[%d].id is required", i)
		}
		if scheduleTypeOrDefault(sched.ScheduleType) == "cron" && sched.CronExpression == "" {
			return fmt.Errorf("schedules[%d].cron_expression is required", i)
		}
		if scheduleTypeOrDefault(sched.ScheduleType) == "calendar" && len(sched.CalendarItems) == 0 {
			return fmt.Errorf("schedules[%d].calendar_items is required for calendar schedules", i)
		}
		// group_names required for workflow/workshop modes, not for multi-agent
		if sched.Mode != "multi-agent" && len(normalizeScheduleGroupNames(sched.GroupNames)) == 0 {
			return fmt.Errorf("schedules[%d].group_names is required", i)
		}
	}

	// Validate auto-improvement framework enum fields if set.
	if m.OversightMode != "" {
		switch m.OversightMode {
		case OversightManual, OversightSupervised, OversightAutonomous:
		default:
			return fmt.Errorf("invalid oversight_mode: %s", m.OversightMode)
		}
	}
	return nil
}

func normalizeScheduleGroupNames(groupNames []string) []string {
	seen := make(map[string]struct{}, len(groupNames))
	normalized := make([]string, 0, len(groupNames))
	for _, groupName := range groupNames {
		trimmed := strings.TrimSpace(groupName)
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

func validateScheduleGroupNamesForWorkspace(ctx context.Context, workspacePath string, groupNames []string) ([]string, error) {
	normalized := normalizeScheduleGroupNames(groupNames)
	if len(normalized) == 0 {
		return nil, fmt.Errorf("group_names is required and must contain at least one group name")
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
		return nil, fmt.Errorf("workflow has no variable groups; schedules must specify at least one valid group name")
	}

	validGroups := make(map[string]struct{}, len(manifest.Groups))
	available := make([]string, 0, len(manifest.Groups))
	for _, group := range manifest.Groups {
		groupName := strings.TrimSpace(group.Name)
		if groupName == "" {
			continue
		}
		if _, exists := validGroups[groupName]; exists {
			continue
		}
		validGroups[groupName] = struct{}{}
		available = append(available, groupName)
	}
	sort.Strings(available)

	for _, groupName := range normalized {
		if _, ok := validGroups[groupName]; !ok {
			return nil, fmt.Errorf("unknown group name %q; available groups: %s", groupName, strings.Join(available, ", "))
		}
	}

	return normalized, nil
}

// --- Default factory ---

// NewWorkflowManifest creates a manifest with defaults.
func NewWorkflowManifest(label string) *WorkflowManifest {
	now := time.Now().UTC().Format(time.RFC3339)
	noGlobalSecrets := []string{}
	return &WorkflowManifest{
		SchemaVersion: WorkflowManifestSchemaVersion,
		ID:            "wf_" + uuid.New().String()[:8],
		Version:       WorkflowContractCurrentVersion,
		Label:         label,
		Capabilities: WorkflowCapabilities{
			SelectedServers:           []string{},
			SelectedTools:             []string{},
			SelectedSkills:            []string{},
			SelectedSecrets:           []string{},
			SelectedGlobalSecretNames: &noGlobalSecrets,
			BrowserMode:               "auto",
		},
		ExecutionDefs: WorkflowExecutionDefaults{},
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
		// Resilience: the backup/publish blocks are authored by the builder agent,
		// so a shape it chose (e.g. a richer `targets`) must NEVER make the whole
		// manifest unparseable and hide the workflow from the UI. Retry with those
		// optional blocks dropped so the workflow still loads; only a genuinely
		// broken core manifest is a hard error.
		stripped, droppedKeys := stripOptionalConfigBlocks([]byte(content))
		if len(droppedKeys) > 0 {
			if err2 := json.Unmarshal(stripped, &m); err2 == nil {
				log.Printf("[MANIFEST] %s: dropped malformed config block(s) %v so the workflow still loads (parse error: %v)", workspacePath, droppedKeys, err)
				m.MalformedConfig = droppedKeys
			} else {
				return nil, false, fmt.Errorf("failed to parse workflow.json: %w", err)
			}
		} else {
			return nil, false, fmt.Errorf("failed to parse workflow.json: %w", err)
		}
	}

	// Track whether any schedule IDs need auto-assignment before applying defaults.
	hadMissingLabel := strings.TrimSpace(m.Label) == ""
	if hadMissingLabel {
		m.Label = workflowLabelFromWorkspacePath(workspacePath)
	}
	hadEmptyScheduleID := false
	for _, s := range m.Schedules {
		if s.ID == "" {
			hadEmptyScheduleID = true
			break
		}
	}

	// Apply defaults for missing fields from older schema versions
	applyManifestDefaults(&m)
	llmConfigMigrated := workflowtypes.NormalizePresetLLMConfig(m.Capabilities.LLMConfig)

	// Persist auto-assigned schedule IDs so subsequent lookups find the same UUID.
	// Skip the write-back when we had to drop a malformed config block on read —
	// rewriting now would silently erase the user's backup/publish config from disk.
	if (hadMissingLabel || hadEmptyScheduleID || llmConfigMigrated) && len(m.MalformedConfig) == 0 {
		if err := WriteWorkflowManifest(ctx, workspacePath, &m); err != nil {
			log.Printf("[WARN] ReadWorkflowManifest: failed to persist manifest migrations for %s: %v", workspacePath, err)
		}
	}
	return &m, true, nil
}

func workflowLabelFromWorkspacePath(workspacePath string) string {
	normalized := strings.Trim(strings.ReplaceAll(strings.TrimSpace(workspacePath), "\\", "/"), "/")
	if normalized == "" {
		return "Workflow"
	}
	if separator := strings.LastIndex(normalized, "/"); separator >= 0 {
		normalized = normalized[separator+1:]
	}
	if normalized == "" {
		return "Workflow"
	}
	return normalized
}

// stripOptionalConfigBlocks removes the agent-authored optional config blocks
// ("backup", "publish") from raw workflow.json so a malformed one of them can't
// fail the whole manifest parse. Returns the stripped bytes and the keys removed.
// If the top-level JSON itself can't be parsed, returns no dropped keys (the
// caller then surfaces the original, genuine parse error).
func stripOptionalConfigBlocks(content []byte) ([]byte, []string) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil {
		return content, nil
	}
	var dropped []string
	for _, key := range []string{"backup", "publish"} {
		if _, ok := top[key]; ok {
			delete(top, key)
			dropped = append(dropped, key)
		}
	}
	if len(dropped) == 0 {
		return content, nil
	}
	stripped, err := json.Marshal(top)
	if err != nil {
		return content, nil
	}
	return stripped, dropped
}

// WriteWorkflowManifest validates and writes workflow.json to a workspace.
func WriteWorkflowManifest(ctx context.Context, workspacePath string, m *WorkflowManifest) error {
	workflowtypes.NormalizePresetLLMConfig(m.Capabilities.LLMConfig)
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

// --- Internal helpers ---

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
	if m.Backup != nil && m.Backup.Mode == "" {
		m.Backup.Mode = "agent"
	}
	// Auto-assign IDs to schedules that pre-date the ID field.
	for i := range m.Schedules {
		if m.Schedules[i].ID == "" {
			m.Schedules[i].ID = uuid.New().String()
		}
	}

	// Auto-improvement framework defaults. oversight_mode is the one hard-gate
	// field that default-fills — typology, plan-stability, and decision-log
	// handling live as prose in builder/improve.html, not as manifest enums.
	if m.OversightMode == "" {
		m.OversightMode = OversightSupervised
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

	resp, err := workspaceHTTPClient.Do(req)
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
