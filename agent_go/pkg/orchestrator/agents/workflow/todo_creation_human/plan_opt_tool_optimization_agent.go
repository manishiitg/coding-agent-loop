package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// StepCurrentToolConfig represents the current tool configuration for a step (mapped from step_config.json)
type StepCurrentToolConfig struct {
	StepID             string   `json:"step_id"`
	SelectedServers    []string `json:"selected_servers,omitempty"`
	SelectedTools      []string `json:"selected_tools,omitempty"`
	EnabledCustomTools []string `json:"enabled_custom_tools,omitempty"`
	HasConfig          bool     `json:"has_config"` // true if step has any tool configuration in step_config.json
}

// HumanControlledTodoPlannerPlanToolOptimizationTemplate holds template variables for tool optimization prompts
type HumanControlledTodoPlannerPlanToolOptimizationTemplate struct {
	WorkspacePath          string
	PlanJSON               string
	StepConfigJSON         string
	CurrentToolConfigsJSON string // Pre-computed mapping of step IDs to their current tool configurations
	PresetServers          string
	PresetTools            string
	AllowedPaths           string
}

// HumanControlledTodoPlannerPlanToolOptimizationAgent optimizes tool selections in step_config.json
type HumanControlledTodoPlannerPlanToolOptimizationAgent struct {
	*agents.BaseOrchestratorAgent
	baseOrchestrator *orchestrator.BaseOrchestrator // Reference to base orchestrator for RequestHumanFeedback
}

// NewHumanControlledTodoPlannerPlanToolOptimizationAgent creates a new plan tool optimization agent
func NewHumanControlledTodoPlannerPlanToolOptimizationAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanToolOptimizationAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanToolOptimizationAgentType,
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanToolOptimizationAgent{
		BaseOrchestratorAgent: baseAgent,
		baseOrchestrator:      baseOrchestrator,
	}
}

// PlanToolOptimizationManager manages plan tool optimization agent creation independently from controller
type PlanToolOptimizationManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator

	// Session and workflow IDs for human feedback
	sessionID  string
	workflowID string

	// Learning LLM config (primary LLM for plan tool optimization agent)
	presetLearningLLM *AgentLLMConfig
}

// NewPlanToolOptimizationManager creates a new PlanToolOptimizationManager
func NewPlanToolOptimizationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetLearningLLM *AgentLLMConfig,
) *PlanToolOptimizationManager {
	return &PlanToolOptimizationManager{
		BaseOrchestrator:  baseOrchestrator,
		sessionID:         sessionID,
		workflowID:        workflowID,
		presetLearningLLM: presetLearningLLM,
	}
}

// stepConfigFileMutex ensures thread-safe access to step_config.json
var stepConfigFileMutex sync.Mutex

// toolConfigChangelogSessionMutex ensures thread-safe access to tool config changelog session tracking
var toolConfigChangelogSessionMutex sync.Mutex

// toolConfigChangelogSessionFile tracks the current changelog file for the active session
// Format: tool-config-changelog-YYYY-MM-DD-HH-MM-SS.json
var toolConfigChangelogSessionFile string

// toolConfigChangelogSessionStartTime tracks when the current session started
var toolConfigChangelogSessionStartTime time.Time

// ToolConfigChangeLogEntry represents a single change entry in the tool config changelog
type ToolConfigChangeLogEntry struct {
	Timestamp   string                  `json:"timestamp"`   // ISO 8601 timestamp
	ChangeType  string                  `json:"change_type"` // "update_tools", "add_tools", "remove_tools"
	StepIDs     []string                `json:"step_ids"`    // Affected step IDs
	Description string                  `json:"description"` // Human-readable description of the change
	Details     string                  `json:"details"`     // Additional details (JSON string of what changed)
	Changes     []ToolConfigFieldChange `json:"changes"`     // Old and new values for each changed field
}

// ToolConfigFieldChange represents a single field change with old and new values
type ToolConfigFieldChange struct {
	StepID   string      `json:"step_id"`   // Step ID that was changed
	Field    string      `json:"field"`     // Field name (selected_servers, selected_tools, etc.)
	OldValue interface{} `json:"old_value"` // Old value (can be nil if field didn't exist)
	NewValue interface{} `json:"new_value"` // New value
}

// ToolConfigChangeLog represents the changelog structure (used for reading multiple files)
type ToolConfigChangeLog struct {
	Entries []ToolConfigChangeLogEntry `json:"entries"`
}

// writeToolConfigChangelogEntry writes a changelog entry to a session-based file in planning/changelog/
// All changes during a single tool optimization agent execution session are written to the same file
// File format: tool-config-changelog-YYYY-MM-DD-HH-MM-SS.json (session start timestamp)
func writeToolConfigChangelogEntry(ctx context.Context, workspacePath string, entry ToolConfigChangeLogEntry, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	toolConfigChangelogSessionMutex.Lock()
	defer toolConfigChangelogSessionMutex.Unlock()

	// Check if we need to start a new session (no active session or session is too old - more than 1 hour)
	now := time.Now()
	if toolConfigChangelogSessionFile == "" || now.Sub(toolConfigChangelogSessionStartTime) > time.Hour {
		// Start new session
		toolConfigChangelogSessionStartTime = now
		toolConfigChangelogSessionFile = fmt.Sprintf("tool-config-changelog-%s.json", now.Format("2006-01-02-15-04-05"))
		logger.Info(fmt.Sprintf("📝 Starting new tool config changelog session: %s", toolConfigChangelogSessionFile))
	}

	// Ensure entry timestamp is set
	if entry.Timestamp == "" {
		entry.Timestamp = now.Format(time.RFC3339)
	}

	changelogPath := filepath.Join(workspacePath, "planning", "changelog", toolConfigChangelogSessionFile)

	// Read existing changelog if it exists
	var changelog ToolConfigChangeLog
	existingContent, err := readFile(ctx, changelogPath)
	if err == nil {
		// Changelog exists, unmarshal it
		if err := json.Unmarshal([]byte(existingContent), &changelog); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to parse existing tool config changelog, creating new one: %v", err))
			changelog = ToolConfigChangeLog{Entries: []ToolConfigChangeLogEntry{}}
		}
	} else {
		// Changelog doesn't exist, create new one
		changelog = ToolConfigChangeLog{Entries: []ToolConfigChangeLogEntry{}}
	}

	// Add new entry
	changelog.Entries = append(changelog.Entries, entry)

	// Write updated changelog
	data, err := json.MarshalIndent(changelog, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal tool config changelog: %w", err), nil)
	}

	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write tool config changelog file: %w", err), nil)
	}

	logger.Info(fmt.Sprintf("📝 Appended tool config changelog entry to %s: %s - %s", toolConfigChangelogSessionFile, entry.ChangeType, entry.Description))
	return nil
}

// resetToolConfigChangelogSession resets the tool config changelog session (call this at the start of a new tool optimization agent execution)
func resetToolConfigChangelogSession() {
	toolConfigChangelogSessionMutex.Lock()
	defer toolConfigChangelogSessionMutex.Unlock()
	toolConfigChangelogSessionFile = ""
	toolConfigChangelogSessionStartTime = time.Time{}
}

