package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// StepConfig represents a single step's configuration in step_config.json
type StepConfig struct {
	ID               string            `json:"id"`              // Stable step ID (from plan.json) - required identifier
	Title            string            `json:"title,omitempty"` // Step title (optional, for reference/display only)
	AgentConfigs     *AgentConfigs     `json:"agent_configs,omitempty"`
	ValidationSchema *ValidationSchema `json:"validation_schema,omitempty"` // Override pre-validation schema (takes precedence over plan.json)
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
		return nil, fmt.Errorf("failed to parse step_config.json: expected format { \"steps\": [...] }, got error: %w", err)
	}

	// Return the steps array directly
	return configFile.Steps, nil
}

// ReadStepConfigs reads step_config.json from the workspace
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
// NOTE: workspacePath and runWorkspacePath are kept for API compatibility but paths are constructed as relative
// ReadWorkspaceFile auto-prepends the workspace path, so we only pass relative paths
func ReadStepConfigs(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath, configSubdir string) ([]StepConfig, error) {
	// Extract the run folder relative path from runWorkspacePath
	// runWorkspacePath is typically "{workspace}/runs/{selectedRunFolder}" or just "{workspace}"
	// We need to construct relative paths for ReadWorkspaceFile

	// Default configSubdir to "planning" if empty
	if configSubdir == "" {
		configSubdir = "planning"
	}

	// First, try to read from run folder (run-specific config)
	// Use relative path - ReadWorkspaceFile auto-prepends workspacePath
	var runConfigRelativePath string
	if runWorkspacePath != workspacePath && runWorkspacePath != "" {
		// Extract relative run path from runWorkspacePath
		// runWorkspacePath = "{workspace}/runs/{runFolder}" -> relative = "runs/{runFolder}"
		relativePart := runWorkspacePath
		if len(workspacePath) > 0 && len(runWorkspacePath) > len(workspacePath) {
			relativePart = runWorkspacePath[len(workspacePath):]
			relativePart = filepath.Clean(relativePart)
			relativePart = filepath.ToSlash(relativePart)
			if len(relativePart) > 0 && relativePart[0] == '/' {
				relativePart = relativePart[1:]
			}
		}
		runConfigRelativePath = filepath.Join(relativePart, configSubdir, "step_config.json")
	} else {
		runConfigRelativePath = filepath.Join(configSubdir, "step_config.json")
	}

	content, err := bo.ReadWorkspaceFile(ctx, runConfigRelativePath)
	if err == nil {
		// Run folder config exists - use it
		configs, err := ParseStepConfigContent(content)
		if err != nil {
			return nil, fmt.Errorf("failed to parse run folder step_config.json: %w", err)
		}
		bo.GetLogger().Info(fmt.Sprintf("📁 Using run-specific step_config.json from: %s", runConfigRelativePath))
		return configs, nil
	}

	// Fallback to workspace default config
	// Use relative path only - ReadWorkspaceFile auto-prepends workspacePath
	configPath := filepath.Join(configSubdir, "step_config.json")
	content, err = bo.ReadWorkspaceFile(ctx, configPath)
	if err != nil {
		// File doesn't exist yet - return empty array
		if os.IsNotExist(err) {
			bo.GetLogger().Info(fmt.Sprintf("📁 No step_config.json found (neither run-specific nor default) - using defaults"))
			return []StepConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read step_config.json: %w", err)
	}

	configs, err := ParseStepConfigContent(content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse step_config.json: %w", err)
	}

	bo.GetLogger().Info(fmt.Sprintf("📁 Using default step_config.json from: %s", configPath))
	return configs, nil
}

// ReadStepConfigs is a private wrapper that uses receiver fields (for backward compatibility)
// Uses run folder path if available, otherwise falls back to base workspace path
func (hcpo *StepBasedWorkflowOrchestrator) ReadStepConfigs(ctx context.Context) ([]StepConfig, error) {
	workspacePath := hcpo.GetWorkspacePath()
	// Build run folder path if selectedRunFolder is set
	var runWorkspacePath string

	// Determine config subdir based on mode
	configSubdir := "planning"
	if hcpo.isEvaluationMode {
		configSubdir = "evaluation"
	}

	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = filepath.Join(workspacePath, "runs", hcpo.selectedRunFolder)
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Reading step_config.json - will try run folder first: %s/%s/step_config.json", runWorkspacePath, configSubdir))
	} else {
		// No run folder selected yet - use base workspace path
		runWorkspacePath = workspacePath
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Reading step_config.json - no run folder selected, using base workspace: %s/%s/step_config.json", workspacePath, configSubdir))
	}
	return ReadStepConfigs(ctx, hcpo.BaseOrchestrator, workspacePath, runWorkspacePath, configSubdir)
}

