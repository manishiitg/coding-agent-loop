package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/mcpclient"
	"mcpagent/observability"
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
func NewHumanControlledTodoPlannerPlanToolOptimizationAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener, baseOrchestrator *orchestrator.BaseOrchestrator) *HumanControlledTodoPlannerPlanToolOptimizationAgent {
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

	// Preset LLM config for plan tool optimization agent
	presetPlanToolOptimizationLLM *AgentLLMConfig
}

// NewPlanToolOptimizationManager creates a new PlanToolOptimizationManager
func NewPlanToolOptimizationManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
	sessionID string,
	workflowID string,
	presetPlanToolOptimizationLLM *AgentLLMConfig,
) *PlanToolOptimizationManager {
	return &PlanToolOptimizationManager{
		BaseOrchestrator:              baseOrchestrator,
		sessionID:                     sessionID,
		workflowID:                    workflowID,
		presetPlanToolOptimizationLLM: presetPlanToolOptimizationLLM,
	}
}

// stepConfigFileMutex ensures thread-safe access to step_config.json
var stepConfigFileMutex sync.Mutex

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
func readStepConfigFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*StepConfigFile, error) {
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	stepConfigFileMutex.Lock()
	defer stepConfigFileMutex.Unlock()

	content, err := readFile(ctx, configPath)
	if err != nil {
		// File doesn't exist yet - return empty structure
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return &StepConfigFile{Steps: []StepConfig{}}, nil
		}
		return nil, fmt.Errorf("failed to read step_config.json: %w", err)
	}

	var configFile StepConfigFile
	if err := json.Unmarshal([]byte(content), &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse step_config.json: %w", err)
	}

	return &configFile, nil
}

// writeStepConfigToFile writes StepConfigFile to step_config.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
func writeStepConfigToFile(ctx context.Context, workspacePath string, config *StepConfigFile, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger utils.ExtendedLogger) error {
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	stepConfigFileMutex.Lock()
	defer stepConfigFileMutex.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}

	if err := writeFile(ctx, configPath, string(data)); err != nil {
		return fmt.Errorf("failed to write step_config.json: %w", err)
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
func createUpdateStepConfigToolsExecutor(workspacePath string, logger utils.ExtendedLogger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract updated_steps from args
		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf("invalid updated_steps argument")
		}

		// Convert to JSON and unmarshal to PartialStepConfigUpdate array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf("failed to marshal updated_steps: %w", err)
		}

		var partialUpdates []PartialStepConfigUpdate
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf("failed to parse updated_steps: %w", err)
		}

		// Read current step_config.json
		configFile, err := readStepConfigFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read step_config.json: %w", err)
		}

		// Create map of existing step configs by ID
		existingConfigsMap := make(map[string]*StepConfig)
		for i := range configFile.Steps {
			existingConfigsMap[configFile.Steps[i].ID] = &configFile.Steps[i]
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
				configFile.Steps = append(configFile.Steps, newConfig)
			} else {
				// Step config exists - merge update
				mergePartialStepConfigUpdate(existingConfig, partialUpdate)
			}
		}

		// Write updated step_config.json
		if err := writeStepConfigToFile(ctx, workspacePath, configFile, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write step_config.json: %w", err)
		}

		logger.Infof("✅ Updated tool configurations for %d step(s) in step_config.json", len(partialUpdates))
		return fmt.Sprintf("Successfully updated tool configurations for %d step(s) in step_config.json", len(partialUpdates)), nil
	}
}

