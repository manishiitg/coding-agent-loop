package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// Mirrors the subset of frontend/src/lib/reportPlanParser.ts needed to validate
// reports/report_plan.json.

var (
	reportPlanKnownChartTypes = map[string]struct{}{
		"bar": {}, "line": {}, "area": {}, "pie": {},
	}
	reportPlanKnownFormatters = map[string]struct{}{
		"currency-inr": {}, "currency-usd": {},
		"percent": {}, "percent-1dp": {},
		"short-date": {}, "long-date": {}, "datetime": {},
		"number": {}, "number-1dp": {}, "number-2dp": {},
		"bytes": {}, "boolean-icon": {},
	}
	reportPlanKnownAlertSeverities = map[string]struct{}{
		"info": {}, "warning": {}, "error": {}, "success": {},
	}
	reportPlanKnownPivotAggregates = map[string]struct{}{
		"sum": {}, "avg": {}, "count": {}, "min": {}, "max": {}, "first": {},
	}
	reportPlanValidSourceRE = regexp.MustCompile(`^(db/[^/]+\.json|knowledgebase/graph\.json|knowledgebase/index\.json)$`)
	reportPlanFenceRE       = regexp.MustCompile("^```\\s*widget:([\\w-]+)\\s*$")
	reportPlanHexColorRE    = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	reportPlanCSSNamedRE    = regexp.MustCompile(`^[a-zA-Z]+$`)
	// Mirrors evaluateShowIf in reportPlanParser.ts. Intentionally permissive —
	// we flag malformed expressions as warnings, not errors, so the report
	// still renders while the builder fixes them.
	reportPlanShowIfRE = regexp.MustCompile(`^\s*(!)?\s*([^\s!<>=]+)\s*(?:(>=|<=|==|!=|>|<)\s*(.+))?\s*$`)
)

func reportPlanIsPlausibleColor(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	return reportPlanHexColorRE.MatchString(v) || reportPlanCSSNamedRE.MatchString(v)
}

type reportPlanWidget struct {
	Kind   string // "text" | "table" | "chart"
	Source string
	Path   string
	Filter string
	Fields map[string]string // raw optional fields, lowercase keys
	// Location info for error messages
	Section  string
	LineNum  int
	InRow    bool
	RowIndex int
}

type reportPlanSection struct {
	Heading string
	Widgets []*reportPlanWidget
}

type reportPlanDocument struct {
	Version  int                         `json:"version,omitempty"`
	Sections []reportPlanDocumentSection `json:"sections"`
}

type reportPlanDocumentSection struct {
	ID      string                    `json:"id,omitempty"`
	Heading string                    `json:"heading"`
	Entries []reportPlanDocumentEntry `json:"entries"`
}

type reportPlanDocumentEntry struct {
	ID     string                    `json:"id,omitempty"`
	Kind   string                    `json:"kind"`
	Widget *reportPlanDocumentWidget `json:"widget,omitempty"`
	Row    *reportPlanDocumentRow    `json:"row,omitempty"`
}

type reportPlanDocumentRow struct {
	Widgets []reportPlanDocumentWidget `json:"widgets"`
}

type reportPlanDocumentDefaultSort struct {
	Field     string `json:"field"`
	Direction string `json:"direction,omitempty"`
}

type reportPlanDocumentWidget struct {
	ID            string                         `json:"id,omitempty"`
	Hidden        bool                           `json:"hidden,omitempty"`
	Kind          string                         `json:"kind"`
	Source        string                         `json:"source,omitempty"`
	Path          string                         `json:"path,omitempty"`
	Filter        string                         `json:"filter,omitempty"`
	Title         string                         `json:"title,omitempty"`
	Description   string                         `json:"description,omitempty"`
	Height        int                            `json:"height,omitempty"`
	Formats       map[string]string              `json:"formats,omitempty"`
	PageSize      int                            `json:"pageSize,omitempty"`
	EnableSearch  *bool                          `json:"enableSearch,omitempty"`
	DefaultSort   *reportPlanDocumentDefaultSort `json:"defaultSort,omitempty"`
	HideColumns   []string                       `json:"hideColumns,omitempty"`
	ChartType     string                         `json:"chartType,omitempty"`
	XAxis         string                         `json:"xAxis,omitempty"`
	YAxis         string                         `json:"yAxis,omitempty"`
	TopN          int                            `json:"topN,omitempty"`
	Sort          string                         `json:"sort,omitempty"`
	ShowValues    *bool                          `json:"showValues,omitempty"`
	Colors        []string                       `json:"colors,omitempty"`
	ColorsDark    []string                       `json:"colorsDark,omitempty"`
	ColorBy       string                         `json:"colorBy,omitempty"`
	ColorMap      map[string]string              `json:"colorMap,omitempty"`
	ShowIf        string                         `json:"showIf,omitempty"`
	Label         string                         `json:"label,omitempty"`
	Prefix        string                         `json:"prefix,omitempty"`
	Suffix        string                         `json:"suffix,omitempty"`
	Format        string                         `json:"format,omitempty"`
	DeltaPath     string                         `json:"deltaPath,omitempty"`
	DeltaFormat   string                         `json:"deltaFormat,omitempty"`
	TrendPath     string                         `json:"trendPath,omitempty"`
	Severity      string                         `json:"severity,omitempty"`
	Message       string                         `json:"message,omitempty"`
	RowsField     string                         `json:"rowsField,omitempty"`
	ColumnsField  string                         `json:"columnsField,omitempty"`
	ValuesField   string                         `json:"valuesField,omitempty"`
	Aggregate     string                         `json:"aggregate,omitempty"`
	Heatmap       *bool                          `json:"heatmap,omitempty"`
	HeatmapColors []string                       `json:"heatmapColors,omitempty"`
	Series        []string                       `json:"series,omitempty"`
	SeriesColors  []string                       `json:"seriesColors,omitempty"`
	Stacked       *bool                          `json:"stacked,omitempty"`
	CostsScope    string                         `json:"costsScope,omitempty"`
	CostsView     string                         `json:"costsView,omitempty"`
	CostsMetric   string                         `json:"costsMetric,omitempty"`
	EvalsView     string                         `json:"evalsView,omitempty"`
	EvalsMetric   string                         `json:"evalsMetric,omitempty"`
	RunsView      string                         `json:"runsView,omitempty"`
	RunFolder     string                         `json:"runFolder,omitempty"`
	Group         string                         `json:"group,omitempty"`
}

type reportPlanReadResult struct {
	Document   *reportPlanDocument
	RawContent string
	Format     string
	FilePath   string
}

