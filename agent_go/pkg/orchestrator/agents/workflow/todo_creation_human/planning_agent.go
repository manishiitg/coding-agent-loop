package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// HumanControlledTodoPlannerPlanningTemplate holds template variables for human-controlled planning prompts
type HumanControlledTodoPlannerPlanningTemplate struct {
	Objective     string
	WorkspacePath string
}

// HumanControlledTodoPlannerPlanningAgent creates a fast, simplified plan from the objective
type HumanControlledTodoPlannerPlanningAgent struct {
	*agents.BaseOrchestratorAgent
}

// FlexibleContextOutput handles both string and array types for context_output field
// This prevents JSON parsing errors when LLM returns arrays instead of strings
type FlexibleContextOutput string

// UnmarshalJSON implements custom unmarshaling for FlexibleContextOutput
// Handles both string and array formats to prevent parsing errors
func (f *FlexibleContextOutput) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexibleContextOutput(s)
		return nil
	}

	// Try to unmarshal as array
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		// Join array elements with comma and space
		*f = FlexibleContextOutput(strings.Join(arr, ", "))
		return nil
	}

	// If both fail, return the error from string unmarshal
	return fmt.Errorf(fmt.Sprintf("failed to unmarshal context_output as string or array"), nil)
}

// String returns the string value
func (f FlexibleContextOutput) String() string {
	return string(f)
}

// AgentLLMConfig represents LLM configuration for an agent
type AgentLLMConfig struct {
	Provider string `json:"provider,omitempty"` // e.g., "openai", "bedrock", "openrouter", "vertex"
	ModelID  string `json:"model_id,omitempty"` // e.g., "gpt-4o", "claude-3-5-sonnet-20241022"
}

// PrerequisiteRule represents a single prerequisite rule with one step dependency and one description
type PrerequisiteRule struct {
	DependsOnStep string `json:"depends_on_step"` // Step ID this rule depends on
	Description   string `json:"description"`     // User description of when to detect prerequisite failures for this specific step (e.g., "if login session is missing or expired, go back to step 0")
}

// AgentConfigs represents per-agent configuration for a step
type AgentConfigs struct {
	ExecutionLLM                  *AgentLLMConfig    `json:"execution_llm,omitempty"`
	ValidationLLM                 *AgentLLMConfig    `json:"validation_llm,omitempty"`
	LearningLLM                   *AgentLLMConfig    `json:"learning_llm,omitempty"`
	ConditionalLLM                *AgentLLMConfig    `json:"conditional_llm,omitempty"`                   // Step-specific conditional LLM for conditional step evaluation
	ExecutionMaxTurns             *int               `json:"execution_max_turns,omitempty"`               // default: 100
	ValidationMaxTurns            *int               `json:"validation_max_turns,omitempty"`              // default: 100
	LearningMaxTurns              *int               `json:"learning_max_turns,omitempty"`                // default: 100
	DisableValidation             *bool              `json:"disable_validation,omitempty"`                // skip validation entirely (nil = not set/enabled, true = disabled, false = explicitly enabled)
	DisableLearning               *bool              `json:"disable_learning,omitempty"`                  // disable learning for this step (nil = not set/enabled, true = disabled, false = explicitly enabled)
	LockLearnings                 *bool              `json:"lock_learnings,omitempty"`                    // lock learnings - prevents learning agent from running but still uses existing learnings (nil = not set/unlocked, true = locked, false = explicitly unlocked)
	LearningAfterLoopIteration    bool               `json:"learning_after_loop_iteration,omitempty"`     // run learning after each loop iteration
	LearningDetailLevel           string             `json:"learning_detail_level,omitempty"`             // "exact", "general", or "none" (default: "exact")
	SelectedServers               []string           `json:"selected_servers,omitempty"`                  // step-level MCP server selection (subset of preset servers)
	SelectedTools                 []string           `json:"selected_tools,omitempty"`                    // step-level tool selection (format: "server:tool" or "server:*" for all tools)
	EnabledCustomToolCategories   []string           `json:"enabled_custom_tool_categories,omitempty"`    // e.g., ["workspace_tools", "human_tools"] - enables all tools in category
	EnabledCustomTools            []string           `json:"enabled_custom_tools,omitempty"`              // e.g., ["read_workspace_file", "human_feedback"] - enables specific tools (overrides categories if both specified)
	EnableLargeOutputVirtualTools *bool              `json:"enable_large_output_virtual_tools,omitempty"` // Enable/disable large output tools (default: true if nil)
	UseCodeExecutionMode          *bool              `json:"use_code_execution_mode,omitempty"`           // Step-level code execution mode override (nil = use preset default, true/false = override)
	EnablePrerequisiteDetection   *bool              `json:"enable_prerequisite_detection,omitempty"`     // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules             []PrerequisiteRule `json:"prerequisite_rules,omitempty"`                // Array of prerequisite rules, each with one step dependency and one description
}

// PlanStep represents a step in the planning output
type PlanStep struct {
	ID                  string                `json:"id"` // Stable step ID (generated from title) - required
	Title               string                `json:"title"`
	Description         string                `json:"description"`
	SuccessCriteria     string                `json:"success_criteria"`
	ContextDependencies []string              `json:"context_dependencies"`
	ContextOutput       FlexibleContextOutput `json:"context_output"`             // Use flexible type to handle string or array
	HasLoop             bool                  `json:"has_loop"`                   // true if step needs to loop
	LoopCondition       string                `json:"loop_condition"`             // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations       int                   `json:"max_iterations,omitempty"`   // max iterations (default: 10)
	LoopDescription     string                `json:"loop_description,omitempty"` // human-readable explanation
	// Conditional branching fields
	HasCondition      bool       `json:"has_condition"`                   // true if step has conditional branches
	ConditionQuestion string     `json:"condition_question,omitempty"`    // question to ask ConditionalLLM
	ConditionContext  string     `json:"condition_context,omitempty"`     // context to provide to ConditionalLLM
	IfTrueSteps       []PlanStep `json:"if_true_steps,omitempty"`         // nested steps for true branch
	IfFalseSteps      []PlanStep `json:"if_false_steps,omitempty"`        // nested steps for false branch
	IfTrueNextStepID  string     `json:"if_true_next_step_id,omitempty"`  // ID of step to connect to after true branch completes (or "end" to end workflow). When if_true_steps is empty [], this is REQUIRED. When if_true_steps has steps, this is optional (defaults to next step in main plan if not specified).
	IfFalseNextStepID string     `json:"if_false_next_step_id,omitempty"` // ID of step to connect to after false branch completes (or "end" to end workflow). When if_false_steps is empty [], this is REQUIRED. When if_false_steps has steps, this is optional (defaults to next step in main plan if not specified).
	ConditionResult   *bool      `json:"condition_result,omitempty"`      // runtime: stores decision result
	ConditionReason   string     `json:"condition_reason,omitempty"`      // runtime: stores LLM reasoning
	// Decision step fields (execute step, evaluate output, route based on result)
	HasDecisionStep            bool      `json:"has_decision_step,omitempty"`            // true if step executes a single step and routes based on result
	DecisionStep               *PlanStep `json:"decision_step,omitempty"`                // The single step to execute
	DecisionEvaluationQuestion string    `json:"decision_evaluation_question,omitempty"` // Question to evaluate step output
	DecisionResult             *bool     `json:"decision_result,omitempty"`              // runtime: stores evaluation result
	DecisionReason             string    `json:"decision_reason,omitempty"`              // runtime: stores evaluation reasoning
	// Prerequisite failure detection fields (optional - can be configured in plan or later via UI in step_config.json)
	EnablePrerequisiteDetection *bool              `json:"enable_prerequisite_detection,omitempty"` // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules           []PrerequisiteRule `json:"prerequisite_rules,omitempty"`            // Array of prerequisite rules, each with one step dependency and one description
	// AgentConfigs removed - now stored separately in step_config.json
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
}

// PartialPlanStep represents a partial update to a plan step (used only in tool schemas)
type PartialPlanStep struct {
	ExistingStepID              string                `json:"existing_step_id"`                        // Required: ID of existing step to update
	Title                       string                `json:"title,omitempty"`                         // Optional: New title (if renaming)
	Description                 string                `json:"description,omitempty"`                   // Optional: Updated description
	SuccessCriteria             string                `json:"success_criteria,omitempty"`              // Optional: Updated success criteria
	ContextDependencies         []string              `json:"context_dependencies,omitempty"`          // Optional: Updated context dependencies
	ContextOutput               FlexibleContextOutput `json:"context_output,omitempty"`                // Optional: Updated context output
	HasLoop                     *bool                 `json:"has_loop,omitempty"`                      // Optional: Updated has_loop (use pointer to distinguish unset from false)
	LoopCondition               string                `json:"loop_condition,omitempty"`                // Optional: Updated loop condition
	MaxIterations               *int                  `json:"max_iterations,omitempty"`                // Optional: Updated max iterations (use pointer to distinguish unset from 0)
	LoopDescription             string                `json:"loop_description,omitempty"`              // Optional: Updated loop description
	EnablePrerequisiteDetection *bool                 `json:"enable_prerequisite_detection,omitempty"` // Optional: Updated enable_prerequisite_detection (use pointer to distinguish unset from false)
	PrerequisiteRules           []PrerequisiteRule    `json:"prerequisite_rules,omitempty"`            // Optional: Updated prerequisite rules
	// Conditional step fields
	HasCondition      *bool      `json:"has_condition,omitempty"`      // Optional: Updated has_condition (use pointer to distinguish unset from false)
	ConditionQuestion string     `json:"condition_question,omitempty"` // Optional: Updated condition question
	ConditionContext  string     `json:"condition_context,omitempty"`  // Optional: Updated condition context
	IfTrueSteps       []PlanStep `json:"if_true_steps,omitempty"`      // Optional: Updated if_true_steps (nil = not provided, empty array = clear steps)
	IfFalseSteps      []PlanStep `json:"if_false_steps,omitempty"`     // Optional: Updated if_false_steps (nil = not provided, empty array = clear steps)
	// Decision step fields
	HasDecisionStep            *bool     `json:"has_decision_step,omitempty"`            // Optional: Updated has_decision_step
	DecisionStep               *PlanStep `json:"decision_step,omitempty"`                // Optional: Updated decision step
	DecisionEvaluationQuestion string    `json:"decision_evaluation_question,omitempty"` // Optional: Updated decision evaluation question
	// Routing fields (used by both conditional and decision steps)
	IfTrueNextStepID  string `json:"if_true_next_step_id,omitempty"`  // Optional: Updated if_true_next_step_id
	IfFalseNextStepID string `json:"if_false_next_step_id,omitempty"` // Optional: Updated if_false_next_step_id
}

// planFileMutex ensures thread-safe access to plan.json
var planFileMutex sync.Mutex

// changelogSessionMutex ensures thread-safe access to changelog session tracking
var changelogSessionMutex sync.Mutex

// StepNumberMapping represents a mapping from step ID to step number (1-based position)
type StepNumberMapping struct {
	StepID     string
	StepNumber int // 1-based position in plan.steps array
}

// LearningsFolderRename represents a folder rename operation
type LearningsFolderRename struct {
	OldPath string // e.g., "learnings/step-3"
	NewPath string // e.g., "learnings/step-2"
}

// changelogSessionFile tracks the current changelog file for the active session
// Format: changelog-YYYY-MM-DD-HH-MM-SS.json
var changelogSessionFile string

// changelogSessionStartTime tracks when the current session started
var changelogSessionStartTime time.Time

// PlanChangeLogEntry represents a single change entry in the changelog
type PlanChangeLogEntry struct {
	Timestamp   string            `json:"timestamp"`   // ISO 8601 timestamp
	ChangeType  string            `json:"change_type"` // "update", "delete", "add", "convert_to_conditional", "convert_to_regular", "add_branch_steps", "update_branch_steps", "delete_branch_steps", "update_conditional_step"
	StepIDs     []string          `json:"step_ids"`    // Affected step IDs
	Description string            `json:"description"` // Human-readable description of the change
	Details     string            `json:"details"`     // Additional details (JSON string of what changed)
	Changes     []PlanFieldChange `json:"changes"`     // Old and new values for each changed field
	// For revert support: store complete step snapshots
	AddedSteps        []PlanStep `json:"added_steps,omitempty"`          // Complete step data for "add" operations (to restore on revert)
	DeletedSteps      []PlanStep `json:"deleted_steps,omitempty"`        // Complete step data for "delete" operations (to restore on revert)
	InsertAfterStepID string     `json:"insert_after_step_id,omitempty"` // For "add" operations: where the step was inserted (needed for revert)
}

// PlanFieldChange represents a single field change with old and new values
type PlanFieldChange struct {
	StepID   string      `json:"step_id"`   // Step ID that was changed
	Field    string      `json:"field"`     // Field name (title, description, success_criteria, etc.)
	OldValue interface{} `json:"old_value"` // Old value (can be nil if field didn't exist)
	NewValue interface{} `json:"new_value"` // New value
}

// PlanChangeLog represents the changelog structure (used for reading multiple files)
type PlanChangeLog struct {
	Entries []PlanChangeLogEntry `json:"entries"`
}

// writeChangelogEntry writes a changelog entry to a session-based file in planning/changelog/
// All changes during a single planning agent execution session are written to the same file
// File format: changelog-YYYY-MM-DD-HH-MM-SS.json (session start timestamp)
func writeChangelogEntry(ctx context.Context, workspacePath string, entry PlanChangeLogEntry, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	changelogSessionMutex.Lock()
	defer changelogSessionMutex.Unlock()

	// Check if we need to start a new session (no active session or session is too old - more than 1 hour)
	now := time.Now()
	if changelogSessionFile == "" || now.Sub(changelogSessionStartTime) > time.Hour {
		// Start new session
		changelogSessionStartTime = now
		changelogSessionFile = fmt.Sprintf("changelog-%s.json", now.Format("2006-01-02-15-04-05"))
		logger.Info(fmt.Sprintf("📝 Starting new changelog session: %s", changelogSessionFile))
	}

	// Ensure entry timestamp is set
	if entry.Timestamp == "" {
		entry.Timestamp = now.Format(time.RFC3339)
	}

	changelogPath := filepath.Join(workspacePath, "planning", "changelog", changelogSessionFile)

	// Read existing changelog if it exists
	var changelog PlanChangeLog
	existingContent, err := readFile(ctx, changelogPath)
	if err == nil {
		// Changelog exists, unmarshal it
		if err := json.Unmarshal([]byte(existingContent), &changelog); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to parse existing changelog, creating new one: %v", err))
			changelog = PlanChangeLog{Entries: []PlanChangeLogEntry{}}
		}
	} else {
		// Changelog doesn't exist, create new one
		changelog = PlanChangeLog{Entries: []PlanChangeLogEntry{}}
	}

	// Add new entry
	changelog.Entries = append(changelog.Entries, entry)

	// Write updated changelog
	data, err := json.MarshalIndent(changelog, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal changelog: %w", err), nil)
	}

	if err := writeFile(ctx, changelogPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write changelog file: %w", err), nil)
	}

	logger.Info(fmt.Sprintf("📝 Appended changelog entry to %s: %s - %s", changelogSessionFile, entry.ChangeType, entry.Description))
	return nil
}

// resetChangelogSession resets the changelog session (call this at the start of a new planning agent execution)
func resetChangelogSession() {
	changelogSessionMutex.Lock()
	defer changelogSessionMutex.Unlock()
	changelogSessionFile = ""
	changelogSessionStartTime = time.Time{}
}

