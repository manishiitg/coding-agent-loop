package step_based_workflow

import (
	"encoding/json"
)

// EvaluationStep represents a single step in an evaluation plan.
// It implements PlanStepInterface to reuse existing execution infrastructure.
type EvaluationStep struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	PreValidation   *ValidationSchema `json:"pre_validation,omitempty"`
	SuccessCriteria string            `json:"success_criteria"`
	AgentConfigs    *AgentConfigs     `json:"-"`                      // runtime config
	ContextOutput   string            `json:"context_output,omitempty"` // Filename of output produced by the step
	// DBWrite grants this evaluation step write access to db/. Read is always allowed.
	// Off by default: evaluation typically reads db/ to score against real state, and its
	// own writes stay in the sandbox run folder. Set true only for workflows where the eval
	// step is the canonical data producer (the builder prompt warns about this).
	// See docs/workflow/persistent_stores_design.md section 1.
	DBWrite bool `json:"db_write,omitempty"`
}

// Implement PlanStepInterface for EvaluationStep

func (e *EvaluationStep) GetID() string                           { return e.ID }
func (e *EvaluationStep) GetTitle() string                        { return e.Title }
func (e *EvaluationStep) GetDescription() string                  { return e.Description }
func (e *EvaluationStep) GetSuccessCriteria() string              { return e.SuccessCriteria }
func (e *EvaluationStep) GetContextDependencies() []string        { return nil }
func (e *EvaluationStep) GetContextOutput() FlexibleContextOutput { return FlexibleContextOutput(e.ContextOutput) }
func (e *EvaluationStep) GetValidationSchema() *ValidationSchema   { return e.PreValidation }
func (e *EvaluationStep) StepType() StepType                       { return StepTypeRegular }

func (e *EvaluationStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:               e.ID,
		Title:            e.Title,
		Description:      e.Description,
		SuccessCriteria:  e.SuccessCriteria,
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

// EvaluationStepScore represents the score for a single evaluation step
type EvaluationStepScore struct {
	StepID          string             `json:"step_id"`
	StepTitle       string             `json:"step_title"`
	Score           int                `json:"score"`
	MaxScore        int                `json:"max_score"`
	Reasoning       string             `json:"reasoning"`
	Evidence        string             `json:"evidence"`
	SuccessCriteria string             `json:"success_criteria"`
	ContextOutput   string             `json:"context_output,omitempty"`
	OutputContent   *StepOutputContent `json:"output_content,omitempty"`
}

// EvaluationReport represents the final evaluation report with all scores
type EvaluationReport struct {
	TargetRunFolder string                 `json:"target_run_folder"`
	GeneratedAt     string                 `json:"generated_at"`
	TotalScore      int                    `json:"total_score"`
	MaxPossibleScore int                   `json:"max_possible_score"`
	ScorePercentage float64                `json:"score_percentage"`
	StepScores      []*EvaluationStepScore `json:"step_scores"`
	Summary         string                 `json:"summary"`
}
