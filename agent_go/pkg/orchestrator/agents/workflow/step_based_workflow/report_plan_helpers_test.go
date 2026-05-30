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

func TestValidateReportPlanJSONataQueryFeedsShapeValidation(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Paid Total",
								"source": "db/orders.json",
								"query": "$sum(rows[status='paid'].amount)",
								"format": "currency-usd"
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/orders.json": `{"rows":[{"status":"paid","amount":12.5},{"status":"open","amount":99},{"status":"paid","amount":7.5}]}`,
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
}

func TestValidateReportPlanJSONataQueryErrorIsDeterministicError(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Paid Total",
								"source": "db/orders.json",
								"query": "$sum(rows[status='paid'.amount)"
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/orders.json": `{"rows":[{"status":"paid","amount":12.5}]}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "JSONata query failed") {
		t.Fatalf("expected JSONata query failure, got %#v", result.Errors)
	}
}

func TestValidateReportPlanJSONataWrongShapeIsError(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Paid Rows",
								"source": "db/orders.json",
								"query": "rows[status='paid']"
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/orders.json": `{"rows":[{"status":"paid","amount":12.5},{"status":"paid","amount":7.5}]}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "stat widgets require a scalar value") {
		t.Fatalf("expected scalar-value error, got %#v", result.Errors)
	}
}

func TestValidateReportPlanJSONataCanJoinMultipleSources(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/planning/plan.json": `{
			"steps": [
				{
					"type": "regular",
					"id": "collect-runs",
					"title": "Collect Runs",
					"description": "Write run rows.",
					"context_dependencies": [],
					"context_output": "db/runs.json",
					"validation_schema": {
						"files": [
							{
								"file_name": "db/runs.json",
								"must_exist": true,
								"json_checks": [
									{"path": "$.rows", "must_exist": true, "value_type": "array"}
								]
							}
						]
					}
				},
				{
					"type": "regular",
					"id": "collect-costs",
					"title": "Collect Costs",
					"description": "Write cost rows.",
					"context_dependencies": [],
					"context_output": "db/costs.json",
					"validation_schema": {
						"files": [
							{
								"file_name": "db/costs.json",
								"must_exist": true,
								"json_checks": [
									{"path": "$.rows", "must_exist": true, "value_type": "array"}
								]
							}
						]
					}
				}
			]
		}`,
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "stat",
								"title": "Cost Total",
								"sources": {
									"runs": "db/runs.json",
									"costs": "db/costs.json"
								},
								"query": "{\"cost_total\": $sum(costs.rows.amount), \"run_count\": $count(runs.rows)}",
								"path": "cost_total",
								"format": "currency-usd"
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/runs.json":  `{"rows":[{"run_id":"r1"},{"run_id":"r2"}]}`,
		"Workflow/orders/db/costs.json": `{"rows":[{"run_id":"r1","amount":12.5},{"run_id":"r2","amount":7.5}]}`,
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
}

func TestValidateReportPlanErrorsWhenSourceHasNoValidationContract(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/planning/plan.json": `{
			"steps": [
				{
					"type": "regular",
					"id": "collect-orders",
					"title": "Collect Orders",
					"description": "Collect order data and write db/orders.json.",
					"context_dependencies": [],
					"context_output": "db/orders.json"
				}
			]
		}`,
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "table",
								"title": "Orders",
								"source": "db/orders.json",
								"path": "rows",
								"fields": ["status", "amount"]
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/orders.json": `{"rows":[{"status":"paid","amount":12.5}]}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "no validation_schema file rule declares") {
		t.Fatalf("expected missing producer contract error, got %#v", result.Errors)
	}
}

func TestValidateReportPlanErrorsWhenProducerContractMissesReportFields(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/planning/plan.json": `{
			"steps": [
				{
					"type": "regular",
					"id": "collect-orders",
					"title": "Collect Orders",
					"description": "Collect order data and write db/orders.json.",
					"context_dependencies": [],
					"context_output": "db/orders.json",
					"validation_schema": {
						"files": [
							{
								"file_name": "db/orders.json",
								"must_exist": true,
								"json_checks": [
									{"path": "$.rows", "must_exist": true, "value_type": "array"}
								]
							}
						]
					}
				}
			]
		}`,
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [
				{
					"heading": "Overview",
					"entries": [
						{
							"kind": "single",
							"widget": {
								"kind": "table",
								"title": "Orders",
								"source": "db/orders.json",
								"path": "rows",
								"fields": ["status", "amount"]
							}
						}
					]
				}
			]
		}`,
		"Workflow/orders/db/orders.json": `{"rows":[{"status":"paid","amount":12.5}]}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}

	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "does not mention report field") {
		t.Fatalf("expected missing report field error, got %#v", result.Errors)
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

// Locks in bracket / $-root path resolution so validate_report_plan and
// preview_report_render stay in parity with the frontend renderer. The classic
// alert form "$[0].login_success" must resolve against an array source rather
// than silently returning undefined (which used to keep the alert hidden).
func TestResolveReportPlanPathBracketAndRootSigil(t *testing.T) {
	t.Parallel()
	rows := []interface{}{
		map[string]interface{}{"login_success": false, "pan": "AAA"},
		map[string]interface{}{"login_success": true, "pan": "BBB"},
	}
	cases := []struct {
		path string
		data interface{}
		want interface{}
		ok   bool
	}{
		{"$[0].login_success", rows, false, true},
		{"$.0.pan", rows, "AAA", true},
		{"[1].pan", rows, "BBB", true},
		{"entities.0.label", map[string]interface{}{"entities": []interface{}{map[string]interface{}{"label": "x"}}}, "x", true},
		{"rows[0].login_success", map[string]interface{}{"rows": rows}, false, true},
		{"$[9].pan", rows, nil, false},
	}
	for _, c := range cases {
		got, ok := resolveReportPlanPath(c.data, c.path)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("resolveReportPlanPath(%q) = (%v, %v), want (%v, %v)", c.path, got, ok, c.want, c.ok)
		}
	}

	// show_if must now evaluate the same expression instead of failing open.
	if !reportPlanEvaluateShowIf(rows, "$[0].login_success == false") {
		t.Error(`show_if "$[0].login_success == false" should be true when row 0 login failed`)
	}
	if reportPlanEvaluateShowIf(rows, "$[1].login_success == false") {
		t.Error(`show_if "$[1].login_success == false" should be false when row 1 login succeeded`)
	}
}