// createPlanToolOptimizationAgent creates and sets up a plan tool optimization agent with all necessary configuration
// This method handles folder guard setup, LLM config selection, tool combination, and agent initialization
func (ptom *PlanToolOptimizationManager) createPlanToolOptimizationAgent(ctx context.Context, workspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: read-only access to planning/ and both learnings folders, write access to planning/step_config.json only
	planningPath := fmt.Sprintf("%s/planning", workspacePath)
	learningsPath := fmt.Sprintf("%s/learnings", workspacePath)
	learningCodeExecPath := fmt.Sprintf("%s/learning_code_exec", workspacePath)

	// Agent has read-only access to planning/ folder (for plan.json) and both learnings folders (for learning files)
	// Write access to planning/step_config.json only
	// Include both learnings folders since different steps may use different folders based on code execution mode
	readPaths := []string{planningPath, learningsPath, learningCodeExecPath}
	writePaths := []string{planningPath} // Write access to planning/ folder (for step_config.json)
	ptom.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	ptom.GetLogger().Infof("🔧 Setting folder guard for plan tool optimization agent - Read paths: %v, Write paths: %v (read-only access to planning/ and both learnings folders, write access to planning/step_config.json)", readPaths, writePaths)

	// Use preset LLM config if available, otherwise fall back to orchestrator default
	orchestratorLLMConfig := ptom.GetLLMConfig()
	var llmConfigToUse *orchestrator.LLMConfig
	if ptom.presetPlanToolOptimizationLLM != nil && ptom.presetPlanToolOptimizationLLM.Provider != "" && ptom.presetPlanToolOptimizationLLM.ModelID != "" {
		// Use preset LLM config
		llmConfigToUse = &orchestrator.LLMConfig{
			Provider:              ptom.presetPlanToolOptimizationLLM.Provider,
			ModelID:               ptom.presetPlanToolOptimizationLLM.ModelID,
			FallbackModels:        orchestratorLLMConfig.FallbackModels,
			CrossProviderFallback: orchestratorLLMConfig.CrossProviderFallback,
			APIKeys:               orchestratorLLMConfig.APIKeys,
		}
		ptom.GetLogger().Infof("🔧 Using preset plan tool optimization LLM: %s/%s", ptom.presetPlanToolOptimizationLLM.Provider, ptom.presetPlanToolOptimizationLLM.ModelID)
	} else {
		// Fall back to orchestrator default
		llmConfigToUse = orchestratorLLMConfig
		ptom.GetLogger().Infof("🔧 Using orchestrator default tool optimization LLM: %s/%s", ptom.GetProvider(), ptom.GetModel())
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
	ptom.GetLogger().Infof("🔧 Code execution mode disabled for plan tool optimization agent - using direct tool access")

	// Tool optimization agent doesn't need MCP servers - uses workspace tools only
	config.ServerNames = []string{mcpclient.NoServers}

	// Large output virtual tools are enabled for tool optimization (agent may generate large analysis reports)

	// Create wrapper function that returns OrchestratorAgent interface
	createAgentFunc := func(cfg *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
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
		return nil, fmt.Errorf("failed to create and setup plan tool optimization agent: %w", err)
	}

	return agent, nil
}

// PlanToolOptimizationOnly runs only the plan tool optimization phase (standalone, independent from other phases)
// This is a separate workflow phase that can be run independently
func (ptom *PlanToolOptimizationManager) PlanToolOptimizationOnly(ctx context.Context, workspacePath string) (string, error) {
	ptom.GetLogger().Infof("🔧 Starting standalone plan tool optimization for workspace: %s", workspacePath)

	// Set workspace path
	ptom.SetWorkspacePath(workspacePath)

	// Check if plan.json exists - REQUIRED for tool optimization
	planPath := fmt.Sprintf("%s/planning/plan.json", ptom.GetWorkspacePath())
	planExist, existingPlan, err := ptom.checkExistingPlan(ctx, planPath)
	if err != nil {
		return "", fmt.Errorf("failed to check for existing plan: %w", err)
	}
	if !planExist {
		return "", fmt.Errorf("plan.json not found at %s - planning must be run first as a separate phase", planPath)
	}

	// Plan exists - use it for tool optimization
	ptom.GetLogger().Infof("✅ Found plan.json with %d steps for tool optimization", len(existingPlan.Steps))

	// Read current step_config.json
	stepConfigFile, err := readStepConfigFromFile(ctx, ptom.GetWorkspacePath(), ptom.ReadWorkspaceFile)
	if err != nil {
		return "", fmt.Errorf("failed to read step_config.json: %w", err)
	}

	// Create mapping of step IDs to their current tool configurations
	currentToolConfigsMap := createCurrentToolConfigsMapping(stepConfigFile, existingPlan)
	currentToolConfigsJSONBytes, err := json.MarshalIndent(currentToolConfigsMap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal current tool configs mapping to JSON: %w", err)
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
		return "", fmt.Errorf("failed to marshal minimal plan to JSON: %w", err)
	}

	// Prepare step_config.json for template
	stepConfigJSONBytes, err := json.MarshalIndent(stepConfigFile, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal step_config.json to JSON: %w", err)
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
		ptom.GetLogger().Infof("ℹ️ No variables.json found at %s - proceeding without variables", variablesPath)
	} else {
		// Parse variables.json
		var manifest VariablesManifest
		if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
			ptom.GetLogger().Warnf("⚠️ Failed to parse variables.json: %v - proceeding without variables", err)
		} else {
			variablesManifest = &manifest
			ptom.GetLogger().Infof("✅ Loaded %d variables for tool optimization context", len(manifest.Variables))
		}
	}

	// Create tool optimization agent
	toolOptimizationAgent, err := ptom.createPlanToolOptimizationAgent(ctx, ptom.GetWorkspacePath())
	if err != nil {
		return "", fmt.Errorf("failed to create plan tool optimization agent: %w", err)
	}

	// Register custom tool for updating step_config.json
	// Get the underlying MCP agent
	baseAgent := toolOptimizationAgent.(*HumanControlledTodoPlannerPlanToolOptimizationAgent).GetBaseAgent()
	if baseAgent == nil {
		return "", fmt.Errorf("base agent is not initialized")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return "", fmt.Errorf("MCP agent is not initialized")
	}

	// Parse schema and register the custom tool
	updateSchema := getUpdateStepConfigToolsSchema()
	updateParams, err := parseSchemaForToolParameters(updateSchema)
	if err != nil {
		return "", fmt.Errorf("failed to parse update schema: %w", err)
	}

	// Get logger from MCP agent
	logger := mcpAgent.Logger

	// Register custom tool for updating step_config.json
	// Note: human_feedback tool is already available via workspace tools (no need to register separately)
	mcpAgent.RegisterCustomTool(
		"update_step_config_tools",
		"Update tool selections for specific steps in step_config.json. Provide step_id (required) to identify which step to update, and only include the tool fields you want to change (selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools). The step_config.json file is updated immediately when this tool is called. NOTE: Do NOT include read_large_output, search_large_output, or query_large_output in enabled_custom_tools - these are large output virtual tools managed separately via enable_large_output_virtual_tools boolean flag. If you see these tools used in learnings, set enable_large_output_virtual_tools to true.",
		updateParams,
		createUpdateStepConfigToolsExecutor(ptom.GetWorkspacePath(), logger, ptom.ReadWorkspaceFile, ptom.WriteWorkspaceFile),
	)

	// Create mapping of step IDs to their learnings folder paths based on code execution mode
	presetCodeExecMode := ptom.GetUseCodeExecutionMode()
	stepLearningsFolderMapping := createStepLearningsFolderMapping(stepConfigFile, existingPlan, presetCodeExecMode)
	stepLearningsFolderMappingJSONBytes, err := json.MarshalIndent(stepLearningsFolderMapping, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal step learnings folder mapping to JSON: %w", err)
	}
	ptom.GetLogger().Infof("✅ Created learnings folder mapping for %d steps (based on code execution mode)", len(stepLearningsFolderMapping))

	// Prepare template variables
	// Use actual workspace path so agent can navigate correctly
	// Explicitly list allowed paths for the agent (both learnings folders)
	allowedPaths := "['planning/', 'learnings/', 'learning_code_exec/']"
	toolOptimizationTemplateVars := map[string]string{
		"WorkspacePath":                  ptom.GetWorkspacePath(),
		"PlanJSON":                       string(planJSONBytes),
		"StepConfigJSON":                 string(stepConfigJSONBytes),
		"CurrentToolConfigsJSON":         string(currentToolConfigsJSONBytes),
		"StepLearningsFolderMappingJSON": string(stepLearningsFolderMappingJSONBytes),
		"PresetServers":                  string(presetServersJSON),
		"PresetTools":                    string(presetToolsJSON),
		"AllowedPaths":                   allowedPaths,
		"SessionID":                      ptom.sessionID,
		"WorkflowID":                     ptom.workflowID,
	}

	// Add variable names if available (for context about variables in plan)
	if variableNames := FormatVariableNames(variablesManifest); variableNames != "" {
		toolOptimizationTemplateVars["VariableNames"] = variableNames
		ptom.GetLogger().Infof("✅ Added variable names to tool optimization template vars")
	}

	// Execute tool optimization agent
	ptom.GetLogger().Infof("🔧 Executing plan tool optimization agent...")
	result, conversationHistory, err := toolOptimizationAgent.Execute(ctx, toolOptimizationTemplateVars, nil)
	if err != nil {
		return "", fmt.Errorf("plan tool optimization agent execution failed: %w", err)
	}

	ptom.GetLogger().Infof("✅ Plan tool optimization completed successfully")
	ptom.GetLogger().Infof("🔧 Tool optimization result: %s", result)

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
	LearningsPath string `json:"learnings_path"` // "learnings/" or "learning_code_exec/"
	IsCodeExec    bool   `json:"is_code_exec"`   // true if step uses code execution mode
}