type reportPlanDiagnostic struct {
	Severity string `json:"severity"` // "error" | "warning" | "info"
	Section  string `json:"section,omitempty"`
	Line     int    `json:"line,omitempty"`
	Widget   string `json:"widget,omitempty"` // short locator e.g. "chart@knowledgebase/graph.json"
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

type reportPlanValidationResult struct {
	Valid       bool                   `json:"valid"`
	Sections    int                    `json:"sections"`
	Widgets     int                    `json:"widgets"`
	Errors      []reportPlanDiagnostic `json:"errors,omitempty"`
	Warnings    []reportPlanDiagnostic `json:"warnings,omitempty"`
	Suggestions []string               `json:"suggestions,omitempty"`
	// Parsed shows what the parser actually extracted from the markdown. Useful
	// for debugging silent-blank widgets: if a widget isn't here, the fence tag
	// didn't match or the block was missing `source:`. Dumped even when
	// validation succeeds so the LLM can verify "what the parser saw" matches
	// intent without re-reading the markdown.
	Parsed []reportPlanParsedSection `json:"parsed,omitempty"`
}

type reportPlanCapabilityExample struct {
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
}

type reportPlanAPIWidgetCapability struct {
	Views   []string `json:"views"`
	Scopes  []string `json:"scopes,omitempty"`
	Metrics []string `json:"metrics,omitempty"`
}

type reportPlanCapabilitiesResult struct {
	FilePath             string                                   `json:"file_path"`
	ValidationTool       string                                   `json:"validation_tool"`
	PreviewTool          string                                   `json:"preview_tool"`
	WidgetKinds          []string                                 `json:"widget_kinds"`
	SourceBackedWidgets  []string                                 `json:"source_backed_widgets"`
	APIBackedWidgets     map[string]reportPlanAPIWidgetCapability `json:"api_backed_widgets"`
	ValidSourcePatterns  []string                                 `json:"valid_source_patterns"`
	CommonFields         []string                                 `json:"common_fields"`
	SourceBackedRequired []string                                 `json:"source_backed_required_fields"`
	APIBackedRules       []string                                 `json:"api_backed_rules"`
	WidgetSpecificFields map[string][]string                      `json:"widget_specific_fields"`
	RowSyntax            map[string]string                        `json:"row_syntax"`
	WorkflowRules        []string                                 `json:"workflow_rules"`
	Examples             []reportPlanCapabilityExample            `json:"examples"`
}

func getReportPlanCapabilities() reportPlanCapabilitiesResult {
	return reportPlanCapabilitiesResult{
		FilePath:            "reports/report_plan.json",
		ValidationTool:      "validate_report_plan",
		PreviewTool:         "preview_report_render",
		WidgetKinds:         []string{"text", "table", "chart", "stat", "alert", "pivot", "costs", "evals", "runs", "row"},
		SourceBackedWidgets: []string{"text", "table", "chart", "stat", "alert", "pivot"},
		APIBackedWidgets: map[string]reportPlanAPIWidgetCapability{
			"costs": {
				Views:   []string{"summary", "stage-breakdown", "run-table", "step-table", "model-table"},
				Scopes:  []string{"phase", "execution", "evaluation", "all"},
				Metrics: []string{"cost", "total_tokens", "input_tokens", "output_tokens", "llm_calls"},
			},
			"evals": {
				Views:   []string{"summary", "run-chart", "run-table", "step-table"},
				Metrics: []string{"score_percentage", "total_score"},
			},
			"runs": {
				Views: []string{"summary", "duration-chart", "status-chart", "table"},
			},
		},
		ValidSourcePatterns:  []string{"db/<file>.json", "knowledgebase/graph.json", "knowledgebase/index.json"},
		CommonFields:         []string{"title", "description", "height", "filter", "show_if"},
		SourceBackedRequired: []string{"source", "path"},
		APIBackedRules: []string{
			"costs, evals, and runs are API-backed widgets and do not require source or path",
			"if source/path are present on costs/evals/runs, the renderer ignores them",
			"after editing reports/report_plan.json always call validate_report_plan",
			"call preview_report_render to inspect the final widget structure and resolved data preview",
		},
		WidgetSpecificFields: map[string][]string{
			"table": {"formats", "page_size", "enable_search", "default_sort", "hide_columns", "colors", "colors_dark", "color_by", "color_map"},
			"chart": {"chart_type", "x_axis", "y_axis", "top_n", "sort", "show_values", "series", "series_colors", "stacked", "colors", "colors_dark", "color_by", "color_map"},
			"stat":  {"label", "prefix", "suffix", "format", "delta_path", "delta_format", "trend_path"},
			"alert": {"severity", "title", "message", "format"},
			"pivot": {"rows", "columns", "values", "aggregate", "format", "heatmap", "heatmap_colors"},
			"costs": {"view", "scope", "metric", "run_folder", "group"},
			"evals": {"view", "metric", "run_folder", "group"},
			"runs":  {"view", "run_folder", "group"},
		},
		RowSyntax: map[string]string{
			"source_backed": "- {kind} | source: <path> | path: <dot.path> [ | filter: <key=value> ] [ | show_if: <expr> ]",
			"api_backed":    "- costs | view: summary | scope: all ; - evals | view: run-chart ; - runs | view: table",
		},
		WorkflowRules: []string{
			"edit reports/report_plan.json instead of generating HTML or markdown artifacts",
			"if the user wants a visualization the grammar cannot express, say so explicitly and propose either a new widget type or a data-shape change",
			"when the report shows 'No report yet', inspect or create reports/report_plan.json",
			"when the report renders but data is empty, inspect reports/report_plan.json and the actual db/ or knowledgebase files",
		},
		Examples: []reportPlanCapabilityExample{
			{
				Name:     "Source-backed KPI row",
				Markdown: "## Key Metrics\n\n```widget:row\n- stat | source: db/sync_runs.json | path: runs.0.employee_sync.total\n- stat | source: db/sync_runs.json | path: runs.0.payslip_sync.total_records\n```",
			},
			{
				Name:     "Workflow cost summary",
				Markdown: "## Workflow Costs\n\n```widget:costs\ntitle: Workflow cost overview\nview: summary\nscope: all\nmetric: cost\n```",
			},
			{
				Name:     "Evaluation scores",
				Markdown: "## Evaluation Quality\n\n```widget:evals\ntitle: Eval scores by run\nview: run-chart\nmetric: score_percentage\n```",
			},
			{
				Name:     "Run history",
				Markdown: "## Run History\n\n```widget:runs\ntitle: Recent runs\nview: table\n```",
			},
		},
	}
}

type reportPlanParsedSection struct {
	Heading string                   `json:"heading"`
	Widgets []reportPlanParsedWidget `json:"widgets"`
}

type reportPlanParsedWidget struct {
	Kind     string `json:"kind"`
	Source   string `json:"source"`
	Path     string `json:"path,omitempty"`
	Filter   string `json:"filter,omitempty"`
	ShowIf   string `json:"show_if,omitempty"`
	Line     int    `json:"line,omitempty"`
	InRow    bool   `json:"in_row,omitempty"`
	RowIndex int    `json:"row_index,omitempty"`
	// Options captures all non-standard fields the parser saw — so the builder
	// can spot e.g. `type: stat_row` (unrecognized key) or `chrt_type: bar`
	// (typo) being silently ignored.
	Options map[string]string `json:"options,omitempty"`
}

type reportPlanRenderPreviewResult struct {
	Valid           bool                        `json:"valid"`
	Sections        int                         `json:"sections"`
	Widgets         int                         `json:"widgets"`
	PlanFilePath    string                      `json:"plan_file_path,omitempty"`
	PlanFormat      string                      `json:"plan_format,omitempty"`
	PlanMarkdown    string                      `json:"plan_markdown"`
	PlanJSON        *reportPlanDocument         `json:"plan_json,omitempty"`
	PreviewMarkdown string                      `json:"preview_markdown"`
	Validation      *reportPlanValidationResult `json:"validation,omitempty"`
	SectionsPreview []reportPlanPreviewSection  `json:"sections_preview,omitempty"`
}

type reportPlanPreviewSection struct {
	Heading string                    `json:"heading"`
	Widgets []reportPlanPreviewWidget `json:"widgets"`
}

type reportPlanPreviewWidget struct {
	Kind        string      `json:"kind"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Source      string      `json:"source,omitempty"`
	Path        string      `json:"path,omitempty"`
	Line        int         `json:"line,omitempty"`
	InRow       bool        `json:"in_row,omitempty"`
	RowIndex    int         `json:"row_index,omitempty"`
	Visible     bool        `json:"visible"`
	RenderState string      `json:"render_state,omitempty"` // visible | hidden | error
	Reason      string      `json:"reason,omitempty"`
	Summary     string      `json:"summary,omitempty"`
	DataPreview interface{} `json:"data_preview,omitempty"`
}

type reportPlanCostsPreviewData struct {
	HasPhaseLedger        bool     `json:"has_phase_ledger"`
	ExecutionLedgerFiles  int      `json:"execution_ledger_files"`
	EvaluationLedgerFiles int      `json:"evaluation_ledger_files"`
	ExecutionRunFolders   []string `json:"execution_run_folders,omitempty"`
	EvaluationRunFolders  []string `json:"evaluation_run_folders,omitempty"`
}

type reportPlanEvalPreviewItem struct {
	RunFolder        string  `json:"run_folder"`
	GeneratedAt      string  `json:"generated_at,omitempty"`
	ScorePercentage  float64 `json:"score_percentage"`
	TotalScore       int     `json:"total_score"`
	MaxPossibleScore int     `json:"max_possible_score"`
	StepCount        int     `json:"step_count"`
}

type reportPlanRunPreviewItem struct {
	RunFolder  string `json:"run_folder"`
	ModifiedAt string `json:"modified_at,omitempty"`
}

func readReportPlanDocument(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (*reportPlanReadResult, error) {
	jsonPath := normalizePathForWorkspaceAPI(filepath.Join("reports", "report_plan.json"), workspacePath)
	rawJSON, jsonErr := readFile(ctx, jsonPath)
	if jsonErr != nil {
		return &reportPlanReadResult{
			Document:   normalizeReportPlanDocument(&reportPlanDocument{Version: 1}),
			RawContent: "",
			Format:     "json",
			FilePath:   "reports/report_plan.json",
		}, nil
	}
	doc, err := parseReportPlanJSONDocument(rawJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", jsonPath, err)
	}
	return &reportPlanReadResult{
		Document:   normalizeReportPlanDocument(doc),
		RawContent: rawJSON,
		Format:     "json",
		FilePath:   "reports/report_plan.json",
	}, nil
}

func parseReportPlanJSONDocument(raw string) (*reportPlanDocument, error) {
	if strings.TrimSpace(raw) == "" {
		return &reportPlanDocument{Version: 1}, nil
	}
	var doc reportPlanDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func parseReportPlanMarkdownDocument(markdown string) *reportPlanDocument {
	doc := &reportPlanDocument{Version: 1}
	if strings.TrimSpace(markdown) == "" {
		return doc
	}
	lines := strings.Split(markdown, "\n")
	var current *reportPlanDocumentSection
	i := 0
	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			doc.Sections = appendSectionIfPresent(doc.Sections, current)
			current = &reportPlanDocumentSection{Heading: strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))}
			i++
			continue
		}
		if m := reportPlanFenceRE.FindStringSubmatch(trimmed); m != nil {
			kind := strings.ToLower(m[1])
			var body []string
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				body = append(body, lines[i])
				i++
			}
			if i < len(lines) {
				i++
			}
			if current == nil {
				continue
			}
			if kind == "row" {
				row := parseReportPlanMarkdownRowDocument(body)
				if len(row.Widgets) > 0 {
					current.Entries = append(current.Entries, reportPlanDocumentEntry{Kind: "row", Row: row})
				}
				continue
			}
			widget := parseReportPlanMarkdownKeyValueWidget(kind, body)
			if widget != nil {
				current.Entries = append(current.Entries, reportPlanDocumentEntry{Kind: "single", Widget: widget})
			}
			continue
		}
		i++
	}
	doc.Sections = appendSectionIfPresent(doc.Sections, current)
	return doc
}

func appendSectionIfPresent(sections []reportPlanDocumentSection, section *reportPlanDocumentSection) []reportPlanDocumentSection {
	if section == nil {
		return sections
	}
	return append(sections, *section)
}

func parseReportPlanMarkdownKeyValueWidget(kind string, body []string) *reportPlanDocumentWidget {
	if !reportPlanDocumentWidgetKindAllowed(kind) {
		return nil
	}
	fields := map[string]string{}
	for _, line := range body {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		sep := strings.Index(t, ":")
		if sep <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(t[:sep]))
		val := strings.TrimSpace(t[sep+1:])
		if val != "" {
			fields[key] = val
		}
	}
	if !reportPlanWidgetIsAPIBacked(kind) && fields["source"] == "" {
		return nil
	}
	return reportPlanDocumentWidgetFromLegacyFields(kind, fields)
}

func parseReportPlanMarkdownRowDocument(body []string) *reportPlanDocumentRow {
	row := &reportPlanDocumentRow{}
	for _, raw := range body {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "-"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		cleaned := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				cleaned = append(cleaned, part)
			}
		}
		if len(cleaned) < 2 {
			continue
		}
		kind := strings.ToLower(cleaned[0])
		if !reportPlanDocumentWidgetKindAllowed(kind) {
			continue
		}
		fields := map[string]string{}
		for _, seg := range cleaned[1:] {
			sep := strings.Index(seg, ":")
			if sep <= 0 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(seg[:sep]))
			val := strings.TrimSpace(seg[sep+1:])
			if val != "" {
				fields[key] = val
			}
		}
		if !reportPlanWidgetIsAPIBacked(kind) && fields["source"] == "" {
			continue
		}
		if widget := reportPlanDocumentWidgetFromLegacyFields(kind, fields); widget != nil {
			row.Widgets = append(row.Widgets, *widget)
		}
	}
	return row
}

func reportPlanDocumentWidgetKindAllowed(kind string) bool {
	return kind == "text" || kind == "table" || kind == "chart" ||
		kind == "stat" || kind == "alert" || kind == "pivot" ||
		kind == "costs" || kind == "evals" || kind == "runs"
}

func reportPlanDocumentWidgetFromLegacyFields(kind string, fields map[string]string) *reportPlanDocumentWidget {
	widget := &reportPlanDocumentWidget{
		Kind:        kind,
		Source:      fields["source"],
		Path:        normalizeReportPlanPath(fields["path"]),
		Filter:      fields["filter"],
		Title:       fields["title"],
		Description: fields["description"],
		ShowIf:      reportPlanFirstNonEmpty(fields["show_if"], fields["showif"]),
	}
	if n, err := strconv.Atoi(fields["height"]); err == nil && n > 0 {
		widget.Height = n
	}
	if len(fields["formats"]) > 0 {
		widget.Formats = parseReportPlanFormatsField(fields["formats"])
	}
	if n, err := strconv.Atoi(reportPlanFirstNonEmpty(fields["page_size"], fields["pagesize"])); err == nil && n > 0 {
		widget.PageSize = n
	}
	if v, ok := parseReportPlanBoolPointer(reportPlanFirstNonEmpty(fields["enable_search"], fields["enablesearch"])); ok {
		widget.EnableSearch = v
	}
	if raw := reportPlanFirstNonEmpty(fields["default_sort"], fields["defaultsort"]); raw != "" {
		widget.DefaultSort = parseReportPlanDefaultSort(raw)
	}
	if raw := reportPlanFirstNonEmpty(fields["hide_columns"], fields["hidecolumns"]); raw != "" {
		widget.HideColumns = parseReportPlanCSVList(raw)
	}
	widget.ChartType = reportPlanFirstNonEmpty(fields["chart_type"], fields["charttype"])
	widget.XAxis = reportPlanFirstNonEmpty(fields["x_axis"], fields["xaxis"])
	widget.YAxis = reportPlanFirstNonEmpty(fields["y_axis"], fields["yaxis"])
	if n, err := strconv.Atoi(reportPlanFirstNonEmpty(fields["top_n"], fields["topn"])); err == nil && n > 0 {
		widget.TopN = n
	}
	widget.Sort = fields["sort"]
	if v, ok := parseReportPlanBoolPointer(reportPlanFirstNonEmpty(fields["show_values"], fields["showvalues"])); ok {
		widget.ShowValues = v
	}
	widget.Colors = parseReportPlanCSVList(fields["colors"])
	widget.ColorsDark = parseReportPlanCSVList(reportPlanFirstNonEmpty(fields["colors_dark"], fields["colorsdark"]))
	widget.ColorBy = reportPlanFirstNonEmpty(fields["color_by"], fields["colorby"])
	if len(fields["color_map"]) > 0 || len(fields["colormap"]) > 0 {
		widget.ColorMap = parseReportPlanColorMapField(reportPlanFirstNonEmpty(fields["color_map"], fields["colormap"]))
	}
	widget.Label = fields["label"]
	widget.Prefix = fields["prefix"]
	widget.Suffix = fields["suffix"]
	widget.Format = fields["format"]
	widget.DeltaPath = reportPlanFirstNonEmpty(fields["delta_path"], fields["deltapath"])
	widget.DeltaFormat = reportPlanFirstNonEmpty(fields["delta_format"], fields["deltaformat"])
	widget.TrendPath = reportPlanFirstNonEmpty(fields["trend_path"], fields["trendpath"])
	widget.Severity = fields["severity"]
	widget.Message = fields["message"]
	widget.RowsField = fields["rows"]
	widget.ColumnsField = fields["columns"]
	widget.ValuesField = fields["values"]
	widget.Aggregate = fields["aggregate"]
	if v, ok := parseReportPlanBoolPointer(fields["heatmap"]); ok {
		widget.Heatmap = v
	}
	widget.HeatmapColors = parseReportPlanCSVList(reportPlanFirstNonEmpty(fields["heatmap_colors"], fields["heatmapcolors"]))
	widget.Series = parseReportPlanCSVList(fields["series"])
	widget.SeriesColors = parseReportPlanCSVList(reportPlanFirstNonEmpty(fields["series_colors"], fields["seriescolors"]))
	if v, ok := parseReportPlanBoolPointer(fields["stacked"]); ok {
		widget.Stacked = v
	}
	widget.CostsScope = fields["scope"]
	widget.CostsView = fields["view"]
	widget.CostsMetric = fields["metric"]
	widget.EvalsView = fields["view"]
	widget.EvalsMetric = fields["metric"]
	widget.RunsView = fields["view"]
	widget.RunFolder = reportPlanFirstNonEmpty(fields["run_folder"], fields["runfolder"])
	widget.Group = fields["group"]
	return widget
}

func parseReportPlanFormatsField(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(part[:eq])
		val := strings.TrimSpace(part[eq+1:])
		if key != "" && val != "" {
			out[key] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseReportPlanColorMapField(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(part[:eq])
		val := strings.TrimSpace(part[eq+1:])
		if key != "" && val != "" {
			out[key] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseReportPlanCSVList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseReportPlanDefaultSort(raw string) *reportPlanDocumentDefaultSort {
	parts := strings.Split(raw, ":")
	field := strings.TrimSpace(parts[0])
	if field == "" {
		return nil
	}
	sortSpec := &reportPlanDocumentDefaultSort{Field: field, Direction: "asc"}
	if len(parts) > 1 && strings.EqualFold(strings.TrimSpace(parts[1]), "desc") {
		sortSpec.Direction = "desc"
	}
	return sortSpec
}

func parseReportPlanBoolPointer(raw string) (*bool, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	value := parseReportPlanBool(raw)
	return &value, true
}

func normalizeReportPlanDocument(doc *reportPlanDocument) *reportPlanDocument {
	if doc == nil {
		doc = &reportPlanDocument{}
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	normalized := &reportPlanDocument{Version: doc.Version}
	sectionCounter := 0
	for _, section := range doc.Sections {
		if strings.TrimSpace(section.Heading) == "" {
			continue
		}
		sectionCounter++
		if section.ID == "" {
			section.ID = fmt.Sprintf("section-%02d-%s", sectionCounter, reportPlanSlug(section.Heading))
		}
		entryCounter := 0
		nextSection := reportPlanDocumentSection{
			ID:      section.ID,
			Heading: strings.TrimSpace(section.Heading),
			Entries: make([]reportPlanDocumentEntry, 0, len(section.Entries)),
		}
		for _, entry := range section.Entries {
			entryCounter++
			if entry.Kind != "single" && entry.Kind != "row" {
				continue
			}
			if entry.ID == "" {
				entry.ID = fmt.Sprintf("%s-entry-%02d", section.ID, entryCounter)
			}
			if entry.Kind == "single" {
				if entry.Widget == nil || !reportPlanDocumentWidgetKindAllowed(entry.Widget.Kind) {
					continue
				}
				entry.Widget = normalizeReportPlanDocumentWidget(*entry.Widget, section.ID, entryCounter, 0)
				nextSection.Entries = append(nextSection.Entries, entry)
				continue
			}
			if entry.Row == nil {
				continue
			}
			nextRow := &reportPlanDocumentRow{}
			for widgetIndex, widget := range entry.Row.Widgets {
				if !reportPlanDocumentWidgetKindAllowed(widget.Kind) {
					continue
				}
				nextRow.Widgets = append(nextRow.Widgets, *normalizeReportPlanDocumentWidget(widget, section.ID, entryCounter, widgetIndex))
			}
			if len(nextRow.Widgets) == 0 {
				continue
			}
			entry.Row = nextRow
			nextSection.Entries = append(nextSection.Entries, entry)
		}
		normalized.Sections = append(normalized.Sections, nextSection)
	}
	return normalized
}

func normalizeReportPlanDocumentWidget(widget reportPlanDocumentWidget, sectionID string, entryIndex, widgetIndex int) *reportPlanDocumentWidget {
	widget.Kind = strings.ToLower(strings.TrimSpace(widget.Kind))
	widget.Path = normalizeReportPlanPath(widget.Path)
	if widget.ID == "" {
		if widgetIndex > 0 {
			widget.ID = fmt.Sprintf("%s-widget-%02d-%02d", sectionID, entryIndex, widgetIndex)
		} else {
			widget.ID = fmt.Sprintf("%s-widget-%02d", sectionID, entryIndex)
		}
	}
	return &widget
}

func flattenReportPlanDocument(doc *reportPlanDocument) []reportPlanSection {
	if doc == nil || len(doc.Sections) == 0 {
		return nil
	}
	sections := make([]reportPlanSection, 0, len(doc.Sections))
	for _, section := range doc.Sections {
		nextSection := reportPlanSection{Heading: section.Heading}
		for _, entry := range section.Entries {
			if entry.Kind == "single" && entry.Widget != nil {
				nextSection.Widgets = append(nextSection.Widgets, reportPlanLegacyWidgetFromDocumentWidget(*entry.Widget, section.Heading, false, 0))
				continue
			}
			if entry.Kind == "row" && entry.Row != nil {
				for idx, widget := range entry.Row.Widgets {
					nextSection.Widgets = append(nextSection.Widgets, reportPlanLegacyWidgetFromDocumentWidget(widget, section.Heading, true, idx))
				}
			}
		}
		sections = append(sections, nextSection)
	}
	return sections
}

func reportPlanLegacyWidgetFromDocumentWidget(widget reportPlanDocumentWidget, section string, inRow bool, rowIndex int) *reportPlanWidget {
	fields := map[string]string{}
	add := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			fields[key] = strings.TrimSpace(value)
		}
	}
	add("source", widget.Source)
	add("path", widget.Path)
	add("filter", widget.Filter)
	add("title", widget.Title)
	add("description", widget.Description)
	if widget.Height > 0 {
		fields["height"] = strconv.Itoa(widget.Height)
	}
	if len(widget.Formats) > 0 {
		keys := make([]string, 0, len(widget.Formats))
		for key := range widget.Formats {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", key, widget.Formats[key]))
		}
		fields["formats"] = strings.Join(parts, ", ")
	}
	if widget.PageSize > 0 {
		fields["page_size"] = strconv.Itoa(widget.PageSize)
	}
	if widget.EnableSearch != nil {
		fields["enable_search"] = strconv.FormatBool(*widget.EnableSearch)
	}
	if widget.DefaultSort != nil && widget.DefaultSort.Field != "" {
		if widget.DefaultSort.Direction == "desc" {
			fields["default_sort"] = widget.DefaultSort.Field + ":desc"
		} else {
			fields["default_sort"] = widget.DefaultSort.Field
		}
	}
	if len(widget.HideColumns) > 0 {
		fields["hide_columns"] = strings.Join(widget.HideColumns, ", ")
	}
	add("chart_type", widget.ChartType)
	add("x_axis", widget.XAxis)
	add("y_axis", widget.YAxis)
	if widget.TopN > 0 {
		fields["top_n"] = strconv.Itoa(widget.TopN)
	}
	add("sort", widget.Sort)
	if widget.ShowValues != nil {
		fields["show_values"] = strconv.FormatBool(*widget.ShowValues)
	}
	if len(widget.Colors) > 0 {
		fields["colors"] = strings.Join(widget.Colors, ", ")
	}
	if len(widget.ColorsDark) > 0 {
		fields["colors_dark"] = strings.Join(widget.ColorsDark, ", ")
	}
	add("color_by", widget.ColorBy)
	if len(widget.ColorMap) > 0 {
		keys := make([]string, 0, len(widget.ColorMap))
		for key := range widget.ColorMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s=%s", key, widget.ColorMap[key]))
		}
		fields["color_map"] = strings.Join(parts, ", ")
	}
	add("show_if", widget.ShowIf)
	add("label", widget.Label)
	add("prefix", widget.Prefix)
	add("suffix", widget.Suffix)
	add("format", widget.Format)
	add("delta_path", widget.DeltaPath)
	add("delta_format", widget.DeltaFormat)
	add("trend_path", widget.TrendPath)
	add("severity", widget.Severity)
	add("message", widget.Message)
	add("rows", widget.RowsField)
	add("columns", widget.ColumnsField)
	add("values", widget.ValuesField)
	add("aggregate", widget.Aggregate)
	if widget.Heatmap != nil {
		fields["heatmap"] = strconv.FormatBool(*widget.Heatmap)
	}
	if len(widget.HeatmapColors) > 0 {
		fields["heatmap_colors"] = strings.Join(widget.HeatmapColors, ", ")
	}
	if len(widget.Series) > 0 {
		fields["series"] = strings.Join(widget.Series, ", ")
	}
	if len(widget.SeriesColors) > 0 {
		fields["series_colors"] = strings.Join(widget.SeriesColors, ", ")
	}
	if widget.Stacked != nil {
		fields["stacked"] = strconv.FormatBool(*widget.Stacked)
	}
	add("scope", widget.CostsScope)
	add("view", reportPlanFirstNonEmpty(widget.CostsView, widget.EvalsView, widget.RunsView))
	add("metric", reportPlanFirstNonEmpty(widget.CostsMetric, widget.EvalsMetric))
	add("run_folder", widget.RunFolder)
	add("group", widget.Group)
	if widget.Hidden {
		fields["hidden"] = "true"
	}
	return &reportPlanWidget{
		Kind:     widget.Kind,
		Source:   widget.Source,
		Path:     widget.Path,
		Filter:   widget.Filter,
		Fields:   fields,
		Section:  section,
		InRow:    inRow,
		RowIndex: rowIndex,
	}
}

func reportPlanSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "item"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return "item"
	}
	return slug
}

// parseReportPlan walks the markdown and returns sections+widgets. Intentionally
// forgiving — matches the frontend parser behavior so we flag what would silently
// fail to render rather than refusing to parse.
func parseReportPlan(markdown string) []reportPlanSection {
	if strings.TrimSpace(markdown) == "" {
		return nil
	}
	lines := strings.Split(markdown, "\n")
	var sections []reportPlanSection
	var current *reportPlanSection
	i := 0
	for i < len(lines) {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)

		// H2 heading (not H3+) starts a new section.
		if strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &reportPlanSection{Heading: strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))}
			i++
			continue
		}

		if m := reportPlanFenceRE.FindStringSubmatch(trimmed); m != nil {
			kind := m[1]
			startLine := i + 1
			var body []string
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "```" {
				body = append(body, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // skip closing fence
			}
			if current == nil {
				continue // widgets outside a heading are dropped by the renderer
			}
			sectionHeading := current.Heading
			if kind == "row" {
				for idx, w := range parseReportPlanRow(body) {
					w.Section = sectionHeading
					w.LineNum = startLine
					w.InRow = true
					w.RowIndex = idx
					current.Widgets = append(current.Widgets, w)
				}
				continue
			}
			if kind != "text" && kind != "table" && kind != "chart" &&
				kind != "stat" && kind != "alert" && kind != "pivot" &&
				kind != "costs" && kind != "evals" && kind != "runs" {
				continue
			}
			w := parseReportPlanKeyValue(kind, body)
			if w == nil {
				continue
			}
			w.Section = sectionHeading
			w.LineNum = startLine
			current.Widgets = append(current.Widgets, w)
			continue
		}
		i++
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

func parseReportPlanKeyValue(kind string, body []string) *reportPlanWidget {
	fields := map[string]string{}
	for _, line := range body {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		sep := strings.Index(t, ":")
		if sep <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(t[:sep]))
		val := strings.TrimSpace(t[sep+1:])
		if val != "" {
			fields[key] = val
		}
	}
	if !reportPlanWidgetIsAPIBacked(kind) && fields["source"] == "" {
		return nil
	}
	return &reportPlanWidget{
		Kind:   kind,
		Source: fields["source"],
		Path:   normalizeReportPlanPath(fields["path"]),
		Filter: fields["filter"],
		Fields: fields,
	}
}