// getUpdateStepConfigToolsSchema returns the JSON schema for update_step_config_tools tool
func getUpdateStepConfigToolsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"updated_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step in plan.json that you want to update. Use the step's id field from the plan."
						},
						"selected_servers": {
							"type": "array",
							"items": {"type": "string"},
							"description": "OPTIONAL: Updated MCP server selection. Use ['NO_SERVERS'] to explicitly disable all MCP servers (for steps that only need workspace/human tools). Or provide specific server names like ['aws-s3', 'google-sheets']. If omitted, existing value is preserved."
						},
						"selected_tools": {
							"type": "array",
							"items": {"type": "string"},
							"description": "OPTIONAL: Updated MCP tool selection. Format: ['server:tool'] for specific tools, or ['server:*'] for all tools from a server. If omitted, existing value is preserved."
						},
						"enabled_custom_tools": {
							"type": "array",
							"items": {"type": "string"},
							"description": "OPTIONAL: Updated custom tool selection. Format: ['workspace_tools:*'] for all workspace tools, ['workspace_tools:read_workspace_file'] for specific tools, ['human_tools:human_feedback'] for human tools. If omitted, existing value is preserved. NOTE: Do NOT include read_large_output, search_large_output, or query_large_output - these are large output virtual tools managed separately via enable_large_output_virtual_tools boolean flag."
						},
						"enable_large_output_virtual_tools": {
							"type": "boolean",
							"description": "OPTIONAL: Enable or disable large output virtual tools (read_large_output, search_large_output, query_large_output) for this step. Set to true to enable, false to disable. If omitted, existing value is preserved (default: true if not set)."
						}
					},
					"required": ["step_id"]
				},
				"description": "Steps to update. For each step, provide step_id (required) to identify which step to update, and only include the tool fields you want to change."
			}
		},
		"required": ["updated_steps"]
	}`
}

// readStepConfigFromFile reads step_config.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readStepConfigFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) ([]StepConfig, error) {
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	stepConfigFileMutex.Lock()
	defer stepConfigFileMutex.Unlock()

	content, err := readFile(ctx, configPath)
	if err != nil {
		// File doesn't exist yet - return empty array
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return []StepConfig{}, nil
		}
		return nil, fmt.Errorf(fmt.Sprintf("failed to read step_config.json: %w", err), nil)
	}

	configs, err := ParseStepConfigContent(content)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse step_config.json: %w", err), nil)
	}

	return configs, nil
}

// writeStepConfigToFile writes step configs to step_config.json in object format using BaseOrchestrator's WriteWorkspaceFile
// Format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
func writeStepConfigToFile(ctx context.Context, workspacePath string, configs []StepConfig, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	stepConfigFileMutex.Lock()
	defer stepConfigFileMutex.Unlock()

	// Write in object format with "steps" field
	configFile := StepConfigFile{
		Steps: configs,
	}
	data, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal step_config.json: %w", err), nil)
	}

	if err := writeFile(ctx, configPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write step_config.json: %w", err), nil)
	}

	return nil
}

// PartialStepConfigUpdate represents a partial update to a step's tool configuration (used only in tool schemas)
type PartialStepConfigUpdate struct {
	StepID                        string   `json:"step_id"`                                     // Required: ID of existing step to update
	SelectedServers               []string `json:"selected_servers,omitempty"`                  // Optional: Updated server selection
	SelectedTools                 []string `json:"selected_tools,omitempty"`                    // Optional: Updated tool selection
	EnabledCustomTools            []string `json:"enabled_custom_tools,omitempty"`              // Optional: Updated custom tool selection
	EnableLargeOutputVirtualTools *bool    `json:"enable_large_output_virtual_tools,omitempty"` // Optional: Enable/disable large output virtual tools
}

// mergePartialStepConfigUpdate merges a PartialStepConfigUpdate into an existing StepConfig
func mergePartialStepConfigUpdate(existingConfig *StepConfig, partialUpdate PartialStepConfigUpdate) {
	// Ensure AgentConfigs exists
	if existingConfig.AgentConfigs == nil {
		existingConfig.AgentConfigs = &AgentConfigs{}
	}

	// Update fields only if they are provided (not nil/empty)
	if partialUpdate.SelectedServers != nil {
		existingConfig.AgentConfigs.SelectedServers = partialUpdate.SelectedServers
	}
	if partialUpdate.SelectedTools != nil {
		existingConfig.AgentConfigs.SelectedTools = partialUpdate.SelectedTools
	}
	if partialUpdate.EnabledCustomTools != nil {
		existingConfig.AgentConfigs.EnabledCustomTools = partialUpdate.EnabledCustomTools
	}
	if partialUpdate.EnableLargeOutputVirtualTools != nil {
		existingConfig.AgentConfigs.EnableLargeOutputVirtualTools = partialUpdate.EnableLargeOutputVirtualTools
	}
}

// createUpdateStepConfigToolsExecutor creates an executor function for update_step_config_tools tool
func createUpdateStepConfigToolsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract updated_steps from args
		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid updated_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PartialStepConfigUpdate array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal updated_steps: %w", err), nil)
		}

		var partialUpdates []PartialStepConfigUpdate
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse updated_steps: %w", err), nil)
		}

		// Read current step_config.json
		configs, err := readStepConfigFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read step_config.json: %w", err), nil)
		}

		// Create map of existing step configs by ID
		existingConfigsMap := make(map[string]*StepConfig)
		for i := range configs {
			existingConfigsMap[configs[i].ID] = &configs[i]
		}

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingConfig, exists := existingConfigsMap[partialUpdate.StepID]
			if !exists {
				// Step config doesn't exist - create new one
				newConfig := StepConfig{
					ID:           partialUpdate.StepID,
					AgentConfigs: &AgentConfigs{},
				}
				mergePartialStepConfigUpdate(&newConfig, partialUpdate)
				configs = append(configs, newConfig)
			} else {
				// Step config exists - merge update
				mergePartialStepConfigUpdate(existingConfig, partialUpdate)
			}
		}

		// Write updated step_config.json
		if err := writeStepConfigToFile(ctx, workspacePath, configs, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write step_config.json: %w", err), nil)
		}

		// Write changelog entry for tool configuration changes
		stepIDs := make([]string, 0, len(partialUpdates))
		changeDescriptions := make([]string, 0, len(partialUpdates))
		for _, update := range partialUpdates {
			stepIDs = append(stepIDs, update.StepID)
			changes := []string{}
			if update.SelectedServers != nil {
				changes = append(changes, fmt.Sprintf("servers: %v", update.SelectedServers))
			}
			if update.SelectedTools != nil {
				changes = append(changes, fmt.Sprintf("tools: %v", update.SelectedTools))
			}
			if update.EnabledCustomTools != nil {
				changes = append(changes, fmt.Sprintf("custom_tools: %v", update.EnabledCustomTools))
			}
			if update.EnableLargeOutputVirtualTools != nil {
				changes = append(changes, fmt.Sprintf("large_output_virtual_tools: %v", *update.EnableLargeOutputVirtualTools))
			}
			if len(changes) > 0 {
				changeDescriptions = append(changeDescriptions, fmt.Sprintf("%s: %s", update.StepID, strings.Join(changes, ", ")))
			}
		}

		// Determine change type based on what was updated
		changeType := "update_tools"
		detailsJSON, _ := json.MarshalIndent(partialUpdates, "", "  ")

		changelogEntry := ToolConfigChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  changeType,
			StepIDs:     stepIDs,
			Description: fmt.Sprintf("Updated tool configurations for %d step(s): %s", len(partialUpdates), strings.Join(changeDescriptions, "; ")),
			Details:     string(detailsJSON),
		}

		if err := writeToolConfigChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write tool config changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated tool configurations for %d step(s) in step_config.json", len(partialUpdates)))
		return fmt.Sprintf("Successfully updated tool configurations for %d step(s) in step_config.json", len(partialUpdates)), nil
	}
}

// createPlanToolOptimizationAgent creates and sets up a plan tool optimization agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
func (ptom *PlanToolOptimizationManager) createPlanToolOptimizationAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to planning/, learnings/, and runs/ folders, write access to planning/step_config.json only
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	runsPath := fmt.Sprintf("%s/runs", workspacePath)

	// Agent has read-only access to planning/ folder (for plan.json), learnings folder (for learning files), and runs/ folder (for logs/)
	// Write access to planning/step_config.json only
	readPaths := []string{planningPath, learningsPath, runsPath}

	// Step-specific learnings are always enabled - folders are at workspace root using step IDs
	ptom.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings enabled - agent can access step-specific folders in learnings/ (using step IDs from plan.json)"))
	ptom.GetLogger().Info(fmt.Sprintf("📁 Logs access enabled - agent can access execution logs in runs/*/logs/step-*/"))

	writePaths := []string{planningPath} // Write access to planning/ folder (for step_config.json)
	ptom.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	ptom.GetLogger().Info(fmt.Sprintf("🔧 Setting folder guard for plan tool optimization agent - Read paths: %v, Write paths: %v (read-only access to planning/, learnings/, and runs/ folders, write access to planning/step_config.json)", readPaths, writePaths))

	// Use preset learning LLM if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := ptom.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if ptom.presetLearningLLM != nil && ptom.presetLearningLLM.Provider != "" && ptom.presetLearningLLM.ModelID != "" {
		// Use preset learning LLM
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              ptom.presetLearningLLM.Provider,
			ModelID:               ptom.presetLearningLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		ptom.GetLogger().Info(fmt.Sprintf("🔧 Using preset learning LLM for plan tool optimization: %s/%s", ptom.presetLearningLLM.Provider, ptom.presetLearningLLM.ModelID))
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		ptom.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default tool optimization LLM: %s/%s", ptom.GetProvider(), ptom.GetModel()))
	}

	// Use workspace tools directly - they already include human_feedback (created by createCustomTools in server.go)
	// No need to add human tools separately as they're already combined in WorkspaceTools
	allTools := ptom.WorkspaceTools
	allExecutors := ptom.WorkspaceToolExecutors

	// Create agent config with the selected LLM config
	config := ptom.CreateStandardAgentConfigWithLLM("plan-tool-optimization-agent", 100, agents.OutputFormatStructured, llmConfigToUse)

	// Explicitly disable code execution mode for tool optimization agent
	// This agent only needs file read/write operations, not Go code generation
	config.UseCodeExecutionMode = false
	ptom.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode disabled for plan tool optimization agent - using direct tool access"))

	// Tool optimization agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Large output virtual tools are enabled for tool optimization (agent may generate large analysis reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewHumanControlledTodoPlannerPlanToolOptimizationAgent(cfg, logger, tracer, eventBridge, ptom.BaseOrchestrator)
	}

	// Use base orchestrator's CreateAndSetupStandardAgentWithConfig to avoid code duplication
	// This handles initialization, event bridge connection, and tool registration
	// Set overwriteSystemPrompt to true for tool optimization agent (replaces default MCP prompt with agent-specific prompt)
	agent, err := ptom.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		"plan-tool-optimization",
		0, 0, // step, iteration
		createAgentFunc,
		allTools,
		allExecutors,
		true, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to create and setup plan tool optimization agent: %w", err), nil)
	}

	return agent, nil
}

// PlanToolOptimizationOnly runs only the plan tool optimization phase (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
// stepID is optional - if provided, the agent will focus only on that specific step
func (ptom *PlanToolOptimizationManager) PlanToolOptimizationOnly(ctx context.Context, workspacePath string, stepID string) (string, error) {
	ptom.GetLogger().Info(fmt.Sprintf("🔧 Starting standalone plan tool optimization for workspace: %s", workspacePath))

	// Reset changelog session at the start of a new tool optimization execution
	resetToolConfigChangelogSession()

	// Set workspace path
	ptom.SetWorkspacePath(workspacePath)

	// Check if plan.json exists - REQUIRED for tool optimization
	planPath := fmt.Sprintf("%s/planning/plan.json", ptom.GetWorkspacePath())
	planExist, existingPlan, err := ptom.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to check for existing plan: %w", err), nil)
	}
	if !planExist {
		return "", fmt.Errorf(fmt.Sprintf("plan.json not found at %s - planning must be run first as a separate phase", planPath), nil)
	}

	// Plan exists - use it for tool optimization
	ptom.GetLogger().Info(fmt.Sprintf("✅ Found plan.json with %d steps for tool optimization", len(existingPlan.Steps)))

	// Read current step_config.json
	stepConfigs, err := readStepConfigFromFile(ctx, ptom.GetWorkspacePath(), ptom.ReadWorkspaceFile)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to read step_config.json: %w", err), nil)
	}

	// Get preset code execution mode for filtering
	presetCodeExecMode := ptom.GetUseCodeExecutionMode()

	// Create mapping of step IDs to their current tool configurations
	currentToolConfigsMap := createCurrentToolConfigsMapping(stepConfigs, existingPlan, presetCodeExecMode)
	currentToolConfigsJSONBytes, err := json.MarshalIndent(currentToolConfigsMap, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal current tool configs mapping to JSON: %w", err), nil)
	}

	// Create lookup map for tool counts (step ID -> StepCurrentToolConfig)
	toolConfigsLookup := make(map[string]*StepCurrentToolConfig)
	for i := range currentToolConfigsMap {
		toolConfigsLookup[currentToolConfigsMap[i].StepID] = &currentToolConfigsMap[i]
	}

	// Create minimal plan with essential step information (including tool counts)
	minimalPlan := createMinimalPlan(existingPlan, toolConfigsLookup, stepConfigs, presetCodeExecMode)
	planJSONBytes, err := json.MarshalIndent(minimalPlan, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal minimal plan to JSON: %w", err), nil)
	}

	// Prepare step_config.json for template
	stepConfigJSONBytes, err := json.MarshalIndent(stepConfigs, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal step_config.json to JSON: %w", err), nil)
	}

	// Get preset tools info for context
	presetServers := ptom.GetSelectedServers()
	presetTools := ptom.GetSelectedTools()
	presetServersJSON, _ := json.Marshal(presetServers)
	presetToolsJSON, _ := json.Marshal(presetTools)

	// Check for and load variables.json (non-blocking if missing)
	var variablesManifest *VariablesManifest
	variablesPath := fmt.Sprintf("%s/variables/variables.json", ptom.GetWorkspacePath())
	variablesContent, err := ptom.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Variables file doesn't exist - this is OK, tool optimization can proceed without variables
		ptom.GetLogger().Info(fmt.Sprintf("ℹ️ No variables.json found at %s - proceeding without variables", variablesPath))
	} else {
		// Parse variables.json
		var manifest VariablesManifest
		if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
			ptom.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse variables.json: %v - proceeding without variables", err))
		} else {
			variablesManifest = &manifest
			ptom.GetLogger().Info(fmt.Sprintf("✅ Loaded %d variables for tool optimization context", len(manifest.Variables)))
		}
	}

	// Create tool optimization agent
	toolOptimizationAgent, err := ptom.createPlanToolOptimizationAgent(ctx, ptom.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to create plan tool optimization agent: %w", err), nil)
	}

	// Register custom tool for updating step_config.json
	// Get the underlying MCP agent
	baseAgent := toolOptimizationAgent.(*HumanControlledTodoPlannerPlanToolOptimizationAgent).GetBaseAgent()
	if baseAgent == nil {
		return "", fmt.Errorf(fmt.Sprintf("base agent is not initialized"), nil)
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return "", fmt.Errorf(fmt.Sprintf("MCP agent is not initialized"), nil)
	}

	// Parse schema and register the custom tool
	updateSchema := getUpdateStepConfigToolsSchema()
	updateParams, err := parseSchemaForToolParameters(updateSchema)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to parse update schema: %w", err), nil)
	}

	// Get logger from MCP agent
	logger := mcpAgent.Logger

	// Register custom tool for updating step_config.json
	// Note: human_feedback tool is already available via workspace tools (no need to register separately)
	if err := mcpAgent.RegisterCustomTool(
		"update_step_config_tools",
		"Update tool selections for specific steps in step_config.json. Provide step_id (required) to identify which step to update, and only include the tool fields you want to change (selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools). The step_config.json file is updated immediately when this tool is called. For selected_servers: Use ['NO_SERVERS'] if step requires no MCP servers (only workspace/human tools), or provide specific server names. NOTE: Do NOT include read_large_output, search_large_output, or query_large_output in enabled_custom_tools - these are large output virtual tools managed separately via enable_large_output_virtual_tools boolean flag. If you see these tools used in learnings, set enable_large_output_virtual_tools to true.",
		updateParams,
		createUpdateStepConfigToolsExecutor(ptom.GetWorkspacePath(), logger, ptom.ReadWorkspaceFile, ptom.WriteWorkspaceFile),
		"workflow",
	); err != nil {
		return "", fmt.Errorf("failed to register update_step_config_tools tool: %w", err)
	}

	// Create mapping of step IDs to their learnings folder paths (excluding code exec mode steps)
	stepLearningsFolderMapping := createStepLearningsFolderMapping(stepConfigs, existingPlan, presetCodeExecMode, ptom.GetWorkspacePath())
	stepLearningsFolderMappingJSONBytes, err := json.MarshalIndent(stepLearningsFolderMapping, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal step learnings folder mapping to JSON: %w", err), nil)
	}
	ptom.GetLogger().Info(fmt.Sprintf("✅ Created learnings folder mapping for %d steps (excluding code exec mode steps)", len(stepLearningsFolderMapping)))

	// Create mapping of step IDs to their logs folder paths (excluding code exec mode steps)
	stepLogsFolderMapping := createStepLogsFolderMapping(existingPlan, stepConfigs, presetCodeExecMode, ptom.GetWorkspacePath())
	stepLogsFolderMappingJSONBytes, err := json.MarshalIndent(stepLogsFolderMapping, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal step logs folder mapping to JSON: %w", err), nil)
	}
	ptom.GetLogger().Info(fmt.Sprintf("✅ Created logs folder mapping for %d steps", len(stepLogsFolderMapping)))

	// Extract actual tool usage from logs (if available)
	// This scans conversation history files to find tools that were actually used
	toolUsageSummary := extractToolUsageFromLogs(ctx, ptom.GetWorkspacePath(), stepLogsFolderMapping, ptom.ReadWorkspaceFile, ptom.GetLogger())
	toolUsageSummaryJSONBytes, err := json.MarshalIndent(toolUsageSummary, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal tool usage summary to JSON: %w", err), nil)
	}
	ptom.GetLogger().Info(fmt.Sprintf("✅ Extracted tool usage from logs for %d steps", len(toolUsageSummary)))

	// Prepare template variables
	// Use actual workspace path so agent can navigate correctly
	// Explicitly list allowed paths for the agent (learnings folder and logs)
	allowedPaths := "['planning/', 'learnings/', 'learnings/', 'runs/']"
	toolOptimizationTemplateVars := map[string]string{
		"WorkspacePath":                  ptom.GetWorkspacePath(),
		"PlanJSON":                       string(planJSONBytes),
		"StepConfigJSON":                 string(stepConfigJSONBytes),
		"CurrentToolConfigsJSON":         string(currentToolConfigsJSONBytes),
		"StepLearningsFolderMappingJSON": string(stepLearningsFolderMappingJSONBytes),
		"StepLogsFolderMappingJSON":      string(stepLogsFolderMappingJSONBytes),
		"ToolUsageSummaryJSON":           string(toolUsageSummaryJSONBytes),
		"PresetServers":                  string(presetServersJSON),
		"PresetTools":                    string(presetToolsJSON),
		"AllowedPaths":                   allowedPaths,
		"SessionID":                      ptom.sessionID,
		"WorkflowID":                     ptom.workflowID,
	}

	// Add variable names if available (for context about variables in plan)
	if variableNames := FormatVariableNames(variablesManifest); variableNames != "" {
		toolOptimizationTemplateVars["VariableNames"] = variableNames
		ptom.GetLogger().Info(fmt.Sprintf("✅ Added variable names to tool optimization template vars"))
	}

	// Add step ID if provided (for step-specific execution)
	if stepID != "" {
		toolOptimizationTemplateVars["StepID"] = stepID
		ptom.GetLogger().Info(fmt.Sprintf("✅ Added step ID to tool optimization template vars: %s", stepID))
	}

	// Execute tool optimization agent
	ptom.GetLogger().Info(fmt.Sprintf("🔧 Executing plan tool optimization agent..."))
	if stepID != "" {
		ptom.GetLogger().Info(fmt.Sprintf("🔧 Step-specific execution for step: %s", stepID))
	}
	result, conversationHistory, err := toolOptimizationAgent.Execute(ctx, toolOptimizationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("plan tool optimization agent execution failed: %w", err), nil)
	}

	ptom.GetLogger().Info(fmt.Sprintf("✅ Plan tool optimization completed successfully"))
	ptom.GetLogger().Info(fmt.Sprintf("🔧 Tool optimization result: %s", result))

	_ = conversationHistory // Conversation history not used for standalone tool optimization

	return result, nil
}

// MinimalPlanStep represents a step with essential fields for tool optimization
type MinimalPlanStep struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	SuccessCriteria string `json:"success_criteria,omitempty"`
	HasLoop         bool   `json:"has_loop,omitempty"`
	LoopDescription string `json:"loop_description,omitempty"`
	// Tool counts from current configuration (for user selection guidance)
	CurrentToolCount int  `json:"current_tool_count,omitempty"` // Total count of currently configured tools
	HasConfig        bool `json:"has_config,omitempty"`         // Whether step has any tool configuration
}

// isCodeExecutionModeEnabled checks if code execution mode is enabled for given agent configs
// Returns true if agentConfigs explicitly has UseCodeExecutionMode=true OR if preset is enabled and agentConfigs doesn't explicitly disable it
// This follows the same logic as getCodeExecutionMode in controller_agent_factory.go but as a standalone helper
func isCodeExecutionModeEnabled(agentConfigs *AgentConfigs, presetCodeExecMode bool) bool {
	// If step has explicit code exec mode setting, use it
	if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
		return *agentConfigs.UseCodeExecutionMode
	}

	// Otherwise, use preset default
	return presetCodeExecMode
}

// isStepCodeExecutionModeEnabled checks if a step has code execution mode enabled by looking up its config
func isStepCodeExecutionModeEnabled(stepID string, stepConfigs []StepConfig, presetCodeExecMode bool) bool {
	// Find step config by ID
	var agentConfigs *AgentConfigs
	for i := range stepConfigs {
		if stepConfigs[i].ID == stepID {
			agentConfigs = stepConfigs[i].AgentConfigs
			break
		}
	}

	return isCodeExecutionModeEnabled(agentConfigs, presetCodeExecMode)
}

// MinimalPlan represents a plan with essential step information for tool optimization
type MinimalPlan struct {
	Steps []MinimalPlanStep `json:"steps"`
}

// createMinimalPlan creates a minimal plan with essential step information from the full plan
// Recursively handles branch steps (if_true_steps, if_false_steps)
// Excludes steps that have code execution mode enabled
// currentToolConfigsMap: pre-computed mapping of step IDs to their current tool configurations
// stepConfigs: step configurations to check for code execution mode
// presetCodeExecMode: preset code execution mode setting
func createMinimalPlan(fullPlan *PlanningResponse, currentToolConfigsMap map[string]*StepCurrentToolConfig, stepConfigs []StepConfig, presetCodeExecMode bool) *MinimalPlan {
	var minimalSteps []MinimalPlanStep

	var extractSteps func(steps []PlanStepInterface)
	extractSteps = func(steps []PlanStepInterface) {
		for _, step := range steps {
			// Skip steps with code execution mode enabled
			if isStepCodeExecutionModeEnabled(step.GetID(), stepConfigs, presetCodeExecMode) {
				// Still process branch steps even if parent is skipped
				if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
					if len(conditionalStep.IfTrueSteps) > 0 {
						extractSteps(conditionalStep.IfTrueSteps)
					}
					if len(conditionalStep.IfFalseSteps) > 0 {
						extractSteps(conditionalStep.IfFalseSteps)
					}
				}
				continue
			}

			// Get current tool configuration for this step
			currentConfig := currentToolConfigsMap[step.GetID()]
			toolCount := 0
			hasConfig := false
			if currentConfig != nil {
				hasConfig = currentConfig.HasConfig
				// Count total tools: servers + tools + custom tools
				if len(currentConfig.SelectedServers) > 0 && currentConfig.SelectedServers[0] != "NO_SERVERS" {
					toolCount += len(currentConfig.SelectedServers)
				}
				toolCount += len(currentConfig.SelectedTools)
				toolCount += len(currentConfig.EnabledCustomTools)
			}

			// Get loop info (only for RegularPlanStep)
			hasLoop := false
			loopDescription := ""
			if regularStep, ok := step.(*RegularPlanStep); ok {
				hasLoop = regularStep.HasLoop
				loopDescription = regularStep.LoopDescription
			}

			minimalSteps = append(minimalSteps, MinimalPlanStep{
				ID:               step.GetID(),
				Title:            step.GetTitle(),
				Description:      step.GetDescription(),
				SuccessCriteria:  step.GetSuccessCriteria(),
				HasLoop:          hasLoop,
				LoopDescription:  loopDescription,
				CurrentToolCount: toolCount,
				HasConfig:        hasConfig,
			})
			// Recursively extract branch steps (only for ConditionalPlanStep)
			if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
				if len(conditionalStep.IfTrueSteps) > 0 {
					extractSteps(conditionalStep.IfTrueSteps)
				}
				if len(conditionalStep.IfFalseSteps) > 0 {
					extractSteps(conditionalStep.IfFalseSteps)
				}
			}
		}
	}

	extractSteps(fullPlan.Steps)

	return &MinimalPlan{
		Steps: minimalSteps,
	}
}

// StepLearningsFolderMapping maps step IDs to their learnings folder paths
type StepLearningsFolderMapping struct {
	StepID        string `json:"step_id"`
	LearningsPath string `json:"learnings_path"` // Always "learnings/"
}

// createStepLearningsFolderMapping creates a mapping of step IDs to their learnings folder paths
// Returns step-specific paths using step IDs from plan.json:
//   - Regular steps: learnings/{step_id}/ format (at workspace root, not inside runs/)
//   - Branch steps: learnings/{step_id}/ format (where step_id is the branch step's own ID, at workspace root, not inside runs/)
//   - Orchestration sub-agents: learnings/{step_id}/ format (where step_id is the sub-agent's own ID, at workspace root, not inside runs/)
//
// Excludes steps that have code execution mode enabled
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createStepLearningsFolderMapping(stepConfigs []StepConfig, plan *PlanningResponse, presetCodeExecMode bool, workspacePath string) []StepLearningsFolderMapping {
	var mappings []StepLearningsFolderMapping

	// Helper function to extract mappings with branch context
	var extractMappings func(steps []PlanStepInterface, parentStepID string, branchType string, branchIndex int)
	extractMappings = func(steps []PlanStepInterface, parentStepID string, branchType string, branchIndex int) {
		for branchIdx, step := range steps {
			// Skip steps with code execution mode enabled
			if isStepCodeExecutionModeEnabled(step.GetID(), stepConfigs, presetCodeExecMode) {
				// Still process branch steps even if parent is skipped
				if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
					if len(conditionalStep.IfTrueSteps) > 0 {
						extractMappings(conditionalStep.IfTrueSteps, parentStepID, "true", branchIdx)
					}
					if len(conditionalStep.IfFalseSteps) > 0 {
						extractMappings(conditionalStep.IfFalseSteps, parentStepID, "false", branchIdx)
					}
				}
				continue
			}

			// Determine learnings folder (always use step-specific paths with step IDs)
			// All steps (regular, branch, sub-agent) have their own unique step IDs - just use the step ID directly
			learningsPath := fmt.Sprintf("learnings/%s/", step.GetID())

			mappings = append(mappings, StepLearningsFolderMapping{
				StepID:        step.GetID(),
				LearningsPath: learningsPath,
			})

			// Recursively extract branch steps (nested conditionals)
			// Pass current step ID as parent for branch steps
			currentParentStepID := step.GetID()
			if branchType != "" {
				currentParentStepID = parentStepID
			}

			// Only ConditionalPlanStep has branch steps
			if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
				if len(conditionalStep.IfTrueSteps) > 0 {
					extractMappings(conditionalStep.IfTrueSteps, currentParentStepID, "true", branchIdx)
				}
				if len(conditionalStep.IfFalseSteps) > 0 {
					extractMappings(conditionalStep.IfFalseSteps, currentParentStepID, "false", branchIdx)
				}
			}
		}
	}

	// Start extraction with no parent (regular steps)
	extractMappings(plan.Steps, "", "", -1)

	return mappings
}

// StepLogsFolderMapping maps step IDs to their logs folder paths
type StepLogsFolderMapping struct {
	StepID    string   `json:"step_id"`
	LogsPaths []string `json:"logs_paths"` // Array of possible log paths (runs/{iteration}/logs/step-{X}/ or logs/step-{X}/)
}

// createStepLogsFolderMapping creates a mapping of step IDs to their logs folder paths
// Returns step-specific paths:
//   - Regular steps: logs/step-{X}/ format
//   - Branch steps: logs/step-{parentStep}-{true/false}-{branchIdx}/ format
//   - Checks both runs/{iteration}/logs/ and logs/ at workspace root
//
// Excludes steps that have code execution mode enabled
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createStepLogsFolderMapping(plan *PlanningResponse, stepConfigs []StepConfig, presetCodeExecMode bool, workspacePath string) []StepLogsFolderMapping {
	var mappings []StepLogsFolderMapping
	stepNumber := 0

	// Helper function to extract mappings with branch context
	var extractMappings func(steps []PlanStepInterface, parentStepNumber int, branchType string, branchIndex int)
	extractMappings = func(steps []PlanStepInterface, parentStepNumber int, branchType string, branchIndex int) {
		for branchIdx, step := range steps {
			// Skip steps with code execution mode enabled
			if isStepCodeExecutionModeEnabled(step.GetID(), stepConfigs, presetCodeExecMode) {
				// Still process branch steps even if parent is skipped
				if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
					if len(conditionalStep.IfTrueSteps) > 0 {
						extractMappings(conditionalStep.IfTrueSteps, parentStepNumber, "true", branchIdx)
					}
					if len(conditionalStep.IfFalseSteps) > 0 {
						extractMappings(conditionalStep.IfFalseSteps, parentStepNumber, "false", branchIdx)
					}
				}
				continue
			}

			// Determine logs folder (always use step-specific paths)
			var logsPathBase string
			if branchType != "" {
				// Branch step: logs/step-{parentStep}-{true/false}-{branchIdx}/
				logsPathBase = fmt.Sprintf("step-%d-%s-%d", parentStepNumber, branchType, branchIdx)
			} else {
				// Regular step: logs/step-{X}/
				stepNumber++
				logsPathBase = fmt.Sprintf("step-%d", stepNumber)
			}

			// Create array of possible log paths (check both runs/{iteration}/logs/ and logs/ at workspace root)
			logsPaths := []string{
				fmt.Sprintf("runs/*/logs/%s/", logsPathBase), // Pattern for any iteration folder
				fmt.Sprintf("logs/%s/", logsPathBase),        // Direct logs folder at workspace root
			}

			mappings = append(mappings, StepLogsFolderMapping{
				StepID:    step.GetID(),
				LogsPaths: logsPaths,
			})

			// Determine current step number for branch steps (use parent step number for branch steps)
			currentStepNumber := stepNumber
			if branchType != "" {
				currentStepNumber = parentStepNumber
			}

			// Recursively extract branch steps (nested conditionals) - only for ConditionalPlanStep
			if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
				if len(conditionalStep.IfTrueSteps) > 0 {
					extractMappings(conditionalStep.IfTrueSteps, currentStepNumber, "true", branchIdx)
				}
				if len(conditionalStep.IfFalseSteps) > 0 {
					extractMappings(conditionalStep.IfFalseSteps, currentStepNumber, "false", branchIdx)
				}
			}
		}
	}

	// Start extraction with no parent (regular steps)
	extractMappings(plan.Steps, 0, "", -1)

	return mappings
}

// ToolUsageEntry represents a tool that was used in a step's execution
type ToolUsageEntry struct {
	ToolName   string `json:"tool_name"`
	UsageCount int    `json:"usage_count"`  // Number of times tool was called
	LastUsedIn string `json:"last_used_in"` // Path to last conversation file where tool was used
	HasSuccess bool   `json:"has_success"`  // Whether tool was used in a successful execution
}

// StepToolUsageSummary represents tool usage summary for a step
type StepToolUsageSummary struct {
	StepID               string           `json:"step_id"`
	ToolsUsed            []ToolUsageEntry `json:"tools_used"`            // Tools that were actually used
	TotalExecutions      int              `json:"total_executions"`      // Total number of execution attempts found
	SuccessfulExecutions int              `json:"successful_executions"` // Number of successful executions
	HasLogs              bool             `json:"has_logs"`              // Whether any logs were found for this step
}

// extractToolUsageFromLogs scans logs folders for conversation history files and extracts actual tool usage
// This provides a summary of tools that were actually called during execution
func extractToolUsageFromLogs(
	ctx context.Context,
	workspacePath string,
	logsMappings []StepLogsFolderMapping,
	readFile func(context.Context, string) (string, error),
	logger loggerv2.Logger,
) []StepToolUsageSummary {
	var summaries []StepToolUsageSummary

	for _, mapping := range logsMappings {
		summary := StepToolUsageSummary{
			StepID:    mapping.StepID,
			ToolsUsed: []ToolUsageEntry{},
			HasLogs:   false,
		}

		// Track tool usage across all log paths
		toolUsageMap := make(map[string]*ToolUsageEntry)

		// Try each possible log path
		for _, logsPathPattern := range mapping.LogsPaths {
			// For patterns with *, we need to search for actual iteration folders
			if strings.Contains(logsPathPattern, "*") {
				// Pattern: runs/*/logs/step-{X}/
				// Try to find actual iteration folders
				// We'll check common iteration folder names
				commonIterations := []string{"iteration-same", "iteration-1", "iteration-2", "iteration-3"}
				for _, iter := range commonIterations {
					// Extract step path from pattern (e.g., "step-1" from "runs/*/logs/step-1/")
					stepPath := strings.TrimPrefix(strings.TrimSuffix(logsPathPattern, "/"), "runs/*/logs/")
					fullPath := fmt.Sprintf("%s/runs/%s/logs/%s/execution", workspacePath, iter, stepPath)
					extractToolsFromLogsPath(ctx, fullPath, toolUsageMap, readFile, logger, &summary)
				}
			} else {
				// Direct path: logs/step-{X}/
				stepPath := strings.TrimPrefix(strings.TrimSuffix(logsPathPattern, "/"), "logs/")
				fullPath := fmt.Sprintf("%s/logs/%s/execution", workspacePath, stepPath)
				extractToolsFromLogsPath(ctx, fullPath, toolUsageMap, readFile, logger, &summary)
			}
		}

		// Convert map to slice
		for _, entry := range toolUsageMap {
			summary.ToolsUsed = append(summary.ToolsUsed, *entry)
		}

		if len(summary.ToolsUsed) > 0 {
			summary.HasLogs = true
		}

		summaries = append(summaries, summary)
	}

	return summaries
}

// extractToolsFromLogsPath reads conversation history files from a logs path and extracts tool usage
func extractToolsFromLogsPath(
	ctx context.Context,
	logsPath string,
	toolUsageMap map[string]*ToolUsageEntry,
	readFile func(context.Context, string) (string, error),
	logger loggerv2.Logger,
	summary *StepToolUsageSummary,
) {
	// Try to read conversation history files
	// Pattern: execution-attempt-{N}-iteration-{M}-conversation.json
	// We'll try a few common patterns
	for attempt := 1; attempt <= 5; attempt++ {
		for iteration := 0; iteration <= 5; iteration++ {
			conversationPath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d-conversation.json", logsPath, attempt, iteration)
			content, err := readFile(ctx, conversationPath)
			if err != nil {
				// File doesn't exist, try next
				continue
			}

			// Parse conversation history JSON
			var conversationData map[string]interface{}
			if err := json.Unmarshal([]byte(content), &conversationData); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to parse conversation history from %s: %v", conversationPath, err))
				continue
			}

			// Extract conversation_history array
			convHistoryRaw, ok := conversationData["conversation_history"]
			if !ok {
				continue
			}

			// Convert to []llmtypes.MessageContent
			convHistoryJSON, err := json.Marshal(convHistoryRaw)
			if err != nil {
				continue
			}

			var convHistory []llmtypes.MessageContent
			if err := json.Unmarshal(convHistoryJSON, &convHistory); err != nil {
				// Try alternative structure
				continue
			}

			// Extract tool calls from conversation history
			toolCalls := ExtractToolCallsFromMessages(convHistory)
			summary.TotalExecutions++

			// Check if this execution was successful (by checking execution result file)
			executionPath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d.json", logsPath, attempt, iteration)
			execContent, execErr := readFile(ctx, executionPath)
			isSuccess := false
			if execErr == nil {
				var execData map[string]interface{}
				if err := json.Unmarshal([]byte(execContent), &execData); err == nil {
					if execResult, ok := execData["execution_result"].(string); ok {
						// Consider it successful if execution_result doesn't contain error indicators
						isSuccess = !strings.Contains(strings.ToLower(execResult), "error") &&
							!strings.Contains(strings.ToLower(execResult), "failed") &&
							!strings.Contains(strings.ToLower(execResult), "failure")
					}
				}
			}

			if isSuccess {
				summary.SuccessfulExecutions++
			}

			// Track tool usage
			for _, toolName := range toolCalls {
				if entry, exists := toolUsageMap[toolName]; exists {
					entry.UsageCount++
					if isSuccess {
						entry.HasSuccess = true
					}
					entry.LastUsedIn = conversationPath
				} else {
					toolUsageMap[toolName] = &ToolUsageEntry{
						ToolName:   toolName,
						UsageCount: 1,
						LastUsedIn: conversationPath,
						HasSuccess: isSuccess,
					}
				}
			}
		}
	}
}

// createCurrentToolConfigsMapping creates a mapping of step IDs to their current tool configurations from step_config.json
// Excludes steps that have code execution mode enabled
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createCurrentToolConfigsMapping(stepConfigs []StepConfig, plan *PlanningResponse, presetCodeExecMode bool) []StepCurrentToolConfig {
	// Create lookup map: step ID -> AgentConfigs
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigs {
		if stepConfigs[i].ID != "" {
			idConfigMap[stepConfigs[i].ID] = stepConfigs[i].AgentConfigs
		}
	}

	var configs []StepCurrentToolConfig

	var extractConfigs func(steps []PlanStepInterface)
	extractConfigs = func(steps []PlanStepInterface) {
		for _, step := range steps {
			// Skip steps with code execution mode enabled
			if isStepCodeExecutionModeEnabled(step.GetID(), stepConfigs, presetCodeExecMode) {
				// Still process branch steps even if parent is skipped
				if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
					if len(conditionalStep.IfTrueSteps) > 0 {
						extractConfigs(conditionalStep.IfTrueSteps)
					}
					if len(conditionalStep.IfFalseSteps) > 0 {
						extractConfigs(conditionalStep.IfFalseSteps)
					}
				}
				continue
			}

			agentConfigs := idConfigMap[step.GetID()]
			hasConfig := agentConfigs != nil && (len(agentConfigs.SelectedServers) > 0 ||
				len(agentConfigs.SelectedTools) > 0 ||
				len(agentConfigs.EnabledCustomTools) > 0)

			config := StepCurrentToolConfig{
				StepID:    step.GetID(),
				HasConfig: hasConfig,
			}

			if agentConfigs != nil {
				if len(agentConfigs.SelectedServers) > 0 {
					config.SelectedServers = agentConfigs.SelectedServers
				}
				if len(agentConfigs.SelectedTools) > 0 {
					config.SelectedTools = agentConfigs.SelectedTools
				}
				if len(agentConfigs.EnabledCustomTools) > 0 {
					config.EnabledCustomTools = agentConfigs.EnabledCustomTools
				}
			}

			configs = append(configs, config)

			// Recursively extract branch steps (only for ConditionalPlanStep)
			if conditionalStep, ok := step.(*ConditionalPlanStep); ok {
				if len(conditionalStep.IfTrueSteps) > 0 {
					extractConfigs(conditionalStep.IfTrueSteps)
				}
				if len(conditionalStep.IfFalseSteps) > 0 {
					extractConfigs(conditionalStep.IfFalseSteps)
				}
			}
		}
	}

	extractConfigs(plan.Steps)

	return configs
}

// checkExistingPlan checks if a plan.json file already exists in the workspace and returns the parsed plan if found
// Uses the shared readPlanFromFile helper which ensures thread-safe access via planFileMutex
func (ptom *PlanToolOptimizationManager) checkExistingPlan(ctx context.Context, planPath string) (bool, *PlanningResponse, error) {
	ptom.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing plan at %s", planPath))

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, ptom.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			ptom.GetLogger().Info(fmt.Sprintf("📋 No existing plan found: %v", err))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf(fmt.Sprintf("failed to check existing plan: %w", err), nil)
	}

	ptom.GetLogger().Info(fmt.Sprintf("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps)))
	return true, plan, nil
}

// Execute implements the OrchestratorAgent interface
func (agent *HumanControlledTodoPlannerPlanToolOptimizationAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Extract variables from template variables
	workspacePath := templateVars["WorkspacePath"]
	planJSON := templateVars["PlanJSON"]
	stepConfigJSON := templateVars["StepConfigJSON"]
	currentToolConfigsJSON := templateVars["CurrentToolConfigsJSON"]
	stepLearningsFolderMappingJSON := templateVars["StepLearningsFolderMappingJSON"]
	presetServers := templateVars["PresetServers"]
	presetTools := templateVars["PresetTools"]

	// Provide default allowed paths if not present
	allowedPaths := templateVars["AllowedPaths"]
	if allowedPaths == "" {
		allowedPaths = "['planning/', 'learnings/', 'learnings/', 'runs/']"
	}

	// Extract logs mapping and tool usage summary from template vars
	stepLogsFolderMappingJSON := templateVars["StepLogsFolderMappingJSON"]
	toolUsageSummaryJSON := templateVars["ToolUsageSummaryJSON"]

	// Prepare template variables
	toolOptimizationTemplateVars := map[string]string{
		"WorkspacePath":                  workspacePath,
		"PlanJSON":                       planJSON,
		"StepConfigJSON":                 stepConfigJSON,
		"CurrentToolConfigsJSON":         currentToolConfigsJSON,
		"StepLearningsFolderMappingJSON": stepLearningsFolderMappingJSON,
		"StepLogsFolderMappingJSON":      stepLogsFolderMappingJSON,
		"ToolUsageSummaryJSON":           toolUsageSummaryJSON,
		"PresetServers":                  presetServers,
		"PresetTools":                    presetTools,
		"AllowedPaths":                   allowedPaths,
	}

	// Add step ID if provided (for step-specific execution)
	if stepID := templateVars["StepID"]; stepID != "" {
		toolOptimizationTemplateVars["StepID"] = stepID
	}

	// Create template data for tool optimization
	templateData := HumanControlledTodoPlannerPlanToolOptimizationTemplate{
		WorkspacePath:          workspacePath,
		PlanJSON:               planJSON,
		StepConfigJSON:         stepConfigJSON,
		CurrentToolConfigsJSON: currentToolConfigsJSON,
		PresetServers:          presetServers,
		PresetTools:            presetTools,
		AllowedPaths:           allowedPaths,
	}

	// Generate system prompt and user message separately
	systemPrompt := agent.toolOptimizationSystemPromptProcessor(toolOptimizationTemplateVars)
	userMessage := agent.toolOptimizationUserMessageProcessor(toolOptimizationTemplateVars)

	// Get logger from base agent's MCP agent
	baseAgent := agent.GetBaseAgent()
	var logger loggerv2.Logger
	if baseAgent != nil {
		mcpAgent := baseAgent.Agent()
		if mcpAgent != nil && mcpAgent.Logger != nil {
			logger = mcpAgent.Logger
		}
	}

	// Maximum iterations for tool optimization analysis
	maxIterations := 20
	iteration := 0
	currentResult := ""
	currentConversationHistory := conversationHistory

	// Extract sessionID and workflowID from template vars
	sessionID := templateVars["SessionID"]
	workflowID := templateVars["WorkflowID"]

	// Main execution loop with blocking human feedback
	for iteration < maxIterations {
		iteration++
		if logger != nil {
			logger.Info(fmt.Sprintf("🔧 Plan tool optimization agent iteration %d/%d", iteration, maxIterations))
		}

		// Create a simple input processor that returns the user message
		inputProcessor := func(map[string]string) string {
			return userMessage
		}

		// Execute with system prompt and user message (overwrite=true to replace default MCP prompt with agent-specific prompt)
		result, updatedConversationHistory, err := agent.ExecuteWithTemplateValidation(ctx, toolOptimizationTemplateVars, inputProcessor, currentConversationHistory, templateData, systemPrompt, true)
		if err != nil {
			return "", nil, err
		}

		currentResult = result
		currentConversationHistory = updatedConversationHistory

		// Check if plan/step_config modification tools were called in this iteration and emit event immediately
		// This ensures the frontend is notified of plan changes right away, not waiting for agent completion
		if agent.baseOrchestrator != nil {
			// Extract tool calls from this iteration's conversation history
			toolCalls := ExtractToolCallsFromMessages(updatedConversationHistory)
			planUpdateToolCalled := false
			for _, toolName := range toolCalls {
				if IsPlanModificationTool(toolName) || IsStepConfigModificationTool(toolName) {
					planUpdateToolCalled = true
					break
				}
			}

			if planUpdateToolCalled {
				if logger != nil {
					logger.Info(fmt.Sprintf("🔍 [PlanToolOptimizationAgent] Plan/step_config modification tool detected in iteration %d, emitting event immediately", iteration))
				}
				CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, updatedConversationHistory, workspacePath, agent.baseOrchestrator.ReadWorkspaceFile)
			}
		}

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Info(fmt.Sprintf("🔧 Plan tool optimization agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations))
			}

			// Generate unique request ID
			requestID := fmt.Sprintf("plan_tool_optimization_continue_%d_%d", iteration, time.Now().UnixNano())

			// Request human feedback (blocking call)
			approved, feedback, err := agent.baseOrchestrator.RequestHumanFeedback(
				ctx,
				requestID,
				fmt.Sprintf("Plan tool optimization analysis is complete (iteration %d/%d). Would you like to ask more questions or request additional optimizations?", iteration, maxIterations),
				currentResult,
				sessionID,
				workflowID,
			)
			if err != nil {
				if logger != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to get user feedback: %v", err))
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Info(fmt.Sprintf("✅ User approved - plan tool optimization complete"))
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Info(fmt.Sprintf("📝 User provided feedback: %s", feedback))
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Info(fmt.Sprintf("ℹ️ No feedback provided, continuing with same context"))
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Info(fmt.Sprintf("🔧 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations))
			}
			break
		}
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("🔧 Plan tool optimization completed after %d iterations", iteration))
	}

	// Check if step_config modification tools were called and emit event if needed
	// This ensures the frontend is notified of step_config.json changes
	// The frontend merges plan.json + step_config.json, so any change should trigger refresh
	CheckAndEmitPlanUpdateEvent(ctx, agent.baseOrchestrator, currentConversationHistory, workspacePath, agent.baseOrchestrator.ReadWorkspaceFile)

	return currentResult, currentConversationHistory, nil
}

// toolOptimizationSystemPromptProcessor creates the system prompt for plan tool optimization
func (agent *HumanControlledTodoPlannerPlanToolOptimizationAgent) toolOptimizationSystemPromptProcessor(templateVars map[string]string) string {
	// Build variables section if available
	variablesSection := ""
	if variableNames := templateVars["VariableNames"]; variableNames != "" {
		variablesSection = `
