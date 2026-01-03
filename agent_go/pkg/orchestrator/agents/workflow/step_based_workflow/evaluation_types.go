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
	AgentConfigs    *AgentConfigs     `json:"-"` // runtime config
}

// Implement PlanStepInterface for EvaluationStep

func (e *EvaluationStep) GetID() string                           { return e.ID }
func (e *EvaluationStep) GetTitle() string                        { return e.Title }
func (e *EvaluationStep) GetDescription() string                  { return e.Description }
func (e *EvaluationStep) GetSuccessCriteria() string              { return e.SuccessCriteria }
func (e *EvaluationStep) GetContextDependencies() []string        { return nil }
func (e *EvaluationStep) GetContextOutput() FlexibleContextOutput { return "" }
func (e *EvaluationStep) GetEnablePrerequisiteDetection() *bool {
	val := false
	return &val
}
func (e *EvaluationStep) GetPrerequisiteRules() []PrerequisiteRule { return nil }
func (e *EvaluationStep) GetValidationSchema() *ValidationSchema   { return e.PreValidation }
func (e *EvaluationStep) StepType() StepType                       { return StepTypeRegular }

func (e *EvaluationStep) GetCommonFields() CommonStepFields {
	return CommonStepFields{
		ID:              e.ID,
		Title:           e.Title,
		Description:     e.Description,
		SuccessCriteria: e.SuccessCriteria,
		ValidationSchema: e.PreValidation,
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
func (ep *EvaluationPlan) UnmarshalJSON(data []byte) error {
	var temp struct {
		Steps []*EvaluationStep `json:"steps"`
	}
	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}
	ep.Steps = temp.Steps
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
