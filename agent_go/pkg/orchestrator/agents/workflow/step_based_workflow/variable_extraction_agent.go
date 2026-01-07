package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	loggerv2 "mcpagent/logger/v2"
)

// Variable represents a single variable definition
type Variable struct {
	Name        string `json:"name"`        // e.g., "AWS_ACCOUNT_ID"
	Value       string `json:"value"`       // Original value from objective (used in single-group mode)
	Description string `json:"description"` // e.g., "AWS account number for deployment"
}

// VariableGroup represents a single set of variable values for batch execution
type VariableGroup struct {
	GroupID     string            `json:"group_id"`     // e.g., "group-1", "group-2" (used as fallback for folder names)
	DisplayName string            `json:"display_name"` // Optional user-friendly name (e.g., "Production", "Staging")
	Values      map[string]string `json:"values"`       // Variable name -> value mapping
	Enabled     bool              `json:"enabled"`      // Whether to include in execution
}

// VariablesManifest contains all extracted variables
// Supports both single-group (backward compatible) and multi-group modes
type VariablesManifest struct {
	Objective      string          `json:"objective"`        // Templated objective with {{VARS}}
	Variables      []Variable      `json:"variables"`        // List of variable definitions
	Groups         []VariableGroup `json:"groups,omitempty"` // Array of variable groups (multi-group mode)
	ExtractionDate string          `json:"extraction_date"`
}

// HasGroups returns true if the manifest has multiple variable groups
func (m *VariablesManifest) HasGroups() bool {
	return len(m.Groups) > 0
}

// GetEnabledGroups returns only the enabled groups
func (m *VariablesManifest) GetEnabledGroups() []VariableGroup {
	if !m.HasGroups() {
		// Single group mode: create a virtual group from Variables
		values := make(map[string]string)
		for _, v := range m.Variables {
			values[v.Name] = v.Value
		}
		return []VariableGroup{{
			GroupID: "group-1",
			Values:  values,
			Enabled: true,
		}}
	}

	var enabled []VariableGroup
	for _, g := range m.Groups {
		if g.Enabled {
			enabled = append(enabled, g)
		}
	}
	return enabled
}

// GetVariableValues returns variable values for a specific group
// If groupID is empty and no groups exist, returns values from Variables directly
func (m *VariablesManifest) GetVariableValues(groupID string) map[string]string {
	if !m.HasGroups() {
		// Old format: values are in Variables[].Value
		values := make(map[string]string)
		for _, v := range m.Variables {
			values[v.Name] = v.Value
		}
		return values
	}

	// New format: find group by ID
	for _, g := range m.Groups {
		if g.GroupID == groupID {
			return g.Values
		}
	}
	return nil
}

// GetVariableNames returns just the variable names (for display in UI)
func (m *VariablesManifest) GetVariableNames() []string {
	names := make([]string, len(m.Variables))
	for i, v := range m.Variables {
		names[i] = v.Name
	}
	return names
}

// AddGroup adds a new variable group with empty values
func (m *VariablesManifest) AddGroup() *VariableGroup {
	// Generate next group ID
	nextID := len(m.Groups) + 1
	groupID := fmt.Sprintf("group-%d", nextID)

	// Create empty values for all variables
	values := make(map[string]string)
	for _, v := range m.Variables {
		values[v.Name] = ""
	}

	newGroup := VariableGroup{
		GroupID: groupID,
		Values:  values,
		Enabled: true,
	}

	m.Groups = append(m.Groups, newGroup)
	return &m.Groups[len(m.Groups)-1]
}

// DeleteGroup removes a group by ID
func (m *VariablesManifest) DeleteGroup(groupID string) bool {
	for i, g := range m.Groups {
		if g.GroupID == groupID {
			m.Groups = append(m.Groups[:i], m.Groups[i+1:]...)
			return true
		}
	}
	return false
}

// ToggleGroup enables or disables a group
func (m *VariablesManifest) ToggleGroup(groupID string, enabled bool) bool {
	for i := range m.Groups {
		if m.Groups[i].GroupID == groupID {
			m.Groups[i].Enabled = enabled
			return true
		}
	}
	return false
}

// UpdateGroupValues updates the values for a specific group
func (m *VariablesManifest) UpdateGroupValues(groupID string, values map[string]string) bool {
	for i := range m.Groups {
		if m.Groups[i].GroupID == groupID {
			m.Groups[i].Values = values
			return true
		}
	}
	return false
}