## 🔑 AVAILABLE VARIABLES
` + variableNames + `
`
	}

	learningsLocationNote := `
## LEARNING FILES LOCATION

Learning files are stored at workspace root (not inside runs/):
- **Base learnings folder**: {WorkspacePath}/learnings/ (for reading existing shared learnings)
- **Step-specific learnings**: {WorkspacePath}/learnings/{step_id}/ (using step IDs from plan.json, where step_id is the step's own ID)

**Structure**:
- All steps (regular, branch, sub-agent) use their own step ID: learnings/{step_id}/
- Example: learnings/deploy-application/, learnings/auth-error-handler/, learnings/verify-deployment-health/
- Step IDs are stable identifiers that don't change when steps are reordered

The StepLearningsFolderMappingJSON provides step-specific paths. Check BOTH the base learnings folder (learnings/) AND step-specific folders (learnings/{step_id}/) when extracting tools.

## EXECUTION LOGS LOCATION

Execution logs contain ACTUAL tool usage from previous runs:
- Logs location: {WorkspacePath}/runs/{iteration}/logs/step-{X}/ or {WorkspacePath}/logs/step-{X}/
- Conversation history: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}-conversation.json
- Execution results: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}.json

The StepLogsFolderMappingJSON provides step-specific log paths. The ToolUsageSummaryJSON shows tools that were ACTUALLY USED in successful executions.

