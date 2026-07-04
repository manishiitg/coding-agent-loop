package step_based_workflow

import (
	"encoding/json"
)

// EvaluationStep represents a single step in an evaluation plan.
// It implements PlanStepInterface to reuse existing execution infrastructure.
//
// Note: success_criteria has been removed. The eval step's description should
// fully encode what passing/failing looks like (deterministic checks via
// scripted, or LLM judgment grounded in the description).
type EvaluationStep struct {
	ID              string                         `json:"id"`
	Title           string                         `json:"title"`
	Description     string                         `json:"description"`
	PreValidation   *ValidationSchema              `json:"pre_validation,omitempty"`
	AgentConfigs    *AgentConfigs                  `json:"-"`                        // runtime config
	ContextOutput   string                         `json:"context_output,omitempty"` // Filename of output produced by the step
	AppliesToRoutes []EvaluationRouteApplicability `json:"applies_to_routes,omitempty"`
	// DBWrite grants this evaluation step write access to db/. Read is always allowed.
	// Off by default: evaluation typically reads db/ to score against real state, and its
	// own writes stay in the sandbox run folder. Set true only for workflows where the eval
	// step is the canonical data producer (the builder prompt warns about this).
	// See docs/workflow/persistent_stores_design.md section 1.
	DBWrite bool `json:"db_write,omitempty"`
}

// EvaluationRouteApplicability gates an evaluation step to one or more selected
// routes from a routing step in the target workflow run. When set, the eval step
// only runs if the target run's routing-evaluation.json selected one of RouteIDs.
type EvaluationRouteApplicability struct {
	RoutingStepID string   `json:"routing_step_id"`
	RouteIDs      []string `json:"route_ids"`
}

// Implement PlanStepInterface for EvaluationStep

func (e *EvaluationStep) GetID() string                    { return e.ID }
func (e *EvaluationStep) GetTitle() string                 { return e.Title }
func (e *EvaluationStep) GetDescription() string           { return e.Description }
func (e *EvaluationStep) GetSuccessCriteria() string       { return "" } // dropped — see struct doc
func (e *EvaluationStep) GetContextDependencies() []string { return nil }
func (e *EvaluationStep) GetContextOutput() FlexibleContextOutput {
	return FlexibleContextOutput(e.ContextOutput)
}
func (e *EvaluationStep) GetValidationSchema() *ValidationSchema { return e.PreValidation }
func (e *EvaluationStep) StepType() StepType                     { return StepTypeRegular }

func (e *EvaluationStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:               e.ID,
		Title:            e.Title,
		Description:      e.Description,
		ValidationSchema: e.PreValidation,
		ContextOutput:    FlexibleContextOutput(e.ContextOutput),
	}
}

// MarshalJSON ensures the type field is always set when marshaling (if needed by frontend)
func (e *EvaluationStep) MarshalJSON() ([]byte, error) {
	type Alias EvaluationStep
	return json.Marshal(&struct {
		Type StepType `json:"type"`
		*Alias
	}{
		Type:  StepTypeRegular,
		Alias: (*Alias)(e),
	})
}

// EvaluationPlan represents the structured evaluation plan
type EvaluationPlan struct {
	Steps []*EvaluationStep `json:"steps"`
}

// UnmarshalJSON implements custom unmarshaling for EvaluationPlan
// Handles multiple formats:
// 1. {"steps": [...]} - expected format
// 2. {"eval_steps": [...]} - alternate key format
// 3. [...] - legacy format (array at top level)
func (ep *EvaluationPlan) UnmarshalJSON(data []byte) error {
	// First, try to unmarshal as object with "steps" or "eval_steps" field
	var temp struct {
		Steps     []*EvaluationStep `json:"steps"`
		EvalSteps []*EvaluationStep `json:"eval_steps"`
	}
	if err := json.Unmarshal(data, &temp); err == nil {
		if temp.Steps != nil {
			ep.Steps = temp.Steps
			return nil
		}
		if temp.EvalSteps != nil {
			ep.Steps = temp.EvalSteps
			return nil
		}
	}

	// If that fails, try to unmarshal as a top-level array (legacy format)
	var stepsArray []*EvaluationStep
	if err := json.Unmarshal(data, &stepsArray); err != nil {
		return err
	}
	ep.Steps = stepsArray
	return nil
}