func parseReportPlanRow(body []string) []*reportPlanWidget {
	var out []*reportPlanWidget
	for _, raw := range body {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		if len(cleaned) < 2 {
			continue
		}
		kind := strings.ToLower(cleaned[0])
		if kind != "text" && kind != "table" && kind != "chart" &&
			kind != "stat" && kind != "alert" && kind != "pivot" &&
			kind != "costs" && kind != "evals" && kind != "runs" {
			continue
		}
		fields := map[string]string{}
		for _, seg := range cleaned[1:] {
			sep := strings.Index(seg, ":")
			if sep <= 0 {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(seg[:sep]))
			val := strings.TrimSpace(seg[sep+1:])
			if val != "" {
				fields[key] = val
			}
		}
		if !reportPlanWidgetIsAPIBacked(kind) && fields["source"] == "" {
			continue
		}
		out = append(out, &reportPlanWidget{
			Kind:   kind,
			Source: fields["source"],
			Path:   normalizeReportPlanPath(fields["path"]),
			Filter: fields["filter"],
			Fields: fields,
		})
	}
	return out
}

func reportPlanWidgetIsAPIBacked(kind string) bool {
	return kind == "costs" || kind == "evals" || kind == "runs"
}

func normalizeReportPlanPath(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" || t == "$" || t == "$[*]" || t == "." || t == "*" {
		return ""
	}
	return t
}

