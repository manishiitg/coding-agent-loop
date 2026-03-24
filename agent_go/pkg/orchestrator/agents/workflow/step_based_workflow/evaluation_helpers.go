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
func readEvaluationPlanFromFile(ctx context.Context, workspacePath string, readFile func(context.Context, string) (string, error)) (*EvaluationPlan, error) {
	planPath := filepath.Join("evaluation", "evaluation_plan.json")
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

// writeEvaluationPlanToFile writes evaluation_plan.json to the workspace.
func writeEvaluationPlanToFile(ctx context.Context, workspacePath string, plan *EvaluationPlan, writeFile func(context.Context, string, string) error) error {
	planPath := filepath.Join("evaluation", "evaluation_plan.json")
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(ctx, planPath, string(data))
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
				if strings.TrimSpace(step.SuccessCriteria) == "" {
					return "", fmt.Errorf("evaluation step %q is missing success_criteria", step.ID)
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
