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

func diagnosticsContain(diags []reportPlanDiagnostic, want string) bool {
	for _, diag := range diags {
		if strings.Contains(diag.Message, want) {
			return true
		}
	}
	return false
}

// Regression: theme and section.Layout must survive normalization. Reports are
// HTML-only documents now, so the widget is a file artifact.
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
					Kind:         "file",
					Source:       "docs/report.html",
					RenderFormat: "html",
					Layout:       &reportPlanDocumentWidgetLayout{Span: 6},
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

// Normalization drops legacy data-viz and markdown widget kinds since reports
// are HTML file documents only.
func TestNormalizeReportPlanDropsLegacyWidgetKinds(t *testing.T) {
	t.Parallel()

	doc := &reportPlanDocument{
		Version: 1,
		Sections: []reportPlanDocumentSection{{
			Heading: "Overview",
			Entries: []reportPlanDocumentEntry{
				{Kind: "single", Widget: &reportPlanDocumentWidget{Kind: "stat", Source: "db/summary.json"}},
				{Kind: "single", Widget: &reportPlanDocumentWidget{Kind: "markdown", Source: "docs/intro.md"}},
				{Kind: "single", Widget: &reportPlanDocumentWidget{Kind: "file", Source: "db/reports/report.html", RenderFormat: "html"}},
			},
		}},
	}

	out := normalizeReportPlanDocument(doc)
	if len(out.Sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(out.Sections))
	}
	if got := len(out.Sections[0].Entries); got != 1 {
		t.Fatalf("expected only the HTML file widget to survive, got %d entries", got)
	}
	if widget := out.Sections[0].Entries[0].Widget; widget.Kind != "file" || widget.RenderFormat != "html" {
		t.Fatalf("expected surviving widget to be file/html, got %+v", widget)
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
		      {"kind": "single", "widget": {"kind": "file", "source": "db/reports/report.html", "renderFormat": "html"}},
		      {"kind": "single", "widget": {"kind": "file-list", "source": "docs/evidence", "listFormat": "gallery", "extensions": ["png", "pdf"]}}
		    ]
		  }]
		}`,
	}
	// No queryDB needed — file widgets don't touch the db.
	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
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
	result, err := validateReportPlan(context.Background(), workspacePath, fakeReportPlanReadFile(files))
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