func resolveReportPlanPath(data interface{}, path string) (interface{}, bool) {
	if data == nil {
		return nil, false
	}
	if path == "" {
		return data, true
	}
	current := data
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		if current == nil {
			return nil, false
		}
		switch typed := current.(type) {
		case []interface{}:
			idx, err := strconv.Atoi(seg)
			if err != nil || idx < 0 || idx >= len(typed) {
				return nil, false
			}
			current = typed[idx]
		case map[string]interface{}:
			v, ok := typed[seg]
			if !ok {
				return nil, false
			}
			current = v
		default:
			return nil, false
		}
	}
	return current, true
}

func applyReportPlanFilter(value interface{}, filter string) interface{} {
	if filter == "" {
		return value
	}
	arr, ok := value.([]interface{})
	if !ok {
		return value
	}
	eq := strings.Index(filter, "=")
	if eq <= 0 {
		return value
	}
	key := strings.TrimSpace(filter[:eq])
	match := strings.TrimSpace(filter[eq+1:])
	if key == "" {
		return value
	}
	filtered := make([]interface{}, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if fmt.Sprintf("%v", obj[key]) == match {
			filtered = append(filtered, obj)
		}
	}
	return filtered
}

// validateReportPlan parses the canonical JSON report plan (with markdown fallback)
// and checks each widget against its referenced source file. Returns a structured
// result for the LLM to act on.
func validateReportPlan(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (*reportPlanValidationResult, error) {
	planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return nil, err
	}

	var sections []reportPlanSection
	if planRead.Format == "markdown" {
		sections = parseReportPlan(planRead.RawContent)
	} else {
		sections = flattenReportPlanDocument(planRead.Document)
	}
	result := &reportPlanValidationResult{Valid: true, Sections: len(sections)}

	if len(sections) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error",
			Message:  fmt.Sprintf("%s has no usable report sections.", planRead.FilePath),
			Hint:     "Add at least one section with a heading and widget entries.",
		})
		return result, nil
	}

	// Cache parsed JSON per source so we don't re-read the same file 10×.
	sourceCache := map[string]interface{}{}
	sourceMissing := map[string]bool{}

	for _, section := range sections {
		for _, w := range section.Widgets {
			result.Widgets++
			locator := fmt.Sprintf("%s@%s", w.Kind, w.Source)
			if w.InRow {
				locator = fmt.Sprintf("row[%d]:%s", w.RowIndex, locator)
			}

			if reportPlanWidgetIsAPIBacked(w.Kind) {
				validateReportPlanAPIWidget(w, section.Heading, locator, result)
				validateReportPlanOptions(w, section.Heading, locator, result)
				continue
			}

			// 1. Source path allowlist.
			if !reportPlanValidSourceRE.MatchString(w.Source) {
				result.Valid = false
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("source %q is not a valid widget source.", w.Source),
					Hint:    "Use db/<file>.json, knowledgebase/graph.json, or knowledgebase/index.json.",
				})
				continue
			}

			// 2. Read (cached) and JSON-parse the source.
			data, hasData := sourceCache[w.Source]
			if !hasData && !sourceMissing[w.Source] {
				content, readErr := readFile(ctx, normalizePathForWorkspaceAPI(w.Source, workspacePath))
				if readErr != nil {
					sourceMissing[w.Source] = true
					result.Valid = false
					result.Errors = append(result.Errors, reportPlanDiagnostic{
						Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("source file %s not found or unreadable: %v", w.Source, readErr),
						Hint:    "Confirm a workflow step actually writes this file — or remove the widget until it does.",
					})
					continue
				}
				var parsed interface{}
				if unmarshalErr := json.Unmarshal([]byte(content), &parsed); unmarshalErr != nil {
					sourceMissing[w.Source] = true
					result.Valid = false
					result.Errors = append(result.Errors, reportPlanDiagnostic{
						Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("source %s is not valid JSON: %v", w.Source, unmarshalErr),
					})
					continue
				}
				sourceCache[w.Source] = parsed
				data = parsed
				hasData = true
			}
			if !hasData {
				continue
			}

			// 3. Resolve dot-path.
			resolved, ok := resolveReportPlanPath(data, w.Path)
			if !ok {
				result.Valid = false
				pathLabel := w.Path
				if pathLabel == "" {
					pathLabel = "(root)"
				}
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("path %q does not resolve inside %s.", pathLabel, w.Source),
					Hint:    "Open the source JSON and pick a real key. Use dot-notation (e.g. entities.0.label); bare `$` means the whole document.",
				})
				continue
			}

			// 4. Filter eligibility — only meaningful on arrays.
			if w.Filter != "" {
				if _, isArr := resolved.([]interface{}); !isArr {
					result.Warnings = append(result.Warnings, reportPlanDiagnostic{
						Severity: "warning", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("filter %q is set but the resolved value is not an array; filter will be ignored.", w.Filter),
					})
				} else if !strings.Contains(w.Filter, "=") {
					result.Warnings = append(result.Warnings, reportPlanDiagnostic{
						Severity: "warning", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("filter %q has no `=`; only `key=value` string equality is supported.", w.Filter),
					})
				}
			}
			resolved = applyReportPlanFilter(resolved, w.Filter)

			// 5. Widget-kind-specific shape checks.
			switch w.Kind {
			case "table":
				validateReportPlanTableShape(w, resolved, section.Heading, locator, result)
			case "chart":
				validateReportPlanChartShape(w, resolved, section.Heading, locator, result)
			case "text":
				// text widgets accept scalars, objects, arrays — nothing to enforce.
			case "stat":
				validateReportPlanStatShape(w, data, section.Heading, locator, result)
			case "alert":
				validateReportPlanAlertShape(w, data, section.Heading, locator, result)
			case "pivot":
				validateReportPlanPivotShape(w, resolved, section.Heading, locator, result)
			}

			// 5b. show_if expression syntax — warn only. Same grammar as
			// evaluateShowIf in reportPlanParser.ts. Resolved against `data`
			// (raw source), not the post-path value.
			if rawShowIf := reportPlanFirstNonEmpty(w.Fields["show_if"], w.Fields["showif"]); rawShowIf != "" {
				if !reportPlanShowIfRE.MatchString(rawShowIf) {
					result.Warnings = append(result.Warnings, reportPlanDiagnostic{
						Severity: "warning", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("show_if %q does not match the supported grammar; widget will always render.", rawShowIf),
						Hint:    "Use `<path>` (truthy), `!<path>` (falsy), or `<path> <op> <value>` where op is >, <, >=, <=, ==, !=.",
					})
				}
			}

			// 6. Option-key sanity (warn, never fatal).
			validateReportPlanOptions(w, section.Heading, locator, result)
		}
	}

	// Dump the parsed plan so the LLM sees exactly what the parser extracted —
	// surfaces the common failure mode where a widget block "looks right" but
	// the fence tag didn't match and every field silently lands in `options`
	// instead of the recognized keys.
	result.Parsed = buildReportPlanParsedDump(sections)

	// Global advice for builder on how to read the result.
	if len(result.Errors) == 0 && len(result.Warnings) == 0 && result.Widgets > 0 {
		result.Suggestions = append(result.Suggestions, "All widgets resolved against real data. Open the Report tab to preview layout.")
	} else {
		if len(result.Errors) > 0 {
			result.Valid = false
		}
		result.Suggestions = append(result.Suggestions, "Fix errors first. Review warnings too — some still degrade rendering even when the report remains technically valid.")
	}
	return result, nil
}

func validateReportPlanTableShape(
	w *reportPlanWidget, resolved interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	arr, ok := resolved.([]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "widget:table needs an array of objects — resolved value is not an array.",
			Hint:    "Point `path:` at an array (e.g. entities, or `$` if the whole file is a list).",
		})
		return
	}
	if len(arr) == 0 {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "table resolves to an empty array — the widget will render nothing until the source is populated.",
		})
		return
	}
	first, ok := arr[0].(map[string]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "widget:table needs array-of-objects — array contains scalars.",
			Hint:    "Reshape the step output to `[{col1: ..., col2: ...}, ...]`.",
		})
		return
	}
	if colorBy := reportPlanFirstNonEmpty(w.Fields["color_by"], w.Fields["colorby"]); colorBy != "" {
		if _, ok := first[colorBy]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("color_by=%q not found on first row of data — rows won't be tinted.", colorBy),
			})
		}
	}
}

func validateReportPlanChartShape(
	w *reportPlanWidget, resolved interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	arr, ok := resolved.([]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "widget:chart needs an array — resolved value is not an array.",
			Hint:    "Charts plot points; point `path:` at an array.",
		})
		return
	}
	if len(arr) == 0 {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "chart resolves to an empty array — nothing will plot until the source is populated.",
		})
		return
	}
	first, ok := arr[0].(map[string]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "chart array must contain objects (got scalars).",
			Hint:    "Reshape to `[{label: ..., value: ...}, ...]` or set x_axis/y_axis to real field names.",
		})
		return
	}
	xAxis := w.Fields["x_axis"]
	if xAxis == "" {
		xAxis = w.Fields["xaxis"]
	}
	yAxis := w.Fields["y_axis"]
	if yAxis == "" {
		yAxis = w.Fields["yaxis"]
	}
	_, hasLabel := first["label"]
	_, hasValue := first["value"]
	if xAxis == "" && yAxis == "" && !(hasLabel && hasValue) {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "chart data has no `label`/`value` keys and no `x_axis`/`y_axis` set — renderer will guess columns.",
			Hint:    "Either pre-shape data to `{label, value}` or set `x_axis: <field>` and `y_axis: <field>`.",
		})
	}
	if xAxis != "" {
		if _, ok := first[xAxis]; !ok {
			if !strings.Contains(xAxis, ".") { // dot-paths are resolved at render-time; skip those
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("x_axis=%q not found on first row of data.", xAxis),
				})
			}
		}
	}
	if yAxis != "" && !strings.Contains(yAxis, ".") {
		if _, ok := first[yAxis]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("y_axis=%q not found on first row of data.", yAxis),
			})
		}
	}
	if colorBy := reportPlanFirstNonEmpty(w.Fields["color_by"], w.Fields["colorby"]); colorBy != "" {
		if _, ok := first[colorBy]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("color_by=%q not found on first row of data — chart will fall back to default palette cycling.", colorBy),
			})
		}
	}
	// Multi-series: every field in `series` must exist on the first row. When
	// series is set, x_axis is the label key; its presence was already checked
	// above. Stacked requires a compatible chart_type (bar/area only).
	if rawSeries := w.Fields["series"]; rawSeries != "" {
		seriesFields := []string{}
		for _, part := range strings.Split(rawSeries, ",") {
			p := strings.TrimSpace(part)
			if p != "" {
				seriesFields = append(seriesFields, p)
			}
		}
		if len(seriesFields) == 0 {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "series is set but parses to an empty list — multi-series is off.",
			})
		}
		for _, f := range seriesFields {
			if _, ok := first[f]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("series field %q not found on first row of data.", f),
				})
			}
		}
		if stacked := parseReportPlanBool(reportPlanFirstNonEmpty(w.Fields["stacked"])); stacked {
			ct := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["chart_type"], w.Fields["charttype"]))
			if ct != "" && ct != "bar" && ct != "area" {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("stacked has no effect on chart_type=%q — only bar and area stack.", ct),
				})
			}
		}
	}
}

