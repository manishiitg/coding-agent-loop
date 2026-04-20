package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

type evaluationCapabilityExample struct {
	Name string `json:"name"`
	JSON string `json:"json"`
}

type evaluationCapabilitiesResult struct {
	FilePath                 string                      `json:"file_path"`
	ValidationTool           string                      `json:"validation_tool"`
	ExecutionTool            string                      `json:"execution_tool"`
	ScoringConfigFile        string                      `json:"scoring_config_file"`
	RequiredStepFields       []string                    `json:"required_step_fields"`
	OptionalStepFields       []string                    `json:"optional_step_fields"`
	StepModeGuidance         map[string]string           `json:"step_mode_guidance"`
	WorkflowRules            []string                    `json:"workflow_rules"`
	TargetRunPathPlaceholder string                      `json:"target_run_path_placeholder"`
	ScoringAgentOptions      map[string]string           `json:"scoring_agent_options"`
	Examples                 []evaluationCapabilityExample `json:"examples"`
}

func getEvaluationCapabilities() evaluationCapabilitiesResult {
	return evaluationCapabilitiesResult{
		FilePath:          "evaluation/evaluation_plan.json",
		ValidationTool:    "validate_evaluation_plan",
		ExecutionTool:     "run_full_evaluation",
		ScoringConfigFile: "evaluation/step_config.json",
		RequiredStepFields: []string{
			"id", "title", "description",
		},
		OptionalStepFields: []string{
			"pre_validation",
		},
		StepModeGuidance: map[string]string{
			"learn_code": "Prefer for deterministic checks and stable scoring logic.",
			"code_exec":  "Prefer for adaptive reasoning or cases that still need model judgment.",
		},
		WorkflowRules: []string{
			"focus eval steps on workflow outcomes, not intermediate files, unless a file check is truly the outcome",
			"after editing evaluation/evaluation_plan.json always call validate_evaluation_plan",
			"run_full_evaluation always scores against the current workflow run (iteration-0, optionally iteration-0/<group>)",
			"eval step IDs must not collide with execution-plan step IDs because they share learnings/{stepID}/",
			"pre_validation checks files inside the eval step execution folder, not the original run folder",
		},
		TargetRunPathPlaceholder: "{{TARGET_RUN_PATH}}",
		ScoringAgentOptions: map[string]string{
			"use_code_execution_mode_false": "Set __evaluation_scoring__.agent_configs.use_code_execution_mode=false for lean tool-call scoring when supported.",
			"use_code_execution_mode_true":  "Set __evaluation_scoring__.agent_configs.use_code_execution_mode=true to force code-exec on non-CLI providers.",
			"declared_execution_mode_learn_code": "Set __evaluation_scoring__.agent_configs.declared_execution_mode=learn_code to generate and reuse a deterministic scoring script.",
		},
		Examples: []evaluationCapabilityExample{
			{
				Name: "Minimal evaluation plan",
				JSON: "{\n  \"steps\": [\n    {\n      \"id\": \"verify-final-output\",\n      \"title\": \"Verify final output\",\n      \"description\": \"Check that the workflow produced the required final output in the target system.\"\n    }\n  ]\n}",
			},
			{
				Name: "Evaluation step with pre_validation",
				JSON: "{\n  \"steps\": [\n    {\n      \"id\": \"score-report\",\n      \"title\": \"Score generated report\",\n      \"description\": \"Read artifacts under {{TARGET_RUN_PATH}} and score whether the final report meets the success criteria.\",\n      \"pre_validation\": {\n        \"type\": \"object\",\n        \"required\": [\"evaluation_report.json\"]\n      }\n    }\n  ]\n}",
			},
			{
				Name: "Scoring agent config",
				JSON: "{\n  \"id\": \"__evaluation_scoring__\",\n  \"agent_configs\": {\n    \"use_code_execution_mode\": false,\n    \"declared_execution_mode\": \"learn_code\"\n  }\n}",
			},
		},
	}
}

// checkExistingEvaluationPlan reads and parses an evaluation plan from the workspace.
// Used by evaluation_execution.go (live evaluation-execution phase).
func (hcpo *StepBasedWorkflowOrchestrator) checkExistingEvaluationPlan(ctx context.Context, planPath string) (bool, *EvaluationPlan, error) {
	content, err := hcpo.ReadWorkspaceFile(ctx, planPath)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return false, nil, nil
		}
		return false, nil, err
	}

	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return false, nil, err
	}
	return true, &plan, nil
}

// readEvaluationPlanFromFile reads evaluation_plan.json from the workspace.
// The workspace read API expects paths relative to the workspace-docs root, so
// workflow-relative paths must be normalized via normalizePathForWorkspaceAPI
// (the same pattern used by readPlanFromFile).
func readEvaluationPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*EvaluationPlan, error) {
	planPath := normalizePathForWorkspaceAPI(filepath.Join("evaluation", "evaluation_plan.json"), workspacePath)
	content, err := readFile(ctx, planPath)
	if err != nil {
		return nil, err
	}
	var plan EvaluationPlan
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, err
	}
	return &plan, nil
}