// createStepLearningsFolderMapping creates a mapping of step IDs to their learnings folder paths
// based on UseCodeExecutionMode setting in step_config.json
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createStepLearningsFolderMapping(stepConfigFile *StepConfigFile, plan *PlanningResponse, presetCodeExecMode bool) []StepLearningsFolderMapping {
	// Create lookup map: step ID -> AgentConfigs
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigFile.Steps {
		if stepConfigFile.Steps[i].ID != "" {
			idConfigMap[stepConfigFile.Steps[i].ID] = stepConfigFile.Steps[i].AgentConfigs
		}
	}

	var mappings []StepLearningsFolderMapping

	var extractMappings func(steps []PlanStep)
	extractMappings = func(steps []PlanStep) {
		for _, step := range steps {
			agentConfigs := idConfigMap[step.ID]

			// Determine code execution mode: step config > preset default
			isCodeExec := presetCodeExecMode
			if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
				isCodeExec = *agentConfigs.UseCodeExecutionMode
			}

			// Determine learnings folder based on code execution mode
			learningsPath := "learnings/"
			if isCodeExec {
				learningsPath = "learning_code_exec/"
			}

			mappings = append(mappings, StepLearningsFolderMapping{
				StepID:        step.ID,
				LearningsPath: learningsPath,
				IsCodeExec:    isCodeExec,
			})

			// Recursively extract branch steps
			if len(step.IfTrueSteps) > 0 {
				extractMappings(step.IfTrueSteps)
			}
			if len(step.IfFalseSteps) > 0 {
				extractMappings(step.IfFalseSteps)
			}
		}
	}

	extractMappings(plan.Steps)

	return mappings
}

