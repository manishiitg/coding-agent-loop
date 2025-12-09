package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
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
	ExecutionMaxTurns             *int               `json:"execution_max_turns,omitempty"`               // default: 25
	ValidationMaxTurns            *int               `json:"validation_max_turns,omitempty"`              // default: 25
	LearningMaxTurns              *int               `json:"learning_max_turns,omitempty"`                // default: 25
	DisableValidation             *bool              `json:"disable_validation,omitempty"`                // skip validation entirely (nil = not set/enabled, true = disabled, false = explicitly enabled)
	DisableLearning               *bool              `json:"disable_learning,omitempty"`                  // disable learning for this step (nil = not set/enabled, true = disabled, false = explicitly enabled)
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
	ID                       string                `json:"id"` // Stable step ID (generated from title) - required
	Title                    string                `json:"title"`
	Description              string                `json:"description"`
	SuccessCriteria          string                `json:"success_criteria"`
	WhyThisStep              string                `json:"why_this_step,omitempty"` // Optional explanation of why this step is needed
	ContextDependencies      []string              `json:"context_dependencies"`
	ContextOutput            FlexibleContextOutput `json:"context_output"`                        // Use flexible type to handle string or array
	LearningFilesToReference []string              `json:"learning_files_to_reference,omitempty"` // learning files to read for context (execution agent reads full files)
	HasLoop                  bool                  `json:"has_loop"`                              // true if step needs to loop
	LoopCondition            string                `json:"loop_condition"`                        // condition description (same as success criteria) - REQUIRED when has_loop=true
	MaxIterations            int                   `json:"max_iterations,omitempty"`              // max iterations (default: 10)
	LoopDescription          string                `json:"loop_description,omitempty"`            // human-readable explanation
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
	// Prerequisite failure detection fields (optional - can be configured in plan or later via UI in step_config.json)
	EnablePrerequisiteDetection *bool              `json:"enable_prerequisite_detection,omitempty"` // Enable prerequisite failure detection for this step (default: false)
	PrerequisiteRules           []PrerequisiteRule `json:"prerequisite_rules,omitempty"`            // Array of prerequisite rules, each with one step dependency and one description
	// AgentConfigs removed - now stored separately in step_config.json
}

// AddPlanStep represents a step to be added with insertion position
type AddPlanStep struct {
	PlanStep
	InsertAfterStepID string `json:"insert_after_step_id"` // REQUIRED: ID of step to insert after (use empty string "" to insert at beginning)
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
}

// planFileMutex ensures thread-safe access to plan.json
var planFileMutex sync.Mutex

// changelogSessionMutex ensures thread-safe access to changelog session tracking
var changelogSessionMutex sync.Mutex

// changelogSessionFile tracks the current changelog file for the active session
// Format: changelog-YYYY-MM-DD-HH-MM-SS.json
var changelogSessionFile string

// changelogSessionStartTime tracks when the current session started
var changelogSessionStartTime time.Time