// validateCrossPlanStepIDUniqueness loads the evaluation plan (if present) and
// verifies no eval step IDs collide with execution plan IDs. The eval plan is
// optional — a workflow without evaluation is not an error.
//
// This guards the shared learnings/{stepID}/ namespace: both exec and eval
// steps resolve to the same folder, so duplicate IDs silently clobber saved
// scripts and metadata.
func validateCrossPlanStepIDUniqueness(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	execPlan *PlanningResponse,
) error {
	if execPlan == nil {
		return nil
	}
	evalPlan, err := readEvaluationPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return fmt.Errorf("failed to read evaluation plan for cross-check: %w", err)
	}
	if evalPlan == nil || len(evalPlan.Steps) == 0 {
		return nil
	}

	execIDs := make(map[string]string)
	if err := collectStepIDsRecursive(execPlan.Steps, "plan.steps", execIDs); err != nil {
		return err
	}
	if err := collectStepIDsRecursive(execPlan.OrphanSteps, "plan.orphan_steps", execIDs); err != nil {
		return err
	}

	evalIDs := make(map[string]string)
	for i, step := range evalPlan.Steps {
		if step == nil || strings.TrimSpace(step.ID) == "" {
			continue
		}
		loc := fmt.Sprintf("evaluation_plan.steps[%d] (title: %q)", i, step.Title)
		if prev, dup := evalIDs[step.ID]; dup {
			return fmt.Errorf("duplicate step ID %q in evaluation plan: first at %s, again at %s", step.ID, prev, loc)
		}
		if execLoc, collision := execIDs[step.ID]; collision {
			return fmt.Errorf("step ID %q collides across plans: %s (execution) and %s (evaluation) — both map to learnings/%s/, which would silently clobber saved scripts", step.ID, execLoc, loc, step.ID)
		}
		evalIDs[step.ID] = loc
	}
	return nil
}

// registerEvaluationValidationTools registers the validate_evaluation_plan tool on an MCP agent.
// Used by planning_exports.go for workflow-builder chat sessions.
func registerEvaluationValidationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
) error {
	validationSchema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	validationParams, _ := parseSchemaForToolParameters(validationSchema)

	mcpAgent.RegisterCustomTool(
		"validate_evaluation_plan",
		"Validate evaluation/evaluation_plan.json after editing it via shell/file tools.",
		validationParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			plan, err := readEvaluationPlanFromFile(ctx, workspacePath, readFile)
			if err != nil {
				return "", fmt.Errorf("failed to read evaluation/evaluation_plan.json: %w", err)
			}

			if len(plan.Steps) == 0 {
				return "Evaluation plan is valid JSON, but no evaluation steps are configured.", nil
			}

			// Cross-check eval step IDs against execution plan IDs. Both namespaces
			// share learnings/{stepID}/ folders, so collisions silently clobber.
			if execPlan, err := readPlanFromFile(ctx, workspacePath, readFile); err == nil && execPlan != nil {
				if err := validateCrossPlanStepIDUniqueness(ctx, workspacePath, readFile, execPlan); err != nil {
					return "", err
				}
			}

			seenIDs := make(map[string]struct{}, len(plan.Steps))
			for idx, step := range plan.Steps {
				if step == nil {
					return "", fmt.Errorf("evaluation step %d is null", idx+1)
				}
				if strings.TrimSpace(step.ID) == "" {
					return "", fmt.Errorf("evaluation step %d is missing id", idx+1)
				}
				if _, exists := seenIDs[step.ID]; exists {
					return "", fmt.Errorf("duplicate evaluation step id %q", step.ID)
				}
				seenIDs[step.ID] = struct{}{}
				if strings.TrimSpace(step.Title) == "" {
					return "", fmt.Errorf("evaluation step %q is missing title", step.ID)
				}
				if strings.TrimSpace(step.Description) == "" {
					return "", fmt.Errorf("evaluation step %q is missing description", step.ID)
				}
				if step.PreValidation != nil {
					if err := validateRegexPatternsInSchema(step.PreValidation); err != nil {
						return "", fmt.Errorf("evaluation step %q has invalid pre_validation regex: %w", step.ID, err)
					}
					if err := validateJSONPathSyntax(step.PreValidation); err != nil {
						return "", fmt.Errorf("evaluation step %q has invalid pre_validation jsonpath: %w", step.ID, err)
					}
					if err := validateArrayLengthConsistencyChecks(step.PreValidation); err != nil {
						return "", fmt.Errorf("evaluation step %q has invalid pre_validation consistency checks: %w", step.ID, err)
					}
				}
			}

			normalized, err := json.MarshalIndent(plan, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal normalized evaluation plan: %w", err)
			}

			return fmt.Sprintf(
				"Evaluation plan is valid.\nsteps: %d\nnormalized_plan:\n%s",
				len(plan.Steps),
				string(normalized),
			), nil
		},
		"workflow",
	)

	logger.Info("✅ Registered evaluation plan validation tool")
	return nil
}

func registerEvaluationCapabilitiesTool(
	mcpAgent *mcpagent.Agent,
	logger loggerv2.Logger,
) error {
	validationSchema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(validationSchema)

	mcpAgent.RegisterCustomTool(
		"get_evaluation_capabilities",
		"Get the evaluation-plan grammar and workflow-specific eval rules for evaluation/evaluation_plan.json. Call this before editing the eval plan or answering detailed eval-design questions. Returns required fields, optional fields, mode guidance, scoring-agent options, the {{TARGET_RUN_PATH}} rule, and compact examples. After editing the eval plan, still call validate_evaluation_plan and then run_full_evaluation when you want to test it.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			out, err := json.MarshalIndent(getEvaluationCapabilities(), "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal evaluation capabilities: %w", err)
			}
			return "evaluation capabilities loaded\n" + string(out), nil
		},
		"workflow",
	)

	logger.Info("✅ Registered evaluation capabilities tool")
	return nil
}
