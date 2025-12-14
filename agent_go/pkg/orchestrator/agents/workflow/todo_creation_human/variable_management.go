package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcpagent/events"
)

// VariablesExtractedEvent represents the event when variables are extracted from objective
type VariablesExtractedEvent struct {
	events.BaseEventData
	Variables          []Variable `json:"variables"`
	TemplatedObjective string     `json:"templated_objective"`
	WorkspacePath      string     `json:"workspace_path"`       // Workspace path for file operations (required)
	RunFolder          string     `json:"run_folder,omitempty"` // Run folder name for run-specific configs
}

// GetEventType returns the event type for VariablesExtractedEvent
func (e *VariablesExtractedEvent) GetEventType() events.EventType {
	return events.VariablesExtracted
}

// VariableManager manages variable extraction and state independently from controller
type VariableManager struct {
	// Base orchestrator for common functionality
	*orchestrator.BaseOrchestrator
}

// NewVariableManager creates a new VariableManager
func NewVariableManager(
	baseOrchestrator *orchestrator.BaseOrchestrator,
) *VariableManager {
	return &VariableManager{
		BaseOrchestrator: baseOrchestrator,
	}
}

// checkExistingVariables checks if variables.json already exists and loads it
// This is the main method used by controller and planning to check for existing variables
func (vm *VariableManager) checkExistingVariables(ctx context.Context, variablesPath string) (bool, *VariablesManifest, error) {
	vm.GetLogger().Info(fmt.Sprintf("🔍 Checking for existing variables at %s", variablesPath))

	// Try to read variables.json
	variablesContent, err := vm.ReadWorkspaceFile(ctx, variablesPath)
	if err != nil {
		// Check if it's a "file not found" error (various error message formats)
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "not found") ||
			strings.Contains(errMsg, "no such file") ||
			strings.Contains(errMsg, "does not exist") ||
			strings.Contains(errMsg, "file does not exist") {
			vm.GetLogger().Info(fmt.Sprintf("📋 No existing variables found at %s - proceeding without variables", variablesPath))
			return false, nil, nil
		}
		// Other errors should be returned
		return false, nil, fmt.Errorf(fmt.Sprintf("failed to check existing variables: %w", err), nil)
	}

	// Parse the existing variables manifest
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		vm.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing variables.json: %w", err))
		return false, nil, fmt.Errorf(fmt.Sprintf("failed to parse variables.json: %w", err), nil)
	}

	vm.GetLogger().Info(fmt.Sprintf("✅ Found existing variables.json with %d variables", len(manifest.Variables)))
	return true, &manifest, nil
}

// NOTE: Variable extraction methods (runVariableExtractionPhase, requestVariableApproval,
// createVariableExtractionAgent, ExtractVariablesOnly, emitVariablesExtractedEvent) have been
// removed. Variable extraction is now handled by planning agent tools (extract_variables, update_variable).

// LoadVariableValues loads variable values from variables.json file
// Public method that accepts BaseOrchestrator, workspacePath, and runWorkspacePath as parameters
func LoadVariableValues(ctx context.Context, bo *orchestrator.BaseOrchestrator, workspacePath, runWorkspacePath string) (map[string]string, error) {
	// Try to load from run folder first (run-specific variables), then fallback to workspace default
	runVariablesPath := fmt.Sprintf("%s/variables/variables.json", runWorkspacePath)
	workspaceVariablesPath := fmt.Sprintf("%s/variables/variables.json", workspacePath)

	var variablesContent string
	var err error

	// Try run folder first
	variablesContent, err = bo.ReadWorkspaceFile(ctx, runVariablesPath)
	if err != nil {
		// Fallback to workspace folder
		variablesContent, err = bo.ReadWorkspaceFile(ctx, workspaceVariablesPath)
		if err != nil {
			return nil, fmt.Errorf(fmt.Sprintf("failed to read variables.json from both locations: %w", err), nil)
		}
		bo.GetLogger().Info(fmt.Sprintf("📁 Loaded variables from workspace folder: %s", workspaceVariablesPath))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("📁 Loaded variables from runs folder: %s", runVariablesPath))
	}

	// Parse variables.json to get current values
	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(variablesContent), &manifest); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse variables.json: %w", err), nil)
	}

	// Load values into the variableValues map
	variableValues := make(map[string]string)
	for _, variable := range manifest.Variables {
		variableValues[variable.Name] = variable.Value
	}

	bo.GetLogger().Info(fmt.Sprintf("✅ Loaded variable values from variables.json: %d variables", len(variableValues)))
	return variableValues, nil
}

