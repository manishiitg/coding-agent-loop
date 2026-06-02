package step_based_workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// fakeReportPlanReadFile serves report_plan.json (and any other files) from memory.
func fakeReportPlanReadFile(files map[string]string) func(context.Context, string) (string, error) {
	return func(_ context.Context, path string) (string, error) {
		content, ok := files[path]
		if !ok {
			return "", fmt.Errorf("file not found: %s", path)
		}
		return content, nil
	}
}

// fakeReportPlanQueryDB returns rows for a data widget's SQL from an in-memory
// map keyed by the (trimmed) SQL string. An unknown query returns an error,
// mirroring a SQL failure against db/db.sqlite.
func fakeReportPlanQueryDB(bySQL map[string][]map[string]interface{}) reportPlanQueryFunc {
	return func(_ context.Context, _ string, sql string) ([]map[string]interface{}, error) {
		rows, ok := bySQL[strings.TrimSpace(sql)]
		if !ok {
			return nil, fmt.Errorf("no such query: %s", sql)
		}
		return rows, nil
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

// A stat widget whose SQL resolves (via path) to a scalar is valid; an unknown
// format preset is a (non-fatal) warning.
func TestValidateReportPlanStatScalarValidWithFormatWarning(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/linkedin"
	files := map[string]string{
		"Workflow/linkedin/reports/report_plan.json": `{
			"version": 1,
			"sections": [{
				"heading": "Overview",
				"entries": [{
					"kind": "single",
					"widget": {
						"kind": "stat",
						"title": "Total Posts",
						"db": "db/db.sqlite",
						"sql": "SELECT total_posts FROM summary",
						"path": "0.total_posts",
						"format": "count"
					}
				}]
			}]
		}`,
	}
	// Values mirror JSON-parsed query results (numbers arrive as float64).
	queryDB := fakeReportPlanQueryDB(map[string][]map[string]interface{}{
		"SELECT total_posts FROM summary": {{"total_posts": float64(14)}},
	})

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), queryDB)
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected result.Valid=true, got false with errors %#v", result.Errors)
	}
	if !diagnosticsContain(result.Warnings, `unknown format preset "count"`) {
		t.Fatalf("expected unknown format warning, got %#v", result.Warnings)
	}
}

// A stat widget whose SQL/path resolves to an array (not a scalar) is an error.
func TestValidateReportPlanStatArrayIsError(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/linkedin"
	files := map[string]string{
		"Workflow/linkedin/reports/report_plan.json": `{
			"version": 1,
			"sections": [{
				"heading": "Overview",
				"entries": [{
					"kind": "single",
					"widget": {
						"kind": "stat",
						"title": "Active Strategies",
						"db": "db/db.sqlite",
						"sql": "SELECT id FROM strategies WHERE active=1"
					}
				}]
			}]
		}`,
	}
	queryDB := fakeReportPlanQueryDB(map[string][]map[string]interface{}{
		"SELECT id FROM strategies WHERE active=1": {{"id": "s-1"}, {"id": "s-2"}},
	})

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), queryDB)
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

// A table widget whose SQL returns an array-of-objects validates cleanly.
func TestValidateReportPlanTableFromSQLValid(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [{
				"heading": "Overview",
				"entries": [{
					"kind": "single",
					"widget": {
						"kind": "table",
						"title": "Orders",
						"db": "db/db.sqlite",
						"sql": "SELECT status, amount FROM orders",
						"fields": ["status", "amount"]
					}
				}]
			}]
		}`,
	}
	queryDB := fakeReportPlanQueryDB(map[string][]map[string]interface{}{
		"SELECT status, amount FROM orders": {
			{"status": "paid", "amount": 12.5},
			{"status": "open", "amount": 99.0},
		},
	})

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), queryDB)
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

// A failing SQL query surfaces as a deterministic validation error.
func TestValidateReportPlanSQLErrorIsError(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [{
				"heading": "Overview",
				"entries": [{
					"kind": "single",
					"widget": {
						"kind": "table",
						"title": "Orders",
						"db": "db/db.sqlite",
						"sql": "SELECT * FROM no_such_table"
					}
				}]
			}]
		}`,
	}
	// queryDB knows no queries → every query errors.
	queryDB := fakeReportPlanQueryDB(map[string][]map[string]interface{}{})

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), queryDB)
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "SQL query failed") {
		t.Fatalf("expected SQL query failure, got %#v", result.Errors)
	}
}