// variablesFileMutex ensures thread-safe access to variables.json
var variablesFileMutex sync.Mutex

// variableChangelogSessionMutex ensures thread-safe access to variable changelog session tracking
var variableChangelogSessionMutex sync.Mutex

// variableChangelogSessionFile tracks the current changelog file for the active session
// Format: changelog-YYYY-MM-DD-HH-MM-SS.json
var variableChangelogSessionFile string

// variableChangelogSessionStartTime tracks when the current session started
var variableChangelogSessionStartTime time.Time

// VariableChangeLogEntry represents a single change entry in the variable changelog
type VariableChangeLogEntry struct {
	Timestamp    string                `json:"timestamp"`               // ISO 8601 timestamp
	ChangeType   string                `json:"change_type"`             // "add", "update", "delete", "objective_update", "extraction"
	VariableName string                `json:"variable_name,omitempty"` // Affected variable name (if applicable)
	Description  string                `json:"description"`             // Human-readable description of the change
	Details      string                `json:"details"`                 // Additional details (JSON string of what changed)
	Changes      []VariableFieldChange `json:"changes"`                 // Old and new values for each changed field
	// For revert support: store complete variable snapshots
	AddedVariable   *Variable `json:"added_variable,omitempty"`   // Complete variable data for "add" operations (to restore on revert)
	DeletedVariable *Variable `json:"deleted_variable,omitempty"` // Complete variable data for "delete" operations (to restore on revert)
	OldObjective    string    `json:"old_objective,omitempty"`    // Old objective value for "objective_update"
	NewObjective    string    `json:"new_objective,omitempty"`    // New objective value for "objective_update"
}

// VariableFieldChange represents a single field change with old and new values
type VariableFieldChange struct {
	VariableName string      `json:"variable_name"` // Variable name that was changed
	Field        string      `json:"field"`         // Field name (name, value, description)
	OldValue     interface{} `json:"old_value"`     // Old value (can be nil if field didn't exist)
	NewValue     interface{} `json:"new_value"`     // New value
}

// VariableChangeLog represents the changelog structure (used for reading multiple files)
type VariableChangeLog struct {
	Entries []VariableChangeLogEntry `json:"entries"`
}

// writeVariableChangelogEntry writes a changelog entry to a session-based file in variables/changelog/
// All changes during a single variable management session are written to the same file
// File format: changelog-YYYY-MM-DD-HH-MM-SS.json (session start timestamp)
func writeVariableChangelogEntry(ctx context.Context, workspacePath string, entry VariableChangeLogEntry, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	variableChangelogSessionMutex.Lock()
	defer variableChangelogSessionMutex.Unlock()

	// Check if we need to start a new session (no active session or session is too old - more than 1 hour)
	now := time.Now()
	if variableChangelogSessionFile == "" || now.Sub(variableChangelogSessionStartTime) > time.Hour {
		// Start new session
		variableChangelogSessionStartTime = now
		variableChangelogSessionFile = fmt.Sprintf("changelog-%s.json", now.Format("2006-01-02-15-04-05"))
		logger.Info(fmt.Sprintf("📝 Starting new variable changelog session: %s", variableChangelogSessionFile))
	}

	// Ensure entry timestamp is set
	if entry.Timestamp == "" {
		entry.Timestamp = now.Format(time.RFC3339)
	}

	// Use relative path only - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	changelogPath := filepath.Join("variables", "changelog", variableChangelogSessionFile)

	// Read existing changelog if it exists
	var changelog VariableChangeLog
	existingContent, err := readFile(ctx, changelogPath)
	if err == nil {
		// Changelog exists, unmarshal it
		if err := json.Unmarshal([]byte(existingContent), &changelog); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to parse existing variable changelog, creating new one: %v", err))
			changelog = VariableChangeLog{Entries: []VariableChangeLogEntry{}}
		}
	} else {
		// Changelog doesn't exist, create new one
		changelog = VariableChangeLog{Entries: []VariableChangeLogEntry{}}
	}

	// Add new entry
	changelog.Entries = append(changelog.Entries, entry)

	// Write updated changelog
	data, err := json.MarshalIndent(changelog, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal variable changelog: %w", err)
	}

	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf("failed to write variable changelog file: %w", err)
	}

	logger.Info(fmt.Sprintf("📝 Appended variable changelog entry to %s: %s - %s", variableChangelogSessionFile, entry.ChangeType, entry.Description))
	return nil
}