// widget:stat — `path:` must resolve to a scalar (string or number). delta_path
// and trend_path are optional; when present they must also resolve.
func validateReportPlanStatShape(
	w *reportPlanWidget, data interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	v, ok := resolveReportPlanPath(data, w.Path)
	if !ok {
		result.Valid = false
		pathLabel := w.Path
		if pathLabel == "" {
			pathLabel = "(root)"
		}
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("stat `path:` %q does not resolve in %s.", pathLabel, w.Source),
			Hint:    "Point `path:` at a scalar field (number or short string).",
		})
		return
	}
	switch v.(type) {
	case nil, bool, float64, string:
		// acceptable scalars (json.Unmarshal uses float64 for numbers)
	default:
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "stat `path:` resolves to an object or array — stat widgets require a scalar value.",
			Hint:    "Pick a leaf field or write a summary JSON file with precomputed scalar metrics; otherwise the UI will stringify the JSON into the KPI tile.",
		})
	}
	if dp := reportPlanFirstNonEmpty(w.Fields["delta_path"], w.Fields["deltapath"]); dp != "" {
		if _, ok := resolveReportPlanPath(data, dp); !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("delta_path %q does not resolve in %s — the delta arrow won't render.", dp, w.Source),
			})
		}
	}
	if tp := reportPlanFirstNonEmpty(w.Fields["trend_path"], w.Fields["trendpath"]); tp != "" {
		resolved, okTP := resolveReportPlanPath(data, tp)
		if !okTP {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("trend_path %q does not resolve in %s — sparkline won't render.", tp, w.Source),
			})
		} else if _, isArr := resolved.([]interface{}); !isArr {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "trend_path resolves to a non-array — sparkline needs an array of numbers.",
			})
		}
	}
}

// widget:alert — severity must be known; either title/message should be set
// (otherwise the banner renders only the raw value). show_if is strongly
// recommended on alerts but not required.
func validateReportPlanAlertShape(
	w *reportPlanWidget, data interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	if sev := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["severity"])); sev != "" {
		if _, ok := reportPlanKnownAlertSeverities[sev]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown severity %q — defaulting to info.", sev),
				Hint:    "Use one of: info, warning, error, success.",
			})
		}
	}
	if w.Fields["title"] == "" && w.Fields["message"] == "" {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "alert has no title or message — it will render just the resolved value.",
			Hint:    "Add `title:` or `message:`. Use `{value}` in message to interpolate the resolved path.",
		})
	}
	if showIf := reportPlanFirstNonEmpty(w.Fields["show_if"], w.Fields["showif"]); showIf == "" {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "alert without show_if renders unconditionally — usually alerts should only show when a condition is true.",
			Hint:    "Add `show_if: <path> > 0` (or similar) so the banner appears only when relevant.",
		})
	}
	// Validate that path resolves when set (used for {value} interpolation).
	if w.Path != "" {
		if _, ok := resolveReportPlanPath(data, w.Path); !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("alert path %q does not resolve — {value} interpolation will render empty.", w.Path),
			})
		}
	}
}

// widget:pivot — rows/columns/values are required; resolved must be array of
// objects; aggregate must be known; first row must have all three fields.
func validateReportPlanPivotShape(
	w *reportPlanWidget, resolved interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	rows := reportPlanFirstNonEmpty(w.Fields["rows"])
	cols := reportPlanFirstNonEmpty(w.Fields["columns"])
	vals := reportPlanFirstNonEmpty(w.Fields["values"])
	if rows == "" || cols == "" || vals == "" {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "pivot requires `rows:`, `columns:`, and `values:` fields.",
			Hint:    "Example: `rows: team`, `columns: month`, `values: net_salary`, `aggregate: sum`.",
		})
		return
	}
	if agg := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["aggregate"])); agg != "" {
		if _, ok := reportPlanKnownPivotAggregates[agg]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown aggregate %q — defaulting to sum.", agg),
				Hint:    "Use one of: sum, avg, count, min, max, first.",
			})
		}
	}
	arr, ok := resolved.([]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "pivot needs an array of objects — resolved value is not an array.",
			Hint:    "Point `path:` at an array (e.g. records).",
		})
		return
	}
	if len(arr) == 0 {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "pivot resolves to an empty array — the grid will render blank until the source is populated.",
		})
		return
	}
	first, ok := arr[0].(map[string]interface{})
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: "pivot array must contain objects (got scalars).",
		})
		return
	}
	for _, f := range []string{rows, cols, vals} {
		if _, ok := first[f]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("pivot field %q not found on first row of data.", f),
			})
		}
	}
}

func validateReportPlanAPIWidget(
	w *reportPlanWidget, section, locator string, result *reportPlanValidationResult,
) {
	if w.Kind == "costs" {
		if raw := reportPlanFirstNonEmpty(w.Fields["scope"]); raw != "" {
			scope := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"phase": {}, "execution": {}, "evaluation": {}, "all": {}}[scope]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown costs scope %q.", raw),
					Hint:    "Use one of: phase, execution, evaluation, all.",
				})
			}
		}
		if raw := reportPlanFirstNonEmpty(w.Fields["view"]); raw != "" {
			view := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"summary": {}, "stage-breakdown": {}, "run-table": {}, "step-table": {}, "model-table": {}}[view]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown costs view %q.", raw),
					Hint:    "Use one of: summary, stage-breakdown, run-table, step-table, model-table.",
				})
			}
		}
		if raw := reportPlanFirstNonEmpty(w.Fields["metric"]); raw != "" {
			metric := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"cost": {}, "total_tokens": {}, "input_tokens": {}, "output_tokens": {}, "llm_calls": {}}[metric]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown costs metric %q.", raw),
					Hint:    "Use one of: cost, total_tokens, input_tokens, output_tokens, llm_calls.",
				})
			}
		}
		return
	}

	if w.Kind == "evals" {
		if raw := reportPlanFirstNonEmpty(w.Fields["view"]); raw != "" {
			view := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"summary": {}, "run-chart": {}, "run-table": {}, "step-table": {}}[view]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown evals view %q.", raw),
					Hint:    "Use one of: summary, run-chart, run-table, step-table.",
				})
			}
		}
		if raw := reportPlanFirstNonEmpty(w.Fields["metric"]); raw != "" {
			metric := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"score_percentage": {}, "total_score": {}}[metric]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown evals metric %q.", raw),
					Hint:    "Use one of: score_percentage, total_score.",
				})
			}
		}
		return
	}

	if w.Kind == "runs" {
		if raw := reportPlanFirstNonEmpty(w.Fields["view"]); raw != "" {
			view := strings.ToLower(raw)
			if _, ok := map[string]struct{}{"summary": {}, "duration-chart": {}, "status-chart": {}, "table": {}}[view]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown runs view %q.", raw),
					Hint:    "Use one of: summary, duration-chart, status-chart, table.",
				})
			}
		}
	}
}

// Small helper mirroring parseBool in reportPlanParser.ts. Only treats 'true'
// / 'yes' / '1' as true; everything else is false.
func parseReportPlanBool(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "true" || s == "yes" || s == "1"
}

func validateReportPlanOptions(
	w *reportPlanWidget, section, locator string, result *reportPlanValidationResult,
) {
	if ct := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["chart_type"], w.Fields["charttype"])); ct != "" {
		if _, ok := reportPlanKnownChartTypes[ct]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown chart_type %q — renderer will fall back to bar.", ct),
				Hint:    "Use one of: bar, line, area, pie.",
			})
		}
	}
	if raw := w.Fields["formats"]; raw != "" {
		for _, part := range strings.Split(raw, ",") {
			eq := strings.Index(part, "=")
			if eq <= 0 {
				continue
			}
			preset := strings.TrimSpace(part[eq+1:])
			if preset == "" {
				continue
			}
			if _, ok := reportPlanKnownFormatters[preset]; !ok {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("unknown format preset %q — cell will fall back to default formatting.", preset),
					Hint:    "Valid: currency-inr, currency-usd, percent, percent-1dp, short-date, long-date, datetime, number, number-1dp, number-2dp, bytes, boolean-icon.",
				})
			}
		}
	}
	if raw := reportPlanFirstNonEmpty(w.Fields["format"]); raw != "" {
		if _, ok := reportPlanKnownFormatters[raw]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown format preset %q — the widget formatter will be ignored.", raw),
				Hint:    "Valid: currency-inr, currency-usd, percent, percent-1dp, short-date, long-date, datetime, number, number-1dp, number-2dp, bytes, boolean-icon.",
			})
		}
	}
	if raw := reportPlanFirstNonEmpty(w.Fields["delta_format"], w.Fields["deltaformat"]); raw != "" {
		if _, ok := reportPlanKnownFormatters[raw]; !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown delta_format preset %q — the stat delta formatter will be ignored.", raw),
				Hint:    "Valid: currency-inr, currency-usd, percent, percent-1dp, short-date, long-date, datetime, number, number-1dp, number-2dp, bytes, boolean-icon.",
			})
		}
	}
	if raw := reportPlanFirstNonEmpty(w.Fields["sort"]); raw != "" {
		s := strings.ToLower(raw)
		if s != "asc" && s != "desc" && s != "none" {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("sort=%q is invalid — expected asc|desc|none.", raw),
			})
		}
	}
	// Color field validation — warn on invalid entries but never fatal; the renderer
	// silently drops bad colors, so surface them here so the builder can fix them.
	validateReportPlanColorList(w, w.Fields["colors"], "colors", section, locator, result)
	validateReportPlanColorList(w, reportPlanFirstNonEmpty(w.Fields["colors_dark"], w.Fields["colorsdark"]), "colors_dark", section, locator, result)
	if colorBy := reportPlanFirstNonEmpty(w.Fields["color_by"], w.Fields["colorby"]); colorBy != "" {
		if w.Kind == "text" {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "color_by has no effect on widget:text — only chart and table use it.",
			})
		}
	}
	if raw := reportPlanFirstNonEmpty(w.Fields["color_map"], w.Fields["colormap"]); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			eq := strings.Index(part, "=")
			if eq <= 0 {
				continue
			}
			value := strings.TrimSpace(part[:eq])
			color := strings.TrimSpace(part[eq+1:])
			if value == "" || color == "" {
				continue
			}
			if !reportPlanIsPlausibleColor(color) {
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("color_map entry %q has invalid color %q.", value, color),
					Hint:    "Use hex (#rrggbb or #rgb) or a CSS color name (red, green, blue, etc.).",
				})
			}
		}
		if w.Fields["color_by"] == "" && w.Fields["colorby"] == "" {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "color_map is set but color_by is not — the map will be ignored.",
				Hint:    "Add `color_by: <field>` naming which row field the map's keys should be compared against.",
			})
		}
	}
}

func validateReportPlanColorList(
	w *reportPlanWidget, raw, fieldName, section, locator string, result *reportPlanValidationResult,
) {
	if raw == "" {
		return
	}
	if w.Kind == "text" {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("%s has no effect on widget:text.", fieldName),
		})
		return
	}
	for _, part := range strings.Split(raw, ",") {
		c := strings.TrimSpace(part)
		if c == "" {
			continue
		}
		if !reportPlanIsPlausibleColor(c) {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("%s contains invalid color %q — it will be skipped.", fieldName, c),
				Hint:    "Use hex (#rrggbb or #rgb) or a CSS color name (red, green, blue, etc.).",
			})
		}
	}
}

// Converts parsed sections into the JSON-friendly dump returned as
// `result.parsed`. Fields captured in the dump mirror the parser's recognized
// keys; anything else the builder wrote in the block lands in `options` so
// typos like `chrt_type` or `show-if` surface visibly.
func buildReportPlanParsedDump(sections []reportPlanSection) []reportPlanParsedSection {
	if len(sections) == 0 {
		return nil
	}
	// Keys the parser consumes into named widget fields. Everything outside this
	// set is surfaced under `options` — that's where typos and unrecognized
	// keys become visible.
	recognized := map[string]struct{}{
		"source": {}, "path": {}, "filter": {}, "show_if": {}, "showif": {},
	}
	out := make([]reportPlanParsedSection, 0, len(sections))
	for _, s := range sections {
		ps := reportPlanParsedSection{Heading: s.Heading}
		for _, w := range s.Widgets {
			pw := reportPlanParsedWidget{
				Kind:     w.Kind,
				Source:   w.Source,
				Path:     w.Path,
				Filter:   w.Filter,
				ShowIf:   reportPlanFirstNonEmpty(w.Fields["show_if"], w.Fields["showif"]),
				Line:     w.LineNum,
				InRow:    w.InRow,
				RowIndex: w.RowIndex,
			}
			// Everything else in w.Fields — the kind-specific options the parser
			// read but didn't promote to a named struct field.
			var opts map[string]string
			for k, v := range w.Fields {
				if _, known := recognized[k]; known {
					continue
				}
				if v == "" {
					continue
				}
				if opts == nil {
					opts = map[string]string{}
				}
				opts[k] = v
			}
			if len(opts) > 0 {
				pw.Options = opts
			}
			ps.Widgets = append(ps.Widgets, pw)
		}
		out = append(out, ps)
	}
	return out
}

func reportPlanFirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func previewReportRender(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (*reportPlanRenderPreviewResult, error) {
	planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return nil, err
	}

	validation, validationErr := validateReportPlan(ctx, workspacePath, readFile)
	if validationErr != nil {
		return nil, validationErr
	}

	var sections []reportPlanSection
	if planRead.Format == "markdown" {
		sections = parseReportPlan(planRead.RawContent)
	} else {
		sections = flattenReportPlanDocument(planRead.Document)
	}
	result := &reportPlanRenderPreviewResult{
		Valid:      validation.Valid,
		Sections:   len(sections),
		Validation: validation,
	}
	result.PlanFormat = planRead.Format
	result.PlanFilePath = planRead.FilePath
	if planRead.Format == "markdown" {
		result.PlanMarkdown = planRead.RawContent
	} else {
		result.PlanJSON = planRead.Document
	}

	sourceCache := map[string]interface{}{}
	sourceErrors := map[string]string{}
	absWorkspacePath := filepath.Join(GetPromptDocsRoot(), workspacePath)
	costsPreview := loadReportPreviewCosts(absWorkspacePath)
	evalsPreview := loadReportPreviewEvaluations(absWorkspacePath)
	runsPreview := loadReportPreviewRuns(absWorkspacePath)

	var rendered strings.Builder
	for _, section := range sections {
		previewSection := reportPlanPreviewSection{Heading: section.Heading}
		rendered.WriteString("## " + section.Heading + "\n\n")
		for _, widget := range section.Widgets {
			result.Widgets++
			pw := buildReportPlanWidgetPreview(
				ctx,
				widget,
				workspacePath,
				readFile,
				sourceCache,
				sourceErrors,
				costsPreview,
				evalsPreview,
				runsPreview,
			)
			previewSection.Widgets = append(previewSection.Widgets, pw)
			state := pw.RenderState
			if state == "" {
				state = "visible"
			}
			title := pw.Title
			if title == "" {
				title = widget.Kind
			}
			rendered.WriteString(fmt.Sprintf("- `%s` %q: %s\n", state, title, pw.Summary))
			if pw.Reason != "" {
				rendered.WriteString(fmt.Sprintf("  reason: %s\n", pw.Reason))
			}
		}
		rendered.WriteString("\n")
		result.SectionsPreview = append(result.SectionsPreview, previewSection)
	}
	result.PreviewMarkdown = strings.TrimSpace(rendered.String())
	return result, nil
}

func buildReportPlanWidgetPreview(
	ctx context.Context,
	w *reportPlanWidget,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	sourceCache map[string]interface{},
	sourceErrors map[string]string,
	costsPreview reportPlanCostsPreviewData,
	evalsPreview []reportPlanEvalPreviewItem,
	runsPreview []reportPlanRunPreviewItem,
) reportPlanPreviewWidget {
	out := reportPlanPreviewWidget{
		Kind:        w.Kind,
		Title:       reportPlanFirstNonEmpty(w.Fields["title"]),
		Description: reportPlanFirstNonEmpty(w.Fields["description"]),
		Source:      w.Source,
		Path:        w.Path,
		Line:        w.LineNum,
		InRow:       w.InRow,
		RowIndex:    w.RowIndex,
		Visible:     true,
		RenderState: "visible",
	}

	if parseReportPlanBool(reportPlanFirstNonEmpty(w.Fields["hidden"])) {
		out.Visible = false
		out.RenderState = "hidden"
		out.Reason = "widget is hidden"
		out.Summary = "hidden"
		return out
	}

	if reportPlanWidgetIsAPIBacked(w.Kind) {
		return buildReportPlanAPIWidgetPreview(w, out, costsPreview, evalsPreview, runsPreview)
	}

	raw, readErr := reportPlanLoadSourceData(ctx, workspacePath, w.Source, readFile, sourceCache, sourceErrors)
	if readErr != "" {
		out.Visible = false
		out.RenderState = "error"
		out.Reason = readErr
		out.Summary = readErr
		return out
	}

	if !reportPlanEvaluateShowIf(raw, reportPlanFirstNonEmpty(w.Fields["show_if"], w.Fields["showif"])) {
		out.Visible = false
		out.RenderState = "hidden"
		out.Reason = "show_if evaluated to false"
		out.Summary = "hidden by show_if"
		return out
	}

	resolved, ok := resolveReportPlanPath(raw, w.Path)
	if !ok && w.Path != "" {
		out.Visible = false
		out.RenderState = "error"
		out.Reason = fmt.Sprintf("path %q does not resolve", w.Path)
		out.Summary = out.Reason
		return out
	}
	if ok {
		resolved = applyReportPlanFilter(resolved, w.Filter)
	} else {
		resolved = raw
	}

	out.DataPreview = reportPlanPreviewValue(resolved)
	out.Summary = reportPlanSummarizeWidgetData(w, resolved)
	return out
}

func buildReportPlanAPIWidgetPreview(
	w *reportPlanWidget,
	out reportPlanPreviewWidget,
	costsPreview reportPlanCostsPreviewData,
	evalsPreview []reportPlanEvalPreviewItem,
	runsPreview []reportPlanRunPreviewItem,
) reportPlanPreviewWidget {
	if w.Kind == "costs" {
		scope := reportPlanFirstNonEmpty(w.Fields["scope"])
		if scope == "" {
			scope = "all"
		}
		view := reportPlanFirstNonEmpty(w.Fields["view"])
		if view == "" {
			view = "summary"
		}
		out.DataPreview = costsPreview
		out.Summary = fmt.Sprintf(
			"costs preview (%s/%s): phase ledger=%t, execution ledgers=%d, evaluation ledgers=%d",
			scope, view, costsPreview.HasPhaseLedger, costsPreview.ExecutionLedgerFiles, costsPreview.EvaluationLedgerFiles,
		)
		return out
	}

	if w.Kind == "evals" {
		selected := filterReportPreviewEvals(evalsPreview, reportPlanFirstNonEmpty(w.Fields["group"]), reportPlanFirstNonEmpty(w.Fields["run_folder"], w.Fields["runfolder"]))
		out.DataPreview = previewSlice(selected, 5)
		out.Summary = fmt.Sprintf("evals preview: %d report(s) matched", len(selected))
		if len(selected) == 0 {
			out.Reason = "no evaluation reports matched current filters"
		}
		return out
	}

	selectedRuns := filterReportPreviewRuns(runsPreview, reportPlanFirstNonEmpty(w.Fields["group"]), reportPlanFirstNonEmpty(w.Fields["run_folder"], w.Fields["runfolder"]))
	out.DataPreview = previewSlice(selectedRuns, 8)
	out.Summary = fmt.Sprintf("runs preview: %d run folder(s) matched", len(selectedRuns))
	if len(selectedRuns) == 0 {
		out.Reason = "no run folders matched current filters"
	}
	return out
}

func reportPlanLoadSourceData(
	ctx context.Context,
	workspacePath string,
	source string,
	readFile func(context.Context, string) (string, error),
	sourceCache map[string]interface{},
	sourceErrors map[string]string,
) (interface{}, string) {
	if source == "" {
		return nil, "source is empty"
	}
	if errMsg, ok := sourceErrors[source]; ok {
		return nil, errMsg
	}
	if cached, ok := sourceCache[source]; ok {
		return cached, ""
	}
	content, err := readFile(ctx, normalizePathForWorkspaceAPI(source, workspacePath))
	if err != nil {
		msg := fmt.Sprintf("source %s is unreadable: %v", source, err)
		sourceErrors[source] = msg
		return nil, msg
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		msg := fmt.Sprintf("source %s is not valid JSON: %v", source, err)
		sourceErrors[source] = msg
		return nil, msg
	}
	sourceCache[source] = parsed
	return parsed, ""
}

func reportPlanEvaluateShowIf(data interface{}, expr string) bool {
	if strings.TrimSpace(expr) == "" {
		return true
	}
	match := reportPlanShowIfRE.FindStringSubmatch(expr)
	if match == nil {
		return true
	}
	negate := match[1] == "!"
	path := match[2]
	op := match[3]
	rhsRaw := strings.TrimSpace(match[4])
	resolved, ok := resolveReportPlanPath(data, path)
	if !ok {
		resolved = nil
	}
	if op == "" {
		truthy := resolved != nil && resolved != false && resolved != 0 && resolved != ""
		if negate {
			return !truthy
		}
		return truthy
	}

	rhsUnquoted := strings.Trim(strings.TrimSpace(rhsRaw), `"'`)
	lhsNum, lhsNumOK := reportPlanAsNumber(resolved)
	rhsNum, rhsNumErr := strconv.ParseFloat(rhsUnquoted, 64)
	if lhsNumOK && rhsNumErr == nil {
		switch op {
		case ">":
			return lhsNum > rhsNum
		case "<":
			return lhsNum < rhsNum
		case ">=":
			return lhsNum >= rhsNum
		case "<=":
			return lhsNum <= rhsNum
		case "==":
			return lhsNum == rhsNum
		case "!=":
			return lhsNum != rhsNum
		}
	}

	lhsStr := ""
	if resolved != nil {
		lhsStr = fmt.Sprintf("%v", resolved)
	}
	switch op {
	case "==":
		return lhsStr == rhsUnquoted
	case "!=":
		return lhsStr != rhsUnquoted
	case ">":
		return lhsStr > rhsUnquoted
	case "<":
		return lhsStr < rhsUnquoted
	case ">=":
		return lhsStr >= rhsUnquoted
	case "<=":
		return lhsStr <= rhsUnquoted
	default:
		return true
	}
}

func reportPlanAsNumber(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		n, err := t.Float64()
		return n, err == nil
	default:
		if v == nil {
			return 0, false
		}
		n, err := strconv.ParseFloat(fmt.Sprintf("%v", v), 64)
		return n, err == nil
	}
}

func reportPlanSummarizeWidgetData(w *reportPlanWidget, resolved interface{}) string {
	switch value := resolved.(type) {
	case nil:
		return "resolved value is null"
	case []interface{}:
		count := len(value)
		if w.Kind == "table" || w.Kind == "chart" || w.Kind == "pivot" {
			return fmt.Sprintf("%d row(s) resolved", count)
		}
		return fmt.Sprintf("%d item(s) resolved", count)
	case map[string]interface{}:
		return fmt.Sprintf("object with %d key(s)", len(value))
	case string:
		if len(value) > 120 {
			return fmt.Sprintf("text (%d chars)", len(value))
		}
		return fmt.Sprintf("text: %q", value)
	default:
		return fmt.Sprintf("value: %v", value)
	}
}

func reportPlanPreviewValue(v interface{}) interface{} {
	switch typed := v.(type) {
	case nil:
		return nil
	case string:
		if len(typed) > 240 {
			return typed[:240] + "..."
		}
		return typed
	case []interface{}:
		return previewSlice(typed, 3)
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		preview := map[string]interface{}{}
		for i, key := range keys {
			if i >= 8 {
				preview["..."] = fmt.Sprintf("%d more key(s)", len(keys)-i)
				break
			}
			preview[key] = reportPlanPreviewValue(typed[key])
		}
		return preview
	default:
		return typed
	}
}

func previewSlice[T any](items []T, max int) []T {
	if len(items) <= max {
		return items
	}
	return items[:max]
}

func loadReportPreviewCosts(absWorkspacePath string) reportPlanCostsPreviewData {
	preview := reportPlanCostsPreviewData{}
	phasePath := filepath.Join(absWorkspacePath, "costs", "phase", "token_usage.json")
	if _, err := os.Stat(phasePath); err == nil {
		preview.HasPhaseLedger = true
	}
	preview.ExecutionLedgerFiles, preview.ExecutionRunFolders = countCostLedgerFiles(filepath.Join(absWorkspacePath, "costs", "execution"))
	preview.EvaluationLedgerFiles, preview.EvaluationRunFolders = countCostLedgerFiles(filepath.Join(absWorkspacePath, "costs", "evaluation"))
	return preview
}