// readChangelog reads all changelog files from planning/changelog/ directory and combines them
// Returns all entries sorted by timestamp (oldest first)
func readChangelog(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error), listFiles func(context.Context, string) ([]string, error)) (*PlanChangeLog, error) {
	changelogDir := filepath.Join(workspacePath, "planning", "changelog")

	// List all files in changelog directory
	files, err := listFiles(ctx, changelogDir)
	if err != nil {
		// Directory doesn't exist or can't be read, return empty changelog
		return &PlanChangeLog{Entries: []PlanChangeLogEntry{}}, nil
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
		return &PlanChangeLog{Entries: []PlanChangeLogEntry{}}, nil
	}

	// Read all changelog files and combine entries
	// Each file now contains a PlanChangeLog with multiple entries (session-based)
	allEntries := make([]PlanChangeLogEntry, 0)
	for _, filename := range changelogFiles {
		filePath := filepath.Join(changelogDir, filename)
		content, err := readFile(ctx, filePath)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		// Try to unmarshal as PlanChangeLog (new format - session file with multiple entries)
		var changelog PlanChangeLog
		if err := json.Unmarshal([]byte(content), &changelog); err == nil {
			// Successfully parsed as PlanChangeLog - add all entries
			allEntries = append(allEntries, changelog.Entries...)
		} else {
			// Try old format (single entry per file) for backward compatibility
			var entry PlanChangeLogEntry
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

	return &PlanChangeLog{Entries: allEntries}, nil
}

// getUpdatePlanStepsSchema returns the JSON schema for update_plan_steps tool
func getUpdatePlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"updated_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"existing_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step in the existing plan that you want to update. Use the step's id field from the plan."
						},
						"title": {
							"type": "string",
							"description": "OPTIONAL: New title for the step. Only include if you want to rename the step. If omitted, the existing title is preserved."
						},
						"description": {
							"type": "string",
							"description": "OPTIONAL: Updated description. Only include if you want to change the description. If omitted, the existing description is preserved."
						},
						"success_criteria": {
							"type": "string",
							"description": "OPTIONAL: Updated success criteria. Only include if you want to change it. If omitted, the existing success criteria is preserved."
						},
						"context_dependencies": {
							"type": "array",
							"items": { "type": "string" },
							"description": "OPTIONAL: Updated context dependencies. Only include if you want to change them. If omitted, the existing context dependencies are preserved."
						},
						"context_output": {
							"type": "string",
							"description": "OPTIONAL: Updated context output. Only include if you want to change it. If omitted, the existing context output is preserved."
						},
						"has_loop": {
							"type": "boolean",
							"description": "OPTIONAL: Updated has_loop flag. Only include if you want to change it. If omitted, the existing has_loop value is preserved."
						},
						"loop_condition": {
							"type": "string",
							"description": "OPTIONAL: Updated loop condition. Only include if you want to change it. If omitted, the existing loop condition is preserved."
						},
						"max_iterations": {
							"type": "integer",
							"description": "OPTIONAL: Updated max iterations. Only include if you want to change it. If omitted, the existing max iterations is preserved."
						},
						"loop_description": {
							"type": "string",
							"description": "OPTIONAL: Updated loop description. Only include if you want to change it. If omitted, the existing loop description is preserved."
						},
						"enable_prerequisite_detection": {
							"type": "boolean",
							"description": "OPTIONAL: Updated enable_prerequisite_detection flag. Only include if you want to change it. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). If omitted, the existing value is preserved."
						},
						"prerequisite_rules": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"depends_on_step": {
										"type": "string",
										"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan."
									},
									"description": {
										"type": "string",
										"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1'."
									}
								},
								"required": ["depends_on_step", "description"]
							},
							"description": "OPTIONAL: Updated prerequisite rules array. Only include if you want to change it. Each rule specifies one step dependency and one description of when to detect prerequisite failures. If omitted, the existing prerequisite rules are preserved."
						},
						"has_decision_step": {
							"type": "boolean",
							"description": "OPTIONAL: Updated has_decision_step flag. Only include if you want to change it. Set to true when step executes a single step, evaluates its output, and routes based on the result. CRITICAL: When setting has_decision_step to true, you MUST also provide: decision_step, decision_evaluation_question, if_true_next_step_id, and if_false_next_step_id. If omitted, the existing value is preserved."
						},
						"decision_step": {
							"type": "object",
							"description": "OPTIONAL: Updated decision step. Only include if you want to change it. REQUIRED when has_decision_step is true: The single step to execute. Must include all required fields: id, title, description, success_criteria, has_loop, context_output. If omitted, the existing decision step is preserved.",
							"properties": {
								"id": {"type": "string", "description": "REQUIRED: Stable step ID for the decision step"},
								"title": {"type": "string", "description": "REQUIRED: Title of the decision step"},
								"description": {"type": "string", "description": "REQUIRED: Description of what the decision step does"},
								"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the decision step completed successfully"},
								"context_dependencies": {"type": "array", "items": {"type": "string"}},
								"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
								"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop"},
								"loop_condition": {"type": "string"},
								"max_iterations": {"type": "integer"},
								"loop_description": {"type": "string"}
							},
							"required": ["id", "title", "description", "success_criteria", "has_loop", "context_output"]
						},
						"decision_evaluation_question": {
							"type": "string",
							"description": "OPTIONAL: Updated decision evaluation question. Only include if you want to change it. REQUIRED when has_decision_step is true: Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?'). If omitted, the existing question is preserved."
						},
						"if_true_next_step_id": {
							"type": "string",
							"description": "OPTIONAL: Updated if_true_next_step_id. Only include if you want to change it. For conditional steps: ID of step to connect to after true branch completes (REQUIRED when if_true_steps is empty []). For decision steps: REQUIRED when has_decision_step is true - ID of step to route to after evaluation is true. Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
						},
						"if_false_next_step_id": {
							"type": "string",
							"description": "OPTIONAL: Updated if_false_next_step_id. Only include if you want to change it. For conditional steps: ID of step to connect to after false branch completes (REQUIRED when if_false_steps is empty []). For decision steps: REQUIRED when has_decision_step is true - ID of step to route to after evaluation is false. Use step's id field from the plan, or 'end' to terminate. If omitted, the existing value is preserved."
						},
						"has_condition": {
							"type": "boolean",
							"description": "OPTIONAL: Updated has_condition flag. Only include if you want to change it. Set to true when step has conditional branches. If omitted, the existing value is preserved."
						},
						"condition_question": {
							"type": "string",
							"description": "OPTIONAL: Updated condition question. Only include if you want to change it. Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?', 'Is user already logged in?'). If omitted, the existing question is preserved."
						},
						"condition_context": {
							"type": "string",
							"description": "OPTIONAL: Updated condition context. Only include if you want to change it. Context to provide to ConditionalLLM (e.g., context files, status information). Can be empty string to clear existing context. If omitted, the existing context is preserved."
						},
						"if_true_steps": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
									"title": {"type": "string"},
									"description": {"type": "string"},
									"success_criteria": {"type": "string"},
									"context_dependencies": {"type": "array", "items": {"type": "string"}},
									"context_output": {"type": "string"},
									"has_loop": {"type": "boolean"},
									"loop_condition": {"type": "string"},
									"max_iterations": {"type": "integer"},
									"loop_description": {"type": "string"},
									"has_condition": {"type": "boolean"},
									"condition_question": {"type": "string"},
									"condition_context": {"type": "string"},
									"if_true_steps": {"type": "array", "items": {"type": "object"}},
									"if_false_steps": {"type": "array", "items": {"type": "object"}}
								},
								"required": ["id", "title", "description", "success_criteria", "has_loop"]
							},
							"description": "OPTIONAL: Updated if_true_steps array. Only include if you want to change it. Array of steps to execute if condition is true. Can be empty array [] to clear all steps. If omitted, the existing if_true_steps are preserved."
						},
						"if_false_steps": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
									"title": {"type": "string"},
									"description": {"type": "string"},
									"success_criteria": {"type": "string"},
									"context_dependencies": {"type": "array", "items": {"type": "string"}},
									"context_output": {"type": "string"},
									"has_loop": {"type": "boolean"},
									"loop_condition": {"type": "string"},
									"max_iterations": {"type": "integer"},
									"loop_description": {"type": "string"},
									"has_condition": {"type": "boolean"},
									"condition_question": {"type": "string"},
									"condition_context": {"type": "string"},
									"if_true_steps": {"type": "array", "items": {"type": "object"}},
									"if_false_steps": {"type": "array", "items": {"type": "object"}}
								},
								"required": ["id", "title", "description", "success_criteria", "has_loop"]
							},
							"description": "OPTIONAL: Updated if_false_steps array. Only include if you want to change it. Array of steps to execute if condition is false. Can be empty array [] to clear all steps. If omitted, the existing if_false_steps are preserved."
						}
					},
					"required": ["existing_step_id"]
				},
				"description": "Steps to update. For each step, provide existing_step_id (required) to identify which step to update, and only include the fields you want to change."
			}
		},
		"required": ["updated_steps"]
	}`
}

// getDeletePlanStepsSchema returns the JSON schema for delete_plan_steps tool
func getDeletePlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"deleted_step_ids": {
				"type": "array",
				"items": { "type": "string" },
				"description": "IDs of steps to delete from the plan. Use the step's id field from the plan."
			}
		},
		"required": ["deleted_step_ids"]
	}`
}

// getAddRegularStepSchema returns the JSON schema for add_regular_step tool
func getAddRegularStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this new step. Generate a unique, URL-friendly ID based on the step title (e.g., 'deploy-application' from 'Deploy Application')."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the new step"
			},
			"description": {
				"type": "string",
				"description": "REQUIRED: COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context."
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive. CRITICAL: Success criteria MUST be file-verifiable. The validation agent will check file outputs to verify completion. Reference specific files (especially the context_output file) and what to look for in them (specific text, patterns, data, status indicators). Examples: 'File step_1_results.json exists and contains status: \"success\" field', 'Context output file contains database_count: 10 field and databases array with all database names'. Avoid vague statements like 'Task completed successfully' that cannot be verified through files."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder."
			},
			"has_loop": {
				"type": "boolean",
				"description": "REQUIRED: Whether this step needs to loop until condition is met. Set to true when step requires polling, retrying, or waiting for external systems. For regular steps, typically set to false."
			},
			"loop_condition": {
				"type": "string",
				"description": "OPTIONAL: Condition that must be met to exit the loop (REQUIRED when has_loop is true). This should be the same as success_criteria."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Maximum number of loop iterations allowed (default: 10). Only include when has_loop is true."
			},
			"loop_description": {
				"type": "string",
				"description": "OPTIONAL: Describe what happens in EACH ITERATION of the loop. Only include when has_loop is true."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "description", "success_criteria", "context_output", "has_loop", "insert_after_step_id"]
	}`
}

// getAddConditionalStepSchema returns the JSON schema for add_conditional_step tool
func getAddConditionalStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this conditional step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the conditional step"
			},
			"condition_question": {
				"type": "string",
				"description": "REQUIRED: Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?', 'Is user already logged in?')"
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Context to provide to ConditionalLLM (e.g., context files, status information). Can be empty string if not needed."
			},
			"if_true_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is true. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field."
			},
			"if_false_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {"type": "string", "description": "REQUIRED: Stable step ID"},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is false. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: ID of step to connect to after true branch completes, or 'end' to end the workflow. REQUIRED when if_true_steps is empty []. Optional when if_true_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the plan, or 'end' to terminate."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "OPTIONAL: ID of step to connect to after false branch completes, or 'end' to end the workflow. REQUIRED when if_false_steps is empty []. Optional when if_false_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the plan, or 'end' to terminate."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "condition_question", "if_true_steps", "if_false_steps", "insert_after_step_id"]
	}`
}

// getAddDecisionStepSchema returns the JSON schema for add_decision_step tool
func getAddDecisionStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this decision step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the decision step"
			},
			"decision_step": {
				"type": "object",
				"description": "REQUIRED: The single step to execute. Must include all required fields.",
				"properties": {
					"id": {"type": "string", "description": "REQUIRED: Stable step ID for the decision step"},
					"title": {"type": "string", "description": "REQUIRED: Title of the decision step"},
					"description": {"type": "string", "description": "REQUIRED: Description of what the decision step does"},
					"success_criteria": {"type": "string", "description": "REQUIRED: How to verify the decision step completed successfully"},
					"context_dependencies": {"type": "array", "items": {"type": "string"}},
					"context_output": {"type": "string", "description": "REQUIRED: Context file this step will create"},
					"has_loop": {"type": "boolean", "description": "REQUIRED: Whether this step needs to loop"},
					"loop_condition": {"type": "string"},
					"max_iterations": {"type": "integer"},
					"loop_description": {"type": "string"}
				},
				"required": ["id", "title", "description", "success_criteria", "has_loop", "context_output"]
			},
			"decision_evaluation_question": {
				"type": "string",
				"description": "REQUIRED: Question to evaluate the decision step's execution output (e.g., 'Is the deployment healthy and all services running?')"
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to route to after evaluation is true. Use step's id field from the plan, or 'end' to terminate workflow."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "REQUIRED: ID of step to route to after evaluation is false. Use step's id field from the plan, or 'end' to terminate workflow."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "decision_step", "decision_evaluation_question", "if_true_next_step_id", "if_false_next_step_id", "insert_after_step_id"]
	}`
}

// getAddLoopStepSchema returns the JSON schema for add_loop_step tool
func getAddLoopStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"id": {
				"type": "string",
				"description": "REQUIRED: Stable step ID for this loop step. Generate a unique, URL-friendly ID based on the step title."
			},
			"title": {
				"type": "string",
				"description": "REQUIRED: Short, clear title for the loop step"
			},
			"description": {
				"type": "string",
				"description": "REQUIRED: COMPREHENSIVE, DETAILED description of what this step accomplishes in each iteration."
			},
			"success_criteria": {
				"type": "string",
				"description": "REQUIRED: Detailed explanation of how to verify this step was completed successfully - must be file-verifiable."
			},
			"context_dependencies": {
				"type": "array",
				"items": { "type": "string" },
				"description": "OPTIONAL: List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
			},
			"context_output": {
				"type": "string",
				"description": "REQUIRED: What context file this step will create for subsequent steps - e.g., 'step_1_results.json'."
			},
			"loop_condition": {
				"type": "string",
				"description": "REQUIRED: Condition that must be met to exit the loop. This should be the same as success_criteria and must be file-verifiable."
			},
			"max_iterations": {
				"type": "integer",
				"description": "OPTIONAL: Maximum number of loop iterations allowed (default: 10). Recommended: 10 for normal operations, 20-50 for long-running operations, 3-5 for quick status checks."
			},
			"loop_description": {
				"type": "string",
				"description": "REQUIRED: Describe what happens in EACH ITERATION of the loop. Be specific about what the step does in each iteration."
			},
			"insert_after_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
			}
		},
		"required": ["id", "title", "description", "success_criteria", "context_output", "loop_condition", "loop_description", "insert_after_step_id"]
	}`
}

// getConvertStepToConditionalSchema returns the JSON schema for convert_step_to_conditional tool
func getConvertStepToConditionalSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the step to convert to conditional. Use the step's id field from the plan."
			},
			"condition_question": {
				"type": "string",
				"description": "REQUIRED: Question to ask the ConditionalLLM for decision making (e.g., 'Is the deployment healthy?')"
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Context to provide to ConditionalLLM (e.g., context files, status information). Can be empty string if not needed."
			},
			"if_true_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'verify-deployment-health' from 'Verify Deployment Health')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is true. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field. **IMPORTANT**: When if_true_steps is empty [], you MUST also provide if_true_next_step_id to specify which step comes next. Example: If checking 'Is user logged in?' and you want to skip to step 5 when true, use empty array [] and set if_true_next_step_id to 'step-5-id'."
			},
			"if_false_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'rollback-deployment' from 'Rollback Deployment')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: Array of steps to execute if condition is false. Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Each step MUST include an 'id' field. **IMPORTANT**: When if_false_steps is empty [], you MUST also provide if_false_next_step_id to specify which step comes next. Example: If checking 'Is user logged in?' and you want to execute login steps when false, provide [login_step1, login_step2]. If you want to skip when false, use empty array [] and set if_false_next_step_id to 'step-5-id'."
			},
			"if_true_next_step_id": {
				"type": "string",
				"description": "ID of step to connect to after true branch completes, or 'end' to end the workflow. REQUIRED when if_true_steps is empty []. Optional when if_true_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after true branch, set if_true_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
			},
			"if_false_next_step_id": {
				"type": "string",
				"description": "ID of step to connect to after false branch completes, or 'end' to end the workflow. REQUIRED when if_false_steps is empty []. Optional when if_false_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after false branch, set if_false_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
			}
		},
		"required": ["step_id", "condition_question", "if_true_steps", "if_false_steps"]
	}`
}

// getAddBranchStepsSchema returns the JSON schema for add_branch_steps tool
func getAddBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to add steps to - 'if_true' or 'if_false'"
			},
			"new_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this branch step. Generate a unique, URL-friendly ID based on the step title (e.g., 'verify-deployment-health' from 'Verify Deployment Health')."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"},
						"if_true_steps": {"type": "array", "items": {"type": "object"}},
						"if_false_steps": {"type": "array", "items": {"type": "object"}}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				},
				"description": "REQUIRED: New steps to add to the specified branch. Provide complete step definitions with IDs."
			}
		},
		"required": ["parent_step_id", "branch_type", "new_steps"]
	}`
}

// getUpdateBranchStepsSchema returns the JSON schema for update_branch_steps tool
func getUpdateBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to update - 'if_true' or 'if_false'"
			},
			"updated_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"existing_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step within the branch to update. Use the step's id field from the plan."
						},
						"title": {"type": "string"},
						"description": {"type": "string"},
						"success_criteria": {"type": "string"},
						"context_dependencies": {"type": "array", "items": {"type": "string"}},
						"context_output": {"type": "string"},
						"has_loop": {"type": "boolean"},
						"loop_condition": {"type": "string"},
						"max_iterations": {"type": "integer"},
						"loop_description": {"type": "string"},
						"has_condition": {"type": "boolean"},
						"condition_question": {"type": "string"},
						"condition_context": {"type": "string"}
					},
					"required": ["existing_step_id"]
				},
				"description": "REQUIRED: Steps to update within the branch. For each step, provide existing_step_id (required) and only include fields you want to change."
			}
		},
		"required": ["parent_step_id", "branch_type", "updated_steps"]
	}`
}

// getDeleteBranchStepsSchema returns the JSON schema for delete_branch_steps tool
func getDeleteBranchStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"parent_step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step (parent step). Use the step's id field from the plan."
			},
			"branch_type": {
				"type": "string",
				"enum": ["if_true", "if_false"],
				"description": "REQUIRED: Which branch to delete steps from - 'if_true' or 'if_false'"
			},
			"deleted_step_ids": {
				"type": "array",
				"items": {"type": "string"},
				"description": "REQUIRED: IDs of steps to delete from the branch. Use the step's id field from the plan."
			}
		},
		"required": ["parent_step_id", "branch_type", "deleted_step_ids"]
	}`
}

