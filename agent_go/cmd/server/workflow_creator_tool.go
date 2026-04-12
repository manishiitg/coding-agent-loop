package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
)

// kebabCaseWorkflowName matches a kebab-case workflow folder name:
// lowercase letters and digits, separated by single hyphens, starting with a letter.
var kebabCaseWorkflowName = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// registerWorkflowCreatorTool registers the create_workflow tool on the multi-agent chat agent.
// This tool is the privileged path for creating new workflow folders — it bypasses the
// session's Chats/ folder guard by writing directly via os.WriteFile into the shared
// workspace-docs root. The handler enforces:
//   - kebab-case name validation
//   - required-field validation for workflow.json and plan.json
//   - no-overwrite of existing workflows
//
// Registered only for multi-agent chat (not workflow phase).
func (api *StreamingAPI) registerWorkflowCreatorTool(underlyingAgent *mcpagent.Agent) error {
	if underlyingAgent == nil {
		return fmt.Errorf("underlying agent is nil")
	}

	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"folder_name": map[string]interface{}{
				"type":        "string",
				"description": "Shell-safe folder name under Workflow/ — kebab-case (lowercase letters, digits, hyphens only). No spaces, no underscores, no uppercase, no special characters. Examples: 'customer-onboarding', 'sales-report', 'api-health-check'. This is ONLY the on-disk folder name so shell commands like `ls Workflow/<folder_name>/` work without quoting. The human-readable display name goes in workflow_json.label and can be any string.",
			},
			"workflow_json": map[string]interface{}{
				"type":                 "object",
				"description":          "The full workflow.json manifest object. Required fields: schema_version (int, 1), id (string, e.g. 'wf_<folder_name>'), label (string, free-form human-readable name — can contain spaces, capitalization, anything). Should include objective, success_criteria, and a capabilities object with selected_servers/skills/etc picked smartly from the current chat context.",
				"additionalProperties": true,
			},
			"plan_json": map[string]interface{}{
				"type":                 "object",
				"description":          "The full plan.json object. Required field: steps (array, at least 1 step). Each step needs type, id (kebab-case, unique), and title. Should also include objective and success_criteria at the root.",
				"additionalProperties": true,
			},
		},
		"required": []string{"folder_name", "workflow_json", "plan_json"},
	}

	description := "Create a new workflow at Workflow/<folder_name>/ with the given workflow.json and planning/plan.json. This is the ONLY way to write under Workflow/ — the multi-agent chat folder guard blocks direct shell writes there. folder_name must be kebab-case (shell-safe); the human-readable display name goes in workflow_json.label and can be any string. The tool enforces required JSON fields and refuses to overwrite existing workflows. Returns the folder path on success."

	return underlyingAgent.RegisterCustomTool(
		"create_workflow",
		description,
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			return api.handleWorkflowCreatorTool(ctx, args)
		},
		"workflow_creator",
	)
}

// handleWorkflowCreatorTool validates the arguments, creates the workflow folder, and writes
// workflow.json and planning/plan.json directly via the filesystem.
func (api *StreamingAPI) handleWorkflowCreatorTool(ctx context.Context, args map[string]interface{}) (string, error) {
	_ = ctx

	// 1. Validate folder_name (the on-disk path segment — must be shell-safe).
	// The human-readable display name lives in workflow_json.label and can be anything.
	folderName, _ := args["folder_name"].(string)
	folderName = strings.TrimSpace(folderName)
	if folderName == "" {
		return "", fmt.Errorf("folder_name is required")
	}
	if !kebabCaseWorkflowName.MatchString(folderName) {
		return "", fmt.Errorf("folder_name %q is not valid kebab-case — must be lowercase letters/digits separated by single hyphens, e.g. 'customer-onboarding' (the human-readable display name goes in workflow_json.label and can be any string)", folderName)
	}
	if len(folderName) > 64 {
		return "", fmt.Errorf("folder_name %q is too long (max 64 chars)", folderName)
	}

	// 2. Extract workflow_json and plan_json
	workflowRaw, hasWorkflow := args["workflow_json"]
	if !hasWorkflow {
		return "", fmt.Errorf("workflow_json is required")
	}
	planRaw, hasPlan := args["plan_json"]
	if !hasPlan {
		return "", fmt.Errorf("plan_json is required")
	}

	workflowMap, ok := workflowRaw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("workflow_json must be a JSON object")
	}
	planMap, ok := planRaw.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("plan_json must be a JSON object")
	}

	// 3. Validate workflow.json required fields
	if err := validateWorkflowJSONRequiredFields(workflowMap); err != nil {
		return "", err
	}

	// 4. Validate plan.json required fields
	if err := validatePlanJSONRequiredFields(planMap); err != nil {
		return "", err
	}

	// 5. Resolve filesystem paths (shared workspace root — Workflow/ is not per-user)
	docsPath := getWorkspaceDocsAbsPath()
	if docsPath == "" {
		return "", fmt.Errorf("workspace docs path is not configured")
	}
	workflowFolder := filepath.Join(docsPath, "Workflow", folderName)
	planningFolder := filepath.Join(workflowFolder, "planning")
	workflowJSONPath := filepath.Join(workflowFolder, "workflow.json")
	planJSONPath := filepath.Join(planningFolder, "plan.json")

	// 6. Refuse to overwrite existing workflows
	if _, err := os.Stat(workflowFolder); err == nil {
		return "", fmt.Errorf("workflow folder Workflow/%s already exists — pick a different folder_name or update the existing workflow via the workflow canvas", folderName)
	}

	// 7. Create folders
	if err := os.MkdirAll(planningFolder, 0755); err != nil {
		return "", fmt.Errorf("failed to create workflow folder: %w", err)
	}

	// 8. Marshal and write workflow.json
	workflowBytes, err := json.MarshalIndent(workflowMap, "", "  ")
	if err != nil {
		// Cleanup partial state on failure
		_ = os.RemoveAll(workflowFolder)
		return "", fmt.Errorf("failed to marshal workflow.json: %w", err)
	}
	if err := os.WriteFile(workflowJSONPath, workflowBytes, 0644); err != nil {
		_ = os.RemoveAll(workflowFolder)
		return "", fmt.Errorf("failed to write workflow.json: %w", err)
	}

	// 9. Marshal and write plan.json
	planBytes, err := json.MarshalIndent(planMap, "", "  ")
	if err != nil {
		_ = os.RemoveAll(workflowFolder)
		return "", fmt.Errorf("failed to marshal plan.json: %w", err)
	}
	if err := os.WriteFile(planJSONPath, planBytes, 0644); err != nil {
		_ = os.RemoveAll(workflowFolder)
		return "", fmt.Errorf("failed to write plan.json: %w", err)
	}

	log.Printf("[WORKFLOW CREATOR] Created new workflow: Workflow/%s (workflow.json=%d bytes, plan.json=%d bytes)", folderName, len(workflowBytes), len(planBytes))

	// 10. Collect step summary for the response
	stepSummary := summarizePlanSteps(planMap)

	result := map[string]interface{}{
		"folder_path":   fmt.Sprintf("Workflow/%s", folderName),
		"workflow_json": fmt.Sprintf("Workflow/%s/workflow.json", folderName),
		"plan_json":     fmt.Sprintf("Workflow/%s/planning/plan.json", folderName),
		"label":         workflowMap["label"],
		"objective":     workflowMap["objective"],
		"step_count":    stepSummary.count,
		"steps":         stepSummary.items,
		"message":       fmt.Sprintf("Workflow Workflow/%s/ created. The user can activate it from the workflow picker.", folderName),
	}

	resultJSON, marshalErr := json.MarshalIndent(result, "", "  ")
	if marshalErr != nil {
		return fmt.Sprintf("%v", result), nil
	}
	return string(resultJSON), nil
}