// ReadStepConfigsFromSubdir reads the workspace-default step_config.json from
// a specific subdirectory such as "planning" or "evaluation". It intentionally
// bypasses run-specific configs because workshop config edits should update the
// durable workspace config, not a selected run's snapshot.
func (hcpo *StepBasedWorkflowOrchestrator) ReadStepConfigsFromSubdir(ctx context.Context, configSubdir string) ([]StepConfig, error) {
	workspacePath := hcpo.GetWorkspacePath()
	if configSubdir == "" {
		configSubdir = "planning"
	}
	return ReadStepConfigs(ctx, hcpo.BaseOrchestrator, workspacePath, workspacePath, configSubdir)
}

// WriteStepConfigs writes step_config.json to the workspace in object format
// Format: { "steps": [{ "id": "...", "agent_configs": {...} }] }
// Uses the orchestrator's WriteWorkspaceFile method
// Note: Directory creation is handled automatically by the workspace API
// WriteWorkspaceFile auto-prepends the workspace path, so we only pass the relative path
func (hcpo *StepBasedWorkflowOrchestrator) WriteStepConfigs(ctx context.Context, configs []StepConfig) error {
	// Determine config subdir based on mode
	configSubdir := "planning"
	if hcpo.isEvaluationMode {
		configSubdir = "evaluation"
	}
	return hcpo.WriteStepConfigsToSubdir(ctx, configSubdir, configs)
}

// WriteStepConfigsToSubdir writes step_config.json into a specific config
// subdirectory such as "planning" or "evaluation".
func (hcpo *StepBasedWorkflowOrchestrator) WriteStepConfigsToSubdir(ctx context.Context, configSubdir string, configs []StepConfig) error {
	if configSubdir == "" {
		configSubdir = "planning"
	}
	// Use relative path only - WriteWorkspaceFile auto-prepends workspacePath
	configPath := filepath.Join(configSubdir, "step_config.json")

	// Write in object format with "steps" field
	configFile := StepConfigFile{
		Steps: configs,
	}
	jsonData, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}

	// WriteWorkspaceFile will automatically create the directory structure via the workspace API
	if err := hcpo.WriteWorkspaceFile(ctx, configPath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write step_config.json: %w", err)
	}

	return nil
}