// getUpdateConditionalStepSchema returns the JSON schema for update_conditional_step tool
func getUpdateConditionalStepSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step to update. Use the step's id field from the plan."
			},
			"condition_question": {
				"type": "string",
				"description": "OPTIONAL: Updated condition question. Only include if you want to change it. If omitted, the existing question is preserved."
			},
			"condition_context": {
				"type": "string",
				"description": "OPTIONAL: Updated condition context. Only include if you want to change it. If omitted, the existing context is preserved."
			}
		},
		"required": ["step_id"]
	}`
}

// getConvertConditionalToRegularSchema returns the JSON schema for convert_conditional_to_regular tool
func getConvertConditionalToRegularSchema() string {
	return `{
		"type": "object",
		"properties": {
			"step_id": {
				"type": "string",
				"description": "REQUIRED: The ID of the conditional step to convert back to regular. Use the step's id field from the plan. This will remove all conditional properties and branch steps."
			}
		},
		"required": ["step_id"]
	}`
}

// readPlanFromFile reads plan.json from the workspace using BaseOrchestrator's ReadWorkspaceFile
func readPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*PlanningResponse, error) {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	content, err := readFile(ctx, planPath)
	if err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to read plan.json: %w", err), nil)
	}

	var plan PlanningResponse
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse plan.json: %w", err), nil)
	}

	return &plan, nil
}

// writePlanToFile writes PlanningResponse to plan.json in the workspace using BaseOrchestrator's WriteWorkspaceFile
// Validates that all steps have IDs before saving (planning agent should always generate them)
func writePlanToFile(ctx context.Context, workspacePath string, plan *PlanningResponse, _ func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, logger loggerv2.Logger) error {
	planPath := filepath.Join(workspacePath, "planning", "plan.json")

	planFileMutex.Lock()
	defer planFileMutex.Unlock()

	// Validate that all steps have IDs (planning agent should always generate them)
	if err := validatePlanStepIDs(plan.Steps); err != nil {
		return fmt.Errorf(fmt.Sprintf("plan validation failed: %w", err), nil)
	}

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to marshal plan: %w", err), nil)
	}

	if err := writeFile(ctx, planPath, string(data)); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to write plan.json: %w", err), nil)
	}

	return nil
}

// validateNestingDepth checks if the maximum nesting depth (2 levels) is exceeded
// Returns error if depth > 2, nil otherwise
func validateNestingDepth(step PlanStep, currentDepth int) error {
	const maxDepth = 2
	if currentDepth > maxDepth {
		return fmt.Errorf(fmt.Sprintf("nesting depth exceeds maximum allowed depth of %d (current: %d)", maxDepth, currentDepth), nil)
	}

	// Check nested steps in branches
	if step.HasCondition {
		for _, branchStep := range step.IfTrueSteps {
			if branchStep.HasCondition {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
		for _, branchStep := range step.IfFalseSteps {
			if branchStep.HasCondition {
				if err := validateNestingDepth(branchStep, currentDepth+1); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// mergePartialStepUpdate merges a PartialPlanStep update into an existing PlanStep
func mergePartialStepUpdate(existingStep PlanStep, partialUpdate PartialPlanStep) PlanStep {
	merged := existingStep

	// Update fields only if they are provided (not zero values)
	if partialUpdate.Title != "" {
		merged.Title = partialUpdate.Title
	}
	if partialUpdate.Description != "" {
		merged.Description = partialUpdate.Description
	}
	if partialUpdate.SuccessCriteria != "" {
		merged.SuccessCriteria = partialUpdate.SuccessCriteria
	}
	if partialUpdate.ContextDependencies != nil {
		merged.ContextDependencies = partialUpdate.ContextDependencies
	}
	if partialUpdate.ContextOutput != "" {
		merged.ContextOutput = partialUpdate.ContextOutput
	}
	if partialUpdate.HasLoop != nil {
		merged.HasLoop = *partialUpdate.HasLoop
	}
	if partialUpdate.LoopCondition != "" {
		merged.LoopCondition = partialUpdate.LoopCondition
	}
	if partialUpdate.MaxIterations != nil {
		merged.MaxIterations = *partialUpdate.MaxIterations
	}
	if partialUpdate.LoopDescription != "" {
		merged.LoopDescription = partialUpdate.LoopDescription
	}
	if partialUpdate.EnablePrerequisiteDetection != nil {
		merged.EnablePrerequisiteDetection = partialUpdate.EnablePrerequisiteDetection
	}
	if partialUpdate.PrerequisiteRules != nil {
		merged.PrerequisiteRules = partialUpdate.PrerequisiteRules
	}
	// Conditional step fields
	if partialUpdate.HasCondition != nil {
		merged.HasCondition = *partialUpdate.HasCondition
	}
	// ConditionQuestion: update if provided and non-empty
	// Note: We can't distinguish "not provided" from "empty string" in JSON unmarshaling
	// So we only update if non-empty. To clear, use update_conditional_step tool.
	if partialUpdate.ConditionQuestion != "" {
		merged.ConditionQuestion = partialUpdate.ConditionQuestion
	}
	// ConditionContext: update if provided and non-empty
	// Note: Similar to ConditionQuestion, we can't distinguish "not provided" from "empty string"
	// To clear condition_context, use update_conditional_step tool which handles empty strings explicitly
	if partialUpdate.ConditionContext != "" {
		merged.ConditionContext = partialUpdate.ConditionContext
	}
	if partialUpdate.IfTrueSteps != nil {
		merged.IfTrueSteps = partialUpdate.IfTrueSteps
	}
	if partialUpdate.IfFalseSteps != nil {
		merged.IfFalseSteps = partialUpdate.IfFalseSteps
	}
	// Decision step fields
	if partialUpdate.HasDecisionStep != nil {
		merged.HasDecisionStep = *partialUpdate.HasDecisionStep
	}
	if partialUpdate.DecisionStep != nil {
		merged.DecisionStep = partialUpdate.DecisionStep
	}
	if partialUpdate.DecisionEvaluationQuestion != "" {
		merged.DecisionEvaluationQuestion = partialUpdate.DecisionEvaluationQuestion
	}
	// Routing fields (used by both conditional and decision steps)
	if partialUpdate.IfTrueNextStepID != "" {
		merged.IfTrueNextStepID = partialUpdate.IfTrueNextStepID
	}
	if partialUpdate.IfFalseNextStepID != "" {
		merged.IfFalseNextStepID = partialUpdate.IfFalseNextStepID
	}

	return merged
}

// NewHumanControlledTodoPlannerPlanningAgent creates a new human-controlled todo planner planning agent
func NewHumanControlledTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType, // Reuse the same type for now
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// createUpdatePlanStepsExecutor creates an executor function for update_plan_steps tool
func createUpdatePlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract updated_steps from args
		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid updated_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PartialPlanStep array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal updated_steps: %w", err), nil)
		}

		var partialUpdates []PartialPlanStep
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse updated_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Create map of existing steps by ID
		existingStepsMap := make(map[string]*PlanStep)
		for i := range plan.Steps {
			existingStepsMap[plan.Steps[i].ID] = &plan.Steps[i]
		}

		// Track updated step IDs and changes for changelog
		updatedStepIDs := make([]string, 0, len(partialUpdates))
		changeDetails := make([]map[string]interface{}, 0, len(partialUpdates))
		fieldChanges := make([]PlanFieldChange, 0)

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepID]
			if !exists {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", partialUpdate.ExistingStepID, availableIDs), nil)
			}

			// Track what changed with old and new values
			updatedStepIDs = append(updatedStepIDs, partialUpdate.ExistingStepID)
			changeDetail := map[string]interface{}{
				"step_id": partialUpdate.ExistingStepID,
				"title":   existingStep.Title,
			}
			changedFields := []string{}

			// Track each field change with old and new values
			if partialUpdate.Title != "" {
				changedFields = append(changedFields, "title")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "title",
					OldValue: existingStep.Title,
					NewValue: partialUpdate.Title,
				})
			}
			if partialUpdate.Description != "" {
				changedFields = append(changedFields, "description")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "description",
					OldValue: existingStep.Description,
					NewValue: partialUpdate.Description,
				})
			}
			if partialUpdate.SuccessCriteria != "" {
				changedFields = append(changedFields, "success_criteria")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "success_criteria",
					OldValue: existingStep.SuccessCriteria,
					NewValue: partialUpdate.SuccessCriteria,
				})
			}
			if partialUpdate.ContextDependencies != nil {
				changedFields = append(changedFields, "context_dependencies")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_dependencies",
					OldValue: existingStep.ContextDependencies,
					NewValue: partialUpdate.ContextDependencies,
				})
			}
			if partialUpdate.ContextOutput != "" {
				changedFields = append(changedFields, "context_output")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_output",
					OldValue: existingStep.ContextOutput,
					NewValue: partialUpdate.ContextOutput,
				})
			}
			if partialUpdate.HasLoop != nil {
				changedFields = append(changedFields, "has_loop")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "has_loop",
					OldValue: existingStep.HasLoop,
					NewValue: *partialUpdate.HasLoop,
				})
			}
			if partialUpdate.LoopCondition != "" {
				changedFields = append(changedFields, "loop_condition")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_condition",
					OldValue: existingStep.LoopCondition,
					NewValue: partialUpdate.LoopCondition,
				})
			}
			if partialUpdate.MaxIterations != nil {
				changedFields = append(changedFields, "max_iterations")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "max_iterations",
					OldValue: existingStep.MaxIterations,
					NewValue: *partialUpdate.MaxIterations,
				})
			}
			if partialUpdate.LoopDescription != "" {
				changedFields = append(changedFields, "loop_description")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_description",
					OldValue: existingStep.LoopDescription,
					NewValue: partialUpdate.LoopDescription,
				})
			}
			if partialUpdate.EnablePrerequisiteDetection != nil {
				changedFields = append(changedFields, "enable_prerequisite_detection")
				var oldValue interface{} = nil
				if existingStep.EnablePrerequisiteDetection != nil {
					oldValue = *existingStep.EnablePrerequisiteDetection
				}
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "enable_prerequisite_detection",
					OldValue: oldValue,
					NewValue: *partialUpdate.EnablePrerequisiteDetection,
				})
			}
			if partialUpdate.PrerequisiteRules != nil {
				changedFields = append(changedFields, "prerequisite_rules")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "prerequisite_rules",
					OldValue: existingStep.PrerequisiteRules,
					NewValue: partialUpdate.PrerequisiteRules,
				})
			}
			if partialUpdate.HasDecisionStep != nil {
				changedFields = append(changedFields, "has_decision_step")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "has_decision_step",
					OldValue: existingStep.HasDecisionStep,
					NewValue: *partialUpdate.HasDecisionStep,
				})
			}
			if partialUpdate.DecisionStep != nil {
				changedFields = append(changedFields, "decision_step")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "decision_step",
					OldValue: existingStep.DecisionStep,
					NewValue: partialUpdate.DecisionStep,
				})
			}
			if partialUpdate.DecisionEvaluationQuestion != "" {
				changedFields = append(changedFields, "decision_evaluation_question")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "decision_evaluation_question",
					OldValue: existingStep.DecisionEvaluationQuestion,
					NewValue: partialUpdate.DecisionEvaluationQuestion,
				})
			}
			if partialUpdate.IfTrueNextStepID != "" {
				changedFields = append(changedFields, "if_true_next_step_id")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "if_true_next_step_id",
					OldValue: existingStep.IfTrueNextStepID,
					NewValue: partialUpdate.IfTrueNextStepID,
				})
			}
			if partialUpdate.IfFalseNextStepID != "" {
				changedFields = append(changedFields, "if_false_next_step_id")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "if_false_next_step_id",
					OldValue: existingStep.IfFalseNextStepID,
					NewValue: partialUpdate.IfFalseNextStepID,
				})
			}
			// Conditional step fields
			if partialUpdate.HasCondition != nil {
				changedFields = append(changedFields, "has_condition")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "has_condition",
					OldValue: existingStep.HasCondition,
					NewValue: *partialUpdate.HasCondition,
				})
			}
			if partialUpdate.ConditionQuestion != "" {
				changedFields = append(changedFields, "condition_question")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "condition_question",
					OldValue: existingStep.ConditionQuestion,
					NewValue: partialUpdate.ConditionQuestion,
				})
			}
			if partialUpdate.ConditionContext != "" {
				changedFields = append(changedFields, "condition_context")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "condition_context",
					OldValue: existingStep.ConditionContext,
					NewValue: partialUpdate.ConditionContext,
				})
			}
			if partialUpdate.IfTrueSteps != nil {
				changedFields = append(changedFields, "if_true_steps")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "if_true_steps",
					OldValue: existingStep.IfTrueSteps,
					NewValue: partialUpdate.IfTrueSteps,
				})
			}
			if partialUpdate.IfFalseSteps != nil {
				changedFields = append(changedFields, "if_false_steps")
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "if_false_steps",
					OldValue: existingStep.IfFalseSteps,
					NewValue: partialUpdate.IfFalseSteps,
				})
			}
			changeDetail["changed_fields"] = changedFields
			changeDetails = append(changeDetails, changeDetail)

			// Merge partial update
			*existingStep = mergePartialStepUpdate(*existingStep, partialUpdate)
		}

		// Validate all steps including decision steps after merge but before writing
		if err := validatePlanStepIDs(plan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed after update: %w", err), nil)
		}

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		detailsJSON, _ := json.Marshal(changeDetails)
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "update",
			StepIDs:     updatedStepIDs,
			Description: fmt.Sprintf("Updated %d step(s): %v", len(partialUpdates), updatedStepIDs),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated %d steps in plan", len(partialUpdates)))
		return fmt.Sprintf("Successfully updated %d step(s) in the plan", len(partialUpdates)), nil
	}
}

// createDeletePlanStepsExecutor creates an executor function for delete_plan_steps tool
func createDeletePlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract deleted_step_ids from args
		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid deleted_step_ids argument"), nil)
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf(fmt.Sprintf("invalid step ID in deleted_step_ids: %v", id), nil)
			}
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range oldPlan.Steps {
			existingStepsMap[step.ID] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(oldPlan.Steps))
				for _, step := range oldPlan.Steps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot delete). Available step IDs: %v", id, availableIDs), nil)
			}
		}

		// Capture deleted steps BEFORE filtering (for changelog revert support)
		deletedSteps := make([]PlanStep, 0, len(deletedIDs))
		for _, step := range oldPlan.Steps {
			if deletedSet[step.ID] {
				deletedSteps = append(deletedSteps, step)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(oldPlan.Steps))
		for _, step := range oldPlan.Steps {
			if !deletedSet[step.ID] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		newPlan := &PlanningResponse{Steps: filteredSteps}

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with complete deleted step data for revert support
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"deleted_step_ids": deletedIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:    time.Now().Format(time.RFC3339),
			ChangeType:   "delete",
			StepIDs:      deletedIDs,
			Description:  fmt.Sprintf("Deleted %d step(s): %v", len(deletedIDs), deletedIDs),
			Details:      string(detailsJSON),
			DeletedSteps: deletedSteps, // Store complete deleted step data for revert
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from plan", len(deletedIDs)))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan", len(deletedIDs)), nil
	}
}

// createAddRegularStepExecutor creates an executor function for add_regular_step tool
func createAddRegularStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "regular")
}

// createAddConditionalStepExecutor creates an executor function for add_conditional_step tool
func createAddConditionalStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "conditional")
}

// createAddDecisionStepExecutor creates an executor function for add_decision_step tool
func createAddDecisionStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "decision")
}

// createAddLoopStepExecutor creates an executor function for add_loop_step tool
func createAddLoopStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return createSingleStepAdder(workspacePath, logger, readFile, writeFile, moveFile, "loop")
}

// createSingleStepAdder is a shared executor that handles adding a single step to the plan
// stepType is used for logging and validation purposes
func createSingleStepAdder(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, moveFile func(context.Context, string, string) error, stepType string) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Convert args to JSON and unmarshal to PlanStep
		stepJSON, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal step: %w", err), nil)
		}

		var step PlanStep
		if err := json.Unmarshal(stepJSON, &step); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse step: %w", err), nil)
		}

		// Set step type flags based on stepType parameter
		switch stepType {
		case "conditional":
			step.HasCondition = true
		case "decision":
			step.HasDecisionStep = true
		case "loop":
			step.HasLoop = true
		case "regular":
			// Regular steps may or may not have loops, so don't force has_loop
			// The has_loop field should be set by the LLM in the args
		}

		// Validate step has ID
		if step.ID == "" {
			return "", fmt.Errorf(fmt.Sprintf("step is missing required ID field. Step title: %q", step.Title), nil)
		}

		// Read current plan
		oldPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find insertion point
		var insertAfterStepID string
		if insertID, ok := args["insert_after_step_id"].(string); ok {
			insertAfterStepID = insertID
		} else {
			return "", fmt.Errorf(fmt.Sprintf("missing required insert_after_step_id field"), nil)
		}

		// Create map of step IDs to indices
		idToIndex := make(map[string]int)
		for i, s := range oldPlan.Steps {
			idToIndex[s.ID] = i
		}

		var afterIndex int
		var found bool

		if insertAfterStepID == "" {
			// Insert at beginning
			afterIndex = -1
			found = true
		} else {
			// Find the step to insert after
			afterIndex, found = idToIndex[insertAfterStepID]
			if !found {
				availableIDs := make([]string, 0, len(oldPlan.Steps))
				for _, s := range oldPlan.Steps {
					availableIDs = append(availableIDs, s.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot insert after it). Available step IDs: %v", insertAfterStepID, availableIDs), nil)
			}
		}

		// Build new plan with insertion
		newPlanSteps := make([]PlanStep, 0, len(oldPlan.Steps)+1)

		// Insert at beginning if needed
		if afterIndex == -1 {
			newPlanSteps = append(newPlanSteps, step)
		}

		// Add existing steps and insert new step at the right position
		for i, originalStep := range oldPlan.Steps {
			newPlanSteps = append(newPlanSteps, originalStep)
			if i == afterIndex {
				// Insert new step after this one
				newPlanSteps = append(newPlanSteps, step)
			}
		}

		newPlan := &PlanningResponse{Steps: newPlanSteps}

		// Validate all steps including the new one
		if err := validatePlanStepIDs(newPlan.Steps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("plan validation failed before writing: %w", err), nil)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, newPlan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with complete step data for revert support
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"step_id":              step.ID,
			"title":                step.Title,
			"step_type":            stepType,
			"insert_after_step_id": insertAfterStepID,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:         time.Now().Format(time.RFC3339),
			ChangeType:        "add",
			StepIDs:           []string{step.ID},
			Description:       fmt.Sprintf("Added %s step: %s (ID: %s)", stepType, step.Title, step.ID),
			Details:           string(detailsJSON),
			AddedSteps:        []PlanStep{step},  // Store complete step data for revert
			InsertAfterStepID: insertAfterStepID, // Store insertion point for revert
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Added %s step '%s' (ID: %s) to plan", stepType, step.Title, step.ID))
		return fmt.Sprintf("Successfully added %s step '%s' (ID: %s) to the plan", stepType, step.Title, step.ID), nil
	}
}

// extractToolCallsFromMessages scans messages for tool calls and returns the tool names that were called
func extractToolCallsFromMessages(messages []llmtypes.MessageContent) []string {
	toolNames := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role != llmtypes.ChatMessageTypeAI {
			continue
		}
		for _, part := range msg.Parts {
			if toolCall, ok := part.(llmtypes.ToolCall); ok {
				if toolCall.FunctionCall != nil {
					toolNames[toolCall.FunctionCall.Name] = true
				}
			}
		}
	}
	result := make([]string, 0, len(toolNames))
	for name := range toolNames {
		result = append(result, name)
	}
	return result
}

// registerPlanModificationTools registers all plan modification tools (plan update tools only)
// Note: human_feedback is NOT registered here because it's already included in WorkspaceTools
// This shared function is used by both planning agent and plan improvement agent
func registerPlanModificationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	moveFile func(context.Context, string, string) error,
	agentName string, // e.g., "planning agent" or "plan improvement agent"
) error {
	// Note: human_feedback is already registered via WorkspaceTools (created by createCustomTools in server.go)
	// No need to register it again here to avoid duplicate registration errors

	// Register workflow-specific plan tools with "workflow" category
	updateSchema := getUpdatePlanStepsSchema()
	updateParams, err := parseSchemaForToolParameters(updateSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_plan_steps",
		"Update existing steps in the plan. Provide existing_step_id (required) to identify which step to update, and only include the fields you want to change. The plan.json file is updated immediately when this tool is called.",
		updateParams,
		createUpdatePlanStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_plan_steps tool: %w", err), nil)
	}

	deleteSchema := getDeletePlanStepsSchema()
	deleteParams, err := parseSchemaForToolParameters(deleteSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse delete schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_plan_steps",
		"Delete steps from the plan by providing their IDs. Use the step's id field from the plan. The plan.json file is updated immediately when this tool is called.",
		deleteParams,
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile, moveFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_plan_steps tool: %w", err), nil)
	}

	// Register type-specific step addition tools
	regularSchema := getAddRegularStepSchema()
	regularParams, err := parseSchemaForToolParameters(regularSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse regular step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_regular_step",
		"Add a regular execution step to the plan. Use this for standard steps that execute once and produce output. Provide all required fields: id, title, description, success_criteria, context_output, has_loop, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		regularParams,
		createAddRegularStepExecutor(workspacePath, logger, readFile, writeFile, moveFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_regular_step tool: %w", err), nil)
	}

	conditionalSchema := getAddConditionalStepSchema()
	conditionalParams, err := parseSchemaForToolParameters(conditionalSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse conditional step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_conditional_step",
		"Add a conditional step to the plan. Use this for if/else logic based on runtime conditions. Conditional steps evaluate a question and execute different branch steps based on the result. They do NOT execute the step itself - only evaluate the condition. Provide: id, title, condition_question, if_true_steps, if_false_steps, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		conditionalParams,
		createAddConditionalStepExecutor(workspacePath, logger, readFile, writeFile, moveFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_conditional_step tool: %w", err), nil)
	}

	decisionSchema := getAddDecisionStepSchema()
	decisionParams, err := parseSchemaForToolParameters(decisionSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse decision step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_decision_step",
		"Add a decision step to the plan. Use this when you need to execute a step first, evaluate its output, and route based on the result. Decision steps EXECUTE a single step, then evaluate the output to determine routing. Provide: id, title, decision_step (the step to execute), decision_evaluation_question, if_true_next_step_id, if_false_next_step_id, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		decisionParams,
		createAddDecisionStepExecutor(workspacePath, logger, readFile, writeFile, moveFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_decision_step tool: %w", err), nil)
	}

	loopSchema := getAddLoopStepSchema()
	loopParams, err := parseSchemaForToolParameters(loopSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse loop step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_loop_step",
		"Add a loop step to the plan. Use this for steps that need to repeat until a condition is met (polling, retrying, waiting). Provide: id, title, description, success_criteria, context_output, loop_condition, loop_description, insert_after_step_id. The plan.json file is updated immediately when this tool is called.",
		loopParams,
		createAddLoopStepExecutor(workspacePath, logger, readFile, writeFile, moveFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_loop_step tool: %w", err), nil)
	}

	// Register conditional step tools
	convertToConditionalSchema := getConvertStepToConditionalSchema()
	convertToConditionalParams, err := parseSchemaForToolParameters(convertToConditionalSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse convert_step_to_conditional schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_step_to_conditional",
		"Convert a regular step to a conditional step with if/else branches. Provide step_id, condition_question, condition_context (optional), if_true_steps, and if_false_steps. The step will become a conditional decision point that executes one branch based on the condition evaluation.",
		convertToConditionalParams,
		createConvertStepToConditionalExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register convert_step_to_conditional tool: %w", err), nil)
	}

	addBranchStepsSchema := getAddBranchStepsSchema()
	addBranchStepsParams, err := parseSchemaForToolParameters(addBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse add_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_branch_steps",
		"Add new steps to a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type ('if_true' or 'if_false'), and new_steps array. The steps will be appended to the specified branch.",
		addBranchStepsParams,
		createAddBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_branch_steps tool: %w", err), nil)
	}

	updateBranchStepsSchema := getUpdateBranchStepsSchema()
	updateBranchStepsParams, err := parseSchemaForToolParameters(updateBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_branch_steps",
		"Update existing steps within a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and updated_steps array. For each step, provide existing_step_id (required) and only include fields you want to change.",
		updateBranchStepsParams,
		createUpdateBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_branch_steps tool: %w", err), nil)
	}

	deleteBranchStepsSchema := getDeleteBranchStepsSchema()
	deleteBranchStepsParams, err := parseSchemaForToolParameters(deleteBranchStepsSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse delete_branch_steps schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"delete_branch_steps",
		"Delete steps from a specific branch (if_true or if_false) of a conditional step. Provide parent_step_id, branch_type, and deleted_step_ids array. Use the step's id field from the plan.",
		deleteBranchStepsParams,
		createDeleteBranchStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_branch_steps tool: %w", err), nil)
	}

	updateConditionalStepSchema := getUpdateConditionalStepSchema()
	updateConditionalStepParams, err := parseSchemaForToolParameters(updateConditionalStepSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_conditional_step schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_conditional_step",
		"Update the condition question or context of a conditional step without modifying its branches. Provide step_id and optionally condition_question and/or condition_context. Only provided fields will be updated.",
		updateConditionalStepParams,
		createUpdateConditionalStepExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_conditional_step tool: %w", err), nil)
	}

	convertToRegularSchema := getConvertConditionalToRegularSchema()
	convertToRegularParams, err := parseSchemaForToolParameters(convertToRegularSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse convert_conditional_to_regular schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"convert_conditional_to_regular",
		"Convert a conditional step back to a regular step. This removes all conditional properties and branch steps. Provide step_id of the conditional step to convert.",
		convertToRegularParams,
		createConvertConditionalToRegularExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register convert_conditional_to_regular tool: %w", err), nil)
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered all plan modification tools for %s", agentName))
	}

	return nil
}

// getExtractVariablesSchema returns the JSON schema for extract_variables tool
func getExtractVariablesSchema() string {
	return `{
		"type": "object",
		"properties": {
			"text": {
				"type": "string",
				"description": "Text or objective to extract variables from. Look for hard-coded values like URLs, account IDs, ports, credentials, resource names, environment values, hosts/endpoints, specific identifiers, paths, and configurations."
			}
		},
		"required": ["text"]
	}`
}

// createExtractVariablesExecutor creates an executor function for extract_variables tool
func createExtractVariablesExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract text to analyze
		textRaw, ok := args["text"].(string)
		if !ok || textRaw == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid text argument"), nil)
		}
		text := textRaw

		// Check if variables.json already exists, create if it doesn't
		manifest, err := readVariablesFromFile(ctx, workspacePath, readFile)
		if err != nil {
			// Variables file doesn't exist - create new manifest
			manifest = &VariablesManifest{
				Variables:      []Variable{},
				Groups:         []VariableGroup{},
				ExtractionDate: time.Now().Format(time.RFC3339),
				Objective:      "", // Not important anymore, but keep for backward compatibility
			}
			// Write the new manifest
			if err := writeVariablesToFile(ctx, workspacePath, manifest, readFile, writeFile, logger); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("failed to create variables.json: %w", err), nil)
			}
		}

		// Log the extraction request
		textPreview := text
		if len(text) > 100 {
			textPreview = text[:100] + "..."
		}
		logger.Info(fmt.Sprintf("📝 Extract variables tool called with text: %s", textPreview))

		// Write changelog entry for extraction initiation
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"text_preview":             textPreview,
			"existing_variables_count": len(manifest.Variables),
		})
		changelogEntry := VariableChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "extraction",
			Description: fmt.Sprintf("Variable extraction initiated from text (existing variables: %d)", len(manifest.Variables)),
			Details:     string(detailsJSON),
			Changes:     []VariableFieldChange{},
		}
		if err := writeVariableChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write variable changelog entry: %v", err))
		}

		// Return guidance for the LLM to extract variables
		// The LLM should analyze the text and use update_variable tool to add each variable
		return fmt.Sprintf("Variables file ready. Analyze the provided text and extract hard-coded values as variables. Use update_variable tool with action='add' to add each extracted variable. Look for: URLs, account IDs, ports, credentials, resource names, environment values, hosts/endpoints, specific identifiers, paths, and configurations. For each value found, create a variable with UPPER_SNAKE_CASE name, the original value, and a clear description. Current variables file has %d variables.", len(manifest.Variables)), nil
	}
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// registerVariableExtractionTools registers variable extraction and management tools
// This allows the planning agent to extract and manage variables based on human input
func registerVariableExtractionTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
	agentName string, // e.g., "planning agent"
) error {
	// Register extract_variables tool
	extractSchema := getExtractVariablesSchema()
	extractParams, err := parseSchemaForToolParameters(extractSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse extract_variables schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"extract_variables",
		"Extract variables from text or objective. Provide the text to analyze, and the tool will guide you to extract hard-coded values (URLs, account IDs, ports, credentials, resource names, etc.) as variables. After extraction, use update_variable tool to add each variable.",
		extractParams,
		createExtractVariablesExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register extract_variables tool: %w", err), nil)
	}

	// Register update_variable tool (reuse from variable_extraction_agent.go)
	updateVariableSchema := getUpdateVariableSchema()
	updateVariableParams, err := parseSchemaForToolParameters(updateVariableSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse update_variable schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"update_variable",
		"Update, add, or delete variables in variables.json. Provide action (required: 'update', 'add', or 'delete'), existing_variable_name (required for update/delete), and fields to update. The variables.json file is updated immediately when this tool is called.",
		updateVariableParams,
		createUpdateVariableExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register update_variable tool: %w", err), nil)
	}

	if logger != nil {
		logger.Info(fmt.Sprintf("✅ Registered variable extraction tools for %s", agentName))
	}

	return nil
}

// createConvertStepToConditionalExecutor creates an executor function for convert_step_to_conditional tool
func createConvertStepToConditionalExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing step_id"), nil)
		}

		conditionQuestion, ok := args["condition_question"].(string)
		if !ok || conditionQuestion == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing condition_question"), nil)
		}

		conditionContext, _ := args["condition_context"].(string) // Optional

		// Extract if_true_steps and if_false_steps
		ifTrueStepsRaw, ok := args["if_true_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid if_true_steps argument"), nil)
		}
		ifFalseStepsRaw, ok := args["if_false_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid if_false_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PlanStep arrays
		ifTrueStepsJSON, err := json.Marshal(ifTrueStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal if_true_steps: %w", err), nil)
		}
		var ifTrueSteps []PlanStep
		if err := json.Unmarshal(ifTrueStepsJSON, &ifTrueSteps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse if_true_steps: %w", err), nil)
		}

		ifFalseStepsJSON, err := json.Marshal(ifFalseStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal if_false_steps: %w", err), nil)
		}
		var ifFalseSteps []PlanStep
		if err := json.Unmarshal(ifFalseStepsJSON, &ifFalseSteps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse if_false_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the step to convert by ID
		var stepToConvert *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				stepToConvert = &plan.Steps[i]
				break
			}
		}
		if stepToConvert == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs), nil)
		}

		// Validate nesting depth for branch steps (starting from depth 1 since this step becomes conditional)
		for _, branchStep := range ifTrueSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("if_true_steps validation failed: %w", err), nil)
			}
		}
		for _, branchStep := range ifFalseSteps {
			if err := validateNestingDepth(branchStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("if_false_steps validation failed: %w", err), nil)
			}
		}

		// Capture old values BEFORE converting (for changelog)
		oldHasCondition := stepToConvert.HasCondition
		oldConditionQuestion := stepToConvert.ConditionQuestion
		oldConditionContext := stepToConvert.ConditionContext
		oldIfTrueSteps := make([]PlanStep, len(stepToConvert.IfTrueSteps))
		copy(oldIfTrueSteps, stepToConvert.IfTrueSteps)
		oldIfFalseSteps := make([]PlanStep, len(stepToConvert.IfFalseSteps))
		copy(oldIfFalseSteps, stepToConvert.IfFalseSteps)

		// Convert step to conditional
		stepToConvert.HasCondition = true
		stepToConvert.ConditionQuestion = conditionQuestion
		stepToConvert.ConditionContext = conditionContext
		stepToConvert.IfTrueSteps = ifTrueSteps
		stepToConvert.IfFalseSteps = ifFalseSteps

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		branchStepIDs := make([]string, 0)
		for _, step := range ifTrueSteps {
			branchStepIDs = append(branchStepIDs, step.ID)
		}
		for _, step := range ifFalseSteps {
			branchStepIDs = append(branchStepIDs, step.ID)
		}
		fieldChanges := make([]PlanFieldChange, 0)
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "has_condition",
			OldValue: oldHasCondition,
			NewValue: true,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_question",
			OldValue: oldConditionQuestion,
			NewValue: conditionQuestion,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_context",
			OldValue: oldConditionContext,
			NewValue: conditionContext,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_true_steps",
			OldValue: oldIfTrueSteps,
			NewValue: ifTrueSteps,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_false_steps",
			OldValue: oldIfFalseSteps,
			NewValue: ifFalseSteps,
		})
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"step_id":            stepID,
			"condition_question": conditionQuestion,
			"if_true_steps":      len(ifTrueSteps),
			"if_false_steps":     len(ifFalseSteps),
			"branch_step_ids":    branchStepIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "convert_to_conditional",
			StepIDs:     []string{stepID},
			Description: fmt.Sprintf("Converted step '%s' to conditional with %d true branch steps and %d false branch steps", stepToConvert.Title, len(ifTrueSteps), len(ifFalseSteps)),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Converted step '%s' to conditional with %d true branch steps and %d false branch steps", stepToConvert.Title, len(ifTrueSteps), len(ifFalseSteps)))
		return fmt.Sprintf("Successfully converted step '%s' to conditional", stepToConvert.Title), nil
	}
}

// createAddBranchStepsExecutor creates an executor function for add_branch_steps tool
func createAddBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		newStepsRaw, ok := args["new_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid new_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PlanStep array
		newStepsJSON, err := json.Marshal(newStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal new_steps: %w", err), nil)
		}
		var newSteps []PlanStep
		if err := json.Unmarshal(newStepsJSON, &newSteps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse new_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Validate that all new branch steps have IDs (required for config matching)
		for i, newStep := range newSteps {
			if newStep.ID == "" {
				return "", fmt.Errorf(fmt.Sprintf("branch step at index %d is missing required ID field. Step title: %q", i, newStep.Title), nil)
			}
		}

		// Validate nesting depth for new steps (starting from depth 1 since they're being added to a conditional)
		for _, newStep := range newSteps {
			if err := validateNestingDepth(newStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("new_steps validation failed: %w", err), nil)
			}
		}

		// Capture old branch steps BEFORE adding (for changelog)
		var oldBranchSteps []PlanStep
		if branchType == "if_true" {
			oldBranchSteps = make([]PlanStep, len(parentStep.IfTrueSteps))
			copy(oldBranchSteps, parentStep.IfTrueSteps)
			parentStep.IfTrueSteps = append(parentStep.IfTrueSteps, newSteps...)
		} else {
			oldBranchSteps = make([]PlanStep, len(parentStep.IfFalseSteps))
			copy(oldBranchSteps, parentStep.IfFalseSteps)
			parentStep.IfFalseSteps = append(parentStep.IfFalseSteps, newSteps...)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		newBranchStepIDs := make([]string, 0, len(newSteps))
		for _, step := range newSteps {
			newBranchStepIDs = append(newBranchStepIDs, step.ID)
		}
		fieldChanges := make([]PlanFieldChange, 0)
		fieldName := fmt.Sprintf("%s_steps", branchType) // "if_true_steps" or "if_false_steps"
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   parentStepID,
			Field:    fieldName,
			OldValue: oldBranchSteps,
			NewValue: func() []PlanStep {
				if branchType == "if_true" {
					return parentStep.IfTrueSteps
				}
				return parentStep.IfFalseSteps
			}(),
		})
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"parent_step_id": parentStepID,
			"branch_type":    branchType,
			"new_step_ids":   newBranchStepIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "add_branch_steps",
			StepIDs:     newBranchStepIDs,
			Description: fmt.Sprintf("Added %d step(s) to %s branch of conditional step '%s'", len(newSteps), branchType, parentStep.Title),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Added %d steps to %s branch of conditional step '%s'", len(newSteps), branchType, parentStep.Title))
		return fmt.Sprintf("Successfully added %d step(s) to %s branch of conditional step '%s'", len(newSteps), branchType, parentStep.Title), nil
	}
}

// createUpdateBranchStepsExecutor creates an executor function for update_branch_steps tool
func createUpdateBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		updatedStepsRaw, ok := args["updated_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid updated_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to PartialPlanStep array
		updatedStepsJSON, err := json.Marshal(updatedStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal updated_steps: %w", err), nil)
		}
		var partialUpdates []PartialPlanStep
		if err := json.Unmarshal(updatedStepsJSON, &partialUpdates); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse updated_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStep
		if branchType == "if_true" {
			branchSteps = &parentStep.IfTrueSteps
		} else {
			branchSteps = &parentStep.IfFalseSteps
		}

		// Create map of existing branch steps by ID
		existingStepsMap := make(map[string]*PlanStep)
		for i := range *branchSteps {
			existingStepsMap[(*branchSteps)[i].ID] = &(*branchSteps)[i]
		}

		// Track changes for changelog
		updatedBranchStepIDs := make([]string, 0, len(partialUpdates))
		fieldChanges := make([]PlanFieldChange, 0)

		// Apply updates
		for _, partialUpdate := range partialUpdates {
			existingStep, exists := existingStepsMap[partialUpdate.ExistingStepID]
			if !exists {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in %s branch. Available step IDs: %v", partialUpdate.ExistingStepID, branchType, availableIDs), nil)
			}

			// Track old values before updating
			oldStep := *existingStep
			updatedBranchStepIDs = append(updatedBranchStepIDs, partialUpdate.ExistingStepID)

			// Track each field change with old and new values (same logic as update_plan_steps)
			if partialUpdate.Title != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "title",
					OldValue: oldStep.Title,
					NewValue: partialUpdate.Title,
				})
			}
			if partialUpdate.Description != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "description",
					OldValue: oldStep.Description,
					NewValue: partialUpdate.Description,
				})
			}
			if partialUpdate.SuccessCriteria != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "success_criteria",
					OldValue: oldStep.SuccessCriteria,
					NewValue: partialUpdate.SuccessCriteria,
				})
			}
			if partialUpdate.ContextDependencies != nil {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_dependencies",
					OldValue: oldStep.ContextDependencies,
					NewValue: partialUpdate.ContextDependencies,
				})
			}
			if partialUpdate.ContextOutput != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "context_output",
					OldValue: oldStep.ContextOutput,
					NewValue: partialUpdate.ContextOutput,
				})
			}
			if partialUpdate.HasLoop != nil {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "has_loop",
					OldValue: oldStep.HasLoop,
					NewValue: *partialUpdate.HasLoop,
				})
			}
			if partialUpdate.LoopCondition != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_condition",
					OldValue: oldStep.LoopCondition,
					NewValue: partialUpdate.LoopCondition,
				})
			}
			if partialUpdate.MaxIterations != nil {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "max_iterations",
					OldValue: oldStep.MaxIterations,
					NewValue: *partialUpdate.MaxIterations,
				})
			}
			if partialUpdate.LoopDescription != "" {
				fieldChanges = append(fieldChanges, PlanFieldChange{
					StepID:   partialUpdate.ExistingStepID,
					Field:    "loop_description",
					OldValue: oldStep.LoopDescription,
					NewValue: partialUpdate.LoopDescription,
				})
			}

			// Merge partial update
			*existingStep = mergePartialStepUpdate(*existingStep, partialUpdate)

			// Validate nesting depth after update
			if err := validateNestingDepth(*existingStep, 1); err != nil {
				return "", fmt.Errorf(fmt.Sprintf("updated step validation failed: %w", err), nil)
			}
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"parent_step_id":   parentStepID,
			"branch_type":      branchType,
			"updated_step_ids": updatedBranchStepIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "update_branch_steps",
			StepIDs:     updatedBranchStepIDs,
			Description: fmt.Sprintf("Updated %d step(s) in %s branch of conditional step '%s'", len(partialUpdates), branchType, parentStep.Title),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated %d steps in %s branch of conditional step '%s'", len(partialUpdates), branchType, parentStep.Title))
		return fmt.Sprintf("Successfully updated %d step(s) in %s branch of conditional step '%s'", len(partialUpdates), branchType, parentStep.Title), nil
	}
}

// createDeleteBranchStepsExecutor creates an executor function for delete_branch_steps tool
func createDeleteBranchStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		parentStepID, ok := args["parent_step_id"].(string)
		if !ok || parentStepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing parent_step_id"), nil)
		}

		branchType, ok := args["branch_type"].(string)
		if !ok || (branchType != "if_true" && branchType != "if_false") {
			return "", fmt.Errorf(fmt.Sprintf("invalid branch_type: must be 'if_true' or 'if_false'"), nil)
		}

		deletedIDsRaw, ok := args["deleted_step_ids"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid deleted_step_ids argument"), nil)
		}

		// Convert to string array
		deletedIDs := make([]string, 0, len(deletedIDsRaw))
		for _, id := range deletedIDsRaw {
			if idStr, ok := id.(string); ok {
				deletedIDs = append(deletedIDs, idStr)
			} else {
				return "", fmt.Errorf(fmt.Sprintf("invalid step ID in deleted_step_ids: %v", id), nil)
			}
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the parent conditional step by ID
		var parentStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == parentStepID {
				parentStep = &plan.Steps[i]
				break
			}
		}
		if parentStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("parent step ID '%s' not found in existing plan. Available step IDs: %v", parentStepID, availableIDs), nil)
		}

		if !parentStep.HasCondition {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", parentStepID), nil)
		}

		// Get the appropriate branch
		var branchSteps *[]PlanStep
		if branchType == "if_true" {
			branchSteps = &parentStep.IfTrueSteps
		} else {
			branchSteps = &parentStep.IfFalseSteps
		}

		// Capture old branch steps BEFORE deleting (for changelog)
		oldBranchSteps := make([]PlanStep, len(*branchSteps))
		copy(oldBranchSteps, *branchSteps)

		// Create set of deleted step IDs
		deletedSet := make(map[string]bool)
		for _, id := range deletedIDs {
			deletedSet[id] = true
		}

		// Validate that all deleted steps exist
		existingStepsMap := make(map[string]bool)
		for _, step := range *branchSteps {
			existingStepsMap[step.ID] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				availableIDs := make([]string, 0, len(*branchSteps))
				for _, step := range *branchSteps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in %s branch (cannot delete). Available step IDs: %v", id, branchType, availableIDs), nil)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(*branchSteps))
		for _, step := range *branchSteps {
			if !deletedSet[step.ID] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		*branchSteps = filteredSteps

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		fieldChanges := make([]PlanFieldChange, 0)
		fieldName := fmt.Sprintf("%s_steps", branchType) // "if_true_steps" or "if_false_steps"
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   parentStepID,
			Field:    fieldName,
			OldValue: oldBranchSteps,
			NewValue: filteredSteps,
		})
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"parent_step_id":   parentStepID,
			"branch_type":      branchType,
			"deleted_step_ids": deletedIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "delete_branch_steps",
			StepIDs:     deletedIDs,
			Description: fmt.Sprintf("Deleted %d step(s) from %s branch of conditional step '%s'", len(deletedIDs), branchType, parentStep.Title),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from %s branch of conditional step '%s'", len(deletedIDs), branchType, parentStep.Title))
		return fmt.Sprintf("Successfully deleted %d step(s) from %s branch of conditional step '%s'", len(deletedIDs), branchType, parentStep.Title), nil
	}
}

// createUpdateConditionalStepExecutor creates an executor function for update_conditional_step tool
func createUpdateConditionalStepExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing step_id"), nil)
		}

		// Check if keys exist in args to distinguish "not provided" from "empty string"
		_, conditionQuestionProvided := args["condition_question"]
		_, conditionContextProvided := args["condition_context"]

		conditionQuestion, _ := args["condition_question"].(string)
		conditionContext, _ := args["condition_context"].(string)

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the conditional step by ID
		var conditionalStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				conditionalStep = &plan.Steps[i]
				break
			}
		}
		if conditionalStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs), nil)
		}

		if !conditionalStep.HasCondition {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", stepID), nil)
		}

		// Capture old values BEFORE updating (for changelog)
		oldConditionQuestion := conditionalStep.ConditionQuestion
		oldConditionContext := conditionalStep.ConditionContext

		// Update conditional properties (only if provided)
		// For condition_question: only update if provided and non-empty
		if conditionQuestionProvided && conditionQuestion != "" {
			conditionalStep.ConditionQuestion = conditionQuestion
		}
		// For condition_context: update if provided (even if empty string, to allow clearing)
		if conditionContextProvided {
			conditionalStep.ConditionContext = conditionContext
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		changedFields := []string{}
		fieldChanges := make([]PlanFieldChange, 0)

		// Track condition_question if provided and non-empty
		if conditionQuestionProvided && conditionQuestion != "" {
			changedFields = append(changedFields, "condition_question")
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   stepID,
				Field:    "condition_question",
				OldValue: oldConditionQuestion,
				NewValue: conditionQuestion,
			})
		}
		// Track condition_context if provided (even if empty, to show clearing)
		if conditionContextProvided {
			changedFields = append(changedFields, "condition_context")
			fieldChanges = append(fieldChanges, PlanFieldChange{
				StepID:   stepID,
				Field:    "condition_context",
				OldValue: oldConditionContext,
				NewValue: conditionContext,
			})
		}

		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"step_id":        stepID,
			"changed_fields": changedFields,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "update_conditional_step",
			StepIDs:     []string{stepID},
			Description: fmt.Sprintf("Updated conditional step '%s'", conditionalStep.Title),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated conditional step '%s'", conditionalStep.Title))
		return fmt.Sprintf("Successfully updated conditional step '%s'", conditionalStep.Title), nil
	}
}

// createConvertConditionalToRegularExecutor creates an executor function for convert_conditional_to_regular tool
func createConvertConditionalToRegularExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		stepID, ok := args["step_id"].(string)
		if !ok || stepID == "" {
			return "", fmt.Errorf(fmt.Sprintf("invalid or missing step_id"), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Find the conditional step by ID
		var conditionalStep *PlanStep
		for i := range plan.Steps {
			if plan.Steps[i].ID == stepID {
				conditionalStep = &plan.Steps[i]
				break
			}
		}
		if conditionalStep == nil {
			availableIDs := make([]string, 0, len(plan.Steps))
			for _, step := range plan.Steps {
				availableIDs = append(availableIDs, step.ID)
			}
			return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan. Available step IDs: %v", stepID, availableIDs), nil)
		}

		if !conditionalStep.HasCondition {
			return "", fmt.Errorf(fmt.Sprintf("step with ID '%s' is not a conditional step", stepID), nil)
		}

		// Capture old values BEFORE converting (for changelog)
		oldHasCondition := conditionalStep.HasCondition
		oldConditionQuestion := conditionalStep.ConditionQuestion
		oldConditionContext := conditionalStep.ConditionContext
		oldIfTrueSteps := make([]PlanStep, len(conditionalStep.IfTrueSteps))
		copy(oldIfTrueSteps, conditionalStep.IfTrueSteps)
		oldIfFalseSteps := make([]PlanStep, len(conditionalStep.IfFalseSteps))
		copy(oldIfFalseSteps, conditionalStep.IfFalseSteps)

		// Convert back to regular step (remove conditional properties and branches)
		conditionalStep.HasCondition = false
		conditionalStep.ConditionQuestion = ""
		conditionalStep.ConditionContext = ""
		conditionalStep.IfTrueSteps = nil
		conditionalStep.IfFalseSteps = nil
		conditionalStep.ConditionResult = nil
		conditionalStep.ConditionReason = ""

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry with old/new values
		fieldChanges := make([]PlanFieldChange, 0)
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "has_condition",
			OldValue: oldHasCondition,
			NewValue: false,
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_question",
			OldValue: oldConditionQuestion,
			NewValue: "",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "condition_context",
			OldValue: oldConditionContext,
			NewValue: "",
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_true_steps",
			OldValue: oldIfTrueSteps,
			NewValue: []PlanStep{},
		})
		fieldChanges = append(fieldChanges, PlanFieldChange{
			StepID:   stepID,
			Field:    "if_false_steps",
			OldValue: oldIfFalseSteps,
			NewValue: []PlanStep{},
		})
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"step_id": stepID,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "convert_to_regular",
			StepIDs:     []string{stepID},
			Description: fmt.Sprintf("Converted conditional step '%s' back to regular step", conditionalStep.Title),
			Details:     string(detailsJSON),
			Changes:     fieldChanges,
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Converted conditional step '%s' back to regular step", conditionalStep.Title))
		return fmt.Sprintf("Successfully converted conditional step '%s' back to regular step", conditionalStep.Title), nil
	}
}

// buildStepIDToNumberMapping creates a mapping from step ID to step number (1-based position)
// Only maps top-level steps (regular, conditional, decision, loop) - NOT branch steps
// Branch steps use their parent's step number (handled separately in calculateBranchFolderRenames)
func buildStepIDToNumberMapping(steps []PlanStep) map[string]int {
	mapping := make(map[string]int)
	stepNumber := 1

	var processSteps func([]PlanStep, bool)
	processSteps = func(stepsToProcess []PlanStep, isBranchContext bool) {
		for _, step := range stepsToProcess {
			// Only assign step numbers to top-level steps (not branch steps)
			if !isBranchContext {
				// Map top-level step (regular, conditional, decision, or loop)
				mapping[step.ID] = stepNumber
				stepNumber++
			}
			// Note: Branch steps don't get their own step numbers - they use parent's number

			// Process branch steps if this is a conditional step
			if step.HasCondition {
				// Process if_true_steps (mark as branch context)
				for _, branchStep := range step.IfTrueSteps {
					processSteps([]PlanStep{branchStep}, true)
				}
				// Process if_false_steps (mark as branch context)
				for _, branchStep := range step.IfFalseSteps {
					processSteps([]PlanStep{branchStep}, true)
				}
			}

			// Process decision step if present
			if step.HasDecisionStep && step.DecisionStep != nil {
				// Decision step inner step doesn't get its own number, it's part of the parent
				// But we still need to track it if it has an ID
				if step.DecisionStep.ID != "" {
					// Use parent step number for decision step (it's not a separate numbered step)
					mapping[step.DecisionStep.ID] = mapping[step.ID]
				}
			}
		}
	}

	processSteps(steps, false) // Start with top-level context (not branch)
	return mapping
}

// calculateLearningsFolderRenames determines which learnings folders need to be renamed
// after steps are added or deleted. Returns a list of rename operations.
func calculateLearningsFolderRenames(oldPlan *PlanningResponse, newPlan *PlanningResponse) []LearningsFolderRename {
	renames := []LearningsFolderRename{}

	// Build mappings for old and new plans
	oldMapping := buildStepIDToNumberMapping(oldPlan.Steps)
	newMapping := buildStepIDToNumberMapping(newPlan.Steps)

	// Find steps that changed position (same ID, different number)
	for stepID, newNumber := range newMapping {
		if oldNumber, exists := oldMapping[stepID]; exists {
			if oldNumber != newNumber {
				// Step moved - need to rename folder
				oldPath := fmt.Sprintf("learnings/step-%d", oldNumber)
				newPath := fmt.Sprintf("learnings/step-%d", newNumber)
				renames = append(renames, LearningsFolderRename{
					OldPath: oldPath,
					NewPath: newPath,
				})
			}
		}
	}

	// Also check for branch step folders that need renaming
	// Branch steps use format: learnings/step-{parentStep}-{true/false}-{branchIdx}
	// If parent step number changed, branch folders need to be renamed too
	renames = append(renames, calculateBranchFolderRenames(oldPlan, newPlan, oldMapping, newMapping)...)

	return renames
}

// calculateBranchFolderRenames calculates renames needed for branch step folders
func calculateBranchFolderRenames(oldPlan *PlanningResponse, newPlan *PlanningResponse, oldMapping map[string]int, newMapping map[string]int) []LearningsFolderRename {
	renames := []LearningsFolderRename{}

	// Helper to extract branch folders from a plan
	extractBranchFolders := func(plan *PlanningResponse, idToNumber map[string]int) map[string]string {
		// Map: step ID -> folder path
		folders := make(map[string]string)

		var processBranchSteps func([]PlanStep, string, string)
		processBranchSteps = func(steps []PlanStep, parentStepID string, branchType string) {
			for i, step := range steps {
				if parentStepID != "" {
					// This is a branch step
					parentNumber := idToNumber[parentStepID]
					folderPath := fmt.Sprintf("learnings/step-%d-%s-%d", parentNumber, branchType, i)
					folders[step.ID] = folderPath
				}
				// Recurse into nested branches
				if step.HasCondition {
					processBranchSteps(step.IfTrueSteps, step.ID, "true")
					processBranchSteps(step.IfFalseSteps, step.ID, "false")
				}
			}
		}

		// Process top-level steps
		for _, step := range plan.Steps {
			if step.HasCondition {
				processBranchSteps(step.IfTrueSteps, step.ID, "true")
				processBranchSteps(step.IfFalseSteps, step.ID, "false")
			}
		}

		return folders
	}

	oldFolders := extractBranchFolders(oldPlan, oldMapping)
	newFolders := extractBranchFolders(newPlan, newMapping)

	// Find branch folders that changed
	for stepID, newPath := range newFolders {
		if oldPath, exists := oldFolders[stepID]; exists {
			if oldPath != newPath {
				renames = append(renames, LearningsFolderRename{
					OldPath: oldPath,
					NewPath: newPath,
				})
			}
		}
	}

	return renames
}

// syncLearningsFolders renames learnings folders to match new step numbers
// Uses move_workspace_file tool to rename folders efficiently
// Returns a formatted string describing what was moved, or empty string if nothing was moved
func syncLearningsFolders(ctx context.Context, workspacePath string, renames []LearningsFolderRename, logger loggerv2.Logger, moveFile func(context.Context, string, string) error) (string, error) {
	if len(renames) == 0 {
		return "", nil // Nothing to do
	}

	logger.Info(fmt.Sprintf("🔄 Syncing %d learnings folder(s) after plan changes", len(renames)))

	var movedFolders []string
	var skippedFolders []string
	var failedFolders []string

	for _, rename := range renames {
		oldFullPath := filepath.Join(workspacePath, rename.OldPath)
		newFullPath := filepath.Join(workspacePath, rename.NewPath)

		// Use move_workspace_file to rename folder (workspace API handles both files and directories)
		if err := moveFile(ctx, oldFullPath, newFullPath); err != nil {
			errStr := err.Error()
			// Check if error is because folder doesn't exist (that's okay, skip it)
			if strings.Contains(errStr, "not found") || strings.Contains(errStr, "does not exist") {
				logger.Info(fmt.Sprintf("ℹ️ Learnings folder %s does not exist (skipping)", rename.OldPath))
				skippedFolders = append(skippedFolders, rename.OldPath)
				continue
			}
			// Check if error is because destination already exists
			if strings.Contains(errStr, "already exists") || strings.Contains(errStr, "Destination") {
				logger.Warn(fmt.Sprintf("⚠️ Destination folder %s already exists, cannot move %s -> %s", rename.NewPath, rename.OldPath, rename.NewPath))
				failedFolders = append(failedFolders, fmt.Sprintf("%s -> %s (destination already exists)", rename.OldPath, rename.NewPath))
				continue
			}
			logger.Warn(fmt.Sprintf("⚠️ Failed to rename %s -> %s: %v", rename.OldPath, rename.NewPath, err))
			failedFolders = append(failedFolders, fmt.Sprintf("%s -> %s (%s)", rename.OldPath, rename.NewPath, errStr))
			continue
		}

		logger.Info(fmt.Sprintf("✅ Renamed learnings folder: %s -> %s", rename.OldPath, rename.NewPath))
		movedFolders = append(movedFolders, fmt.Sprintf("%s -> %s", rename.OldPath, rename.NewPath))
	}

	logger.Info(fmt.Sprintf("✅ Completed learnings folder sync"))

	// Build response message
	var responseParts []string
	if len(movedFolders) > 0 {
		responseParts = append(responseParts, fmt.Sprintf("**Moved %d learnings folder(s):**", len(movedFolders)))
		for _, moved := range movedFolders {
			responseParts = append(responseParts, fmt.Sprintf("- %s", moved))
		}
	}
	if len(skippedFolders) > 0 {
		responseParts = append(responseParts, fmt.Sprintf("\n**Skipped %d folder(s) (did not exist):**", len(skippedFolders)))
		for _, skipped := range skippedFolders {
			responseParts = append(responseParts, fmt.Sprintf("- %s", skipped))
		}
	}
	if len(failedFolders) > 0 {
		responseParts = append(responseParts, fmt.Sprintf("\n**Failed to move %d folder(s):**", len(failedFolders)))
		for _, failed := range failedFolders {
			responseParts = append(responseParts, fmt.Sprintf("- %s", failed))
		}
	}

	if len(responseParts) > 0 {
		return strings.Join(responseParts, "\n"), nil
	}
	return "", nil
}

// parseSchemaForToolParameters parses a JSON schema string and extracts properties for tool parameters
// This is a local copy of the function from mcpagent to avoid circular dependencies
func parseSchemaForToolParameters(schemaString string) (map[string]interface{}, error) {
	var schema map[string]interface{}
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to parse schema JSON: %w", err), nil)
	}

	// Extract properties - this becomes the tool parameters
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf(fmt.Sprintf("schema missing 'properties' field or it's not an object"), nil)
	}

	// Build tool parameter schema with type "object"
	toolParams := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}

	// Add required fields if present
	if required, ok := schema["required"].([]interface{}); ok {
		toolParams["required"] = required
	}

	return toolParams, nil
}

// Execute implements the OrchestratorAgent interface
// NOTE: This method is not used - planning agent execution is handled in runPlanningPhase() in planning_management.go
// which uses BaseAgent().Execute() directly after registering tools
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	return "", conversationHistory, fmt.Errorf("Execute() is not used for planning agent - use runPlanningPhase() instead")
}

// planningSystemPromptProcessorForCreate generates system prompt for creating a new plan
func planningSystemPromptProcessorForCreate(templateVars map[string]string) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Calculate execution workspace path (execution folder only)
	executionWorkspacePath := fmt.Sprintf("%s/execution", templateVars["WorkspacePath"])

	templateData := map[string]string{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": executionWorkspacePath,
		"VariableNames":          templateVars["VariableNames"],
		"CurrentDate":            currentDate,
		"CurrentTime":            currentTime,
	}

	templateStr := `## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Create Mode)
