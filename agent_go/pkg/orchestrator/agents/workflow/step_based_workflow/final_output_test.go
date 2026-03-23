package step_based_workflow

import "testing"

func TestParseWorkflowOutputPlanSupportsFlatSingleStepShape(t *testing.T) {
	content := `{
  "id": "report-summary",
  "title": "HRMS Sync Execution Summary",
  "instructions": "Create a report",
  "output_filename": "final_output.md",
  "enabled": true
}`

	plan, err := ParseWorkflowOutputPlan(content)
	if err != nil {
		t.Fatalf("ParseWorkflowOutputPlan returned error: %v", err)
	}
	if plan == nil || plan.Step == nil {
		t.Fatalf("expected parsed plan with primary step, got %#v", plan)
	}
	if plan.Step.ID != "report-summary" {
		t.Fatalf("expected step id report-summary, got %q", plan.Step.ID)
	}
	if !plan.Step.Enabled {
		t.Fatalf("expected parsed step to be enabled")
	}
	if got := plan.Step.OutputFilename; got != "final_output.md" {
		t.Fatalf("expected output filename final_output.md, got %q", got)
	}
}