// ReadStepOverrides reads step overrides from workflow.json execution_defaults.
// Returns nil if no overrides are configured.
func (hcpo *StepBasedWorkflowOrchestrator) ReadStepOverrides(ctx context.Context) (*AgentConfigs, error) {
	manifestContent, err := hcpo.ReadWorkspaceFile(ctx, "workflow.json")
	if err != nil {
		return nil, nil
	}

	var manifest struct {
		ExecutionDefaults struct {
			DisableParallelToolExecution *bool    `json:"disable_parallel_tool_execution"`
			ExecutionMaxTurns            *int     `json:"execution_max_turns"`
			EnabledCustomTools           []string `json:"enabled_custom_tools"`
			GlobalSkillObjective         string   `json:"global_skill_objective"`
		} `json:"execution_defaults"`
	}
	if err := json.Unmarshal([]byte(manifestContent), &manifest); err != nil {
		return nil, nil
	}

	ed := manifest.ExecutionDefaults
	if ed.DisableParallelToolExecution == nil && ed.ExecutionMaxTurns == nil && len(ed.EnabledCustomTools) == 0 && ed.GlobalSkillObjective == "" {
		return nil, nil
	}

	hcpo.GetLogger().Info("📁 Using step overrides from workflow.json execution_defaults")
	return &AgentConfigs{
		DisableParallelToolExecution: ed.DisableParallelToolExecution,
		ExecutionMaxTurns:            ed.ExecutionMaxTurns,
		EnabledCustomTools:           ed.EnabledCustomTools,
		GlobalSkillObjective:         ed.GlobalSkillObjective,
	}, nil
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
			return nil, fmt.Errorf("step at index %d is missing required ID field. Step title: %q", i, stepTitle)
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
	if source.LearningObjective != "" {
		target.LearningObjective = source.LearningObjective
	}
	if source.LearningsAccess != "" {
		target.LearningsAccess = source.LearningsAccess
	}
	if source.ExecutionLLM != nil {
		target.ExecutionLLM = source.ExecutionLLM
	}
	if source.ExecutionTier != "" {
		target.ExecutionTier = source.ExecutionTier
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
	if source.EnabledCustomTools != nil {
		target.EnabledCustomTools = source.EnabledCustomTools
		logger.Info(fmt.Sprintf("🔧 Using step config (ID: %s) - enabled_custom_tools: %v", stepID, source.EnabledCustomTools))
	}
	if source.EnabledSkills != nil {
		target.EnabledSkills = source.EnabledSkills
	}
	if source.DisableParallelToolExecution != nil {
		target.DisableParallelToolExecution = source.DisableParallelToolExecution
		logger.Info(fmt.Sprintf("🔧 Using step config (ID: %s) - disable_parallel_tool_execution: %v", stepID, *source.DisableParallelToolExecution))
	}
	if source.DisableTierOptimization != nil {
		target.DisableTierOptimization = source.DisableTierOptimization
		logger.Info(fmt.Sprintf("🔧 Using step config (ID: %s) - disable_tier_optimization: %v", stepID, *source.DisableTierOptimization))
	}
	if source.DeclaredExecutionMode != "" {
		target.DeclaredExecutionMode = source.DeclaredExecutionMode
	}
	if source.DeclaredExecutionModeReason != "" {
		target.DeclaredExecutionModeReason = source.DeclaredExecutionModeReason
	}
	if source.DescriptionReviewed != nil {
		target.DescriptionReviewed = source.DescriptionReviewed
	}
	if source.ReviewNotes != "" {
		target.ReviewNotes = source.ReviewNotes
	}
	if source.GlobalSkillObjective != "" {
		target.GlobalSkillObjective = source.GlobalSkillObjective
	}
}

// ApplyStepConfigFromFile loads step_config.json and applies matched config to the step.
// If step has no AgentConfigs, it creates one and copies all matched fields.
// If step already has AgentConfigs, it merges only the fields from matched config.
// Returns error if config file cannot be read.
func ApplyStepConfigFromFile(
	ctx context.Context,
	step PlanStepInterface,
	orchestrator *StepBasedWorkflowOrchestrator,
) error {
	if step.GetID() == "" {
		return nil // No ID, skip config matching
	}

	stepConfigs, err := orchestrator.ReadStepConfigs(ctx)
	if err != nil {
		return fmt.Errorf("failed to read step configs: %w", err)
	}

	matchedConfig := MatchStepConfigByID(step.GetID(), stepConfigs)
	if matchedConfig != nil {
		// Initialize AgentConfigs if not present
		agentConfigs := getAgentConfigs(step)
		if agentConfigs == nil {
			// Need to set AgentConfigs on the step - this requires type assertion
			switch s := step.(type) {
			case *RegularPlanStep:
				s.AgentConfigs = matchedConfig
			case *ConditionalPlanStep:
				s.AgentConfigs = matchedConfig
			case *TodoTaskPlanStep:
				s.AgentConfigs = matchedConfig
			case *HumanInputPlanStep:
				s.AgentConfigs = matchedConfig
			case *EvaluationStep:
				s.AgentConfigs = matchedConfig
			case *RoutingPlanStep:
				s.AgentConfigs = matchedConfig
			case *MessageSequencePlanStep:
				s.AgentConfigs = matchedConfig
			default:
				return fmt.Errorf("unknown step type: %T", step)
			}
			orchestrator.GetLogger().Info(fmt.Sprintf("✅ Applied full config for step %s (ID: %s)", step.GetTitle(), step.GetID()))
		} else {
			// Merge matched config into existing config
			MergeAgentConfigFields(agentConfigs, matchedConfig, step.GetID(), orchestrator.GetLogger())
		}

		// Sync declared_execution_mode to boolean flags (use_code_execution_mode, etc.)
		// This ensures configs written manually or by older tools still set the right flags.
		if finalConfigs := getAgentConfigs(step); finalConfigs != nil {
			syncDeclaredExecutionModeConfig(finalConfigs)
		}
	}

	// Apply global overrides from workflow.json execution_defaults (highest priority)
	// This must run even when no step_config.json match exists, since execution_defaults
	// (e.g., global_skill_objective, disable_learning) apply to ALL steps regardless of per-step config.
	overrides, err := orchestrator.ReadStepOverrides(ctx)
	if err != nil {
		orchestrator.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_override.json in ApplyStepConfigFromFile: %v", err))
	} else if overrides != nil {
		currentConfigs := getAgentConfigs(step)
		if currentConfigs == nil {
			// Set overrides as the config
			switch s := step.(type) {
			case *RegularPlanStep:
				s.AgentConfigs = overrides
			case *ConditionalPlanStep:
				s.AgentConfigs = overrides
			case *TodoTaskPlanStep:
				s.AgentConfigs = overrides
			case *HumanInputPlanStep:
				s.AgentConfigs = overrides
			case *EvaluationStep:
				s.AgentConfigs = overrides
			case *RoutingPlanStep:
				s.AgentConfigs = overrides
			case *MessageSequencePlanStep:
				s.AgentConfigs = overrides
			}
		} else {
			MergeAgentConfigFields(currentConfigs, overrides, step.GetID(), orchestrator.GetLogger())
		}
		orchestrator.GetLogger().Info(fmt.Sprintf("🔧 Applied global overrides for step %s (ID: %s)", step.GetTitle(), step.GetID()))
	}

	return nil
}

// readStepConfigViaFileCallback reads planning/step_config.json using the same
// readFile callback shape that plan-mod tool executors get. Returns an empty
// slice if the file doesn't exist (no configs declared yet) — that's a normal
// state, not an error.
func readStepConfigViaFileCallback(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) ([]StepConfig, error) {
	configPath := normalizePathForWorkspaceAPI(filepath.Join("planning", "step_config.json"), workspacePath)
	content, err := readFile(ctx, configPath)
	if err != nil {
		// File-not-found is the common case for fresh plans; treat as empty.
		return []StepConfig{}, nil
	}
	if content == "" {
		return []StepConfig{}, nil
	}
	return ParseStepConfigContent(content)
}

// writeStepConfigViaFileCallback persists the given configs back to
// planning/step_config.json using the same writeFile callback shape that
// plan-mod tool executors get.
func writeStepConfigViaFileCallback(ctx context.Context, workspacePath string, configs []StepConfig, writeFile func(context.Context, string, string) error) error {
	configPath := normalizePathForWorkspaceAPI(filepath.Join("planning", "step_config.json"), workspacePath)
	file := StepConfigFile{Steps: configs}
	out, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal step_config.json: %w", err)
	}
	return writeFile(ctx, configPath, string(out))
}

// pruneStepConfigsByID removes entries whose IDs are in deletedSet and returns
// (newConfigs, deletedIDsActuallyRemoved). The second slice is empty when
// nothing matched, letting callers skip the write.
func pruneStepConfigsByID(configs []StepConfig, deletedSet map[string]bool) ([]StepConfig, []string) {
	if len(configs) == 0 || len(deletedSet) == 0 {
		return configs, nil
	}
	kept := make([]StepConfig, 0, len(configs))
	var removed []string
	for _, cfg := range configs {
		if deletedSet[cfg.ID] {
			removed = append(removed, cfg.ID)
			continue
		}
		kept = append(kept, cfg)
	}
	return kept, removed
}
