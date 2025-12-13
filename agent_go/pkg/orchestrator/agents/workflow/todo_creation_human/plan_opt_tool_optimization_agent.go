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
							"description": "OPTIONAL: Updated MCP server selection. Use ['NO_SERVERS'] to disable MCP servers, or provide specific server names. If omitted, existing value is preserved."
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

	// Step-specific learnings are always enabled - folders are at workspace root
	ptom.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings enabled - agent can access step-specific folders in learnings/step-*/ and learnings/step-*/"))
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

	// Create mapping of step IDs to their current tool configurations
	currentToolConfigsMap := createCurrentToolConfigsMapping(stepConfigs, existingPlan)
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
	minimalPlan := createMinimalPlan(existingPlan, toolConfigsLookup)
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
		"Update tool selections for specific steps in step_config.json. Provide step_id (required) to identify which step to update, and only include the tool fields you want to change (selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools). The step_config.json file is updated immediately when this tool is called. NOTE: Do NOT include read_large_output, search_large_output, or query_large_output in enabled_custom_tools - these are large output virtual tools managed separately via enable_large_output_virtual_tools boolean flag. If you see these tools used in learnings, set enable_large_output_virtual_tools to true.",
		updateParams,
		createUpdateStepConfigToolsExecutor(ptom.GetWorkspacePath(), logger, ptom.ReadWorkspaceFile, ptom.WriteWorkspaceFile),
		"workflow",
	); err != nil {
		return "", fmt.Errorf("failed to register update_step_config_tools tool: %w", err)
	}

	// Create mapping of step IDs to their learnings folder paths based on code execution mode
	presetCodeExecMode := ptom.GetUseCodeExecutionMode()
	stepLearningsFolderMapping := createStepLearningsFolderMapping(stepConfigs, existingPlan, presetCodeExecMode, ptom.GetWorkspacePath())
	stepLearningsFolderMappingJSONBytes, err := json.MarshalIndent(stepLearningsFolderMapping, "", "  ")
	if err != nil {
		return "", fmt.Errorf(fmt.Sprintf("failed to marshal step learnings folder mapping to JSON: %w", err), nil)
	}
	ptom.GetLogger().Info(fmt.Sprintf("✅ Created learnings folder mapping for %d steps (based on code execution mode)", len(stepLearningsFolderMapping)))

	// Create mapping of step IDs to their logs folder paths
	stepLogsFolderMapping := createStepLogsFolderMapping(existingPlan, ptom.GetWorkspacePath())
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

// MinimalPlan represents a plan with essential step information for tool optimization
type MinimalPlan struct {
	Steps []MinimalPlanStep `json:"steps"`
}

