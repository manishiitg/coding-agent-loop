package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mcp-agent/agent_go/pkg/orchestrator"
)

// StepConfig represents a single step's configuration in step_config.json
type StepConfig struct {
	ID           string        `json:"id"`              // Stable step ID (from plan.json) - required identifier
	Title        string        `json:"title,omitempty"` // Step title (optional, for reference/display only)
	AgentConfigs *AgentConfigs `json:"agent_configs,omitempty"`
}

// StepConfigFile represents the entire step_config.json file structure
type StepConfigFile struct {
	Steps []StepConfig `json:"steps"`
}

// ReadStepConfigs reads step_config.json from the workspace
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
func ReadStepConfigs(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath string) (*StepConfigFile, error) {
	// First, try to read from run folder (run-specific config)
	runConfigPath := filepath.Join(runWorkspacePath, "planning", "step_config.json")
	content, err := bo.ReadWorkspaceFile(ctx, runConfigPath)
	if err == nil {
		// Run folder config exists - use it
		var configFile StepConfigFile
		if err := json.Unmarshal([]byte(content), &configFile); err != nil {
			return nil, fmt.Errorf("failed to parse run folder step_config.json: %w", err)
		}
		bo.GetLogger().Infof("📁 Using run-specific step_config.json from: %s", runConfigPath)
		return &configFile, nil
	}

	// Fallback to workspace default config
	// Note: configs are saved to workspacePath/planning/step_config.json
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")
	content, err = bo.ReadWorkspaceFile(ctx, configPath)
	if err != nil {
		// File doesn't exist yet - return empty structure
		if os.IsNotExist(err) {
			bo.GetLogger().Infof("📁 No step_config.json found (neither run-specific nor default) - using defaults")
			return &StepConfigFile{Steps: []StepConfig{}}, nil
		}
		return nil, fmt.Errorf("failed to read step_config.json: %w", err)
	}

	var configFile StepConfigFile
	if err := json.Unmarshal([]byte(content), &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse step_config.json: %w", err)
	}

	bo.GetLogger().Infof("📁 Using default step_config.json from: %s", configPath)
	return &configFile, nil
}

// ReadStepConfigs is a private wrapper that uses receiver fields (for backward compatibility)
// Uses run folder path if available, otherwise falls back to base workspace path
func (hcpo *HumanControlledTodoPlannerOrchestrator) ReadStepConfigs(ctx context.Context) (*StepConfigFile, error) {
	workspacePath := hcpo.GetWorkspacePath()
	// Build run folder path if selectedRunFolder is set
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = filepath.Join(workspacePath, "runs", hcpo.selectedRunFolder)
		hcpo.GetLogger().Infof("📁 Reading step_config.json - will try run folder first: %s/planning/step_config.json", runWorkspacePath)
	} else {
		// No run folder selected yet - use base workspace path
		runWorkspacePath = workspacePath
		hcpo.GetLogger().Infof("📁 Reading step_config.json - no run folder selected, using base workspace: %s/planning/step_config.json", workspacePath)
	}
	return ReadStepConfigs(ctx, hcpo.BaseOrchestrator, workspacePath, runWorkspacePath)
}

// WriteStepConfigs writes step_config.json to the workspace
// Uses the orchestrator's WriteWorkspaceFile method
func (hcpo *HumanControlledTodoPlannerOrchestrator) WriteStepConfigs(ctx context.Context, configs *StepConfigFile) error {
	workspacePath := hcpo.GetWorkspacePath()
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	// Ensure planning directory exists
	planningDir := filepath.Join(workspacePath, "planning")
	if err := os.MkdirAll(planningDir, 0750); err != nil {
		return fmt.Errorf("failed to create planning directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, configPath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write step_config.json: %w", err)
	}

	return nil
}

// MatchStepConfigs matches new plan steps with existing configs by ID only
// Returns a map of step index -> matched AgentConfigs
func MatchStepConfigs(newSteps []PlanStep, oldConfigs *StepConfigFile) map[int]*AgentConfigs {
	result := make(map[int]*AgentConfigs)

	// Create lookup map: ID -> config
	idConfigMap := make(map[string]*AgentConfigs)

	for i := range oldConfigs.Steps {
		if oldConfigs.Steps[i].AgentConfigs != nil && oldConfigs.Steps[i].ID != "" {
			idConfigMap[oldConfigs.Steps[i].ID] = oldConfigs.Steps[i].AgentConfigs
		}
	}

	// Match new steps to old configs by ID only
	// Steps always have IDs from backend - throw error if missing
	for i := range newSteps {
		// Use existing step ID (required) - steps always have IDs from plan.json
		stepID := newSteps[i].ID
		if stepID == "" {
			// This should never happen - steps always have IDs from backend
			// Log error but don't crash - just skip matching for this step
			// In production, this indicates a bug in plan generation
			continue
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

	return result
}

// MatchStepConfigByID matches a step config by ID (for branch steps)
// stepID: the step ID to match (from plan.json)
// Returns the matched AgentConfigs or nil if not found
func MatchStepConfigByID(stepID string, oldConfigs *StepConfigFile) *AgentConfigs {
	if stepID == "" {
		return nil
	}

	// Look up by ID
	for i := range oldConfigs.Steps {
		if oldConfigs.Steps[i].ID == stepID {
			return oldConfigs.Steps[i].AgentConfigs
		}
	}

	return nil
}
