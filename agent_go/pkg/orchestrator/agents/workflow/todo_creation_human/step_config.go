package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"mcp-agent/agent_go/pkg/orchestrator"
)

// StepConfig represents a single step's configuration in step_config.json
type StepConfig struct {
	ID           string        `json:"id"`              // Stable step ID (generated from title) - required identifier
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

// GenerateStepID generates a stable ID from a step title
// Uses a simple hash-like approach to create a consistent ID
func GenerateStepID(title string) string {
	if title == "" {
		return ""
	}

	// Create a URL-friendly slug from the title
	slug := strings.ToLower(strings.TrimSpace(title))
	// Remove special characters
	reg := regexp.MustCompile(`[^\w\s-]`)
	slug = reg.ReplaceAllString(slug, "")
	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")
	// Replace multiple hyphens with single
	reg2 := regexp.MustCompile(`-+`)
	slug = reg2.ReplaceAllString(slug, "-")
	// Remove leading/trailing hyphens
	slug = strings.Trim(slug, "-")

	// Add a simple hash to ensure uniqueness (first 8 chars of a hash)
	// This matches the frontend implementation: Math.abs(hash).toString(36).substring(0, 8)
	hash := 0
	for i := 0; i < len(title); i++ {
		char := int(title[i])
		hash = ((hash << 5) - hash) + char
		// Note: In JavaScript, `hash & hash` converts to 32-bit signed integer
		// In Go, this is redundant but we keep the same calculation for consistency
	}
	// Convert to absolute value and base 36 (matching frontend)
	absHash := hash
	if absHash < 0 {
		absHash = -absHash
	}
	hashStr := strconv.FormatInt(int64(absHash), 36)
	if len(hashStr) > 8 {
		hashStr = hashStr[:8]
	}

	return fmt.Sprintf("%s-%s", slug, hashStr)
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
	for i := range newSteps {
		// Match by ID (if step has a title)
		if newSteps[i].Title != "" {
			stepID := GenerateStepID(newSteps[i].Title)
			config := idConfigMap[stepID]
			if config != nil {
				result[i] = config
			}
		}
		// If not found, result[i] will be nil (no config for this step)
	}

	return result
}

// MatchStepConfigByID matches a step config by ID (for branch steps)
// parentTitle: title of the parent step
// branchType: "true" or "false"
// nestedIndex: index of the branch step within the branch
// branchTitle: title of the branch step
// Returns the matched AgentConfigs or nil if not found
func MatchStepConfigByID(parentTitle, branchType string, nestedIndex int, branchTitle string, oldConfigs *StepConfigFile) *AgentConfigs {
	if branchTitle == "" {
		return nil
	}

	// Generate ID: parent-title + branch-type + nested-index + branch-title
	idInput := fmt.Sprintf("%s-%s-%d-%s", parentTitle, branchType, nestedIndex, branchTitle)
	stepID := GenerateStepID(idInput)

	// Look up by ID
	for i := range oldConfigs.Steps {
		if oldConfigs.Steps[i].ID == stepID {
			return oldConfigs.Steps[i].AgentConfigs
		}
	}

	return nil
}