// validateWorkflowJSONRequiredFields checks that workflow.json has the bare minimum
// fields that the WorkflowManifest struct requires on unmarshal.
func validateWorkflowJSONRequiredFields(m map[string]interface{}) error {
	if _, ok := m["schema_version"]; !ok {
		return fmt.Errorf("workflow_json missing required field: schema_version (int, must be 1)")
	}
	id, ok := m["id"].(string)
	if !ok || strings.TrimSpace(id) == "" {
		return fmt.Errorf("workflow_json missing required field: id (non-empty string, e.g. 'wf_<name>')")
	}
	label, ok := m["label"].(string)
	if !ok || strings.TrimSpace(label) == "" {
		return fmt.Errorf("workflow_json missing required field: label (non-empty string, human-readable name)")
	}
	return nil
}

// validatePlanJSONRequiredFields checks that plan.json has a non-empty steps array,
// each step has type/id/title, and every step id is unique and kebab-case.
func validatePlanJSONRequiredFields(m map[string]interface{}) error {
	stepsRaw, ok := m["steps"]
	if !ok {
		return fmt.Errorf("plan_json missing required field: steps (array of at least 1 step)")
	}
	steps, ok := stepsRaw.([]interface{})
	if !ok {
		return fmt.Errorf("plan_json.steps must be an array")
	}
	if len(steps) == 0 {
		return fmt.Errorf("plan_json.steps must contain at least 1 step")
	}
	seenIDs := make(map[string]bool, len(steps))
	for i, stepRaw := range steps {
		step, ok := stepRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("plan_json.steps[%d] must be an object", i)
		}
		stepType, _ := step["type"].(string)
		if strings.TrimSpace(stepType) == "" {
			return fmt.Errorf("plan_json.steps[%d].type is required (e.g. 'regular', 'decision', 'conditional', 'routing', 'human_input', 'todo_task')", i)
		}
		stepID, _ := step["id"].(string)
		stepID = strings.TrimSpace(stepID)
		if stepID == "" {
			return fmt.Errorf("plan_json.steps[%d].id is required (kebab-case)", i)
		}
		if !kebabCaseWorkflowName.MatchString(stepID) {
			return fmt.Errorf("plan_json.steps[%d].id %q must be kebab-case (lowercase letters/digits separated by hyphens)", i, stepID)
		}
		if seenIDs[stepID] {
			return fmt.Errorf("plan_json.steps[%d].id %q is duplicated — each step id must be unique", i, stepID)
		}
		seenIDs[stepID] = true
		title, _ := step["title"].(string)
		if strings.TrimSpace(title) == "" {
			return fmt.Errorf("plan_json.steps[%d].title is required (human-readable title)", i)
		}
	}
	return nil
}

// planStepSummary captures a compact view of the plan's steps for the tool response.
type planStepSummary struct {
	count int
	items []map[string]string
}

// summarizePlanSteps returns an id+title list for the steps in the plan.
// Used in the create_workflow result so the manager can report back to the user
// without re-parsing its own plan_json argument.
func summarizePlanSteps(planMap map[string]interface{}) planStepSummary {
	steps, ok := planMap["steps"].([]interface{})
	if !ok {
		return planStepSummary{}
	}
	items := make([]map[string]string, 0, len(steps))
	for _, raw := range steps {
		step, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := step["id"].(string)
		title, _ := step["title"].(string)
		items = append(items, map[string]string{
			"id":    id,
			"title": title,
		})
	}
	return planStepSummary{
		count: len(items),
		items: items,
	}
}
