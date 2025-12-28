package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	loggerv2 "mcpagent/logger/v2"
)

// StepConfig represents a single step's configuration in step_config.json
type StepConfig struct {
	ID           string        `json:"id"`              // Stable step ID (from plan.json) - required identifier
	Title        string        `json:"title,omitempty"` // Step title (optional, for reference/display only)
	AgentConfigs *AgentConfigs `json:"agent_configs,omitempty"`
}

// StepConfigFile represents the step_config.json file format
// Format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
type StepConfigFile struct {
	Steps []StepConfig `json:"steps"`
}

// ParseStepConfigContent parses step_config.json content in object format:
// Format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
func ParseStepConfigContent(content string) ([]StepConfig, error) {
	// Parse as object format with "steps" field
	var configFile StepConfigFile
	if err := json.Unmarshal([]byte(content), &configFile); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse step_config.json: expected format { \"steps\": [...] }, got error: %w", err), nil)
	}

	// Return the steps array directly
	return configFile.Steps, nil
}

// ReadStepConfigs reads step_config.json from the workspace
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
func ReadStepConfigs(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath string) ([]StepConfig, error) {
	// First, try to read from run folder (run-specific config)
	runConfigPath := filepath.Join(runWorkspacePath, "planning", "step_config.json")
	content, err := bo.ReadWorkspaceFile(ctx, runConfigPath)
	if err == nil {
		// Run folder config exists - use it
		configs, err := ParseStepConfigContent(content)
		if err != nil {
			return nil, fmt.Errorf(fmt.Sprintf("failed to parse run folder step_config.json: %w", err), nil)
		}
		bo.GetLogger().Info(fmt.Sprintf("📁 Using run-specific step_config.json from: %s", runConfigPath))
		return configs, nil
	}

	// Fallback to workspace default config
	// Note: configs are saved to workspacePath/planning/step_config.json
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")
	content, err = bo.ReadWorkspaceFile(ctx, configPath)
	if err != nil {
		// File doesn't exist yet - return empty array
		if os.IsNotExist(err) {
			bo.GetLogger().Info(fmt.Sprintf("📁 No step_config.json found (neither run-specific nor default) - using defaults"))
			return []StepConfig{}, nil
		}
		return nil, fmt.Errorf(fmt.Sprintf("failed to read step_config.json: %w", err), nil)
	}

	configs, err := ParseStepConfigContent(content)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse step_config.json: %w", err), nil)
	}

	bo.GetLogger().Info(fmt.Sprintf("📁 Using default step_config.json from: %s", configPath))
	return configs, nil
}

// ReadStepConfigs is a private wrapper that uses receiver fields (for backward compatibility)
// Uses run folder path if available, otherwise falls back to base workspace path
func (hcpo *HumanControlledTodoPlannerOrchestrator) ReadStepConfigs(ctx context.Context) ([]StepConfig, error) {
	workspacePath := hcpo.GetWorkspacePath()
	// Build run folder path if selectedRunFolder is set
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = filepath.Join(workspacePath, "runs", hcpo.selectedRunFolder)
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Reading step_config.json - will try run folder first: %s/planning/step_config.json", runWorkspacePath))
	} else {
		// No run folder selected yet - use base workspace path
		runWorkspacePath = workspacePath
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Reading step_config.json - no run folder selected, using base workspace: %s/planning/step_config.json", workspacePath))
	}
	return ReadStepConfigs(ctx, hcpo.BaseOrchestrator, workspacePath, runWorkspacePath)
}

// WriteStepConfigs writes step_config.json to the workspace in object format
// Format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
// Uses the orchestrator's WriteWorkspaceFile method
// Note: Directory creation is handled automatically by the workspace API
func (hcpo *HumanControlledTodoPlannerOrchestrator) WriteStepConfigs(ctx context.Context, configs []StepConfig) error {
	workspacePath := hcpo.GetWorkspacePath()
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	// Write in object format with "steps" field
	configFile := StepConfigFile{
		Steps: configs,
	}
	jsonData, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal step_config.json: %w", err), nil)
	}

	// WriteWorkspaceFile will automatically create the directory structure via the workspace API
	if err := hcpo.WriteWorkspaceFile(ctx, configPath, string(jsonData)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write step_config.json: %w", err), nil)
	}

	return nil
}