func countCostLedgerFiles(root string) (int, []string) {
	seenRuns := map[string]struct{}{}
	count := 0
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		count++
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var payload struct {
			RunFolders map[string]json.RawMessage `json:"run_folders"`
		}
		if json.Unmarshal(content, &payload) == nil {
			for runFolder := range payload.RunFolders {
				seenRuns[runFolder] = struct{}{}
			}
		}
		return nil
	})
	runs := make([]string, 0, len(seenRuns))
	for runFolder := range seenRuns {
		runs = append(runs, runFolder)
	}
	sort.Strings(runs)
	return count, previewSlice(runs, 8)
}

func loadReportPreviewEvaluations(absWorkspacePath string) []reportPlanEvalPreviewItem {
	root := filepath.Join(absWorkspacePath, "evaluation", "runs")
	out := make([]reportPlanEvalPreviewItem, 0)
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() || info.Name() != EvaluationReportFileName {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var report EvaluationReport
		if json.Unmarshal(content, &report) != nil {
			return nil
		}
		runFolder, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			runFolder = filepath.Dir(path)
		}
		runFolder = filepath.ToSlash(runFolder)
		out = append(out, reportPlanEvalPreviewItem{
			RunFolder:        runFolder,
			GeneratedAt:      report.GeneratedAt,
			ScorePercentage:  report.ScorePercentage,
			TotalScore:       report.TotalScore,
			MaxPossibleScore: report.MaxPossibleScore,
			StepCount:        len(report.StepScores),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].GeneratedAt == out[j].GeneratedAt {
			return out[i].RunFolder < out[j].RunFolder
		}
		return out[i].GeneratedAt > out[j].GeneratedAt
	})
	return out
}

func loadReportPreviewRuns(absWorkspacePath string) []reportPlanRunPreviewItem {
	root := filepath.Join(absWorkspacePath, "runs")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	internalNames := map[string]struct{}{
		"execution": {}, "logs": {}, "learning": {}, "validation": {}, "artifacts": {}, "evaluation": {},
	}
	out := make([]reportPlanRunPreviewItem, 0)
	for _, iteration := range entries {
		if !iteration.IsDir() {
			continue
		}
		iterPath := filepath.Join(root, iteration.Name())
		childDirs, _ := os.ReadDir(iterPath)
		groupDirs := make([]os.DirEntry, 0)
		for _, child := range childDirs {
			if !child.IsDir() {
				continue
			}
			if _, isInternal := internalNames[child.Name()]; isInternal {
				continue
			}
			groupDirs = append(groupDirs, child)
		}
		if len(groupDirs) > 0 {
			for _, child := range groupDirs {
				if info, err := child.Info(); err == nil {
					out = append(out, reportPlanRunPreviewItem{
						RunFolder:  filepath.ToSlash(filepath.Join(iteration.Name(), child.Name())),
						ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
					})
				}
			}
			continue
		}
		if info, err := iteration.Info(); err == nil {
			out = append(out, reportPlanRunPreviewItem{
				RunFolder:  iteration.Name(),
				ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModifiedAt == out[j].ModifiedAt {
			return out[i].RunFolder < out[j].RunFolder
		}
		return out[i].ModifiedAt > out[j].ModifiedAt
	})
	return out
}

func filterReportPreviewEvals(items []reportPlanEvalPreviewItem, group, runFolder string) []reportPlanEvalPreviewItem {
	filtered := make([]reportPlanEvalPreviewItem, 0, len(items))
	for _, item := range items {
		if group != "" && item.RunFolder != group && !strings.HasSuffix(item.RunFolder, "/"+group) {
			continue
		}
		filtered = append(filtered, item)
	}
	if runFolder == "" {
		return filtered
	}
	if runFolder == "latest" {
		if len(filtered) == 0 {
			return nil
		}
		return []reportPlanEvalPreviewItem{filtered[0]}
	}
	out := make([]reportPlanEvalPreviewItem, 0, len(filtered))
	for _, item := range filtered {
		if item.RunFolder == runFolder {
			out = append(out, item)
		}
	}
	return out
}

func filterReportPreviewRuns(items []reportPlanRunPreviewItem, group, runFolder string) []reportPlanRunPreviewItem {
	filtered := make([]reportPlanRunPreviewItem, 0, len(items))
	for _, item := range items {
		if group != "" && item.RunFolder != group && !strings.HasSuffix(item.RunFolder, "/"+group) {
			continue
		}
		filtered = append(filtered, item)
	}
	if runFolder == "" {
		return filtered
	}
	if runFolder == "latest" {
		if len(filtered) == 0 {
			return nil
		}
		return []reportPlanRunPreviewItem{filtered[0]}
	}
	out := make([]reportPlanRunPreviewItem, 0, len(filtered))
	for _, item := range filtered {
		if item.RunFolder == runFolder {
			out = append(out, item)
		}
	}
	return out
}

type reportPlanWidgetLocation struct {
	SectionIndex int
	EntryIndex   int
	WidgetIndex  int
	InRow        bool
}

func writeReportPlanDocument(
	ctx context.Context,
	workspacePath string,
	writeFile func(context.Context, string, string) error,
	doc *reportPlanDocument,
) error {
	doc = normalizeReportPlanDocument(doc)
	content, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report plan: %w", err)
	}
	path := normalizePathForWorkspaceAPI(filepath.Join("reports", "report_plan.json"), workspacePath)
	return writeFile(ctx, path, string(content)+"\n")
}

func reportPlanWidgetToMap(widget reportPlanDocumentWidget) (map[string]interface{}, error) {
	raw, err := json.Marshal(widget)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func reportPlanWidgetFromMap(payload map[string]interface{}) (*reportPlanDocumentWidget, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var widget reportPlanDocumentWidget
	if err := json.Unmarshal(raw, &widget); err != nil {
		return nil, err
	}
	if !reportPlanDocumentWidgetKindAllowed(strings.ToLower(strings.TrimSpace(widget.Kind))) {
		return nil, fmt.Errorf("unsupported widget kind %q", widget.Kind)
	}
	widget.Kind = strings.ToLower(strings.TrimSpace(widget.Kind))
	widget.Path = normalizeReportPlanPath(widget.Path)
	return &widget, nil
}

func reportPlanFindSection(doc *reportPlanDocument, sectionID, heading string) int {
	for i, section := range doc.Sections {
		if sectionID != "" && section.ID == sectionID {
			return i
		}
		if heading != "" && strings.EqualFold(strings.TrimSpace(section.Heading), strings.TrimSpace(heading)) {
			return i
		}
	}
	return -1
}

func reportPlanEnsureSection(doc *reportPlanDocument, sectionID, heading string) int {
	if idx := reportPlanFindSection(doc, sectionID, heading); idx >= 0 {
		return idx
	}
	heading = strings.TrimSpace(heading)
	if heading == "" {
		heading = "Overview"
	}
	section := reportPlanDocumentSection{
		ID:      sectionID,
		Heading: heading,
	}
	if section.ID == "" {
		section.ID = fmt.Sprintf("section-%02d-%s", len(doc.Sections)+1, reportPlanSlug(heading))
	}
	doc.Sections = append(doc.Sections, section)
	return len(doc.Sections) - 1
}

func reportPlanFindRowEntry(doc *reportPlanDocument, rowID string) (int, int, bool) {
	if rowID == "" {
		return -1, -1, false
	}
	for sectionIndex, section := range doc.Sections {
		for entryIndex, entry := range section.Entries {
			if entry.Kind == "row" && entry.ID == rowID {
				return sectionIndex, entryIndex, true
			}
		}
	}
	return -1, -1, false
}

func reportPlanFindWidget(doc *reportPlanDocument, widgetID string) (reportPlanWidgetLocation, bool) {
	for sectionIndex, section := range doc.Sections {
		for entryIndex, entry := range section.Entries {
			if entry.Kind == "single" && entry.Widget != nil && entry.Widget.ID == widgetID {
				return reportPlanWidgetLocation{SectionIndex: sectionIndex, EntryIndex: entryIndex}, true
			}
			if entry.Kind == "row" && entry.Row != nil {
				for widgetIndex, widget := range entry.Row.Widgets {
					if widget.ID == widgetID {
						return reportPlanWidgetLocation{
							SectionIndex: sectionIndex,
							EntryIndex:   entryIndex,
							WidgetIndex:  widgetIndex,
							InRow:        true,
						}, true
					}
				}
			}
		}
	}
	return reportPlanWidgetLocation{}, false
}

func reportPlanRemoveWidgetAt(doc *reportPlanDocument, loc reportPlanWidgetLocation) (reportPlanDocumentWidget, error) {
	section := &doc.Sections[loc.SectionIndex]
	entry := &section.Entries[loc.EntryIndex]
	if !loc.InRow {
		if entry.Widget == nil {
			return reportPlanDocumentWidget{}, fmt.Errorf("widget entry is empty")
		}
		widget := *entry.Widget
		section.Entries = append(section.Entries[:loc.EntryIndex], section.Entries[loc.EntryIndex+1:]...)
		return widget, nil
	}
	if entry.Row == nil || loc.WidgetIndex < 0 || loc.WidgetIndex >= len(entry.Row.Widgets) {
		return reportPlanDocumentWidget{}, fmt.Errorf("row widget index is invalid")
	}
	widget := entry.Row.Widgets[loc.WidgetIndex]
	entry.Row.Widgets = append(entry.Row.Widgets[:loc.WidgetIndex], entry.Row.Widgets[loc.WidgetIndex+1:]...)
	if len(entry.Row.Widgets) == 0 {
		section.Entries = append(section.Entries[:loc.EntryIndex], section.Entries[loc.EntryIndex+1:]...)
	}
	return widget, nil
}

func reportPlanInsertWidget(doc *reportPlanDocument, widget reportPlanDocumentWidget, sectionIndex int, rowID string, index int) error {
	if rowID != "" {
		rowSectionIndex, rowEntryIndex, ok := reportPlanFindRowEntry(doc, rowID)
		if !ok {
			return fmt.Errorf("row_id %q was not found", rowID)
		}
		row := doc.Sections[rowSectionIndex].Entries[rowEntryIndex].Row
		if row == nil {
			return fmt.Errorf("row_id %q is not a row entry", rowID)
		}
		if index < 0 || index > len(row.Widgets) {
			index = len(row.Widgets)
		}
		row.Widgets = append(row.Widgets, reportPlanDocumentWidget{})
		copy(row.Widgets[index+1:], row.Widgets[index:])
		row.Widgets[index] = widget
		return nil
	}
	section := &doc.Sections[sectionIndex]
	entry := reportPlanDocumentEntry{
		Kind:   "single",
		Widget: &widget,
	}
	if index < 0 || index > len(section.Entries) {
		index = len(section.Entries)
	}
	section.Entries = append(section.Entries, reportPlanDocumentEntry{})
	copy(section.Entries[index+1:], section.Entries[index:])
	section.Entries[index] = entry
	return nil
}

func cleanupReportPlanDocument(doc *reportPlanDocument) *reportPlanDocument {
	if doc == nil {
		return normalizeReportPlanDocument(&reportPlanDocument{Version: 1})
	}
	nextSections := make([]reportPlanDocumentSection, 0, len(doc.Sections))
	for _, section := range doc.Sections {
		nextEntries := make([]reportPlanDocumentEntry, 0, len(section.Entries))
		for _, entry := range section.Entries {
			if entry.Kind == "single" {
				if entry.Widget != nil {
					nextEntries = append(nextEntries, entry)
				}
				continue
			}
			if entry.Row != nil && len(entry.Row.Widgets) > 0 {
				nextEntries = append(nextEntries, entry)
			}
		}
		section.Entries = nextEntries
		if len(section.Entries) > 0 {
			nextSections = append(nextSections, section)
		}
	}
	doc.Sections = nextSections
	return normalizeReportPlanDocument(doc)
}

// registerReportPlanValidationTools registers the validate_report_plan tool on an
// MCP agent. Parallels registerEvaluationValidationTools in evaluation_helpers.go.
func registerReportPlanValidationTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"validate_report_plan",
		"Validate reports/report_plan.json after editing it. It parses every widget, reads the JSON sources they point to, and checks: source path allowlist, source file readability, dot-path resolution, widget/data shape compatibility, and option validity. Returns structured per-widget errors + warnings + suggestions plus a parsed dump showing exactly what the validator saw.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			res, err := validateReportPlan(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			out, marshalErr := json.MarshalIndent(res, "", "  ")
			if marshalErr != nil {
				return "", fmt.Errorf("failed to marshal validation result: %w", marshalErr)
			}
			// Prefix with a human-readable summary line so the agent sees the headline first.
			summary := fmt.Sprintf(
				"report_plan validation: valid=%v, sections=%d, widgets=%d, errors=%d, warnings=%d\n",
				res.Valid, res.Sections, res.Widgets, len(res.Errors), len(res.Warnings),
			)
			return summary + string(out), nil
		},
		"workflow",
	)

	logger.Info("✅ Registered report plan validation tool")
	return nil
}