**CRITICAL**: Always check logs/ for ACTUAL tool usage before making suggestions. Only suggest tools that appear in successful execution logs.

## READING EXECUTION OUTPUT FILES

Execution output files are stored in logs folders. Use search_large_output tool (if enabled) or read_workspace_file to read them.

### Conversation History File Structure

**File location**: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}-conversation.json

**JSON Structure**:
{
  "step_index": 1,
  "step_path": "step-1",
  "retry_attempt": 1,
  "loop_iteration": 0,
  "conversation_history": [
    {
      "Role": "ai",  // or "role": "ai" (depending on serialization)
      "Parts": [     // or "parts": [] (depending on serialization)
        {
          "FunctionCall": {  // Tool call structure
            "Name": "tool_name",
            "Arguments": "{...}"
          }
          // OR alternative structure:
          // "type": "tool_call",
          // "content": {
          //   "function_name": "tool_name",
          //   "function_args": "{...}"
          // }
        }
      ]
    }
  ],
  "timestamp": "2025-01-27T14:30:25Z"
}

**To extract tool names from conversation history:**
- Use search_large_output with operation="query" and jq query: .conversation_history[] | select(.Role == "ai" or .role == "ai") | .Parts[]? // .parts[]? | select(.FunctionCall != null or .type == "tool_call") | (.FunctionCall.Name // .content.function_name)
- Or read the file and parse manually to find all tool calls in assistant messages