// ToPlanSteps converts EvaluationPlan steps to PlanStepInterface slice
func (ep *EvaluationPlan) ToPlanSteps() []PlanStepInterface {
	steps := make([]PlanStepInterface, len(ep.Steps))
	for i, step := range ep.Steps {
		steps[i] = step
	}
	return steps
}

// StepOutputContent represents the content of a step's output file
type StepOutputContent struct {
	FilePath string      `json:"file_path"`
	Content  interface{} `json:"content"`
	IsJSON   bool        `json:"is_json"`
}

// EvaluationStepScore is the per-step entry in evaluation_report.json.
// The legacy score fields are retained only for old reports; new evaluation
// runs treat output_content as the source of truth.
// step_title and success_criteria are intentionally absent — UI consumers can
// look them up by step_id from evaluation_plan.json (the plan is loaded next
// to the report by the same API endpoint).
type EvaluationStepScore struct {
	StepID        string             `json:"step_id"`
	Score         int                `json:"score,omitempty"`
	MaxScore      int                `json:"max_score,omitempty"`
	Reasoning     string             `json:"reasoning"`
	Evidence      string             `json:"evidence"`
	Skipped       bool               `json:"skipped,omitempty"`
	ContextOutput string             `json:"context_output,omitempty"`
	OutputContent *StepOutputContent `json:"output_content,omitempty"`
}

// EvaluationReport captures eval step outputs for a target run. The aggregate
// score fields are legacy-only; new reports omit them because there is no final
// scoring agent.
type EvaluationReport struct {
	TargetRunFolder  string                 `json:"target_run_folder"`
	GeneratedAt      string                 `json:"generated_at"`
	TotalScore       int                    `json:"total_score,omitempty"`
	MaxPossibleScore int                    `json:"max_possible_score,omitempty"`
	ScorePercentage  float64                `json:"score_percentage,omitempty"`
	StepScores       []*EvaluationStepScore `json:"step_scores"`
}

// EvaluationReportFileName is the filename the Go report phase writes the assembled
// report to inside the eval run folder. Kept as a constant so the validation schema
// and the report writer use the same path.
const EvaluationReportFileName = "evaluation_report.json"

// BuildEvaluationReportValidationSchema returns a fixed pre-validation schema for the
// assembled evaluation report JSON. Same shape as any step's validation_schema, so it flows
// through the existing RunPreValidation engine. Validates per-step structure (score
// range 0-10, min text lengths for reasoning/evidence) and pins the step_scores array
// length to numSteps.
func BuildEvaluationReportValidationSchema(numSteps int) *ValidationSchema {
	intPtr := func(v int) *int { return &v }
	floatPtr := func(v float64) *float64 { return &v }

	checks := []JSONValidationCheck{
		{Path: "$.step_scores", MustExist: true, ValueType: "array",
			MinLength: intPtr(numSteps), MaxLength: intPtr(numSteps)},
		{Path: "$.step_scores[*].step_id", MustExist: true, ValueType: "string"},
		{Path: "$.step_scores[*].score", MustExist: true, ValueType: "number",
			MinValue: floatPtr(0), MaxValue: floatPtr(10)},
		{Path: "$.step_scores[*].reasoning", MustExist: true, ValueType: "string", MinLength: intPtr(20)},
		{Path: "$.step_scores[*].evidence", MustExist: true, ValueType: "string", MinLength: intPtr(10)},
	}

	return &ValidationSchema{
		Files: []FileValidationRule{{
			FileName:   EvaluationReportFileName,
			MustExist:  true,
			JSONChecks: checks,
		}},
	}
}