// resetVariableChangelogSession resets the variable changelog session (call this at the start of a new variable management session)
func resetVariableChangelogSession() {
	variableChangelogSessionMutex.Lock()
	defer variableChangelogSessionMutex.Unlock()
	variableChangelogSessionFile = ""
	variableChangelogSessionStartTime = time.Time{}
}

// readVariableChangelog reads all changelog files from variables/changelog/ directory and combines them
// Returns all entries sorted by timestamp (oldest first)
func readVariableChangelog(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error), listFiles func(context.Context, string) ([]string, error)) (*VariableChangeLog, error) {
	// Use relative path only - ReadWorkspaceFile/WriteWorkspaceFile auto-prepend workspacePath
	changelogDir := filepath.Join("variables", "changelog")

	// List all files in changelog directory
	files, err := listFiles(ctx, changelogDir)
	if err != nil {
		// Directory doesn't exist or can't be read, return empty changelog
		return &VariableChangeLog{Entries: []VariableChangeLogEntry{}}, nil
	}

	// Filter to only changelog-*.json files
	changelogFiles := make([]string, 0)
	for _, file := range files {
		if strings.HasPrefix(file, "changelog-") && strings.HasSuffix(file, ".json") {
			changelogFiles = append(changelogFiles, file)
		}
	}

	if len(changelogFiles) == 0 {
		// No changelog files found
		return &VariableChangeLog{Entries: []VariableChangeLogEntry{}}, nil
	}

	// Read all changelog files and combine entries
	// Each file now contains a VariableChangeLog with multiple entries (session-based)
	allEntries := make([]VariableChangeLogEntry, 0)
	for _, filename := range changelogFiles {
		filePath := filepath.Join(changelogDir, filename)
		content, err := readFile(ctx, filePath)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		// Try to unmarshal as VariableChangeLog (new format - session file with multiple entries)
		var changelog VariableChangeLog
		if err := json.Unmarshal([]byte(content), &changelog); err == nil {
			// Successfully parsed as VariableChangeLog - add all entries
			allEntries = append(allEntries, changelog.Entries...)
		} else {
			// Try old format (single entry per file) for backward compatibility
			var entry VariableChangeLogEntry
			if err := json.Unmarshal([]byte(content), &entry); err == nil {
				allEntries = append(allEntries, entry)
			}
			// If both fail, skip the file
		}
	}

	// Sort entries by timestamp (oldest first)
	sort.Slice(allEntries, func(i, j int) bool {
		timeI, errI := time.Parse(time.RFC3339, allEntries[i].Timestamp)
		timeJ, errJ := time.Parse(time.RFC3339, allEntries[j].Timestamp)
		if errI != nil || errJ != nil {
			// If parsing fails, keep original order
			return i < j
		}
		return timeI.Before(timeJ)
	})

	return &VariableChangeLog{Entries: allEntries}, nil
}

// readVariablesFromFile reads variables.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readVariablesFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*VariablesManifest, error) {
	// Use relative path only - ReadWorkspaceFile auto-prepends workspacePath
	variablesPath := filepath.Join("variables", "variables.json")

	variablesFileMutex.Lock()
	defer variablesFileMutex.Unlock()

	content, err := readFile(ctx, variablesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read variables.json: %w", err)
	}

	var manifest VariablesManifest
	if err := json.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse variables.json: %w", err)
	}

	return &manifest, nil
}

// writeVariablesToFile writes VariablesManifest to variables.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
func writeVariablesToFile(ctx context.Context, workspacePath string, manifest *VariablesManifest, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	// Use relative path only - WriteWorkspaceFile auto-prepends workspacePath
	variablesPath := filepath.Join("variables", "variables.json")

	variablesFileMutex.Lock()
	defer variablesFileMutex.Unlock()

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal variables: %w", err)
	}

	if err := writeFile(ctx, variablesPath, string(data)); err != nil {
		return fmt.Errorf("failed to write variables.json: %w", err)
	}

	return nil
}