// ResolveVariables replaces {{VARIABLE}} placeholders with actual values
// Public method that accepts variableValues as parameter
func ResolveVariables(text string, variableValues map[string]string) string {
	if variableValues == nil {
		return text // No variables to resolve
	}

	resolved := text
	for varName, varValue := range variableValues {
		placeholder := fmt.Sprintf("{{%s}}", varName)
		resolved = strings.ReplaceAll(resolved, placeholder, varValue)
	}
	return resolved
}

// ResolveVariablesArray resolves variables in an array of strings
// Public method that accepts variableValues as parameter
func ResolveVariablesArray(arr []string, variableValues map[string]string) []string {
	if variableValues == nil {
		return arr // No variables to resolve
	}

	resolved := make([]string, len(arr))
	for i, item := range arr {
		resolved[i] = ResolveVariables(item, variableValues)
	}
	return resolved
}

// FormatVariableNames formats the variables manifest into a human-readable string for agent prompts
// Public method that accepts manifest as parameter
func FormatVariableNames(manifest *VariablesManifest) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		builder.WriteString(fmt.Sprintf("- {{%s}} - %s\n", variable.Name, variable.Description))
	}
	return builder.String()
}

// FormatVariableValues formats the variables manifest with their actual values for agent prompts
// Public method that accepts manifest and variableValues as parameters
func FormatVariableValues(manifest *VariablesManifest, variableValues map[string]string) string {
	if manifest == nil || len(manifest.Variables) == 0 {
		return "" // No variables to format
	}

	var builder strings.Builder
	builder.WriteString("\n")
	for _, variable := range manifest.Variables {
		// Get the actual resolved value from variableValues map if available
		actualValue := variable.Value
		if variableValues != nil {
			if resolvedValue, exists := variableValues[variable.Name]; exists {
				actualValue = resolvedValue
			}
		}
		builder.WriteString(fmt.Sprintf("- {{%s}} = %s - %s\n", variable.Name, actualValue, variable.Description))
	}
	return builder.String()
}

// EmitVariablesExtractedEvent emits an event when variables are extracted from objective
// Public method that accepts BaseOrchestrator and other parameters
func EmitVariablesExtractedEvent(ctx context.Context, bo *orchestrator.BaseOrchestrator, variables []Variable, templatedObjective, runFolder, workspacePath string) {
	if bo.GetContextAwareBridge() == nil {
		return
	}

	// Create event data
	eventData := &VariablesExtractedEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Variables:          variables,
		TemplatedObjective: templatedObjective,
		WorkspacePath:      workspacePath,
		RunFolder:          runFolder,
	}

	// Create unified event wrapper
	unifiedEvent := &events.AgentEvent{
		Type:      events.VariablesExtracted,
		Timestamp: time.Now(),
		Data:      eventData,
	}

	// Emit through the context-aware bridge
	bridge := bo.GetContextAwareBridge()
	if err := bridge.HandleEvent(ctx, unifiedEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit variables extracted event: %w", err))
	} else {
		bo.GetLogger().Info(fmt.Sprintf("✅ Emitted variables extracted event: %d variables", len(variables)))
	}
}

// NOTE: ExtractVariablesOnly and emitVariablesExtractedEvent (private method) have been removed.
// Variable extraction is now handled by planning agent tools (extract_variables, update_variable).
// The public EmitVariablesExtractedEvent function is kept below for backward compatibility.