func registerReportPlanManagementTools(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
	writeFile func(context.Context, string, string) error,
) error {
	getPlanSchema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	getPlanParams, _ := parseSchemaForToolParameters(getPlanSchema)

	mcpAgent.RegisterCustomTool(
		"get_report_plan",
		"Read the current report plan from reports/report_plan.json and return its section, entry, row, and widget IDs. Call this before move/remove/toggle operations so you have stable IDs.",
		getPlanParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			payload := map[string]interface{}{
				"canonical_file_path": "reports/report_plan.json",
				"source_file_path":    planRead.FilePath,
				"source_format":       planRead.Format,
				"plan":                planRead.Document,
			}
			out, err := json.MarshalIndent(payload, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal report plan: %w", err)
			}
			return "report plan loaded\n" + string(out), nil
		},
		"workflow",
	)

	widgetConfigSchema := `{
		"type": "object",
		"properties": {
			"id": { "type": "string" },
			"hidden": { "type": "boolean" },
			"source": { "type": "string" },
			"path": { "type": "string" },
			"filter": { "type": "string" },
			"title": { "type": "string" },
			"description": { "type": "string" },
			"height": { "type": "integer" },
			"formats": { "type": "object", "additionalProperties": { "type": "string" } },
			"pageSize": { "type": "integer" },
			"enableSearch": { "type": "boolean" },
			"defaultSort": {
				"type": "object",
				"properties": {
					"field": { "type": "string" },
					"direction": { "type": "string", "enum": ["asc", "desc"] }
				},
				"additionalProperties": false
			},
			"hideColumns": { "type": "array", "items": { "type": "string" } },
			"chartType": { "type": "string", "enum": ["bar", "line", "area", "pie"] },
			"xAxis": { "type": "string" },
			"yAxis": { "type": "string" },
			"topN": { "type": "integer" },
			"sort": { "type": "string", "enum": ["asc", "desc", "none"] },
			"showValues": { "type": "boolean" },
			"colors": { "type": "array", "items": { "type": "string" } },
			"colorsDark": { "type": "array", "items": { "type": "string" } },
			"colorBy": { "type": "string" },
			"colorMap": { "type": "object", "additionalProperties": { "type": "string" } },
			"showIf": { "type": "string" },
			"label": { "type": "string" },
			"prefix": { "type": "string" },
			"suffix": { "type": "string" },
			"format": { "type": "string" },
			"deltaPath": { "type": "string" },
			"deltaFormat": { "type": "string" },
			"trendPath": { "type": "string" },
			"severity": { "type": "string", "enum": ["info", "warning", "error", "success"] },
			"message": { "type": "string" },
			"rowsField": { "type": "string" },
			"columnsField": { "type": "string" },
			"valuesField": { "type": "string" },
			"aggregate": { "type": "string", "enum": ["sum", "avg", "count", "min", "max", "first"] },
			"heatmap": { "type": "boolean" },
			"heatmapColors": { "type": "array", "items": { "type": "string" } },
			"series": { "type": "array", "items": { "type": "string" } },
			"seriesColors": { "type": "array", "items": { "type": "string" } },
			"stacked": { "type": "boolean" },
			"costsScope": { "type": "string", "enum": ["phase", "execution", "evaluation", "all"] },
			"costsView": { "type": "string", "enum": ["summary", "stage-breakdown", "run-table", "step-table", "model-table"] },
			"costsMetric": { "type": "string", "enum": ["cost", "total_tokens", "input_tokens", "output_tokens", "llm_calls"] },
			"evalsView": { "type": "string", "enum": ["summary", "run-chart", "run-table", "step-table"] },
			"evalsMetric": { "type": "string", "enum": ["score_percentage", "total_score"] },
			"runsView": { "type": "string", "enum": ["summary", "duration-chart", "status-chart", "table"] },
			"runFolder": { "type": "string" },
			"group": { "type": "string" }
		},
		"additionalProperties": false
	}`

	upsertSchema := fmt.Sprintf(`{
		"type": "object",
		"properties": {
			"section_id": { "type": "string" },
			"section_heading": { "type": "string" },
			"row_id": { "type": "string" },
			"widget_id": { "type": "string" },
			"kind": { "type": "string", "enum": ["text", "table", "chart", "stat", "alert", "pivot", "costs", "evals", "runs"] },
			"index": { "type": "integer" },
			"config": %s
		},
		"required": ["kind"],
		"additionalProperties": false
	}`, widgetConfigSchema)
	upsertParams, _ := parseSchemaForToolParameters(upsertSchema)

	mcpAgent.RegisterCustomTool(
		"upsert_report_widget",
		"Create or update one report widget in reports/report_plan.json. If widget_id exists, this merges the provided config into the existing widget. If widget_id is omitted, it creates a new widget in the target section; pass row_id to insert into an existing row entry.",
		upsertParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			sectionID, _ := args["section_id"].(string)
			sectionHeading, _ := args["section_heading"].(string)
			rowID, _ := args["row_id"].(string)
			widgetID, _ := args["widget_id"].(string)
			kind, _ := args["kind"].(string)
			kind = strings.ToLower(strings.TrimSpace(kind))
			index := -1
			if value, ok := args["index"].(float64); ok {
				index = int(value)
			}
			config := map[string]interface{}{}
			if rawConfig, ok := args["config"].(map[string]interface{}); ok {
				config = rawConfig
			}

			var base reportPlanDocumentWidget
			hasBase := false
			if widgetID != "" {
				if loc, ok := reportPlanFindWidget(doc, widgetID); ok {
					hasBase = true
					if loc.InRow {
						base = doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Row.Widgets[loc.WidgetIndex]
					} else {
						base = *doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Widget
					}
				}
			}
			payload := map[string]interface{}{}
			if hasBase {
				payload, err = reportPlanWidgetToMap(base)
				if err != nil {
					return "", err
				}
			}
			for key, value := range config {
				payload[key] = value
			}
			payload["kind"] = kind
			if widgetID != "" {
				payload["id"] = widgetID
			}
			widget, err := reportPlanWidgetFromMap(payload)
			if err != nil {
				return "", err
			}
			if widget.ID == "" {
				widget.ID = fmt.Sprintf("widget-%d", time.Now().UnixNano())
			}

			if hasBase {
				loc, _ := reportPlanFindWidget(doc, widget.ID)
				if loc.InRow {
					doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Row.Widgets[loc.WidgetIndex] = *widget
				} else {
					doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Widget = widget
				}
			} else {
				sectionIndex := reportPlanEnsureSection(doc, sectionID, sectionHeading)
				if err := reportPlanInsertWidget(doc, *widget, sectionIndex, rowID, index); err != nil {
					return "", err
				}
			}

			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}

			payloadOut := map[string]interface{}{
				"updated_widget_id": widget.ID,
				"plan":              doc,
			}
			out, err := json.MarshalIndent(payloadOut, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("report widget upserted: %s\n", widget.ID) + string(out), nil
		},
		"workflow",
	)

	removeSchema := `{
		"type": "object",
		"properties": {
			"widget_id": { "type": "string" }
		},
		"required": ["widget_id"],
		"additionalProperties": false
	}`
	removeParams, _ := parseSchemaForToolParameters(removeSchema)
	mcpAgent.RegisterCustomTool(
		"remove_report_widget",
		"Remove one widget from reports/report_plan.json by widget_id. If the widget was the last item in a row, the empty row is removed too. Empty sections are cleaned up automatically.",
		removeParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			widgetID, _ := args["widget_id"].(string)
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			loc, ok := reportPlanFindWidget(doc, widgetID)
			if !ok {
				return "", fmt.Errorf("widget_id %q was not found", widgetID)
			}
			if _, err := reportPlanRemoveWidgetAt(doc, loc); err != nil {
				return "", err
			}
			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(map[string]interface{}{"removed_widget_id": widgetID, "plan": doc}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("report widget removed: %s\n", widgetID) + string(out), nil
		},
		"workflow",
	)

	moveSchema := `{
		"type": "object",
		"properties": {
			"widget_id": { "type": "string" },
			"target_section_id": { "type": "string" },
			"target_section_heading": { "type": "string" },
			"target_row_id": { "type": "string" },
			"target_index": { "type": "integer" }
		},
		"required": ["widget_id"],
		"additionalProperties": false
	}`
	moveParams, _ := parseSchemaForToolParameters(moveSchema)
	mcpAgent.RegisterCustomTool(
		"move_report_widget",
		"Move one widget to a different section position or into an existing row. Use target_row_id to place it inside a row entry; otherwise it becomes a single full-width widget entry in the target section.",
		moveParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			widgetID, _ := args["widget_id"].(string)
			targetSectionID, _ := args["target_section_id"].(string)
			targetSectionHeading, _ := args["target_section_heading"].(string)
			targetRowID, _ := args["target_row_id"].(string)
			targetIndex := -1
			if value, ok := args["target_index"].(float64); ok {
				targetIndex = int(value)
			}
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			loc, ok := reportPlanFindWidget(doc, widgetID)
			if !ok {
				return "", fmt.Errorf("widget_id %q was not found", widgetID)
			}
			widget, err := reportPlanRemoveWidgetAt(doc, loc)
			if err != nil {
				return "", err
			}
			sectionIndex := 0
			if targetRowID == "" {
				sectionIndex = reportPlanEnsureSection(doc, targetSectionID, targetSectionHeading)
			}
			if err := reportPlanInsertWidget(doc, widget, sectionIndex, targetRowID, targetIndex); err != nil {
				return "", err
			}
			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(map[string]interface{}{"moved_widget_id": widgetID, "plan": doc}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("report widget moved: %s\n", widgetID) + string(out), nil
		},
		"workflow",
	)

	toggleSchema := `{
		"type": "object",
		"properties": {
			"widget_id": { "type": "string" },
			"hidden": { "type": "boolean" }
		},
		"required": ["widget_id", "hidden"],
		"additionalProperties": false
	}`
	toggleParams, _ := parseSchemaForToolParameters(toggleSchema)
	mcpAgent.RegisterCustomTool(
		"toggle_report_widget",
		"Set a widget's hidden state in reports/report_plan.json without deleting it. Hidden widgets stay in the plan but do not render in the frontend.",
		toggleParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			widgetID, _ := args["widget_id"].(string)
			hidden, _ := args["hidden"].(bool)
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			loc, ok := reportPlanFindWidget(doc, widgetID)
			if !ok {
				return "", fmt.Errorf("widget_id %q was not found", widgetID)
			}
			if loc.InRow {
				doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Row.Widgets[loc.WidgetIndex].Hidden = hidden
			} else {
				doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Widget.Hidden = hidden
			}
			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(map[string]interface{}{"widget_id": widgetID, "hidden": hidden, "plan": doc}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("report widget toggled: %s hidden=%v\n", widgetID, hidden) + string(out), nil
		},
		"workflow",
	)

	logger.Info("✅ Registered report plan management tools")
	return nil
}

func registerReportRenderPreviewTool(
	mcpAgent *mcpagent.Agent,
	workspacePath string,
	logger loggerv2.Logger,
	readFile func(context.Context, string) (string, error),
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"preview_report_render",
		"Preview how reports/report_plan.json resolves against current workspace data. Returns validation output, a human-readable preview markdown, and per-widget resolved data previews so you can inspect what the final report would show before asking the user to open the Report tab.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			res, err := previewReportRender(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			out, marshalErr := json.MarshalIndent(res, "", "  ")
			if marshalErr != nil {
				return "", fmt.Errorf("failed to marshal report render preview: %w", marshalErr)
			}
			summary := fmt.Sprintf(
				"report render preview: valid=%v, sections=%d, widgets=%d\n",
				res.Valid, res.Sections, res.Widgets,
			)
			return summary + string(out), nil
		},
		"workflow",
	)

	logger.Info("✅ Registered report render preview tool")
	return nil
}