// getUpdateVariableSchema returns the JSON schema for update_variable tool
func getUpdateVariableSchema() string {
	return `{
		"type": "object",
		"properties": {
			"existing_variable_name": {
				"type": "string",
				"description": "Name of existing variable to update/delete (required for update/delete actions)"
			},
			"name": {
				"type": "string",
				"description": "Variable name in UPPER_SNAKE_CASE (required for add, optional for update)"
			},
			"value": {
				"type": "string",
				"description": "Variable value (optional)"
			},
			"description": {
				"type": "string",
				"description": "Variable description (optional)"
			},
			"action": {
				"type": "string",
				"enum": ["update", "add", "delete"],
				"description": "Action to perform: update existing, add new, or delete"
			}
		},
		"required": ["action"]
	}`
}

// getUpdateObjectiveSchema returns the JSON schema for update_objective tool
func getUpdateObjectiveSchema() string {
	return `{
		"type": "object",
		"properties": {
			"objective": {
				"type": "string",
				"description": "Updated templated objective with {{VARIABLE}} placeholders"
			}
		},
		"required": ["objective"]
	}`
}

// createUpdateVariableExecutor creates an executor function for update_variable tool
func createUpdateVariableExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract action
		actionRaw, ok := args["action"].(string)
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid action argument"), nil)
		}
		action := actionRaw

		// Read current variables
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read variables: %w", err)
		}

		switch action {
		case "add":
			// Extract new variable fields
			nameRaw, ok := args["name"].(string)
			if !ok || nameRaw == "" {
				return "", fmt.Errorf(fmt.Sprintf("name is required for add action"), nil)
			}
			name := nameRaw

			valueRaw, _ := args["value"].(string)
			value := valueRaw

			descriptionRaw, _ := args["description"].(string)
			description := descriptionRaw

			// Check if variable already exists
			for _, v := range manifest.Variables {
				if v.Name == name {
					return "", fmt.Errorf("variable %s already exists", name)
				}
			}

			// Add new variable
			newVar := Variable{
				Name:        name,
				Value:       value,
				Description: description,
			}
			manifest.Variables = append(manifest.Variables, newVar)
			logger.Info(fmt.Sprintf("✅ Added new variable: %s", name))

			// Write changelog entry
			detailsJSON, _ := json.Marshal(map[string]interface{}{
				"variable_name": name,
				"value":         value,
				"description":   description,
			})
			changelogEntry := VariableChangeLogEntry{
				Timestamp:     time.Now().Format(time.RFC3339),
				ChangeType:    "add",
				VariableName:  name,
				Description:   fmt.Sprintf("Added variable: %s", name),
				Details:       string(detailsJSON),
				AddedVariable: &newVar, // Store complete variable data for revert
				Changes:       []VariableFieldChange{},
			}
			if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
			}

		case "update":
			// Extract existing variable name
			existingNameRaw, ok := args["existing_variable_name"].(string)
			if !ok || existingNameRaw == "" {
				return "", fmt.Errorf(fmt.Sprintf("existing_variable_name is required for update action"), nil)
			}
			existingName := existingNameRaw

			// Find the variable to update
			found := false
			var oldVar Variable
			var fieldChanges []VariableFieldChange
			for i := range manifest.Variables {
				if manifest.Variables[i].Name == existingName {
					found = true
					// Capture old variable state before updating
					oldVar = manifest.Variables[i]

					// Update fields if provided and track changes
					if nameRaw, ok := args["name"].(string); ok && nameRaw != "" {
						// Check if new name conflicts with existing variable
						if nameRaw != existingName {
							for _, v := range manifest.Variables {
								if v.Name == nameRaw {
									return "", fmt.Errorf("variable %s already exists, cannot rename to it", nameRaw)
								}
							}
						}
						if nameRaw != existingName {
							fieldChanges = append(fieldChanges, VariableFieldChange{
								VariableName: existingName,
								Field:        "name",
								OldValue:     existingName,
								NewValue:     nameRaw,
							})
						}
						manifest.Variables[i].Name = nameRaw
					}
					if valueRaw, ok := args["value"].(string); ok {
						if valueRaw != oldVar.Value {
							fieldChanges = append(fieldChanges, VariableFieldChange{
								VariableName: existingName,
								Field:        "value",
								OldValue:     oldVar.Value,
								NewValue:     valueRaw,
							})
						}
						manifest.Variables[i].Value = valueRaw
					}
					if descriptionRaw, ok := args["description"].(string); ok {
						if descriptionRaw != oldVar.Description {
							fieldChanges = append(fieldChanges, VariableFieldChange{
								VariableName: existingName,
								Field:        "description",
								OldValue:     oldVar.Description,
								NewValue:     descriptionRaw,
							})
						}
						manifest.Variables[i].Description = descriptionRaw
					}
					logger.Info(fmt.Sprintf("✅ Updated variable: %s", existingName))
					break
				}
			}
			if !found {
				return "", fmt.Errorf("variable %s not found", existingName)
			}

			// Write changelog entry if there were changes
			if len(fieldChanges) > 0 {
				detailsJSON, _ := json.Marshal(map[string]interface{}{
					"variable_name":  existingName,
					"changed_fields": fieldChanges,
				})
				changelogEntry := VariableChangeLogEntry{
					Timestamp:    time.Now().Format(time.RFC3339),
					ChangeType:   "update",
					VariableName: existingName,
					Description:  fmt.Sprintf("Updated variable: %s", existingName),
					Details:      string(detailsJSON),
					Changes:      fieldChanges,
				}
				if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
					logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
				}
			}

		case "delete":
			// Extract existing variable name
			existingNameRaw, ok := args["existing_variable_name"].(string)
			if !ok || existingNameRaw == "" {
				return "", fmt.Errorf(fmt.Sprintf("existing_variable_name is required for delete action"), nil)
			}
			existingName := existingNameRaw

			// Find and remove the variable (capture before deletion for changelog)
			found := false
			var deletedVar *Variable
			filtered := make([]Variable, 0, len(manifest.Variables))
			for _, v := range manifest.Variables {
				if v.Name == existingName {
					found = true
					deletedVar = &v // Capture complete variable data for revert
				} else {
					filtered = append(filtered, v)
				}
			}
			if !found {
				return "", fmt.Errorf("variable %s not found", existingName)
			}
			manifest.Variables = filtered
			logger.Info(fmt.Sprintf("✅ Deleted variable: %s", existingName))

			// Write changelog entry
			detailsJSON, _ := json.Marshal(map[string]interface{}{
				"variable_name": existingName,
			})
			changelogEntry := VariableChangeLogEntry{
				Timestamp:       time.Now().Format(time.RFC3339),
				ChangeType:      "delete",
				VariableName:    existingName,
				Description:     fmt.Sprintf("Deleted variable: %s", existingName),
				Details:         string(detailsJSON),
				DeletedVariable: deletedVar, // Store complete variable data for revert
				Changes:         []VariableFieldChange{},
			}
			if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
				logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
			}

		default:
			return "", fmt.Errorf("invalid action: %s (must be 'add', 'update', or 'delete')", action)
		}

		// Preserve extraction_date
		if manifest.ExtractionDate == "" {
			manifest.ExtractionDate = time.Now().Format(time.RFC3339)
		}

		// Write updated variables
		if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write variables: %w", err)
		}

		return fmt.Sprintf("Successfully performed %s action on variables", action), nil
	}
}

// createUpdateObjectiveExecutor creates an executor function for update_objective tool
func createUpdateObjectiveExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract objective
		objectiveRaw, ok := args["objective"].(string)
		if !ok || objectiveRaw == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid objective argument"), nil)
		}
		objective := objectiveRaw

		// Read current variables
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf("failed to read variables: %w", err)
		}

		// Capture old objective before updating
		oldObjective := manifest.Objective

		// Update objective
		manifest.Objective = objective

		// Preserve extraction_date
		if manifest.ExtractionDate == "" {
			manifest.ExtractionDate = time.Now().Format(time.RFC3339)
		}

		// Write updated variables
		if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf("failed to write variables: %w", err)
		}

		// Write changelog entry
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"old_objective": oldObjective,
			"new_objective": objective,
		})
		changelogEntry := VariableChangeLogEntry{
			Timestamp:    time.Now().Format(time.RFC3339),
			ChangeType:   "objective_update",
			Description:  "Updated objective in variables.json",
			Details:      string(detailsJSON),
			OldObjective: oldObjective,
			NewObjective: objective,
			Changes:      []VariableFieldChange{},
		}
		if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated objective in variables.json"))
		return "Successfully updated objective", nil
	}
}