- **Responsibility**: Generate a comprehensive plan by using plan modification tools to build the plan step by step
- **Output Method**: Use type-specific plan modification tools (add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to build the plan incrementally
- **CRITICAL**: Start with an empty plan and use type-specific tools to add steps one by one

## ⚡ QUICK REFERENCE

**Top 3 Rules**:
1. **Output**: Use type-specific tools (add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to build the plan incrementally - start with first step, then continue adding steps
2. **Success Criteria**: Reference file names only (e.g., 'step_1_results.json'), NOT paths
3. **Variables**: Preserve placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) - never replace with actual values

**Key Concepts**:
- **Loops**: Use when polling/retrying (has_loop=true, max_iterations: 10-50)
- **Conditionals**: Use for if/else logic (has_condition=true, max nesting: 2 levels)
- **Prerequisites**: Enable for steps using expiring resources (sessions, tokens, configs)

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

Available variables:
{{.VariableNames}}

**CRITICAL RULES**:
- **PRESERVE variable placeholders** ({{"{{"}}VARIABLE_NAME{{"}}"}}) in all plan steps - never replace with actual values
- **Use existing variables only** - don't create new variable placeholders
- **Why?** Plans must work across dev/staging/prod environments without modification

{{end}}

## 📋 PLANNING GUIDELINES

### Core Principles
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable with detailed descriptions
- **Clear Success Criteria**: Define how to verify each step worked - be specific and detailed
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)

