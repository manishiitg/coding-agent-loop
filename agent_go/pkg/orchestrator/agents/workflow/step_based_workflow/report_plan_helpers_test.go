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

// Regression: theme and section.Layout were silently stripped on every plan
// mutation because normalizeReportPlanDocument reconstructed sections from a
// hand-written field list. set_report_theme and set_section_layout would
// successfully save, then any later upsert/move/toggle/remove would wipe them.
func TestNormalizeReportPlanPreservesThemeAndLayout(t *testing.T) {
	t.Parallel()

	doc := &reportPlanDocument{
		Version: 1,
		Theme:   "brand",
		Sections: []reportPlanDocumentSection{{
			ID:      "section-01-overview",
			Heading: "Overview",
			Layout: &reportPlanDocumentSectionLayout{
				Columns: 12,
				Gap:     16,
			},
			Entries: []reportPlanDocumentEntry{{
				ID:   "section-01-overview-entry-01",
				Kind: "single",
				Widget: &reportPlanDocumentWidget{
					Kind:   "stat",
					Source: "db/strategies.json",
					Path:   "active_strategies",
					Layout: &reportPlanDocumentWidgetLayout{Span: 6},
				},
			}},
		}},
	}

	out := normalizeReportPlanDocument(doc)

	if out.Theme != "brand" {
		t.Fatalf("theme stripped: got %q, want %q", out.Theme, "brand")
	}
	if len(out.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(out.Sections))
	}
	section := out.Sections[0]
	if section.Layout == nil {
		t.Fatalf("section.Layout stripped")
	}
	if section.Layout.Columns != 12 || section.Layout.Gap != 16 {
		t.Fatalf("section.Layout corrupted: got %+v, want columns=12 gap=16", section.Layout)
	}
	if len(section.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(section.Entries))
	}
	widget := section.Entries[0].Widget
	if widget == nil {
		t.Fatalf("widget dropped")
	}
	if widget.Layout == nil || widget.Layout.Span != 6 {
		t.Fatalf("widget.Layout stripped: got %+v", widget.Layout)
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