// createMinimalPlan creates a minimal plan with essential step information from the full plan
// Recursively handles branch steps (if_true_steps, if_false_steps)
// currentToolConfigsMap: pre-computed mapping of step IDs to their current tool configurations
func createMinimalPlan(fullPlan *PlanningResponse, currentToolConfigsMap map[string]*StepCurrentToolConfig) *MinimalPlan {
	var minimalSteps []MinimalPlanStep

	var extractSteps func(steps []PlanStep)
	extractSteps = func(steps []PlanStep) {
		for _, step := range steps {
			// Get current tool configuration for this step
			currentConfig := currentToolConfigsMap[step.ID]
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

			minimalSteps = append(minimalSteps, MinimalPlanStep{
				ID:               step.ID,
				Title:            step.Title,
				Description:      step.Description,
				SuccessCriteria:  step.SuccessCriteria,
				HasLoop:          step.HasLoop,
				LoopDescription:  step.LoopDescription,
				CurrentToolCount: toolCount,
				HasConfig:        hasConfig,
			})
			// Recursively extract branch steps
			if len(step.IfTrueSteps) > 0 {
				extractSteps(step.IfTrueSteps)
			}
			if len(step.IfFalseSteps) > 0 {
				extractSteps(step.IfFalseSteps)
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
	IsCodeExec    bool   `json:"is_code_exec"`   // true if step uses code execution mode
}

// createStepLearningsFolderMapping creates a mapping of step IDs to their learnings folder paths
// based on UseCodeExecutionMode setting in step_config.json
// Returns step-specific paths:
//   - Regular steps: learnings/step-{X}/ format (at workspace root, not inside runs/)
//   - Branch steps: learnings/step-{parentStep}-{true/false}-{branchIdx}/ format (at workspace root, not inside runs/)
//
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createStepLearningsFolderMapping(stepConfigs []StepConfig, plan *PlanningResponse, presetCodeExecMode bool, workspacePath string) []StepLearningsFolderMapping {
	// Create lookup map: step ID -> AgentConfigs
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigs {
		if stepConfigs[i].ID != "" {
			idConfigMap[stepConfigs[i].ID] = stepConfigs[i].AgentConfigs
		}
	}

	var mappings []StepLearningsFolderMapping
	stepNumber := 0

	// Helper function to extract mappings with branch context
	var extractMappings func(steps []PlanStep, parentStepNumber int, branchType string, branchIndex int)
	extractMappings = func(steps []PlanStep, parentStepNumber int, branchType string, branchIndex int) {
		for branchIdx, step := range steps {
			agentConfigs := idConfigMap[step.ID]

			// Determine code execution mode: step config > preset default
			isCodeExec := presetCodeExecMode
			if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
				isCodeExec = *agentConfigs.UseCodeExecutionMode
			}

			// Determine learnings folder (always use step-specific paths)
			var learningsPath string
			if branchType != "" {
				// Branch step: learnings/step-{parentStep}-{true/false}-{branchIdx}/
				learningsPath = fmt.Sprintf("learnings/step-%d-%s-%d/", parentStepNumber, branchType, branchIdx)
			} else {
				// Regular step: learnings/step-{X}/ (at workspace root, not inside runs/)
				stepNumber++
				learningsPath = fmt.Sprintf("learnings/step-%d/", stepNumber)
			}

			mappings = append(mappings, StepLearningsFolderMapping{
				StepID:        step.ID,
				LearningsPath: learningsPath,
				IsCodeExec:    isCodeExec,
			})

			// Determine current step number for branch steps (use parent step number for branch steps)
			currentStepNumber := stepNumber
			if branchType != "" {
				currentStepNumber = parentStepNumber
			}

			// Recursively extract branch steps (nested conditionals)
			if len(step.IfTrueSteps) > 0 {
				extractMappings(step.IfTrueSteps, currentStepNumber, "true", branchIdx)
			}
			if len(step.IfFalseSteps) > 0 {
				extractMappings(step.IfFalseSteps, currentStepNumber, "false", branchIdx)
			}
		}
	}

	// Start extraction with no parent (regular steps)
	extractMappings(plan.Steps, 0, "", -1)

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
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createStepLogsFolderMapping(plan *PlanningResponse, workspacePath string) []StepLogsFolderMapping {
	var mappings []StepLogsFolderMapping
	stepNumber := 0

	// Helper function to extract mappings with branch context
	var extractMappings func(steps []PlanStep, parentStepNumber int, branchType string, branchIndex int)
	extractMappings = func(steps []PlanStep, parentStepNumber int, branchType string, branchIndex int) {
		for branchIdx, step := range steps {
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
				StepID:    step.ID,
				LogsPaths: logsPaths,
			})

			// Determine current step number for branch steps (use parent step number for branch steps)
			currentStepNumber := stepNumber
			if branchType != "" {
				currentStepNumber = parentStepNumber
			}

			// Recursively extract branch steps (nested conditionals)
			if len(step.IfTrueSteps) > 0 {
				extractMappings(step.IfTrueSteps, currentStepNumber, "true", branchIdx)
			}
			if len(step.IfFalseSteps) > 0 {
				extractMappings(step.IfFalseSteps, currentStepNumber, "false", branchIdx)
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
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createCurrentToolConfigsMapping(stepConfigs []StepConfig, plan *PlanningResponse) []StepCurrentToolConfig {
	// Create lookup map: step ID -> AgentConfigs
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigs {
		if stepConfigs[i].ID != "" {
			idConfigMap[stepConfigs[i].ID] = stepConfigs[i].AgentConfigs
		}
	}

	var configs []StepCurrentToolConfig

	var extractConfigs func(steps []PlanStep)
	extractConfigs = func(steps []PlanStep) {
		for _, step := range steps {
			agentConfigs := idConfigMap[step.ID]
			hasConfig := agentConfigs != nil && (len(agentConfigs.SelectedServers) > 0 ||
				len(agentConfigs.SelectedTools) > 0 ||
				len(agentConfigs.EnabledCustomTools) > 0)

			config := StepCurrentToolConfig{
				StepID:    step.ID,
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

			// Recursively extract branch steps
			if len(step.IfTrueSteps) > 0 {
				extractConfigs(step.IfTrueSteps)
			}
			if len(step.IfFalseSteps) > 0 {
				extractConfigs(step.IfFalseSteps)
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

Learning files are stored in step-specific folders:
- Shared learnings: {WorkspacePath}/learnings/ and {WorkspacePath}/learnings/
- Step-specific learnings: {WorkspacePath}/learnings/step-{X}/ and {WorkspacePath}/learnings/step-{X}/ (at workspace root, not inside runs/)

The StepLearningsFolderMappingJSON provides step-specific paths. Check BOTH shared and step-specific locations when extracting tools.

## EXECUTION LOGS LOCATION

Execution logs contain ACTUAL tool usage from previous runs:
- Logs location: {WorkspacePath}/runs/{iteration}/logs/step-{X}/ or {WorkspacePath}/logs/step-{X}/
- Conversation history: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}-conversation.json
- Execution results: logs/step-{X}/execution/execution-attempt-{N}-iteration-{M}.json

The StepLogsFolderMappingJSON provides step-specific log paths. The ToolUsageSummaryJSON shows tools that were ACTUALLY USED in successful executions.

**CRITICAL**: Always check logs/ for ACTUAL tool usage before making suggestions. Only suggest tools that appear in successful execution logs.
`

	return `# Plan Tool Optimization Agent

## PURPOSE
Analyze ACTUAL tool usage from execution logs and learnings to optimize step_config.json. Be CONSERVATIVE - only suggest tools that were actually used successfully.

` + variablesSection + learningsLocationNote + `## WORKFLOW

1. **Ask User** - Use human_feedback to ask which step(s) to optimize
   - Present ALL steps (Title, Tool count, Has config)
   - Wait for response before proceeding

2. **Check Execution Logs FIRST** (MOST IMPORTANT)
   - Use StepLogsFolderMappingJSON to find logs location for each step
   - Read conversation history files: logs/step-{X}/execution/execution-attempt-*-conversation.json
   - Extract ACTUAL tools that were called in successful executions
   - Use ToolUsageSummaryJSON as a quick reference (but verify by reading logs if needed)
   - **ONLY suggest tools that appear in successful execution logs**

3. **Extract Tools from Learnings** (SECONDARY SOURCE)
   - Use StepLearningsFolderMappingJSON to find learnings location for each step
   - When step-specific learnings enabled: paths are in learnings/step-{X}/ or learnings/step-{X}/ (at workspace root, not inside runs/)
   - When step-specific learnings disabled: paths are in learnings/ or learnings/
   - Extract ONLY from ✅ SUCCESS patterns, ignore ❌ failures
   - Filter OUT: read_large_output, search_large_output, query_large_output (use enable_large_output_virtual_tools flag instead)
   - **Only use learnings if logs are not available or incomplete**

4. **Compare Sources** - Prioritize: Logs (actual usage) > Learnings (patterns) > Current Config (existing)

5. **Prepare Proposal** - Be CONSERVATIVE:
   - Only suggest tools found in successful execution logs
   - Prefer keeping existing tools unless logs show they weren't used
   - Don't add tools that weren't actually used (even if mentioned in learnings)

6. **Present with Reasoning** - For each tool, explain source:
   - "Found in execution logs" (highest priority)
   - "Found in learnings" (secondary)
   - "Currently configured" (preserve if no evidence to remove)
   - "Inferred from step description" (only for workspace tools, be cautious)

7. **Update** - After approval, use update_step_config_tools

## TOOL CATEGORIES

| Category | Source | Format |
|----------|--------|--------|
| **MCP Tools** | Learnings or current config only (don't infer) | server:tool |
| **Workspace Tools** | Learnings, config, OR infer from step description | workspace_tools:tool |
| **Human Tools** | Learnings, config, OR step requires approval/decision | human_tools:human_feedback |

### Workspace Tools Inference
- Reading files → workspace_tools:read_workspace_file
- Listing/searching → workspace_tools:list_workspace_files
- Executing commands → workspace_tools:execute_shell_command
- Updating/writing → workspace_tools:update_workspace_file
- Deleting → workspace_tools:delete_workspace_file

### Essential Workspace Tools (ALWAYS include)
- workspace_tools:list_workspace_files
- workspace_tools:read_workspace_file
- workspace_tools:update_workspace_file

### Human Tools Criteria
Recommend human_tools:human_feedback when step:
- Found in ✅ success patterns OR already configured
- Requires approval/confirmation
- Involves decision-making or judgment
- Has sensitive operations (deletions, deployments)

## RULES

- **Request approval** before updating step_config.json
- **Access**: Only ` + templateVars["AllowedPaths"] + `
- **Preset**: Servers ` + templateVars["PresetServers"] + `, Tools ` + templateVars["PresetTools"] + `
- **Edge Cases**: 
  - No logs AND no learnings → preserve config
  - Only failures in logs → preserve config
  - Logs show tool was used successfully → include it
  - Logs show tool was never used → consider removing (but be conservative)
- **Update only**: selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools
- **Be VERY conservative** with removals - only remove if logs clearly show tool was never used
- **Prioritize logs over learnings** - actual usage is more reliable than patterns
- **Step-Specific Learnings**: Check both shared folders (learnings/, learnings/) and step-specific folders in learnings/step-{X}/ and learnings/step-{X}/ (at workspace root, not inside runs/)
- **Logs Structure**: Check runs/{iteration}/logs/step-{X}/execution/ for conversation history files
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