### Execution Result File Structure

**File location**: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}.json

**JSON Structure**:
{
  "step_index": 1,
  "step_path": "step-1",
  "retry_attempt": 1,
  "loop_iteration": 0,
  "execution_result": "The actual execution output text...",
  "timestamp": "2025-01-27T14:30:25Z"
}

**To read execution result:**
- Use search_large_output with operation="query" and jq query: .execution_result
- Or use read_workspace_file to read the entire file
`

	return `# Plan Tool Optimization Agent

## PURPOSE
Analyze ACTUAL tool usage from execution logs and learnings to optimize step_config.json. Be CONSERVATIVE - only suggest tools that were actually used successfully or are clearly needed based on step requirements.

` + variablesSection + learningsLocationNote + `## WORKFLOW

### Step 1: Ask User Which Steps to Optimize
- Use human_feedback tool to ask which step(s) to optimize
- Present ALL steps in a clear table format showing:
  - Step ID and Title
  - Current tool count (if configured)
  - Has config status (true/false)
- Wait for user response before proceeding
- **Exception**: If StepID is provided in template vars, skip this step and focus exclusively on that step

### Step 2: Check Execution Logs FIRST (HIGHEST PRIORITY)
**This is the MOST RELIABLE source - actual tool usage from successful runs.**

