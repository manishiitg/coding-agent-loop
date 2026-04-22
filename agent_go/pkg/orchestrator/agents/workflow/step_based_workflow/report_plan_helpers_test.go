package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestValidateReportPlanStatArrayIsError(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/linkedin"
	files := map[string]string{
		"Workflow/linkedin/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Active Strategies",
								"source": "db/strategies.json",
								"path": "active_strategies",
								"format": "count"
							}
						}
					]
				}
			]
		}`,
		"Workflow/linkedin/db/strategies.json": `{"active_strategies":[{"id":"s-1"},{"id":"s-2"}]}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected at least one validation error, got none")
	}
	if !diagnosticsContain(result.Errors, "stat widgets require a scalar value") {
		t.Fatalf("expected scalar-value error, got %#v", result.Errors)
	}
	if !diagnosticsContain(result.Warnings, `unknown format preset "count"`) {
		t.Fatalf("expected unknown format warning for stat format, got %#v", result.Warnings)
	}
}

func TestValidateReportPlanStatUnknownFormatWarns(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/linkedin"
	files := map[string]string{
		"Workflow/linkedin/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Total Posts",
								"source": "db/summary.json",
								"path": "total_posts",
								"format": "count"
							}
						}
					]
				}
			]
		}`,
		"Workflow/linkedin/db/summary.json": `{"total_posts":14}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if !result.Valid {
		t.Fatalf("expected result.Valid=true, got false with errors %#v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no validation errors, got %#v", result.Errors)
	}
	if !diagnosticsContain(result.Warnings, `unknown format preset "count"`) {
		t.Fatalf("expected unknown format warning for stat format, got %#v", result.Warnings)
	}
}

func fakeReportPlanReadFile(files map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, path string) (string, error) {
		content, ok := files[path]
		if !ok {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return content, nil
	}
}

func diagnosticsContain(diags []reportPlanDiagnostic, want string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, want) {
			return true
		}
	}
	return false
}