// PlanChangeLogEntry represents a single change entry in the changelog
type PlanChangeLogEntry struct {
	Timestamp   string   `json:"timestamp"`   // ISO 8601 timestamp
	ChangeType  string   `json:"change_type"` // "update", "delete", "add", "convert_to_conditional", "convert_to_regular", "add_branch_steps", "update_branch_steps", "delete_branch_steps", "update_conditional_step"
	StepIDs     []string `json:"step_ids"`    // Affected step IDs
	Description string   `json:"description"` // Human-readable description of the change
	Details     string   `json:"details"`     // Additional details (JSON string of what changed)
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

// getAddPlanStepsSchema returns the JSON schema for add_plan_steps tool
func getAddPlanStepsSchema() string {
	return `{
		"type": "object",
		"properties": {
			"new_steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this new step. Generate a unique, URL-friendly ID based on the step title (e.g., 'deploy-application' from 'Deploy Application')."
						},
						"title": {
							"type": "string",
							"description": "Short, clear title for the new step"
						},
						"description": {
							"type": "string",
							"description": "COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context."
						},
						"success_criteria": {
							"type": "string",
							"description": "Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive. CRITICAL: Success criteria MUST be file-verifiable. The validation agent will check file outputs to verify completion. Reference specific files (especially the context_output file) and what to look for in them (specific text, patterns, data, status indicators). Examples: 'File step_1_results.md exists and contains Deployment successful status', 'Context output file contains 10 databases found and lists all database names'. Avoid vague statements like 'Task completed successfully' that cannot be verified through files."
						},
						"context_dependencies": {
							"type": "array",
							"items": { "type": "string" },
							"description": "List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for subsequent steps - e.g., 'step_1_results.md'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder."
						},
						"has_loop": {
							"type": "boolean",
							"description": "Whether this step needs to loop until condition is met. Set to true when step requires polling, retrying, or waiting for external systems."
						},
						"loop_condition": {
							"type": "string",
							"description": "Condition that must be met to exit the loop (REQUIRED when has_loop is true). This should be the same as success_criteria."
						},
						"max_iterations": {
							"type": "integer",
							"description": "Maximum number of loop iterations allowed (default: 10). Only include when has_loop is true."
						},
						"loop_description": {
							"type": "string",
							"description": "Describe what happens in EACH ITERATION of the loop. Only include when has_loop is true."
						},
						"insert_after_step_id": {
							"type": "string",
							"description": "REQUIRED: The ID of the step to insert after. Use the step's id field from the plan. Use empty string \"\" to insert at the beginning of the plan (before the first step)."
						}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop", "insert_after_step_id"]
				},
				"description": "New steps to add to the plan. Provide complete step definitions with all required fields. Each step must specify insert_after_step_id to indicate where to insert it in the plan."
			}
		},
		"required": ["new_steps"]
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

// ExecuteStructured executes the planning agent and returns structured JSON output
// userMessage: The user message to send (e.g., "Generate plan" for CREATE, or human feedback for UPDATE)
// Returns PlanningResponse for CREATE mode
// Note: For UPDATE mode, use ExecuteStructuredUpdate instead
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string) (*PlanningResponse, []llmtypes.MessageContent, error) {

	// CREATE mode: Use existing PlanningResponse schema
	// Define the JSON schema for plan generation (CREATE mode)
	schema := `{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "REQUIRED: Stable step ID for this step. Generate a unique, URL-friendly ID based on the step title (e.g., 'list-bank-statement-files' from 'List Bank Statement Files'). Use lowercase, hyphens instead of spaces, and keep it concise."
						},
						"title": {
							"type": "string",
							"description": "Short, clear title for the step"
						},
					"description": {
						"type": "string",
						"description": "COMPREHENSIVE, DETAILED description of what this step accomplishes. Be thorough and complete - include specific details about what needs to be done, what tools or approaches might be needed, what outcomes are expected, key considerations, and any important context. FOR LOOPING STEPS (has_loop=true): Emphasize that progress MUST be saved after EACH iteration in the context output file. Each iteration should update/append to the context file so progress is preserved and visible to the next iteration. Describe how progress accumulates across iterations. Write a complete, detailed explanation that fully captures the step's requirements and scope."
					},
						"success_criteria": {
							"type": "string",
							"description": "Detailed explanation of how to verify this step was completed successfully - be specific and comprehensive. CRITICAL: Success criteria MUST be file-verifiable. The validation agent will check file outputs to verify completion. Reference specific files (especially the context_output file) and what to look for in them (specific text, patterns, data, status indicators). Examples: 'File step_1_results.md exists and contains Deployment successful status', 'Context output file contains 10 databases found and lists all database names'. Avoid vague statements like 'Task completed successfully' that cannot be verified through files."
						},
						"context_dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of context files from previous steps that this step depends on. Use empty array [] if no dependencies."
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for subsequent steps - e.g., 'step_1_results.md'. IMPORTANT: Execution agents work ONLY in the execution folder, so context output files will be written to the execution folder. Can be string or array (will be converted)."
						},
						"has_loop": {
							"type": "boolean",
							"description": "Whether this step needs to loop until condition is met. Set to true when step requires: (1) Polling/waiting for services or resources to become ready, (2) Retrying operations until they succeed, (3) Iterating until data appears or condition changes, (4) Checking status repeatedly until a goal is achieved, (5) Complex multi-operation tasks where the outcome is uncertain, (6) Tasks depending on external systems/APIs that might not be immediately available."
						},
						"loop_condition": {
							"type": "string",
							"description": "Condition that must be met to exit the loop (REQUIRED when has_loop is true). This should be the same as success_criteria - describe the condition that must be met. CRITICAL: Loop condition MUST be file-verifiable. Reference specific files and what to look for in them to determine if the loop can exit. The validation agent will check file outputs to verify the loop condition."
						},
						"max_iterations": {
							"type": "integer",
							"description": "Maximum number of loop iterations allowed to prevent infinite loops (default: 10). Use higher values (20-50) for long-running operations, use lower values (3-5) for quick status checks. Only include when has_loop is true."
						},
					"loop_description": {
						"type": "string",
						"description": "CRITICAL for looping steps: Describe what happens in EACH ITERATION of the loop. Be specific about: (1) What to check/verify in each iteration, (2) What actions to take in each iteration, (3) What progress indicators to look for, (4) How to save/update progress after each iteration. Example: 'Each iteration: Check deployment status via health endpoint, verify pod readiness count, save current status to context file, wait 30 seconds before next check.' This guides the execution agent on per-iteration behavior. Only include when has_loop is true."
					},
						"has_condition": {
							"type": "boolean",
							"description": "Whether this step has conditional branching (if/else logic). Set to true when you need to check a condition and execute different steps based on the result. CRITICAL: Conditional steps do NOT execute the step itself - they ONLY evaluate the condition and then execute the appropriate branch steps."
						},
						"condition_question": {
							"type": "string",
							"description": "Question to ask the conditional agent for decision making (REQUIRED when has_condition is true). Example: 'Is the user already logged in?', 'Is the deployment healthy?'. The conditional agent will evaluate this question and return true/false."
						},
						"condition_context": {
							"type": "string",
							"description": "Context to provide to the conditional agent (OPTIONAL when has_condition is true). Can include context files, status information, or any relevant data to help the conditional agent make the decision. Can be empty string if not needed."
						},
						"if_true_steps": {
							"type": "array",
							"items": {"type": "object"},
							"description": "Steps to execute if condition is true (REQUIRED when has_condition is true). Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Example: If checking 'Is user logged in?' and you want to skip to next step when true, use empty array []."
						},
						"if_false_steps": {
							"type": "array",
							"items": {"type": "object"},
							"description": "Steps to execute if condition is false (REQUIRED when has_condition is true). Can be empty array [] to skip this branch and continue directly to the next step in the main plan. Example: If checking 'Is user logged in?' and you want to execute login steps when false, provide [login_step1, login_step2]."
						},
						"if_true_next_step_id": {
							"type": "string",
							"description": "ID of step to connect to after true branch completes, or 'end' to end the workflow. REQUIRED when if_true_steps is empty []. Optional when if_true_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after true branch, set if_true_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
						},
						"if_false_next_step_id": {
							"type": "string",
							"description": "ID of step to connect to after false branch completes, or 'end' to end the workflow. REQUIRED when if_false_steps is empty []. Optional when if_false_steps has steps (defaults to next step in main plan if not specified). Use the step's id field from the main plan, or 'end' to terminate. Example: If conditional step is step 4 and you want to go to step 5 after false branch, set if_false_next_step_id to 'step-5-id'. If you want to end the workflow, set to 'end'."
						},
						"enable_prerequisite_detection": {
							"type": "boolean",
							"description": "OPTIONAL: Whether to enable prerequisite failure detection for this step. Set to true when this step depends on outputs from previous steps that might expire or become invalid (e.g., login sessions, API tokens, config files). When enabled, if validation fails due to missing prerequisites, the system will navigate back to the prerequisite step instead of retrying. Default: false (can be configured later via UI)."
						},
						"prerequisite_rules": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"depends_on_step": {
										"type": "string",
										"description": "REQUIRED: The step ID this rule depends on. Must be a step that appears earlier in the plan. Use the step's id field from the plan."
									},
									"description": {
										"type": "string",
										"description": "REQUIRED: Natural language description of when to detect prerequisite failures for this specific step. Examples: 'If login session is missing or expired, go back to step 0', 'If config file is missing, go back to step 1', 'If API token expired, go back to step 2'. The validation agent will use this description to determine if a failure is due to missing prerequisites."
									}
								},
								"required": ["depends_on_step", "description"]
							},
							"description": "OPTIONAL: Array of prerequisite rules for this step. Each rule specifies one step dependency and one description of when to detect prerequisite failures. Only include if enable_prerequisite_detection is true. Examples: [{\"depends_on_step\": \"login-step\", \"description\": \"If login session is missing or expired, go back to login step\"}]. Can be configured later via UI if not included in initial plan."
						}
					},
					"required": ["id", "title", "description", "success_criteria", "has_loop"]
				}
			}
		},
		"required": ["steps"]
	}`

	// Generate system prompt using the processor
	systemPrompt := planningSystemPromptProcessor(templateVars)

	// Create a simple input processor that just returns the user message
	// In CREATE mode: userMessage is "Generate plan"
	inputProcessor := func(map[string]string) string {
		return userMessage
	}

	// Use the new ExecuteStructuredWithInputProcessorViaTool method
	toolName := "submit_planning_response"
	toolDescription := "Submit the final structured planning response in JSON format. This tool should be called when you have completed the plan and are ready to provide the structured output."

	result, updatedHistory, err := agents.ExecuteStructuredWithInputProcessorViaTool[PlanningResponse](hctppa.BaseOrchestratorAgent, ctx, templateVars, inputProcessor, conversationHistory, schema, systemPrompt, false, toolName, toolDescription)
	if err != nil {
		// Check if this is a non-structured response error (text response instead of structured output)
		// IMPORTANT: Return the error directly without wrapping, so runPlanningPhase can detect it
		if agents.IsNonStructuredResponseError(err) {
			// Return the original NonStructuredResponseError with UpdatedHistory so runPlanningPhase can handle it
			// Don't wrap it - wrapping breaks the error type check
			var nonStructuredErr *agents.NonStructuredResponseError
			if errors.As(err, &nonStructuredErr) {
				return nil, nonStructuredErr.UpdatedHistory, err
			}
			return nil, updatedHistory, err
		}
		return nil, nil, err
	}

	return &result, updatedHistory, nil
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

			// Track what changed
			updatedStepIDs = append(updatedStepIDs, partialUpdate.ExistingStepID)
			changeDetail := map[string]interface{}{
				"step_id": partialUpdate.ExistingStepID,
				"title":   existingStep.Title,
			}
			changedFields := []string{}
			if partialUpdate.Title != "" {
				changedFields = append(changedFields, "title")
			}
			if partialUpdate.Description != "" {
				changedFields = append(changedFields, "description")
			}
			if partialUpdate.SuccessCriteria != "" {
				changedFields = append(changedFields, "success_criteria")
			}
			if partialUpdate.ContextDependencies != nil {
				changedFields = append(changedFields, "context_dependencies")
			}
			if partialUpdate.ContextOutput != "" {
				changedFields = append(changedFields, "context_output")
			}
			if partialUpdate.HasLoop != nil {
				changedFields = append(changedFields, "has_loop")
			}
			if partialUpdate.LoopCondition != "" {
				changedFields = append(changedFields, "loop_condition")
			}
			if partialUpdate.MaxIterations != nil {
				changedFields = append(changedFields, "max_iterations")
			}
			if partialUpdate.LoopDescription != "" {
				changedFields = append(changedFields, "loop_description")
			}
			if partialUpdate.EnablePrerequisiteDetection != nil {
				changedFields = append(changedFields, "enable_prerequisite_detection")
			}
			if partialUpdate.PrerequisiteRules != nil {
				changedFields = append(changedFields, "prerequisite_rules")
			}
			changeDetail["changed_fields"] = changedFields
			changeDetails = append(changeDetails, changeDetail)

			// Merge partial update
			*existingStep = mergePartialStepUpdate(*existingStep, partialUpdate)
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
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Updated %d steps in plan", len(partialUpdates)))
		return fmt.Sprintf("Successfully updated %d step(s) in the plan", len(partialUpdates)), nil
	}
}

// createDeletePlanStepsExecutor creates an executor function for delete_plan_steps tool
func createDeletePlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
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
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
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
		for _, step := range plan.Steps {
			existingStepsMap[step.ID] = true
		}
		for _, id := range deletedIDs {
			if !existingStepsMap[id] {
				// Build list of available step IDs for better error message
				availableIDs := make([]string, 0, len(plan.Steps))
				for _, step := range plan.Steps {
					availableIDs = append(availableIDs, step.ID)
				}
				return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot delete). Available step IDs: %v", id, availableIDs), nil)
			}
		}

		// Filter out deleted steps
		filteredSteps := make([]PlanStep, 0, len(plan.Steps))
		for _, step := range plan.Steps {
			if !deletedSet[step.ID] {
				filteredSteps = append(filteredSteps, step)
			}
		}

		plan.Steps = filteredSteps

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"deleted_step_ids": deletedIDs,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "delete",
			StepIDs:     deletedIDs,
			Description: fmt.Sprintf("Deleted %d step(s): %v", len(deletedIDs), deletedIDs),
			Details:     string(detailsJSON),
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Deleted %d steps from plan", len(deletedIDs)))
		return fmt.Sprintf("Successfully deleted %d step(s) from the plan", len(deletedIDs)), nil
	}
}

// createAddPlanStepsExecutor creates an executor function for add_plan_steps tool
func createAddPlanStepsExecutor(workspacePath string, logger loggerv2.Logger, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error) func(context.Context, map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract new_steps from args
		newStepsRaw, ok := args["new_steps"].([]interface{})
		if !ok {
			return "", fmt.Errorf(fmt.Sprintf("invalid new_steps argument"), nil)
		}

		// Convert to JSON and unmarshal to AddPlanStep array
		newStepsJSON, err := json.Marshal(newStepsRaw)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to marshal new_steps: %w", err), nil)
		}

		var addSteps []AddPlanStep
		if err := json.Unmarshal(newStepsJSON, &addSteps); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to parse new_steps: %w", err), nil)
		}

		// Read current plan
		plan, err := readPlanFromFile(ctx, workspacePath, readFile)
		if err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
		}

		// Create map of step IDs to indices for quick lookup
		idToIndex := make(map[string]int)
		for i, step := range plan.Steps {
			idToIndex[step.ID] = i
		}

		// Track which positions we need to insert at (grouped by insertion point)
		insertionPoints := make(map[int][]PlanStep) // original index -> steps to insert after this index

		for i, addStep := range addSteps {
			// Validate that step has ID (LLM should always provide it)
			if addStep.PlanStep.ID == "" {
				return "", fmt.Errorf(fmt.Sprintf("step at index %d in new_steps is missing required ID field. Step title: %q", i, addStep.PlanStep.Title), nil)
			}

			var afterIndex int
			var found bool

			if addStep.InsertAfterStepID == "" {
				// Insert at beginning (before index 0, so afterIndex = -1)
				afterIndex = -1
				found = true
			} else {
				// Find the step to insert after
				afterIndex, found = idToIndex[addStep.InsertAfterStepID]
				if !found {
					// Build list of available step IDs for better error message
					availableIDs := make([]string, 0, len(plan.Steps))
					for _, step := range plan.Steps {
						availableIDs = append(availableIDs, step.ID)
					}
					return "", fmt.Errorf(fmt.Sprintf("step ID '%s' not found in existing plan (cannot insert after it). Available step IDs: %v", addStep.InsertAfterStepID, availableIDs), nil)
				}
			}

			// Add to insertion points map (key is the index to insert after)
			insertionPoints[afterIndex] = append(insertionPoints[afterIndex], addStep.PlanStep)
		}

		// Build new plan with insertions
		// Iterate through original steps and insert new steps at the right positions
		newPlanSteps := make([]PlanStep, 0, len(plan.Steps)+len(addSteps))

		// First, handle insertion at the beginning (afterIndex = -1)
		if stepsToInsert, hasInsertion := insertionPoints[-1]; hasInsertion {
			newPlanSteps = append(newPlanSteps, stepsToInsert...)
		}

		// Then iterate through original steps
		for i, originalStep := range plan.Steps {
			// Add the original step
			newPlanSteps = append(newPlanSteps, originalStep)

			// Insert any steps that should go after this step
			if stepsToInsert, hasInsertion := insertionPoints[i]; hasInsertion {
				newPlanSteps = append(newPlanSteps, stepsToInsert...)
			}
		}

		plan.Steps = newPlanSteps

		// Write updated plan (creates backup automatically)
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		newStepIDs := make([]string, 0, len(addSteps))
		insertionDetails := make([]map[string]interface{}, 0, len(addSteps))
		for _, addStep := range addSteps {
			newStepIDs = append(newStepIDs, addStep.PlanStep.ID)
			insertionDetails = append(insertionDetails, map[string]interface{}{
				"step_id":              addStep.PlanStep.ID,
				"title":                addStep.PlanStep.Title,
				"insert_after_step_id": addStep.InsertAfterStepID,
			})
		}
		detailsJSON, _ := json.Marshal(insertionDetails)
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "add",
			StepIDs:     newStepIDs,
			Description: fmt.Sprintf("Added %d new step(s): %v", len(addSteps), newStepIDs),
			Details:     string(detailsJSON),
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Added %d new steps to plan", len(addSteps)))
		return fmt.Sprintf("Successfully added %d new step(s) to the plan", len(addSteps)), nil
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
		createDeletePlanStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register delete_plan_steps tool: %w", err), nil)
	}

	addSchema := getAddPlanStepsSchema()
	addParams, err := parseSchemaForToolParameters(addSchema)
	if err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to parse add schema: %w", err), nil)
	}
	if err := mcpAgent.RegisterCustomTool(
		"add_plan_steps",
		"Add new steps to the plan. Provide complete step definitions with all required fields (title, description, success_criteria, has_loop, insert_after_step_id). CRITICAL: Each step MUST specify insert_after_step_id (REQUIRED) to indicate where to insert it. Use the step's id field from the plan, or empty string \"\" to insert at the beginning. The plan.json file is updated immediately when this tool is called.",
		addParams,
		createAddPlanStepsExecutor(workspacePath, logger, readFile, writeFile),
		"workflow",
	); err != nil {
		return fmt.Errorf(fmt.Sprintf("failed to register add_plan_steps tool: %w", err), nil)
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

// ExecuteStructuredUpdate executes the planning agent in UPDATE mode using 3 custom tools that directly update plan.json
// readFile and writeFile are BaseOrchestrator's ReadWorkspaceFile and WriteWorkspaceFile methods
// baseOrchestrator is used to emit events when plan is updated
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructuredUpdate(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent, userMessage string, readFile func(context.Context, string) (string, error), writeFile func(context.Context, string, string) error, baseOrchestrator *orchestrator.BaseOrchestrator) (*PlanningResponse, []llmtypes.MessageContent, error) {
	// Reset changelog session at the start of a new planning agent execution
	// This ensures all changes during this execution are written to the same changelog file
	resetChangelogSession()

	// Get workspace path from template vars
	workspacePath := templateVars["WorkspacePath"]
	if workspacePath == "" {
		return nil, nil, fmt.Errorf(fmt.Sprintf("WorkspacePath not found in template vars"), nil)
	}

	// Get the underlying MCP agent
	baseAgent := hctppa.BaseOrchestratorAgent.BaseAgent()
	if baseAgent == nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("base agent is not initialized"), nil)
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, nil, fmt.Errorf(fmt.Sprintf("MCP agent is not initialized"), nil)
	}

	// Get logger from MCP agent (it has a Logger field)
	logger := mcpAgent.Logger

	// Register WorkspaceTools (including human_feedback) before plan modification tools
	// This ensures human_feedback is available for the planning agent to use
	if baseOrchestrator != nil {
		toolsToRegister := baseOrchestrator.WorkspaceTools
		executorsToUse := baseOrchestrator.WorkspaceToolExecutors

		if toolsToRegister != nil && executorsToUse != nil {
			// Wrap executors and enhance tool descriptions with folder guard
			toolsToRegister, wrappedExecutors := baseOrchestrator.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

			logger.Info(fmt.Sprintf("🔧 Registering %d workspace tools (including human_feedback) for planning agent", len(toolsToRegister)))

			for _, tool := range toolsToRegister {
				if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
					var params map[string]interface{}
					if tool.Function.Parameters != nil {
						paramsBytes, err := json.Marshal(tool.Function.Parameters)
						if err == nil {
							json.Unmarshal(paramsBytes, &params)
						}
					}
					if params == nil {
						logger.Warn(fmt.Sprintf("⚠️ Failed to convert parameters for tool %s", tool.Function.Name))
						continue
					}

					if toolExecutor, ok := executor.(func(context.Context, map[string]interface{}) (string, error)); ok {
						if err := mcpAgent.RegisterCustomTool(
							tool.Function.Name,
							tool.Function.Description,
							params,
							toolExecutor,
							"workspace",
						); err != nil {
							logger.Warn(fmt.Sprintf("⚠️ Failed to register workspace tool %s: %v", tool.Function.Name, err))
							// Continue with other tools even if one fails
						} else {
							logger.Debug(fmt.Sprintf("✅ Registered workspace tool: %s", tool.Function.Name))
						}
					}
				}
			}
			logger.Info(fmt.Sprintf("✅ Registered workspace tools for planning agent"))
		}
	}

	// Register all plan modification tools using shared function
	if err := registerPlanModificationTools(mcpAgent, workspacePath, logger, readFile, writeFile, "planning agent"); err != nil {
		return nil, nil, err
	}

	// Generate system prompt for update mode
	systemPrompt := planningSystemPromptProcessorForUpdate(templateVars)

	// Execute the agent with normal Execute (not StructuredOutputViaTool)
	_, updatedHistory, err := baseAgent.Execute(ctx, userMessage, conversationHistory, systemPrompt, false)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf(fmt.Sprintf("agent execution failed: %w", err), nil)
	}

	// Check if any of our custom tools were called
	toolCalls := extractToolCallsFromMessages(updatedHistory)
	planUpdateToolCalled := false
	for _, toolName := range toolCalls {
		if toolName == "update_plan_steps" || toolName == "delete_plan_steps" || toolName == "add_plan_steps" ||
			toolName == "convert_step_to_conditional" || toolName == "add_branch_steps" || toolName == "update_branch_steps" ||
			toolName == "delete_branch_steps" || toolName == "update_conditional_step" || toolName == "convert_conditional_to_regular" {
			planUpdateToolCalled = true
		}
	}

	// Read the current plan.json (whether tools were called or not)
	// In UPDATE mode, conversational responses are normal - not an error
	// If tools were called, plan.json was updated. If not, we return the current plan unchanged.
	currentPlan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		return nil, updatedHistory, fmt.Errorf(fmt.Sprintf("failed to read plan: %w", err), nil)
	}

	if !planUpdateToolCalled {
		// No tools called - this is a normal conversational response, not an error
		// Return the current plan (unchanged) so conversation can continue
		logger.Info(fmt.Sprintf("📝 Planning agent in UPDATE mode: Conversational response (no plan changes). Returning current plan."))
		return currentPlan, updatedHistory, nil
	}

	// Tools were called - plan.json was updated
	logger.Info(fmt.Sprintf("✅ Plan updated via tools (%d steps)", len(currentPlan.Steps)))

	// Emit event to notify frontend that plan was updated
	if baseOrchestrator != nil {
		CheckAndEmitPlanUpdateEvent(ctx, baseOrchestrator, updatedHistory, workspacePath, readFile)
	}

	return currentPlan, updatedHistory, nil
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

		// Write changelog entry
		branchStepIDs := make([]string, 0)
		for _, step := range ifTrueSteps {
			branchStepIDs = append(branchStepIDs, step.ID)
		}
		for _, step := range ifFalseSteps {
			branchStepIDs = append(branchStepIDs, step.ID)
		}
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

		// Add steps to the appropriate branch
		if branchType == "if_true" {
			parentStep.IfTrueSteps = append(parentStep.IfTrueSteps, newSteps...)
		} else {
			parentStep.IfFalseSteps = append(parentStep.IfFalseSteps, newSteps...)
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		newBranchStepIDs := make([]string, 0, len(newSteps))
		for _, step := range newSteps {
			newBranchStepIDs = append(newBranchStepIDs, step.ID)
		}
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
		updatedBranchStepIDs := make([]string, 0, len(partialUpdates))
		for _, update := range partialUpdates {
			updatedBranchStepIDs = append(updatedBranchStepIDs, update.ExistingStepID)
		}
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

		// Write changelog entry
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

		conditionQuestion, _ := args["condition_question"].(string) // Optional
		conditionContext, _ := args["condition_context"].(string)   // Optional

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

		// Update conditional properties (only if provided)
		if conditionQuestion != "" {
			conditionalStep.ConditionQuestion = conditionQuestion
		}
		if conditionContext != "" {
			conditionalStep.ConditionContext = conditionContext
		}

		// Write updated plan
		if err := writePlanToFile(ctx, workspacePath, plan, readFile, writeFile, logger); err != nil {
			return "", fmt.Errorf(fmt.Sprintf("failed to write plan: %w", err), nil)
		}

		// Write changelog entry
		changedFields := []string{}
		if conditionQuestion != "" {
			changedFields = append(changedFields, "condition_question")
		}
		if conditionContext != "" {
			changedFields = append(changedFields, "condition_context")
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

		// Write changelog entry
		detailsJSON, _ := json.Marshal(map[string]interface{}{
			"step_id": stepID,
		})
		changelogEntry := PlanChangeLogEntry{
			Timestamp:   time.Now().Format(time.RFC3339),
			ChangeType:  "convert_to_regular",
			StepIDs:     []string{stepID},
			Description: fmt.Sprintf("Converted conditional step '%s' back to regular step", conditionalStep.Title),
			Details:     string(detailsJSON),
		}
		if err := writeChangelogEntry(ctx, workspacePath, changelogEntry, readFile, writeFile, logger); err != nil {
			logger.Warn(fmt.Sprintf("⚠️ Failed to write changelog entry: %v", err))
		}

		logger.Info(fmt.Sprintf("✅ Converted conditional step '%s' back to regular step", conditionalStep.Title))
		return fmt.Sprintf("Successfully converted conditional step '%s' back to regular step", conditionalStep.Title), nil
	}
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

// Execute implements the OrchestratorAgent interface (kept for compatibility, but ExecuteStructured should be used)
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// This method is kept for interface compatibility but is not used in the current implementation.
	// The controller uses ExecuteStructured instead.
	// Return a simple user message that will work if this method is ever called
	userMessage := "Generate plan"
	if humanFeedback := templateVars["HumanFeedback"]; humanFeedback != "" && strings.TrimSpace(humanFeedback) != "" {
		userMessage = humanFeedback
	}

	result, updatedHistory, err := hctppa.ExecuteStructured(ctx, templateVars, conversationHistory, userMessage)
	if err != nil {
		return "", conversationHistory, err
	}

	// Convert structured response to string for compatibility
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", conversationHistory, fmt.Errorf(fmt.Sprintf("failed to marshal structured response: %w", err), nil)
	}

	return string(resultJSON), updatedHistory, nil
}

// planningSystemPromptProcessor routes to appropriate system prompt based on whether updating or creating
func planningSystemPromptProcessor(templateVars map[string]string) string {
	// Check if we're updating an existing plan
	if existingPlanJSON := templateVars["ExistingPlanJSON"]; existingPlanJSON != "" && strings.TrimSpace(existingPlanJSON) != "" {
		return planningSystemPromptProcessorForUpdate(templateVars)
	}
	return planningSystemPromptProcessorForCreate(templateVars)
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
- **Responsibility**: Generate a comprehensive structured plan in JSON format to execute the objective
- **Output Format**: Structured JSON (not markdown, not files)
- **CRITICAL**: Your only output method is calling the 'submit_planning_response' tool with structured JSON data

## ⚡ QUICK REFERENCE

**Top 3 Rules**:
1. **Output**: Only use 'submit_planning_response' tool - no markdown, no files
2. **Success Criteria**: Reference file names only (e.g., 'step_1_results.md'), NOT paths
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
- "File 'step_1_results.md' exists and contains 'Deployment successful' status"
- "File 'config.json' exists and contains 'status: active' field"
- "Context output file 'step_2_output.md' contains '10 databases found' and lists all database names"
- "File 'deployment_log.md' exists and contains 'All pods running' confirmation"
- "File 'minute_calculation_result.md' exists and contains the correct calculation result"

**❌ BAD Examples** (vague or path-based):
- "Task completed successfully" (too vague, no file reference)
- "Deployment is working" (not verifiable through files)
- "All requirements met" (no specific file or indicator)
- "File exists in execution/step-2-if_true-0/" (path may vary - use file name only)
- "File in execution/step-1/ folder" (don't specify folder paths)

### Requirements for All Steps

**File Reference**:
- Must reference context_output file name (not path) or other file names
- Use file name only (e.g., 'step_1_results.md', 'config.json')
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

### Planning Guidelines

**Context Dependencies**:
- Specify context files needed from previous steps
- Use empty array [] if no dependencies
- Reference by file name only (e.g., 'step_1_results.md')
- Execution agents will locate files automatically

**Context Output**:
- Specify context file name to create for subsequent steps
- Example: 'step_1_results.md'
- Execution agents will write to appropriate step folder
- Use relative paths only - NEVER use absolute paths

## 📤 OUTPUT REQUIREMENTS

**CRITICAL**: 
- Call submit_planning_response tool with structured JSON data when plan is complete
- Output is ONLY via the submit_planning_response tool
- Do NOT include success_patterns/failure_patterns, or add markdown formatting - just pure JSON
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
- **Tools**: Use human_feedback tool to confirm changes, then use update_plan_steps, delete_plan_steps, and add_plan_steps tools to modify the plan. These tools update plan.json immediately when called.

## ⚡ QUICK REFERENCE

**Top 3 Rules**:
1. **Human Confirmation**: ALWAYS use human_feedback tool FIRST before making any plan changes
2. **Success Criteria**: Reference file names only (e.g., 'step_1_results.md'), NOT paths
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

**add_plan_steps**:
- **Purpose**: Add new steps to the plan
- **Required Fields**: title, description, success_criteria, has_loop, insert_after_step_id
- **CRITICAL**: insert_after_step_id is REQUIRED
  - Use step's id field from plan
  - Use empty string "" to insert at beginning
- **Optional**: Can include prerequisite fields if step depends on expiring resources
- **Order**: Multiple steps with same insert_after_step_id inserted in array order
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
- "File 'step_1_results.md' exists and contains 'Deployment successful' status"
- "File 'config.json' exists and contains 'status: active' field"
- "Context output file 'step_2_output.md' contains '10 databases found' and lists all database names"
- "File 'deployment_log.md' exists and contains 'All pods running' confirmation"
- "File 'minute_calculation_result.md' exists and contains the correct calculation result"

**❌ BAD Examples** (vague or path-based):
- "Task completed successfully" (too vague, no file reference)
- "Deployment is working" (not verifiable through files)
- "All requirements met" (no specific file or indicator)
- "File exists in execution/step-2-if_true-0/" (path may vary - use file name only)
- "File in execution/step-1/ folder" (don't specify folder paths)

### Requirements for All Steps

**File Reference**:
- Must reference context_output file name (not path) or other file names
- Use file name only (e.g., 'step_1_results.md', 'config.json')
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

### Planning Guidelines

**Context Dependencies**:
- Specify context files needed from previous steps
- Use empty array [] if no dependencies
- Reference by file name only (e.g., 'step_1_results.md')
- Execution agents will locate files automatically

**Context Output**:
- Specify context file name to create for subsequent steps
- Example: 'step_1_results.md'
- Execution agents will write to appropriate step folder
- Use relative paths only - NEVER use absolute paths

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

**CRITICAL**: Never call update_plan_steps, delete_plan_steps, or add_plan_steps without first getting user confirmation via human_feedback tool.
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
