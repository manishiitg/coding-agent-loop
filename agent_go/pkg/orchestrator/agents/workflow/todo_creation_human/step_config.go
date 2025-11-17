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
	Index        int           `json:"index"`           // Step index (0-based) - primary key for matching
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) ReadStepConfigs(ctx context.Context) (*StepConfigFile, error) {
	return ReadStepConfigs(ctx, hcpo.BaseOrchestrator, hcpo.GetWorkspacePath(), hcpo.GetWorkspacePath())
}

// WriteStepConfigs writes step_config.json to the workspace
// Uses the orchestrator's WriteWorkspaceFile method
func (hcpo *HumanControlledTodoPlannerOrchestrator) WriteStepConfigs(ctx context.Context, configs *StepConfigFile) error {
	workspacePath := hcpo.GetWorkspacePath()
	configPath := filepath.Join(workspacePath, "planning", "step_config.json")

	// Ensure planning directory exists
	planningDir := filepath.Join(workspacePath, "planning")
	if err := os.MkdirAll(planningDir, 0755); err != nil {
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

// MatchStepConfigs matches new plan steps with existing configs by step index
// Returns a map of step index -> matched AgentConfigs
func MatchStepConfigs(newSteps []PlanStep, oldConfigs *StepConfigFile) map[int]*AgentConfigs {
	result := make(map[int]*AgentConfigs)

	// Create a lookup map from old config indices to configs
	oldConfigMap := make(map[int]*AgentConfigs)
	for i := range oldConfigs.Steps {
		index := oldConfigs.Steps[i].Index
		oldConfigMap[index] = oldConfigs.Steps[i].AgentConfigs
	}

	// Match new steps to old configs by index (0-based)
	for i := range newSteps {
		if config, found := oldConfigMap[i]; found {
			result[i] = config
		}
		// If not found, result[i] will be nil (no config for this step)
	}

	return result
}