// createCurrentToolConfigsMapping creates a mapping of step IDs to their current tool configurations from step_config.json
// Recursively handles branch steps (if_true_steps, if_false_steps)
func createCurrentToolConfigsMapping(stepConfigFile *StepConfigFile, plan *PlanningResponse) []StepCurrentToolConfig {
	// Create lookup map: step ID -> AgentConfigs
	idConfigMap := make(map[string]*AgentConfigs)
	for i := range stepConfigFile.Steps {
		if stepConfigFile.Steps[i].ID != "" {
			idConfigMap[stepConfigFile.Steps[i].ID] = stepConfigFile.Steps[i].AgentConfigs
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
	ptom.GetLogger().Infof("🔍 Checking for existing plan at %s", planPath)

	// Extract workspace path from planPath (planPath is workspacePath/planning/plan.json)
	// readPlanFromFile expects workspacePath and constructs the path internally
	workspacePath := filepath.Dir(filepath.Dir(planPath))

	// Use the shared readPlanFromFile helper which acquires planFileMutex for thread-safe access
	plan, err := readPlanFromFile(ctx, workspacePath, ptom.ReadWorkspaceFile)
	if err != nil {
		// Check if it's a "file not found" error vs other errors
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "no such file") {
			ptom.GetLogger().Infof("📋 No existing plan found: %v", err)
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf("failed to check existing plan: %w", err)
	}

	ptom.GetLogger().Infof("✅ Found existing plan at %s with %d steps", planPath, len(plan.Steps))
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
		allowedPaths = "['planning/', 'learnings/', 'learning_code_exec/']"
	}

	// Prepare template variables
	toolOptimizationTemplateVars := map[string]string{
		"WorkspacePath":                  workspacePath,
		"PlanJSON":                       planJSON,
		"StepConfigJSON":                 stepConfigJSON,
		"CurrentToolConfigsJSON":         currentToolConfigsJSON,
		"StepLearningsFolderMappingJSON": stepLearningsFolderMappingJSON,
		"PresetServers":                  presetServers,
		"PresetTools":                    presetTools,
		"AllowedPaths":                   allowedPaths,
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
	var logger utils.ExtendedLogger
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
			logger.Infof("🔧 Plan tool optimization agent iteration %d/%d", iteration, maxIterations)
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

		// After execution, ask if user wants to continue (blocking feedback)
		if iteration < maxIterations && agent.baseOrchestrator != nil {
			if logger != nil {
				logger.Infof("🔧 Plan tool optimization agent completed (iteration %d/%d). Asking user if they want to continue...", iteration, maxIterations)
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
					logger.Warnf("⚠️ Failed to get user feedback: %v", err)
				}
				// Continue without blocking if feedback fails
				break
			}

			// If user clicked Approve button, we're done
			if approved {
				if logger != nil {
					logger.Infof("✅ User approved - plan tool optimization complete")
				}
				break
			}

			// User provided feedback/question - always pass it to the agent and continue
			if feedback != "" && strings.TrimSpace(feedback) != "" {
				if logger != nil {
					logger.Infof("📝 User provided feedback: %s", feedback)
				}
				// Use feedback directly as user message for next iteration
				// Note: BaseAgent.Execute() will automatically add it to conversation history
				userMessage = feedback
			} else {
				// No feedback provided but not approved - continue with same message
				if logger != nil {
					logger.Infof("ℹ️ No feedback provided, continuing with same context")
				}
			}
		} else {
			// Reached max iterations or no base orchestrator
			if logger != nil {
				logger.Infof("🔧 Reached maximum iterations (%d) or no base orchestrator, ending conversation", maxIterations)
			}
			break
		}
	}

	if logger != nil {
		logger.Infof("🔧 Plan tool optimization completed after %d iterations", iteration)
	}

	return currentResult, currentConversationHistory, nil
}