1. Use StepLogsFolderMappingJSON to locate log folders for each step
2. Use ToolUsageSummaryJSON as a quick reference (but verify with actual logs)
3. **Read conversation history files** to extract actual tool calls:
   - File pattern: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}-conversation.json
   - Use search_large_output (if enabled) or read_workspace_file to read the JSON
   - Extract tool names from conversation_history array (see JSON structure in prompt)
   - **Only count tools from successful executions** (check execution_result.json for success indicators)
4. **CRITICAL**: Only suggest tools that appear in successful execution logs

### Step 3: Extract Tools from Learnings (SECONDARY SOURCE)
**Use learnings ONLY if logs are unavailable or incomplete.**

1. Use StepLearningsFolderMappingJSON to find learnings locations
2. Check BOTH locations:
   - **Base folder**: learnings/ (shared learnings)
   - **Step-specific**: learnings/{step_id}/ (step's own learnings)
3. Extract ONLY from ✅ SUCCESS patterns - ignore ❌ failures
4. Filter OUT: read_large_output, search_large_output, query_large_output (these are managed via enable_large_output_virtual_tools flag)

### Step 4: Apply Tool Inclusion Rules
**Priority order**: Execution Logs > Learnings > Step Description Inference > Current Config

**IMPORTANT**: Determine if step needs MCP servers:
- If logs/learnings show NO MCP tools were used → set selected_servers to ['NO_SERVERS']
- If logs/learnings show MCP tools were used → include those servers
- If step only uses workspace/human tools → set selected_servers to ['NO_SERVERS']

For each tool category, apply the following rules:

#### Basic Workspace Tools (ALWAYS INCLUDE)
- **Tools**: list_workspace_files, read_workspace_file, update_workspace_file, delete_workspace_file
- **Rule**: ALWAYS include these 4 tools unless logs clearly show they weren't used
- **Reasoning**: Essential for most file operations

#### MCP Tools (EVIDENCE-BASED ONLY)
- **Format**: server:tool (e.g., aws-s3:list_buckets, google-sheets:read_sheet)
- **Rule**: Include ONLY if found in execution logs OR learnings
- **NO INFERENCE**: Never infer MCP tools from step description
- **NO_SERVERS**: If step requires NO MCP servers at all (only workspace/human tools), set selected_servers to ['NO_SERVERS']
  - Use when: Step only uses workspace tools (read/write files) or human feedback, no external MCP server tools needed
  - Check logs/learnings: If no MCP tools were used in successful executions, set ['NO_SERVERS']
  - This explicitly disables all MCP servers for the step

#### Advanced Workspace Tools (CONDITIONAL)
- **Tools**: move_workspace_file, diff_patch_workspace_file, regex_search_workspace_files, semantic_search_workspace_files
- **Rule**: Include if:
  - Found in execution logs OR learnings, OR
  - Step description mentions: moving/renaming files, patches/diffs, regex patterns, semantic search

#### GitHub Sync Tools (RARELY NEEDED)
- **Tools**: sync_workspace_to_github, get_workspace_github_status
- **Rule**: Include ONLY if:
  - Found in execution logs OR learnings, OR
  - Step description mentions: GitHub, sync, commit, push, repository

#### Execute Shell Command (CONDITIONAL)
- **Tool**: execute_shell_command
- **Rule**: Include if:
  - Found in execution logs OR learnings, OR
  - Step description mentions: executing scripts, running commands, shell, bash, terminal, command line

#### Read Image (CONDITIONAL)
- **Tool**: read_image
- **Rule**: Include if:
  - Found in execution logs OR learnings, OR
  - Step description mentions: images, pictures, photos, visual content, image processing

#### Human Feedback (CONDITIONAL)
- **Tool**: human_feedback
- **Rule**: Include if:
  - Found in execution logs OR learnings, OR
  - Step description mentions: approval, confirmation, decision-making, asking user, human input, requires judgment

### Step 5: Prepare Proposal with Clear Reasoning
For each tool in your proposal, provide explicit reasoning:

- **"Basic workspace tool (always included)"** - for list/read/update/delete
- **"Found in execution logs: [file path]"** - highest priority, cite specific log file
- **"Found in learnings: [learning file path]"** - secondary source
- **"Inferred from step description: [specific phrase]"** - for conditional tools only
- **"Currently configured (preserving)"** - keep existing if no evidence to remove

**Be CONSERVATIVE**: Prefer keeping existing tools unless logs clearly show they weren't used.

### Step 6: Request Approval and Update
1. Present your proposal with clear reasoning for each tool
2. Use human_feedback to request approval
3. After approval, use update_step_config_tools to apply changes
4. Only update fields that changed (selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools)

## TOOL REFERENCE QUICK GUIDE

| Tool Category | Format | Inclusion Rule | Can Infer from Description? |
|--------------|--------|----------------|------------------------------|
| **Basic Workspace** | workspace_tools:list/read/update/delete | Always include | No (always included) |
| **Advanced Workspace** | workspace_tools:move/diff/regex/semantic | Logs/learnings OR description | Yes |
| **GitHub Tools** | workspace_tools:sync_workspace_to_github, get_workspace_github_status | Logs/learnings OR mentions GitHub | Yes (if mentions GitHub) |
| **Execute Shell** | workspace_tools:execute_shell_command | Logs/learnings OR mentions scripts/commands | Yes |
| **Read Image** | workspace_tools:read_image | Logs/learnings OR mentions images | Yes |
| **MCP Tools** | server:tool | Logs/learnings ONLY | **NO** |
| **NO_SERVERS** | ['NO_SERVERS'] | If no MCP tools used | Set when step only needs workspace/human tools |
| **Human Feedback** | human_tools:human_feedback | Logs/learnings OR needs approval | Yes |

**Key Principles**:
- **Execution logs are the gold standard** - actual usage is most reliable
- **Learnings are secondary** - use only if logs unavailable
- **Step description inference** - only for workspace/human tools, never for MCP tools
- **Be conservative** - prefer keeping existing tools unless evidence shows removal is needed

## CRITICAL RULES

### Access and Permissions
- **Access**: Only ` + templateVars["AllowedPaths"] + `
- **Preset Context**: Servers ` + templateVars["PresetServers"] + `, Tools ` + templateVars["PresetTools"] + `
- **Request approval** before updating step_config.json (use human_feedback tool)

### Update Constraints
- **Update only these fields**: selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools
- **Do NOT modify**: Other step configuration fields (agent configs, LLM settings, etc.)
- **NO_SERVERS handling**: If step requires no MCP servers, explicitly set selected_servers to ['NO_SERVERS'] to disable all MCP servers

### Edge Case Handling

| Scenario | Action |
|---------|-------|
| **No logs AND no learnings** | Preserve existing config, but ALWAYS include basic workspace tools (list/read/update/delete) |
| **Only failures in logs** | Preserve existing config (don't use failed execution data) |
| **Logs show tool used successfully** | Include it (highest priority) |
| **Logs show tool never used** | Consider removing, but be VERY conservative (especially for basic tools) |
| **Tool in learnings but not in logs** | Include only if learnings show clear success pattern |
| **Tool in current config but not in logs/learnings** | Preserve if no evidence to remove |

### Conservative Removal Policy
- **Only remove tools** if logs clearly show they were never used in any successful execution
- **Be extra cautious** with basic workspace tools - they're essential for most operations
- **When in doubt, keep the tool** - it's better to have an unused tool than to remove a needed one

### Data Source Priority
1. **Execution Logs** (highest) - Actual tool usage from successful runs
2. **Learnings** (secondary) - Success patterns from previous iterations
3. **Step Description** (inference) - Only for workspace/human tools, never for MCP
4. **Current Config** (preserve) - Keep existing unless evidence shows removal needed

### File Locations
- **Learnings**: Check both learnings/ (base) and learnings/{step_id}/ (step-specific) at workspace root
- **Logs**: Check runs/{iteration}/logs/step-{X}/execution/ for conversation history files
`
}

// toolOptimizationUserMessageProcessor creates the user message for plan tool optimization
func (agent *HumanControlledTodoPlannerPlanToolOptimizationAgent) toolOptimizationUserMessageProcessor(templateVars map[string]string) string {
	// Build step-specific section if step ID is provided
	stepSpecificSection := ""
	if stepID := templateVars["StepID"]; stepID != "" {
		stepSpecificSection = `
**⚠️ STEP-SPECIFIC MODE**: Optimize ONLY step ID: ` + stepID + ` (skip asking user, focus exclusively on this step)

`
	}

	return `# Tool Optimization Task

` + stepSpecificSection + `## DATA

**Workspace**: ` + templateVars["WorkspacePath"] + `

**Plan** (step IDs, titles, descriptions, success criteria):
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan provided."
	}() + `

**Current Tool Configurations** (all steps, has_config=false means no config yet):
` + func() string {
		if templateVars["CurrentToolConfigsJSON"] != "" {
			return templateVars["CurrentToolConfigsJSON"]
		}
		return "No configurations found."
	}() + `

**Learnings Folder Mapping** (is_code_exec determines folder):
` + func() string {
		if templateVars["StepLearningsFolderMappingJSON"] != "" {
			return templateVars["StepLearningsFolderMappingJSON"]
		}
		return "No mapping provided."
	}() + `

**Logs Folder Mapping** (where to find execution logs):
` + func() string {
		if templateVars["StepLogsFolderMappingJSON"] != "" {
			return templateVars["StepLogsFolderMappingJSON"]
		}
		return "No logs mapping provided."
	}() + `

**Tool Usage Summary** (tools actually used in successful executions - check logs to verify):
` + func() string {
		if templateVars["ToolUsageSummaryJSON"] != "" {
			return templateVars["ToolUsageSummaryJSON"]
		}
		return "No tool usage data available."
	}() + `

Follow the workflow in the system prompt. **ALWAYS check execution logs FIRST** before making tool suggestions. Present ALL steps when asking user which to optimize.
`
}