### Loop Steps (has_loop=true)

**When to use**:
- Polling/waiting for services or resources to become ready
- Retrying operations until they succeed
- Iterating until data appears or condition changes
- Checking status repeatedly until a goal is achieved

**Configuration**:
- Set has_loop = true
- Set loop_condition (same as success_criteria - must be file-verifiable)
- Set max_iterations:
  - Default: 10
  - Long-running operations: 20-50
  - Quick status checks: 3-5
- Set loop_description: Describe what happens in EACH iteration

**Progress Tracking**:
- Each iteration must save progress to context_output file
- Use append/update (don't overwrite) so progress accumulates
- Progress indicators must be file-verifiable

### Conditional Branching (has_condition=true)

**When to use**: When you need if/else logic based on runtime conditions

**CRITICAL Understanding**:
- Conditional steps do NOT execute the step itself
- They ONLY evaluate the condition, then execute branch steps
- The conditional agent evaluates the question and returns true/false
- Orchestrator executes either if_true_steps or if_false_steps

**Configuration**:
- Set has_condition = true
- Set condition_question: Question for ConditionalLLM to evaluate
- Set condition_context: Optional context to provide (can be empty string)
- Set if_true_steps: Steps if condition is true (can be empty array [] to skip)
- Set if_false_steps: Steps if condition is false (can be empty array [] to skip)

**Branch Connection Rules**:
- **When branch is empty ([])**:
  - MUST set corresponding next_step_id (if_true_next_step_id or if_false_next_step_id)
  - Use step ID from main plan, or 'end' to terminate workflow
- **When branch has steps**:
  - next_step_id is optional (defaults to next step in main plan if not specified)

**Nesting Limits**:
- Maximum nesting depth: 2 levels
- Conditional step can contain conditional steps in branches, but no deeper

**Example**:
- Question: "Is user already logged in?"
- If true: Skip to step 5 (if_true_steps = [], if_true_next_step_id = "step-5-id")
- If false: Execute login steps (if_false_steps = [login_step1, login_step2])

### Decision Steps (has_decision_step=true)

**When to use**: When you need to execute a step first, evaluate its output, and route based on the result.

**CRITICAL Understanding**:
- Decision steps EXECUTE a single step first (unlike conditional steps which only evaluate)
- Then evaluate the execution output to determine true/false
- Route to different next steps based on the evaluation result
- NO branch arrays (if_true_steps/if_false_steps) - only direct routing via next_step_id

**Configuration**:
- Set has_decision_step = true
- Set decision_step: The single step to execute (with all required fields: id, title, description, success_criteria)
- Set decision_evaluation_question: Question to evaluate the execution output
- Set if_true_next_step_id: REQUIRED - Next step ID if evaluation is true (or "end" to terminate)
- Set if_false_next_step_id: REQUIRED - Next step ID if evaluation is false (or "end" to terminate)

**Key Differences from Conditional Steps**:
| Conditional (has_condition) | Decision (has_decision_step) |
|----------------------------|------------------------------|
| Evaluates question only    | Executes step, then evaluates output |
| Has if_true_steps/if_false_steps arrays | No branch arrays |
| next_step_id optional | next_step_id REQUIRED for both branches |

**Example**:
- Title: "Check Deployment Status"
- decision_step: {id: "query-deployment", title: "Query Deployment API", ...}
- decision_evaluation_question: "Is the deployment healthy and all services running?"
- if_true_next_step_id: "proceed-to-next-phase"
- if_false_next_step_id: "rollback-deployment"

### Prerequisite Failure Detection

**When to use**:
1. Steps that use login sessions/tokens that might expire
2. Steps that depend on config files that might be deleted
3. Steps that use API credentials that might become invalid
4. Steps that depend on external resources that might become unavailable

**Configuration**:
- Set enable_prerequisite_detection = true
- Provide prerequisite_rules array
- Each rule contains:
  - depends_on_step: Step ID to navigate back to
  - description: Natural language description of when to detect failures
    - Example: "If login session is missing or expired, go back to step 0"

**How it works**:
- When validation fails, system checks if failure matches a prerequisite rule's description
- If match found, navigates back to prerequisite step instead of retrying current step

**Example**:
- Step 2 uses login session from step 0
- Configuration:
  - enable_prerequisite_detection: true
  - prerequisite_rules: [{"depends_on_step": "login-step", "description": "If login session is missing or expired, go back to login step"}]

**Note**: Prerequisite rules can be added later via UI if not included in initial plan.

## ✅ SUCCESS CRITERIA REQUIREMENTS (CRITICAL)

**Purpose**: Success criteria are used by the validation agent to verify step completion by checking file outputs.

**CRITICAL Rule**: Success criteria MUST reference file names only, NOT paths.

**Why?** Execution paths vary (workspace location, run iterations, etc.). Validation searches for files by name, not by exact path.

### How Validation Works
- Validation agent searches for files by name within the execution workspace
- Uses workspace tools (read_workspace_file, list_workspace_files) to locate files
- Verifies file existence and checks content for specific patterns/indicators

### Success Criteria Format

**✅ GOOD Examples** (file names only):
- "File 'step_1_results.json' exists and contains 'status: \"success\"' field"
- "File 'config.json' exists and contains 'status: \"active\"' field"
- "Context output file 'step_2_output.json' contains 'database_count: 10' field and 'databases' array with all database names"
- "File 'deployment_status.json' exists and contains 'pods_running: true' field"
- "File 'calculation_result.json' exists and contains the correct 'result' field value"

**❌ BAD Examples** (vague or path-based):
- "Task completed successfully" (too vague, no file reference)
- "Deployment is working" (not verifiable through files)
- "All requirements met" (no specific file or indicator)
- "File exists in execution/step-2-if_true-0/" (path may vary - use file name only)
- "File in execution/step-1/ folder" (don't specify folder paths)

### Requirements for All Steps

**File Reference**:
- Must reference context_output file name (not path) or other file names
- Use file name only (e.g., 'step_1_results.json', 'config.json')
- Do NOT include folder paths or exact locations

**Content Verification**:
- Must specify what to look for in files
- Include specific text, patterns, data, or status indicators
- Be specific enough for validation agent to definitively check

### Special Cases

**Loop Steps**:
- Loop condition (same as success_criteria) must also be file-verifiable
- Each iteration should update context output file with progress indicators
- Loop condition should reference specific file content (by name, not path)

## 🤖 MULTI-AGENT COORDINATION

**Context**: Each step is executed by a different agent. Steps share context via files.

### File Location Context (For Planning Only)

**Note**: This information is for your understanding of how execution works. Do NOT include these paths in success criteria.

- Execution agents work in step-specific folders within {{.ExecutionWorkspacePath}}
- Files are created in execution/step-X/ format where X is the step number (1-based)
- Examples: execution/step-1/, execution/step-2/
- For branch steps: execution/step-PARENT-BRANCHTYPE-BRANCHIDX/

### 🔗 Context Flow (CRITICAL - How Steps Share Data)

**Understanding Context Flow**:
Steps communicate through context files. Each step can:
- **Read** from previous steps via context_dependencies (input files)
- **Write** results via context_output (output file for next steps)

**How It Works**:
1. Each step executes in its own folder (execution/step-X/)
2. Step reads files listed in context_dependencies (from previous steps)
3. Step executes its task
4. Step writes results to file specified in context_output
5. Next step uses that context_output file in its context_dependencies

#### Context Output (REQUIRED for all steps)

**Purpose**: The file name this step creates with its execution results.

**Rules**:
- **REQUIRED**: Every step MUST have a context_output file name
- **File name only**: Use just the filename (e.g., 'step_1_results.json'), NOT paths
- **Naming conventions**:
  - Descriptive names: 'deployment_status.json', 'database_list.json', 'api_credentials.json'
  - Step-based names: 'step_1_results.json', 'step_2_output.json'
  - Use lowercase with underscores or hyphens
- **Must be referenced in success_criteria**: The validation agent uses this file to verify completion
- **Execution agent writes it**: The execution agent automatically writes to the correct step folder

**Examples**:
- ✅ 'step_1_results.json' - Clear, descriptive
- ✅ 'login_session.json' - Describes the content
- ✅ 'deployment_status.json' - Indicates what it contains
- ❌ 'execution/step-1/step_1_results.json' - Don't include paths
- ❌ 'results.json' - Too generic, might conflict

#### Context Dependencies (OPTIONAL - Input files from previous steps)

**Purpose**: List of file names from previous steps that this step needs as input.

**Rules**:
- **OPTIONAL**: Use empty array [] if step doesn't need previous context
- **File names only**: Reference the context_output values from earlier steps
- **Order matters**: List files in order of dependency (if order matters)
- **Execution agent locates automatically**: No need to specify paths

**How to Determine Dependencies**:
1. **What data does this step need?**
   - Credentials from login step? → Add login step's context_output
   - Configuration from setup step? → Add setup step's context_output
   - Results from previous calculation? → Add calculation step's context_output
2. **Does step need multiple files?** → List all in the array
3. **Is step independent?** → Use empty array []

**Examples**:
- ✅ ["step_1_results.json"] - Depends on step 1's output
- ✅ ["login_session.json", "config.json"] - Depends on two previous outputs
- ✅ [] - No dependencies (first step or independent step)

#### Complete Context Flow Example

**Step 1: Login**
- context_dependencies: [] (no previous steps)
- context_output: 'login_session.json'
- **What happens**: Step 1 executes, creates 'login_session.json' with session token

**Step 2: Fetch Data**
- context_dependencies: ["login_session.json"] (needs session from step 1)
- context_output: 'api_data.json'
- **What happens**: Step 2 reads 'login_session.json', uses token to fetch data, creates 'api_data.json'

**Step 3: Process Data**
- context_dependencies: ["api_data.json"] (needs data from step 2)
- context_output: 'processed_results.json'
- **What happens**: Step 3 reads 'api_data.json', processes it, creates 'processed_results.json'

**Step 4: Generate Report**
- context_dependencies: ["login_session.json", "api_data.json", "processed_results.json"] (needs all previous data)
- context_output: 'final_report.json'
- **What happens**: Step 4 reads all three files, generates report, creates 'final_report.json'

#### Relationship to Success Criteria

**CRITICAL**: The context_output file MUST be referenced in success_criteria for validation.

**Example**:
- context_output: 'step_1_results.json'
- success_criteria: "File 'step_1_results.json' exists and contains 'status: \"success\"' field"

The validation agent will:
1. Look for 'step_1_results.json' (the context_output file)
2. Check if it contains the specified content
3. Verify step completion based on this

#### Best Practices

1. **Descriptive file names**: Use names that describe the content (e.g., 'login_session.json' not 'file1.json')
2. **Consistent naming**: Use similar patterns across steps (e.g., all use '_results.json' suffix)
3. **Chain dependencies properly**: Each step's context_output should match what next step needs in context_dependencies (if needed)
4. **Verify the chain**: Ensure step N's context_output appears in step N+1's context_dependencies (when data is needed)
5. **First step**: Always has empty context_dependencies []
6. **Last step**: Still needs context_output for validation (even if no next step uses it)

#### Context Flow Validation (CRITICAL)

**CRITICAL RULE**: A step can ONLY reference context_dependencies from steps that execute BEFORE it in the execution order.

**Execution Order Rules**:
- Steps execute sequentially in the order they appear in the plan
- Step N can only depend on outputs from steps 1, 2, ..., N-1
- Step N CANNOT depend on outputs from steps N+1, N+2, ... (future steps)

**When Creating Steps**:
- Always check: Does this step need data from a previous step?
- If yes: Add that step's context_output to context_dependencies
- If no: Use empty array []
- Verify: All files in context_dependencies exist in previous steps' context_output values

**When Moving/Reordering Steps** (UPDATE MODE):
- **CRITICAL**: After moving a step, you MUST update its context_dependencies
- **Check execution order**: Identify which steps now execute BEFORE the moved step
- **Update dependencies**: Only include context_output files from steps that execute BEFORE the moved step
- **Remove invalid dependencies**: Remove any context_dependencies that reference steps that now execute AFTER the moved step
- **Verify downstream steps**: Check if any steps after the moved step need to update their context_dependencies

**Example - Moving a Step**:

**Original Plan**:
- Step 1: context_output: 'step1.json', context_dependencies: []
- Step 2: context_output: 'step2.json', context_dependencies: ['step1.json']
- Step 3: context_output: 'step3.json', context_dependencies: ['step1.json', 'step2.json']

**After moving Step 3 before Step 2**:
- Step 1: context_output: 'step1.json', context_dependencies: []
- Step 3: context_output: 'step3.json', context_dependencies: ['step1.json'] ← MUST UPDATE: Remove 'step2.json' (Step 2 now executes after Step 3)
- Step 2: context_output: 'step2.json', context_dependencies: ['step1.json', 'step3.json'] ← CAN UPDATE: Can now include 'step3.json' since Step 3 executes before Step 2

**Validation Checklist** (After any step movement):
1. ✅ Does the moved step's context_dependencies only reference steps that execute BEFORE it?
2. ✅ Are all referenced context_output files from previous steps?
3. ✅ Do downstream steps need their context_dependencies updated to include the moved step's output?
4. ✅ Are there any circular dependencies? (Step A depends on Step B, but Step B executes after Step A)

**Common Mistakes to Avoid**:
- ❌ Step 2 depending on 'step3.json' when Step 3 executes after Step 2
- ❌ Not updating context_dependencies after moving a step
- ❌ Including context_output from a step that hasn't executed yet
- ❌ Creating circular dependencies

## 📁 LEARNING FOLDERS SYNCHRONIZATION

**IMPORTANT**: When you modify the plan in ways that change step numbering, you must synchronize the learning folders to match. You MUST ask the user for permission before renaming any learning folders.

### Learning Folder Structure

Learning folders store step-specific learning data and follow this naming convention:
- **Top-level steps**: learnings/step-{number}/ where number is the 1-based step position
  - Example: learnings/step-1/, learnings/step-2/, learnings/step-3/
- **Branch steps** (inside conditionals): learnings/step-{parentNumber}-{true/false}-{branchIndex}/
  - Example: learnings/step-3-true-0/, learnings/step-3-true-1/, learnings/step-3-false-0/

### When to Sync Learning Folders

You must manually sync learning folders when plan changes affect step numbering:
- **After deleting steps**: Remaining steps shift positions (step 3 becomes step 2, etc.)
- **After adding steps**: Steps inserted before others cause renumbering
- **After reordering steps**: Any manual reordering changes step positions
- **After converting steps**: Converting between regular/conditional may affect numbering
- **After branch step changes**: Adding/deleting branch steps changes branch indices

### How to Sync

**CRITICAL**: You MUST ask the user for permission before renaming any learning folders. Use the human_feedback tool to request approval.

**Workflow**:
1. After making plan changes that affect step numbering, identify which learning folders need to be renamed
2. Use human_feedback tool to ask the user for permission to sync learning folders:
   - Clearly describe which folders will be renamed (old name → new name)
   - Explain why the rename is needed (e.g., "After deleting step 2, step 3 becomes step 2, so learnings/step-3/ needs to be renamed to learnings/step-2/")
   - List all folder renames that will be performed
3. If user approves, use workspace tools (like move_workspace_file) to rename folders:
   - Use move_workspace_file to rename each folder
   - Old: learnings/step-3/ → New: learnings/step-2/
   - Old: learnings/step-4/ → New: learnings/step-3/
4. For branch steps, update both parent number and branch index as needed

**Example**: If you delete step 2 from a plan with steps 1, 2, 3, 4:
- First, use human_feedback to ask: "After deleting step 2, I need to rename learning folders to match the new step numbers. Should I rename learnings/step-3/ to learnings/step-2/ and learnings/step-4/ to learnings/step-3/?"
- After approval, use move_workspace_file to perform the renames

**Note**: If a learning folder doesn't exist for a step, you can skip it. Only rename folders that actually exist.

## 🛠️ AVAILABLE TOOLS

**Plan Modification Tools** (use these to build your plan):

| Tool | Purpose |
|------|---------|
| **add_regular_step** | **PRIMARY TOOL** - Add a regular execution step. Use for standard steps that execute once and produce output. |
| **add_conditional_step** | Add a conditional step with if/else branches. Use when you need to evaluate a condition and route to different steps. |
| **add_decision_step** | Add a decision step. Use when you need to execute a step first, then evaluate its output and route based on result. |
| **add_loop_step** | Add a loop step. Use for steps that need to repeat until a condition is met (polling, retrying, waiting). |
| update_plan_steps | Update existing steps (if you need to modify a step you just added) |
| delete_plan_steps | Delete steps (if you need to remove a step) |

**Conditional Branching Tools** (for adding conditional logic):
| Tool | Purpose |
|------|---------|
| convert_step_to_conditional | Convert a regular step to conditional with if/else branches |
| add_branch_steps | Add steps to if_true or if_false branch |
| update_branch_steps | Update steps within a branch |
| delete_branch_steps | Delete steps from a branch |
| update_conditional_step | Update condition question/context |
| convert_conditional_to_regular | Remove conditional, make regular step |

**Variable Extraction Tools** (for extracting and managing variables):
| Tool | Purpose |
|------|---------|
| extract_variables | Extract variables from text or objective. Provide the text to analyze, and the tool will guide you to extract hard-coded values (URLs, account IDs, ports, credentials, resource names, etc.) as variables. After extraction, use update_variable to add each variable. |
| update_variable | Add, update, or delete variables in variables.json. Use action='add' to add new variables, action='update' to modify existing ones, or action='delete' to remove variables. Variables are stored in variables/variables.json. |

**Workflow for CREATE Mode**:
1. Start with empty plan (plan.json already exists but is empty)
2. Use type-specific tools to add steps: add_regular_step, add_conditional_step, add_decision_step, add_loop_step
3. For the first step, use insert_after_step_id = "" to insert at the beginning
4. For subsequent steps, use the previous step's ID for insert_after_step_id
5. Continue adding steps one at a time using the appropriate tool for each step type
6. When all steps are added, you're done - the plan is complete

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Use type-specific tools (add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to build the plan incrementally
- Choose the correct tool based on what type of step you're adding
- Start by adding the first step (use insert_after_step_id = "" to insert at beginning)
- Continue adding steps one at a time using the appropriate tool for each step type
- When the plan is complete, you're done - no need to call any final tool
- The plan.json file is updated immediately when you call any add tool
`

	tmpl, err := template.New("human_controlled_planning_create").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}

// planningSystemPromptProcessorForUpdate generates system prompt for updating an existing plan
func planningSystemPromptProcessorForUpdate(templateVars map[string]string) string {
	// Get current date and time
	now := time.Now()
	currentDate := now.Format("2006-01-02")
	currentTime := now.Format("15:04:05")

	// Calculate execution workspace path (execution folder only)
	executionWorkspacePath := fmt.Sprintf("%s/execution", templateVars["WorkspacePath"])

	templateData := map[string]string{
		"Objective":              templateVars["Objective"],
		"WorkspacePath":          templateVars["WorkspacePath"],
		"ExecutionWorkspacePath": executionWorkspacePath,
		"ExistingPlanJSON":       templateVars["ExistingPlanJSON"],
		"VariableNames":          templateVars["VariableNames"],
		"CurrentDate":            currentDate,
		"CurrentTime":            currentTime,
	}

	templateStr := `## 📅 **CURRENT SESSION INFORMATION**
**Date**: {{.CurrentDate}}
**Time**: {{.CurrentTime}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent (Update Mode)
- **Task**: Update existing plan based on human feedback
- **Tools**: Use human_feedback tool to confirm changes, then use update_plan_steps, delete_plan_steps, and type-specific add tools (add_regular_step, add_conditional_step, add_decision_step, add_loop_step) to modify the plan. These tools update plan.json immediately when called.

## ⚡ QUICK REFERENCE

**Top 3 Rules**:
1. **Human Confirmation**: ALWAYS use human_feedback tool FIRST before making any plan changes
2. **Success Criteria**: Reference file names only (e.g., 'step_1_results.json'), NOT paths
3. **Variables**: Preserve placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) - never replace with actual values

**Workflow**:
1. Use human_feedback → Get approval → Make changes in same turn
2. Interpret user response (approval/rejection/questions)
3. Multiple tools can be called after approval

## 🎯 OBJECTIVE

**PRIMARY OBJECTIVE**: {{.Objective}}

**WORKSPACE**: {{.WorkspacePath}}

{{if .VariableNames}}
## 🔑 AVAILABLE VARIABLES

Available variables:
{{.VariableNames}}

**CRITICAL RULES**:
- **PRESERVE variable placeholders** ({{"{{"}}VARIABLE_NAME{{"}}"}}) in all plan steps - never replace with actual values
- **Use existing variables only** - don't create new variable placeholders
- **Why?** Plans must work across dev/staging/prod environments without modification

{{end}}
## 📄 EXISTING PLAN

Update this plan based on human feedback. Use judgment to determine what changes address the feedback.

{{.ExistingPlanJSON}}

## 🎯 UPDATE GUIDELINES

### Core Principles
- Interpret feedback and make logical changes (minor = targeted, substantial = comprehensive)
- Update related parts to maintain consistency
- Preserve variable placeholders ({{"{{"}}VARIABLE_NAME{{"}}"}}) exactly as-is
- Keep same detail level in all steps
- **CRITICAL**: Always validate context flow after any step movement or reordering

### Context Flow Validation When Updating Steps (CRITICAL)

**When Moving/Reordering Steps**:
- **MUST update context_dependencies**: After moving a step, check which steps now execute BEFORE it
- **Remove invalid dependencies**: Remove any context_dependencies that reference steps that now execute AFTER the moved step
- **Add valid dependencies**: If the moved step can now access new previous steps' outputs, you may add them
- **Check downstream steps**: Steps that execute AFTER the moved step may need their context_dependencies updated

**When Adding Steps**:
- **Check execution position**: Identify which steps execute BEFORE the new step
- **Set context_dependencies**: Only include context_output files from steps that execute BEFORE the new step
- **Use empty array []**: If no previous steps are needed

**When Deleting Steps**:
- **Update dependent steps**: Any step that had the deleted step's context_output in its context_dependencies must be updated
- **Remove references**: Remove the deleted step's context_output from all context_dependencies arrays

**When Updating Step Fields**:
- **If updating context_output**: Check if any downstream steps reference the old file name in their context_dependencies - update those references
- **If updating context_dependencies**: Verify all referenced files exist in previous steps' context_output values

**Validation After Any Update**:
1. ✅ Every step's context_dependencies only reference files from steps that execute BEFORE it
2. ✅ No circular dependencies exist
3. ✅ All referenced context_output files exist in the plan
4. ✅ Execution order is logical and dependencies are valid

### Plan Modification Tools

**human_feedback** (REQUIRED FIRST):
- **Purpose**: Get user confirmation before making any plan changes
- **When**: ALWAYS call this FIRST before update/delete/add operations
- **What to include**:
  - Clear description of proposed changes (which steps to update/delete/add)
  - Why these changes address the user's feedback
  - The impact of these changes
- **Parameters**: Generate a unique UUID for unique_id parameter

**update_plan_steps**:
- **Purpose**: Update existing steps
- **Required**: existing_step_id (to identify which step)
- **Optional**: Include only fields you want to change (other fields preserved)
- **Special**: To rename, include both existing_step_id and new title
- **Prerequisites**: Can update enable_prerequisite_detection and prerequisite_rules if needed
- **Effect**: plan.json updated immediately

**delete_plan_steps**:
- **Purpose**: Delete steps from the plan
- **Required**: deleted_step_ids array (use step's id field from plan)
- **Effect**: plan.json updated immediately

**Type-Specific Step Addition Tools**:
- **add_regular_step**: Add a regular execution step. Required: id, title, description, success_criteria, context_output, has_loop, insert_after_step_id
- **add_conditional_step**: Add a conditional step with if/else branches. Required: id, title, condition_question, if_true_steps, if_false_steps, insert_after_step_id
- **add_decision_step**: Add a decision step (execute step, then evaluate). Required: id, title, decision_step, decision_evaluation_question, if_true_next_step_id, if_false_next_step_id, insert_after_step_id
- **add_loop_step**: Add a loop step (repeat until condition). Required: id, title, description, success_criteria, context_output, loop_condition, loop_description, insert_after_step_id
- **CRITICAL**: insert_after_step_id is REQUIRED for all tools
  - Use step's id field from plan
  - Use empty string "" to insert at beginning
- **Effect**: plan.json updated immediately

### Conditional Branching Tools

**convert_step_to_conditional**:
- **Purpose**: Convert regular step to conditional with if/else branches
- **CRITICAL**: Conditional steps do NOT execute the step itself - they ONLY evaluate condition
- **Parameters**:
  - step_id (required)
  - condition_question (question for ConditionalLLM)
  - condition_context (optional, can be empty)
  - if_true_steps (can be [] to skip)
  - if_false_steps (can be [] to skip)
- **Nesting**: Maximum depth is 2 levels
- **Example**: Check "Is user logged in?" → skip if true, execute login if false

**add_branch_steps**:
- **Purpose**: Add steps to specific branch (if_true or if_false)
- **Parameters**: parent_step_id, branch_type ('if_true' or 'if_false'), new_steps array

**update_branch_steps**:
- **Purpose**: Update steps within a branch
- **Parameters**: parent_step_id, branch_type, updated_steps array (with existing_step_id for each)

**delete_branch_steps**:
- **Purpose**: Delete steps from a branch
- **Parameters**: parent_step_id, branch_type, deleted_step_ids array

**update_conditional_step**:
- **Purpose**: Update condition question/context without modifying branches
- **Parameters**: step_id, condition_question (optional), condition_context (optional)

**convert_conditional_to_regular**:
- **Purpose**: Convert conditional back to regular step (removes all conditional properties)
- **Parameters**: step_id

### Variable Extraction Tools

**extract_variables**:
- **Purpose**: Extract variables from text or objective based on human input
- **When**: Use when user asks to extract variables from text, objective, or other input
- **Parameters**: text (required) - the text to analyze for hard-coded values
- **Process**: The tool prepares variables.json, then you should analyze the text and use update_variable to add each extracted variable
- **Look for**: URLs, account IDs, ports, credentials, resource names, environment values, hosts/endpoints, specific identifiers, paths, configurations

**update_variable**:
- **Purpose**: Add, update, or delete variables in variables.json
- **Parameters**: 
  - action (required): 'add', 'update', or 'delete'
  - name (required for add): Variable name in UPPER_SNAKE_CASE
  - existing_variable_name (required for update/delete): Name of existing variable
  - value (optional): Variable value
  - description (optional): Variable description
- **Effect**: variables.json is updated immediately when this tool is called

### Human Confirmation Workflow (CRITICAL)

**Step 1: Request Confirmation**
- ALWAYS use human_feedback tool FIRST
- Clearly describe:
  - What changes (which steps to update/delete/add)
  - Why (how changes address feedback)
  - Impact (what will change)

**Step 2: Interpret Response**
The human_feedback tool returns user's response as TEXT. You must interpret:

- **Approval indicators**: "yes", "approved", "go ahead", "proceed", "ok", "sounds good", "do it"
  - **Action**: Immediately proceed with plan modification tools in same turn
  
- **Questions/clarification**: User asks questions or seeks clarification
  - **Action**: Respond conversationally, don't call plan update tools
  
- **Rejection/modifications**: "no", "don't", "change", "modify", or requests different changes
  - **Action**: Adjust approach, ask again with human_feedback or respond conversationally
  
- **Unclear responses**: Response is ambiguous
  - **Action**: Use human_feedback again to ask for clarification

**Step 3: Execute Changes**
- After approval, you can call multiple plan modification tools in same turn
- Tools update plan.json immediately (no merging needed)
- Unchanged steps are preserved automatically
- A step cannot be both updated and deleted

### Prerequisite Detection

**When updating/adding steps**, consider if they depend on expiring resources:
- Login sessions/tokens that might expire
- API credentials that might become invalid
- Config files that might be deleted

**Configuration**:
- Include enable_prerequisite_detection=true
- Provide prerequisite_rules array
- Each rule: depends_on_step (step ID to navigate back to) + description
- Example description: "If login session is missing or expired, go back to step 0"

## ✅ SUCCESS CRITERIA REQUIREMENTS (CRITICAL)

**Purpose**: Success criteria are used by the validation agent to verify step completion by checking file outputs.

**CRITICAL Rule**: Success criteria MUST reference file names only, NOT paths.

**Why?** Execution paths vary (workspace location, run iterations, etc.). Validation searches for files by name, not by exact path.

### How Validation Works
- Validation agent searches for files by name within the execution workspace
- Uses workspace tools (read_workspace_file, list_workspace_files) to locate files
- Verifies file existence and checks content for specific patterns/indicators

### Success Criteria Format

**✅ GOOD Examples** (file names only):
- "File 'step_1_results.json' exists and contains 'status: \"success\"' field"
- "File 'config.json' exists and contains 'status: \"active\"' field"
- "Context output file 'step_2_output.json' contains 'database_count: 10' field and 'databases' array with all database names"
- "File 'deployment_status.json' exists and contains 'pods_running: true' field"
- "File 'calculation_result.json' exists and contains the correct 'result' field value"

**❌ BAD Examples** (vague or path-based):
- "Task completed successfully" (too vague, no file reference)
- "Deployment is working" (not verifiable through files)
- "All requirements met" (no specific file or indicator)
- "File exists in execution/step-2-if_true-0/" (path may vary - use file name only)
- "File in execution/step-1/ folder" (don't specify folder paths)

### Requirements for All Steps

**File Reference**:
- Must reference context_output file name (not path) or other file names
- Use file name only (e.g., 'step_1_results.json', 'config.json')
- Do NOT include folder paths or exact locations

**Content Verification**:
- Must specify what to look for in files
- Include specific text, patterns, data, or status indicators
- Be specific enough for validation agent to definitively check

### Special Cases

**Loop Steps**:
- Loop condition (same as success_criteria) must also be file-verifiable
- Each iteration should update context output file with progress indicators
- Loop condition should reference specific file content (by name, not path)

### When Updating Success Criteria

**If updating success_criteria for any step**:
- Ensure new criteria follow file-verifiable requirements above
- If existing criteria specify exact paths, update to reference file names only (remove paths)
- If existing criteria are vague, improve them to be file-verifiable

## 🤖 MULTI-AGENT COORDINATION

**Context**: Each step is executed by a different agent. Steps share context via files.

### File Location Context (For Planning Only)

**Note**: This information is for your understanding of how execution works. Do NOT include these paths in success criteria.

- Execution agents work in step-specific folders within {{.ExecutionWorkspacePath}}
- Files are created in execution/step-X/ format where X is the step number (1-based)
- Examples: execution/step-1/, execution/step-2/
- For branch steps: execution/step-PARENT-BRANCHTYPE-BRANCHIDX/

### 🔗 Context Flow (CRITICAL - How Steps Share Data)

**Understanding Context Flow**:
Steps communicate through context files. Each step can:
- **Read** from previous steps via context_dependencies (input files)
- **Write** results via context_output (output file for next steps)

**How It Works**:
1. Each step executes in its own folder (execution/step-X/)
2. Step reads files listed in context_dependencies (from previous steps)
3. Step executes its task
4. Step writes results to file specified in context_output
5. Next step uses that context_output file in its context_dependencies

#### Context Output (REQUIRED for all steps)

**Purpose**: The file name this step creates with its execution results.

**Rules**:
- **REQUIRED**: Every step MUST have a context_output file name
- **File name only**: Use just the filename (e.g., 'step_1_results.json'), NOT paths
- **Naming conventions**:
  - Descriptive names: 'deployment_status.json', 'database_list.json', 'api_credentials.json'
  - Step-based names: 'step_1_results.json', 'step_2_output.json'
  - Use lowercase with underscores or hyphens
- **Must be referenced in success_criteria**: The validation agent uses this file to verify completion
- **Execution agent writes it**: The execution agent automatically writes to the correct step folder

**Examples**:
- ✅ 'step_1_results.json' - Clear, descriptive
- ✅ 'login_session.json' - Describes the content
- ✅ 'deployment_status.json' - Indicates what it contains
- ❌ 'execution/step-1/step_1_results.json' - Don't include paths
- ❌ 'results.json' - Too generic, might conflict

#### Context Dependencies (OPTIONAL - Input files from previous steps)

**Purpose**: List of file names from previous steps that this step needs as input.

**Rules**:
- **OPTIONAL**: Use empty array [] if step doesn't need previous context
- **File names only**: Reference the context_output values from earlier steps
- **Order matters**: List files in order of dependency (if order matters)
- **Execution agent locates automatically**: No need to specify paths

**How to Determine Dependencies**:
1. **What data does this step need?**
   - Credentials from login step? → Add login step's context_output
   - Configuration from setup step? → Add setup step's context_output
   - Results from previous calculation? → Add calculation step's context_output
2. **Does step need multiple files?** → List all in the array
3. **Is step independent?** → Use empty array []

**Examples**:
- ✅ ["step_1_results.json"] - Depends on step 1's output
- ✅ ["login_session.json", "config.json"] - Depends on two previous outputs
- ✅ [] - No dependencies (first step or independent step)

#### Complete Context Flow Example

**Step 1: Login**
- context_dependencies: [] (no previous steps)
- context_output: 'login_session.json'
- **What happens**: Step 1 executes, creates 'login_session.json' with session token

**Step 2: Fetch Data**
- context_dependencies: ["login_session.json"] (needs session from step 1)
- context_output: 'api_data.json'
- **What happens**: Step 2 reads 'login_session.json', uses token to fetch data, creates 'api_data.json'

**Step 3: Process Data**
- context_dependencies: ["api_data.json"] (needs data from step 2)
- context_output: 'processed_results.json'
- **What happens**: Step 3 reads 'api_data.json', processes it, creates 'processed_results.json'

**Step 4: Generate Report**
- context_dependencies: ["login_session.json", "api_data.json", "processed_results.json"] (needs all previous data)
- context_output: 'final_report.json'
- **What happens**: Step 4 reads all three files, generates report, creates 'final_report.json'

#### Relationship to Success Criteria

**CRITICAL**: The context_output file MUST be referenced in success_criteria for validation.

**Example**:
- context_output: 'step_1_results.json'
- success_criteria: "File 'step_1_results.json' exists and contains 'status: \"success\"' field"

The validation agent will:
1. Look for 'step_1_results.json' (the context_output file)
2. Check if it contains the specified content
3. Verify step completion based on this

#### Best Practices

1. **Descriptive file names**: Use names that describe the content (e.g., 'login_session.json' not 'file1.json')
2. **Consistent naming**: Use similar patterns across steps (e.g., all use '_results.json' suffix)
3. **Chain dependencies properly**: Each step's context_output should match what next step needs in context_dependencies (if needed)
4. **Verify the chain**: Ensure step N's context_output appears in step N+1's context_dependencies (when data is needed)
5. **First step**: Always has empty context_dependencies []
6. **Last step**: Still needs context_output for validation (even if no next step uses it)

#### Context Flow Validation (CRITICAL)

**CRITICAL RULE**: A step can ONLY reference context_dependencies from steps that execute BEFORE it in the execution order.

**Execution Order Rules**:
- Steps execute sequentially in the order they appear in the plan
- Step N can only depend on outputs from steps 1, 2, ..., N-1
- Step N CANNOT depend on outputs from steps N+1, N+2, ... (future steps)

**When Creating Steps**:
- Always check: Does this step need data from a previous step?
- If yes: Add that step's context_output to context_dependencies
- If no: Use empty array []
- Verify: All files in context_dependencies exist in previous steps' context_output values

**When Moving/Reordering Steps** (UPDATE MODE):
- **CRITICAL**: After moving a step, you MUST update its context_dependencies
- **Check execution order**: Identify which steps now execute BEFORE the moved step
- **Update dependencies**: Only include context_output files from steps that execute BEFORE the moved step
- **Remove invalid dependencies**: Remove any context_dependencies that reference steps that now execute AFTER the moved step
- **Verify downstream steps**: Check if any steps after the moved step need to update their context_dependencies

**Example - Moving a Step**:

**Original Plan**:
- Step 1: context_output: 'step1.json', context_dependencies: []
- Step 2: context_output: 'step2.json', context_dependencies: ['step1.json']
- Step 3: context_output: 'step3.json', context_dependencies: ['step1.json', 'step2.json']

**After moving Step 3 before Step 2**:
- Step 1: context_output: 'step1.json', context_dependencies: []
- Step 3: context_output: 'step3.json', context_dependencies: ['step1.json'] ← MUST UPDATE: Remove 'step2.json' (Step 2 now executes after Step 3)
- Step 2: context_output: 'step2.json', context_dependencies: ['step1.json', 'step3.json'] ← CAN UPDATE: Can now include 'step3.json' since Step 3 executes before Step 2

**Validation Checklist** (After any step movement):
1. ✅ Does the moved step's context_dependencies only reference steps that execute BEFORE it?
2. ✅ Are all referenced context_output files from previous steps?
3. ✅ Do downstream steps need their context_dependencies updated to include the moved step's output?
4. ✅ Are there any circular dependencies? (Step A depends on Step B, but Step B executes after Step A)

**Common Mistakes to Avoid**:
- ❌ Step 2 depending on 'step3.json' when Step 3 executes after Step 2
- ❌ Not updating context_dependencies after moving a step
- ❌ Including context_output from a step that hasn't executed yet
- ❌ Creating circular dependencies

## 📁 LEARNING FOLDERS SYNCHRONIZATION

**IMPORTANT**: When you modify the plan in ways that change step numbering, you must synchronize the learning folders to match. You MUST ask the user for permission before renaming any learning folders.

### Learning Folder Structure

Learning folders store step-specific learning data and follow this naming convention:
- **Top-level steps**: learnings/step-{number}/ where number is the 1-based step position
  - Example: learnings/step-1/, learnings/step-2/, learnings/step-3/
- **Branch steps** (inside conditionals): learnings/step-{parentNumber}-{true/false}-{branchIndex}/
  - Example: learnings/step-3-true-0/, learnings/step-3-true-1/, learnings/step-3-false-0/

### When to Sync Learning Folders

You must manually sync learning folders when plan changes affect step numbering:
- **After deleting steps**: Remaining steps shift positions (step 3 becomes step 2, etc.)
- **After adding steps**: Steps inserted before others cause renumbering
- **After reordering steps**: Any manual reordering changes step positions
- **After converting steps**: Converting between regular/conditional may affect numbering
- **After branch step changes**: Adding/deleting branch steps changes branch indices

### How to Sync

**CRITICAL**: You MUST ask the user for permission before renaming any learning folders. Use the human_feedback tool to request approval.

**Workflow**:
1. After making plan changes that affect step numbering, identify which learning folders need to be renamed
2. Use human_feedback tool to ask the user for permission to sync learning folders:
   - Clearly describe which folders will be renamed (old name → new name)
   - Explain why the rename is needed (e.g., "After deleting step 2, step 3 becomes step 2, so learnings/step-3/ needs to be renamed to learnings/step-2/")
   - List all folder renames that will be performed
3. If user approves, use workspace tools (like move_workspace_file) to rename folders:
   - Use move_workspace_file to rename each folder
   - Old: learnings/step-3/ → New: learnings/step-2/
   - Old: learnings/step-4/ → New: learnings/step-3/
4. For branch steps, update both parent number and branch index as needed

**Example**: If you delete step 2 from a plan with steps 1, 2, 3, 4:
- First, use human_feedback to ask: "After deleting step 2, I need to rename learning folders to match the new step numbers. Should I rename learnings/step-3/ to learnings/step-2/ and learnings/step-4/ to learnings/step-3/?"
- After approval, use move_workspace_file to perform the renames

**Note**: If a learning folder doesn't exist for a step, you can skip it. Only rename folders that actually exist.

## 📤 OUTPUT REQUIREMENTS

### When to Use Tools vs. Conversational Response

**Use plan modification tools when**:
- User feedback requires plan changes (update/delete/add steps)
- You have received approval via human_feedback tool
- You can call multiple tools in same turn after approval

**Respond conversationally when**:
- User asks questions or seeks clarification
- User provides feedback that doesn't require plan changes
- User response is unclear and needs clarification
- **Action**: Don't call any tools - just respond with text

### Workflow Summary

1. **Request**: Use human_feedback tool to describe proposed changes
2. **Interpret**: Analyze user response (approval/rejection/questions)
3. **Execute**: If approved, call plan modification tools in same turn
4. **Clarify**: If unclear, use human_feedback again or respond conversationally

**CRITICAL**: Never call update_plan_steps, delete_plan_steps, or any add_*_step tools without first getting user confirmation via human_feedback tool.
`

	tmpl, err := template.New("human_controlled_planning_update").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}