// toolOptimizationSystemPromptProcessor creates the system prompt for plan tool optimization
func (agent *HumanControlledTodoPlannerPlanToolOptimizationAgent) toolOptimizationSystemPromptProcessor(templateVars map[string]string) string {
	// Build variables section if available
	variablesSection := ""
	if variableNames := templateVars["VariableNames"]; variableNames != "" {
		variablesSection = `
## 🔑 AVAILABLE VARIABLES

The plan may contain variable placeholders ({{VARIABLE_NAME}}). Available variables:
` + variableNames + `
**Note**: Variable placeholders in step descriptions help you understand what the step is about, but you don't need to resolve them for tool optimization.

`
	}

	return `# Plan Tool Optimization Agent

## 🤖 AGENT IDENTITY
**PRIMARY PURPOSE**: Analyze successful tool usage from learnings folder and optimize tool selections in step_config.json. Only use tools from successful learnings (✅ success patterns) to suggest tool configurations.

Your main goal is to:
1. Extract tools from SUCCESS patterns only (✅ marked sections in learning files)
2. Compare with currently configured tools in step_config.json
3. Update step_config.json to optimize tool selections (keep only tools from successful learnings, remove unused)

` + variablesSection + `## 🎯 TOOL OPTIMIZATION PROCESS

1. **Ask User Which Steps to Optimize**: **FIRST ACTION - MANDATORY** - Use human_feedback tool to ask which step(s) to optimize:
   - **CRITICAL**: Present ALL steps from the PlanJSON provided in the user message, regardless of whether they have existing configuration or not
   - Use the "Current Plan" section in the user message to get the complete list of ALL steps
   - For each step, show: Title, Current tool count, Has config (true/false)
   - Steps without config (Has config: false) are still valid for optimization - they may need initial tool configuration
   - Use step titles (not IDs) when presenting to user - IDs are for internal use only
   - Wait for user response before proceeding

2. **Understand the Plan**: Review plan.json for selected steps: IDs, titles, descriptions, success criteria, loop info

3. **Extract Tools from SUCCESS Learnings**: Find learning files in the correct learnings folder for each step:
   - **CRITICAL**: Use StepLearningsFolderMappingJSON to determine which folder to search for each step:
     - If step has is_code_exec: true → search in learning_code_exec/ folder
     - If step has is_code_exec: false → search in learnings/ folder
   - For each step being optimized, use the learnings path from the mapping (e.g., "learnings/" or "learning_code_exec/")
   - Search file content to match steps (filenames may not match exactly)
   - Extract tools ONLY from ✅ SUCCESS patterns, ignore ❌ failure patterns
   - Format: MCP tools as "server:tool", workspace tools as "workspace_tools:tool", human tools as "human_tools:human_feedback"
   - **CRITICAL**: Filter out large output virtual tools (read_large_output, search_large_output, query_large_output) when extracting for enabled_custom_tools - these are NOT configurable in enabled_custom_tools. However, if you see these tools used in learnings, suggest enabling enable_large_output_virtual_tools boolean flag (set to true) in the update
   - Note: Workspace tools may be missing from learnings - infer from step description in Step 5

4. **Map Current Configuration**: Use CurrentToolConfigsJSON to see what tools are already configured for each step in step_config.json

5. **Optimize Tool Selections**: Compare current config vs learnings, prepare proposal:
   - **Tool Sources**: Suggest tools from (1) successful learnings, (2) current config, or (3) workspace tools inferred from step description
   - **Workspace Tools**: Often missing from learnings. Infer from step description:
     - Reading files → workspace_tools:read_workspace_file
     - Listing/searching → workspace_tools:list_workspace_files
     - Executing commands → workspace_tools:execute_shell_command
     - Updating/writing → workspace_tools:update_workspace_file
     - Deleting → workspace_tools:delete_workspace_file
   - **CRITICAL - Essential Workspace Tools**: When optimizing workspace tools, ALWAYS keep these essential tools (even if not in learnings):
     - workspace_tools:list_workspace_files (essential for file discovery)
     - workspace_tools:read_workspace_file (essential for reading files)
     - workspace_tools:update_workspace_file (essential for writing/updating files)
   - **MCP/Human Tools**: Only from learnings or current config (don't infer)
   - **Strategy**: Remove only clearly unnecessary tools, keep useful tools from config, add tools from learnings, add inferred workspace tools, ALWAYS include essential workspace tools
   - **Format**: MCP tools as "server:tool", workspace/human tools as "category:tool"

6. **Present Proposal and Request Approval**: Show proposal with detailed reasoning:
   - **For each suggested tool**, provide reasoning:
     - **From Learnings**: List which learning file(s) contained this tool in ✅ success patterns, quote relevant excerpts
     - **From Step Description**: Explain how step description/success criteria indicate this tool is needed (e.g., "Step description mentions 'reading configuration files' → workspace_tools:read_workspace_file")
     - **From Current Config**: If keeping from current config, explain why it's still needed
     - **Inferred Workspace Tools**: Explicitly state the inference (e.g., "Step involves file operations → workspace_tools:read_workspace_file, workspace_tools:update_workspace_file")
   - Show current config, tools from learnings, and planned changes (add/remove/keep)
   - **Format**: For each tool, show: Tool name → Source (Learning file X / Step description / Current config) → Reasoning
   - Request approval before updating

7. **Update step_config.json**: After approval, use update_step_config_tools with step_id and tool fields to update.

## ⚠️ IMPORTANT RULES

- **Always request approval** before updating step_config.json
- **Access**: Only ` + templateVars["AllowedPaths"] + ` subdirectories
- **Preset**: Servers ` + templateVars["PresetServers"] + `, Tools ` + templateVars["PresetTools"] + `
- **Essential Workspace Tools**: When optimizing workspace tools, ALWAYS include these essential tools:
  - workspace_tools:list_workspace_files (essential for file discovery)
  - workspace_tools:read_workspace_file (essential for reading files)
  - workspace_tools:update_workspace_file (essential for writing/updating files)
- **Edge Cases**: No learnings → preserve config. Only failures → preserve config. Multiple learnings → merge tools.

## 🔍 TOOL EXTRACTION

- **CRITICAL**: Use StepLearningsFolderMappingJSON to determine which learnings folder to search for each step:
  - Steps with code execution mode enabled → search in learning_code_exec/ folder
  - Steps with code execution mode disabled → search in learnings/ folder
- Search learning file content to match steps (filenames may differ)
- Extract only from ✅ SUCCESS patterns, ignore ❌ failures
- **FILTER OUT from enabled_custom_tools**: read_large_output, search_large_output, query_large_output (large output virtual tools - not configurable in enabled_custom_tools)
- **If detected in learnings**: Suggest setting enable_large_output_virtual_tools to true (these tools are managed via this boolean flag)

## 📝 UPDATE FORMAT

- Update only: selected_servers, selected_tools, enabled_custom_tools, enable_large_output_virtual_tools
- Preserve other fields (execution_llm, validation_llm, max_turns, etc.)
- **enable_large_output_virtual_tools**: If you see read_large_output, search_large_output, or query_large_output used in learnings, suggest enabling this flag (set to true)

## 🚨 CRITICAL WORKFLOW

1. **FIRST**: Use human_feedback to ask which steps to optimize
2. **THEN**: Extract tools from learnings, map current config, prepare proposal
3. **THEN**: Present proposal and request approval
4. **THEN**: After approval, update step_config.json
`
}