// A data widget with `db` but no `sql` is an error.
func TestValidateReportPlanDataWidgetMissingSQL(t *testing.T) {
	t.Parallel()

	workspacePath := "Workflow/orders"
	files := map[string]string{
		"Workflow/orders/reports/report_plan.json": `{
			"version": 1,
			"sections": [{
				"heading": "Overview",
				"entries": [{
					"kind": "single",
					"widget": {"kind": "table", "title": "Orders", "db": "db/db.sqlite"}
				}]
			}]
		}`,
	}

	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), fakeReportPlanQueryDB(nil))
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected result.Valid=false, got true")
	}
	if !diagnosticsContain(result.Errors, "has no `sql`") {
		t.Fatalf("expected missing-sql error, got %#v", result.Errors)
	}
}

// Regression: theme and section.Layout must survive normalization.
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
					DB:     "db/db.sqlite",
					SQL:    "SELECT n FROM strategies",
					Path:   "0.n",
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
	widget := section.Entries[0].Widget
	if widget == nil {
		t.Fatalf("widget dropped")
	}
	if widget.Layout == nil || widget.Layout.Span != 6 {
		t.Fatalf("widget.Layout stripped: got %+v", widget.Layout)
	}
}

// File / file-list widgets render durable files and need no SQL/DB access.
func TestValidateReportPlanFileWidgetsAllowArtifactsWithoutData(t *testing.T) {
	t.Parallel()
	workspacePath := "Workflow/artifacts"
	files := map[string]string{
		"Workflow/artifacts/reports/report_plan.json": `{
		  "version": 1,
		  "sections": [{
		    "heading": "Artifacts",
		    "entries": [
		      {"kind": "single", "widget": {"kind": "file", "source": "docs/report.md", "renderFormat": "markdown"}},
		      {"kind": "single", "widget": {"kind": "file-list", "source": "docs/evidence", "listFormat": "gallery", "extensions": ["png", "pdf"]}}
		    ]
		  }]
		}`,
	}
	// No queryDB needed — file widgets don't touch the db.
	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), nil)
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}
	if !result.Valid {
		t.Fatalf("expected file widgets to validate; errors=%+v warnings=%+v", result.Errors, result.Warnings)
	}
	if result.Widgets != 2 {
		t.Fatalf("expected 2 widgets, got %d", result.Widgets)
	}
}

func TestValidateReportPlanFileWidgetRejectsOutsideRoots(t *testing.T) {
	t.Parallel()
	workspacePath := "Workflow/artifacts"
	files := map[string]string{
		"Workflow/artifacts/reports/report_plan.json": `{
		  "version": 1,
		  "sections": [{
		    "heading": "Artifacts",
		    "entries": [
		      {"kind": "single", "widget": {"kind": "file", "source": "runs/iteration-0/default/report.md"}}
		    ]
		  }]
		}`,
	}
	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files), nil)
	if err != nil {
		t.Fatalf("validateReportPlan returned error: %v", err)
	}
	if result.Valid {
		t.Fatalf("expected file widget outside allowed roots to be invalid")
	}
	if !diagnosticsContain(result.Errors, "under db/, knowledgebase/, or docs/") {
		t.Fatalf("expected invalid source diagnostic, got %+v", result.Errors)
	}
}

// Locks in bracket / $-root path resolution so validate_report_plan and
// preview_report_render stay in parity with the frontend renderer.
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

	if !reportPlanEvaluateShowIf(rows, "$[0].login_success == false") {
		t.Error(`show_if "$[0].login_success == false" should be true when row 0 login failed`)
	}
	if reportPlanEvaluateShowIf(rows, "$[1].login_success == false") {
		t.Error(`show_if "$[1].login_success == false" should be false when row 1 login succeeded`)
	}
}