// MatchStepConfigs matches new plan steps with existing configs by ID only
// Returns a map of step index -> matched AgentConfigs
// Returns an error if any step is missing a required ID field
func MatchStepConfigs(newSteps []PlanStepInterface, oldConfigs []StepConfig) (map[int]*AgentConfigs, error) {
	result := make(map[int]*AgentConfigs)

	// Create lookup map: ID -> config
	idConfigMap := make(map[string]*AgentConfigs)

	for i := range oldConfigs {
		if oldConfigs[i].AgentConfigs != nil && oldConfigs[i].ID != "" {
			idConfigMap[oldConfigs[i].ID] = oldConfigs[i].AgentConfigs
		}
	}

	// Match new steps to old configs by ID only
	// Steps always have IDs from backend - throw error if missing
	for i, step := range newSteps {
		// Use existing step ID (required) - steps always have IDs from plan.json
		stepID := step.GetID()
		if stepID == "" {
			// This should never happen - steps always have IDs from backend
			// Throw error to match frontend behavior and catch bugs early
			stepTitle := "unknown"
			if step.GetTitle() != "" {
				stepTitle = step.GetTitle()
			}
			return nil, fmt.Errorf(fmt.Sprintf("step at index %d is missing required ID field. Step title: %q", i, stepTitle), nil)
		}

		// Match config by ID
		config := idConfigMap[stepID]
		if config != nil {
			result[i] = config
		} else {
			// Log when step ID doesn't match - helps debug matching issues
			// Only log if there are configs available (to avoid noise when no configs exist)
			if len(idConfigMap) > 0 {
				// Get available IDs for debugging
				availableIDs := make([]string, 0, len(idConfigMap))
				for id := range idConfigMap {
					availableIDs = append(availableIDs, id)
				}
				// Note: Can't use logger here as this is a pure function
				// Logging will be done in the caller
			}
		}
		// If not found, result[i] will be nil (no config for this step)
	}

	return result, nil
}

// MatchStepConfigByID matches a step config by ID (for branch steps)
// stepID: the step ID to match (from plan.json)
// Returns the matched AgentConfigs or nil if not found
func MatchStepConfigByID(stepID string, oldConfigs []StepConfig) *AgentConfigs {
	if stepID == "" {
		return nil
	}

	// Look up by ID
	for i := range oldConfigs {
		if oldConfigs[i].ID == stepID {
			return oldConfigs[i].AgentConfigs
		}
	}

	return nil
}

// MergeAgentConfigFields merges all fields from source config into target config.
// Only non-nil fields from source are copied to target.
// This ensures step-specific configs from step_config.json override defaults.
func MergeAgentConfigFields(target *AgentConfigs, source *AgentConfigs, stepID string, logger loggerv2.Logger) {
	if source == nil {
		return
	}

	if target == nil {
		logger.Warn(fmt.Sprintf("⚠️ Cannot merge config for step %s: target is nil", stepID))
		return
	}

	if source.UseCodeExecutionMode != nil {
		target.UseCodeExecutionMode = source.UseCodeExecutionMode
		logger.Info(fmt.Sprintf("🔧 Using step config (ID: %s) - use_code_execution_mode: %v", stepID, *source.UseCodeExecutionMode))
	}
	if source.LockLearnings != nil {
		target.LockLearnings = source.LockLearnings
	}
	if source.DisableLearning != nil {
		target.DisableLearning = source.DisableLearning
	}
	if source.ExecutionLLM != nil {
		target.ExecutionLLM = source.ExecutionLLM
	}
	if source.ValidationLLM != nil {
		target.ValidationLLM = source.ValidationLLM
	}
	if source.LearningLLM != nil {
		target.LearningLLM = source.LearningLLM
	}
	if source.SelectedServers != nil {
		target.SelectedServers = source.SelectedServers
	}
	if source.SelectedTools != nil {
		target.SelectedTools = source.SelectedTools
	}
	if source.KeepLearningFull != nil {
		target.KeepLearningFull = source.KeepLearningFull
	}
	if source.DisableTempLLM != nil {
		target.DisableTempLLM = source.DisableTempLLM
		logger.Info(fmt.Sprintf("🔧 Using step config (ID: %s) - disable_temp_llm: %v", stepID, *source.DisableTempLLM))
	}
}

// ApplyStepConfigFromFile loads step_config.json and applies matched config to the step.
// If step has no AgentConfigs, it creates one and copies all matched fields.
// If step already has AgentConfigs, it merges only the fields from matched config.
// Returns error if config file cannot be read.
func ApplyStepConfigFromFile(
	ctx context.Context,
	step PlanStepInterface,
	orchestrator *HumanControlledTodoPlannerOrchestrator,
) error {
	if step.GetID() == "" {
		return nil // No ID, skip config matching
	}

	stepConfigs, err := orchestrator.ReadStepConfigs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read step configs: %w", err)
	}

	matchedConfig := MatchStepConfigByID(step.GetID(), stepConfigs)
	if matchedConfig == nil {
		return nil // No matched config, use defaults
	}

	// Initialize AgentConfigs if not present
	agentConfigs := getAgentConfigs(step)
	if agentConfigs == nil {
		// Need to set AgentConfigs on the step - this requires type assertion
		switch s := step.(type) {
		case *RegularPlanStep:
			s.AgentConfigs = matchedConfig
		case *ConditionalPlanStep:
			s.AgentConfigs = matchedConfig
		case *DecisionPlanStep:
			s.AgentConfigs = matchedConfig
		case *OrchestrationPlanStep:
			s.AgentConfigs = matchedConfig
		default:
			return fmt.Errorf("unknown step type: %T", step)
		}
		orchestrator.GetLogger().Info(fmt.Sprintf("✅ Applied full config for step %s (ID: %s)", step.GetTitle(), step.GetID()))
	} else {
		// Merge matched config into existing config
		MergeAgentConfigFields(agentConfigs, matchedConfig, step.GetID(), orchestrator.GetLogger())
	}

	return nil
}