// toolOptimizationUserMessageProcessor creates the user message for plan tool optimization
func (agent *HumanControlledTodoPlannerPlanToolOptimizationAgent) toolOptimizationUserMessageProcessor(templateVars map[string]string) string {
	// Build variables section if available
	variablesSection := ""
	if variableNames := templateVars["VariableNames"]; variableNames != "" {
		variablesSection = `
**Available Variables** (for context - plan may contain {{VARIABLE_NAME}} placeholders):
` + variableNames + `
**Note**: Variable placeholders in step descriptions help you understand what the step is about, but you don't need to resolve them for tool optimization.

`
	}

	return `# Plan Tool Optimization Task

**PRIMARY GOAL**: First ask the user which step(s) to optimize, then analyze successful tool usage from learnings folder for those steps and optimize tool selections in step_config.json. Only use tools from successful learnings (✅ success patterns) to suggest tool configurations.

**Context**:
- **Workspace Path**: ` + templateVars["WorkspacePath"] + `
- **Allowed Paths**: ` + templateVars["AllowedPaths"] + `
- **Preset Servers**: ` + templateVars["PresetServers"] + `
- **Preset Tools**: ` + templateVars["PresetTools"] + `

` + variablesSection + `**Current Plan** (contains step IDs, titles, descriptions, success criteria, and loop information):
` + func() string {
		if templateVars["PlanJSON"] != "" {
			return templateVars["PlanJSON"]
		}
		return "No plan JSON provided."
	}() + `

**NOTE**: The plan above contains step IDs, titles, descriptions, success criteria, and loop information (if applicable). Use this information to better understand what each step does and what tools might be needed.

**Current Tool Configurations** (pre-computed mapping from step_config.json - includes ALL steps from plan, even those without config):
` + func() string {
		if templateVars["CurrentToolConfigsJSON"] != "" {
			return templateVars["CurrentToolConfigsJSON"]
		}
		return "No current tool configurations found."
	}() + `

**IMPORTANT**: The Current Tool Configurations above includes ALL steps from the plan. Steps without existing configuration will have "has_config": false and empty tool arrays. You must still present ALL steps to the user when asking which steps to optimize.

**Full step_config.json** (for reference):
` + func() string {
		if templateVars["StepConfigJSON"] != "" {
			return templateVars["StepConfigJSON"]
		}
		return "No step_config.json provided (will be created if needed)."
	}() + `

**Step Learnings Folder Mapping** (CRITICAL - determines which learnings folder to search for each step):
` + func() string {
		if templateVars["StepLearningsFolderMappingJSON"] != "" {
			return templateVars["StepLearningsFolderMappingJSON"]
		}
		return "No step learnings folder mapping provided."
	}() + `

**IMPORTANT**: Use the Step Learnings Folder Mapping above to determine which folder to search for each step:
- Steps with is_code_exec: true → search in learning_code_exec/ folder
- Steps with is_code_exec: false → search in learnings/ folder
- The learnings_path field shows the exact folder path to use (e.g., "learnings/" or "learning_code_exec/")

**YOUR TASKS**:

1. **FIRST**: Use human_feedback to ask which steps to optimize
2. Extract tools from ✅ SUCCESS patterns in learnings (note which learning files contain each tool):
   - **CRITICAL**: For each step being optimized, check the Step Learnings Folder Mapping to determine which folder to search (learnings/ or learning_code_exec/)
   - Use the correct folder path from the mapping for each step
3. Map current config from step_config.json
4. Prepare optimization proposal (tools from learnings + current config + inferred workspace tools)
5. **Present proposal with detailed reasoning** - For each tool, explain:
   - If from learnings: Which learning file(s) and relevant excerpts
   - If from step description: How description/success criteria indicate need
   - If from current config: Why it's still needed
   - If inferred: The inference logic
6. Request approval before updating
7. After approval, update step_config.json

**REMINDERS**:
- **CRITICAL**: When asking user which steps to optimize, show ALL steps from the plan (including those without existing config)
- **CRITICAL**: When presenting tool proposals, provide detailed reasoning for EACH tool:
  - Reference specific learning files and quote relevant excerpts
  - Reference step descriptions/success criteria for inferred tools
  - Explain why tools from current config are being kept
- **CRITICAL**: Use Step Learnings Folder Mapping to determine which folder to search for each step (learnings/ or learning_code_exec/)
- **CRITICAL - Essential Workspace Tools**: When optimizing workspace tools, ALWAYS include these essential tools (even if not in learnings):
  - workspace_tools:list_workspace_files (essential for file discovery)
  - workspace_tools:read_workspace_file (essential for reading files)
  - workspace_tools:update_workspace_file (essential for writing/updating files)
- Search learning file content (filenames may not match)
- MCP/Human tools: only from learnings or current config
- Workspace tools: infer from step description if missing from learnings, but ALWAYS include essential tools above
- **IGNORE**: read_large_output, search_large_output, query_large_output (large output virtual tools - not configurable)
- Be conservative with removals
- Always request approval before updating
`
}
