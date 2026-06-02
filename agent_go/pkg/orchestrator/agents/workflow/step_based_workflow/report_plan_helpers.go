package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
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
	reportPlanValidSourceRE = regexp.MustCompile(`^db/[^/]+\.json$`)
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
	Source string // file/file-list/markdown widgets: a file path under db/, knowledgebase/, docs/
	DB     string // data widgets: SQLite path, e.g. "db/db.sqlite"
	SQL    string // data widgets: read-only query
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
	Version     int                            `json:"version,omitempty"`
	Theme       string                         `json:"theme,omitempty"`
	ThemeColors *reportPlanDocumentThemeColors `json:"themeColors,omitempty"`
	Sections    []reportPlanDocumentSection    `json:"sections"`
}

// Inline custom palette. When set, the renderer converts each hex value to an
// HSL triplet ("H S% L%") and injects them as CSS variables on the report
// root, overriding the named theme. Anything missing falls through to the
// named theme (or the workspace default if no theme is set). Hex strings only
// — the renderer does the HSL conversion so authors think in colors, not HSL.
type reportPlanDocumentThemeColors struct {
	Primary string   `json:"primary,omitempty"`
	Accent  string   `json:"accent,omitempty"`
	Card    string   `json:"card,omitempty"`
	Muted   string   `json:"muted,omitempty"`
	Border  string   `json:"border,omitempty"`
	Chart   []string `json:"chart,omitempty"` // Up to 5 colors, mapped to --chart-1..--chart-5
}

type reportPlanDocumentSection struct {
	ID      string                           `json:"id,omitempty"`
	Heading string                           `json:"heading" jsonschema:"required"`
	Entries []reportPlanDocumentEntry        `json:"entries"`
	Layout  *reportPlanDocumentSectionLayout `json:"layout,omitempty"`
}

type reportPlanDocumentSectionLayout struct {
	Mode    string `json:"mode,omitempty" jsonschema:"enum=grid,enum=tabs"`
	Columns int    `json:"columns,omitempty"`
	Gap     int    `json:"gap,omitempty"`
}

type reportPlanDocumentWidgetLayout struct {
	Span     int `json:"span,omitempty"`
	MinWidth int `json:"minWidth,omitempty"`
}

type reportPlanDocumentEntry struct {
	ID     string                    `json:"id,omitempty"`
	Kind   string                    `json:"kind" jsonschema:"required,enum=single,enum=row"`
	Tab    string                    `json:"tab,omitempty"`
	Widget *reportPlanDocumentWidget `json:"widget,omitempty"`
	Row    *reportPlanDocumentRow    `json:"row,omitempty"`
}

type reportPlanDocumentRow struct {
	Widgets []reportPlanDocumentWidget `json:"widgets" jsonschema:"required"`
}

type reportPlanDocumentDefaultSort struct {
	Field     string `json:"field"`
	Direction string `json:"direction,omitempty" jsonschema:"enum=asc,enum=desc"`
}

type reportPlanDocumentWidget struct {
	ID           string                         `json:"id,omitempty"`
	Hidden       bool                           `json:"hidden,omitempty"`
	Kind         string                         `json:"kind" jsonschema:"required,enum=text,enum=markdown,enum=chart,enum=table,enum=cards,enum=stat,enum=alert,enum=pivot,enum=file,enum=file-list"`
	Source       string                         `json:"source,omitempty"`
	DB           string                         `json:"db,omitempty"`
	SQL          string                         `json:"sql,omitempty"`
	Path         string                         `json:"path,omitempty"`
	Filter       string                         `json:"filter,omitempty"`
	Title        string                         `json:"title,omitempty"`
	Description  string                         `json:"description,omitempty"`
	Height       int                            `json:"height,omitempty"`
	Formats      map[string]string              `json:"formats,omitempty"`
	PageSize     int                            `json:"pageSize,omitempty"`
	EnableSearch *bool                          `json:"enableSearch,omitempty"`
	DefaultSort  *reportPlanDocumentDefaultSort `json:"defaultSort,omitempty"`
	HideColumns  []string                       `json:"hideColumns,omitempty"`
	// Cards-specific layout overrides. Tables ignore them at render time, but
	// the schema accepts them on every widget so a single hand-edited plan can
	// switch a widget between table and cards without losing the field hints.
	Fields               []string                        `json:"fields,omitempty"`
	CardTitleField       string                          `json:"cardTitleField,omitempty"`
	CardSubtitleField    string                          `json:"cardSubtitleField,omitempty"`
	CardDescriptionField string                          `json:"cardDescriptionField,omitempty"`
	CardLinkField        string                          `json:"cardLinkField,omitempty"`
	CardImageField       string                          `json:"cardImageField,omitempty"`
	ChartType            string                          `json:"chartType,omitempty" jsonschema:"enum=bar,enum=line,enum=area,enum=pie"`
	XAxis                string                          `json:"xAxis,omitempty"`
	YAxis                string                          `json:"yAxis,omitempty"`
	TopN                 int                             `json:"topN,omitempty"`
	Sort                 string                          `json:"sort,omitempty" jsonschema:"enum=asc,enum=desc,enum=none"`
	ShowValues           *bool                           `json:"showValues,omitempty"`
	Colors               []string                        `json:"colors,omitempty"`
	ColorsDark           []string                        `json:"colorsDark,omitempty"`
	ColorBy              string                          `json:"colorBy,omitempty"`
	ColorMap             map[string]string               `json:"colorMap,omitempty"`
	ShowIf               string                          `json:"showIf,omitempty"`
	Label                string                          `json:"label,omitempty"`
	Prefix               string                          `json:"prefix,omitempty"`
	Suffix               string                          `json:"suffix,omitempty"`
	Format               string                          `json:"format,omitempty" jsonschema:"enum=currency-inr,enum=currency-usd,enum=percent,enum=percent-1dp,enum=short-date,enum=long-date,enum=datetime,enum=number,enum=number-1dp,enum=number-2dp,enum=bytes,enum=boolean-icon"`
	DeltaPath            string                          `json:"deltaPath,omitempty"`
	DeltaFormat          string                          `json:"deltaFormat,omitempty" jsonschema:"enum=currency-inr,enum=currency-usd,enum=percent,enum=percent-1dp,enum=short-date,enum=long-date,enum=datetime,enum=number,enum=number-1dp,enum=number-2dp,enum=bytes,enum=boolean-icon"`
	TrendPath            string                          `json:"trendPath,omitempty"`
	Severity             string                          `json:"severity,omitempty" jsonschema:"enum=info,enum=warning,enum=error,enum=success"`
	Message              string                          `json:"message,omitempty"`
	RowsField            string                          `json:"rowsField,omitempty"`
	ColumnsField         string                          `json:"columnsField,omitempty"`
	ValuesField          string                          `json:"valuesField,omitempty"`
	Aggregate            string                          `json:"aggregate,omitempty" jsonschema:"enum=sum,enum=avg,enum=count,enum=min,enum=max,enum=first"`
	Heatmap              *bool                           `json:"heatmap,omitempty"`
	HeatmapColors        []string                        `json:"heatmapColors,omitempty"`
	Series               []string                        `json:"series,omitempty"`
	SeriesColors         []string                        `json:"seriesColors,omitempty"`
	Stacked              *bool                           `json:"stacked,omitempty"`
	RenderFormat         string                          `json:"renderFormat,omitempty" jsonschema:"enum=auto,enum=markdown,enum=html,enum=text,enum=code,enum=json,enum=image,enum=video,enum=audio,enum=pdf,enum=link"`
	ListFormat           string                          `json:"listFormat,omitempty" jsonschema:"enum=list,enum=cards,enum=table,enum=gallery"`
	Recursive            *bool                           `json:"recursive,omitempty"`
	Extensions           []string                        `json:"extensions,omitempty"`
	MaxItems             int                             `json:"maxItems,omitempty"`
	Layout               *reportPlanDocumentWidgetLayout `json:"layout,omitempty"`
}

// Public aliases for schema generation. The package-private types above are
// the canonical definitions; these aliases just expose them under stable
// names that the schema-gen command (and any other downstream tooling) can
// reference without the package having to broaden the surface of every
// callsite. Type aliases are zero-cost — they refer to the same underlying
// type — so no runtime or memory implications.
type (
	ReportPlanDocument              = reportPlanDocument
	ReportPlanDocumentSection       = reportPlanDocumentSection
	ReportPlanDocumentSectionLayout = reportPlanDocumentSectionLayout
	ReportPlanDocumentEntry         = reportPlanDocumentEntry
	ReportPlanDocumentRow           = reportPlanDocumentRow
	ReportPlanDocumentWidget        = reportPlanDocumentWidget
	ReportPlanDocumentWidgetLayout  = reportPlanDocumentWidgetLayout
	ReportPlanDocumentDefaultSort   = reportPlanDocumentDefaultSort
	ReportPlanDocumentThemeColors   = reportPlanDocumentThemeColors
)

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
	Widget   string `json:"widget,omitempty"` // short locator e.g. "table@db/companies.json"
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

type reportPlanParsedSection struct {
	Heading string                   `json:"heading"`
	Widgets []reportPlanParsedWidget `json:"widgets"`
}

type reportPlanParsedWidget struct {
	Kind     string `json:"kind"`
	Source   string `json:"source,omitempty"`
	DB       string `json:"db,omitempty"`
	SQL      string `json:"sql,omitempty"`
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

type reportPlanSourceRequirement struct {
	Source  string
	Widgets []string
	Fields  map[string]struct{}
}

type reportPlanProducerContract struct {
	StepID          string
	StepTitle       string
	Source          string
	FromStepConfig  bool
	HasFileRule     bool
	ValidationPaths []string
	ContextOutputs  []string
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
	DB          string      `json:"db,omitempty"`
	SQL         string      `json:"sql,omitempty"`
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

func reportPlanDocumentWidgetKindAllowed(kind string) bool {
	return kind == "text" || kind == "markdown" ||
		kind == "table" || kind == "cards" || kind == "chart" ||
		kind == "stat" || kind == "alert" || kind == "pivot" ||
		kind == "file" || kind == "file-list"
}

func normalizeReportPlanDocument(doc *reportPlanDocument) *reportPlanDocument {
	if doc == nil {
		doc = &reportPlanDocument{}
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	normalized := &reportPlanDocument{Version: doc.Version, Theme: doc.Theme, ThemeColors: doc.ThemeColors}
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
			Layout:  section.Layout,
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
	add("db", widget.DB)
	add("sql", widget.SQL)
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
	if len(widget.Fields) > 0 {
		fields["fields"] = strings.Join(widget.Fields, ", ")
	}
	add("card_title_field", widget.CardTitleField)
	add("card_subtitle_field", widget.CardSubtitleField)
	add("card_description_field", widget.CardDescriptionField)
	add("card_link_field", widget.CardLinkField)
	add("card_image_field", widget.CardImageField)
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
	add("render_format", widget.RenderFormat)
	add("list_format", widget.ListFormat)
	if widget.Recursive != nil {
		fields["recursive"] = strconv.FormatBool(*widget.Recursive)
	}
	if len(widget.Extensions) > 0 {
		fields["extensions"] = strings.Join(widget.Extensions, ", ")
	}
	if widget.MaxItems > 0 {
		fields["max_items"] = strconv.Itoa(widget.MaxItems)
	}
	if widget.Hidden {
		fields["hidden"] = "true"
	}
	return &reportPlanWidget{
		Kind:     widget.Kind,
		Source:   widget.Source,
		DB:       widget.DB,
		SQL:      widget.SQL,
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
			if !reportPlanDocumentWidgetKindAllowed(kind) {
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
	if fields["source"] == "" && !(fields["db"] != "" && fields["sql"] != "") {
		return nil
	}
	return &reportPlanWidget{
		Kind:   kind,
		Source: fields["source"],
		DB:     fields["db"],
		SQL:    fields["sql"],
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
		if fields["source"] == "" && !(fields["db"] != "" && fields["sql"] != "") {
			continue
		}
		out = append(out, &reportPlanWidget{
			Kind:   kind,
			Source: fields["source"],
			DB:     fields["db"],
			SQL:    fields["sql"],
			Path:   normalizeReportPlanPath(fields["path"]),
			Filter: fields["filter"],
			Fields: fields,
		})
	}
	return out
}

func normalizeReportPlanPath(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" || t == "$" || t == "$[*]" || t == "." || t == "*" {
		return ""
	}
	return t
}

func parseReportPlanSourcesField(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		alias := strings.TrimSpace(part[:eq])
		source := strings.TrimSpace(part[eq+1:])
		if alias != "" && source != "" {
			out[alias] = source
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseReportPlanPathSegments splits a path into segments, understanding dot
// keys, bracket indices, and a leading "$"/"$." document-root sigil. Mirrors the
// frontend parsePathSegments (reportPlanParser.ts) so validate_report_plan and
// preview_report_render agree with the renderer. Examples:
//
//	"entities.0.label"      -> ["entities","0","label"]
//	"rows[0].login_success" -> ["rows","0","login_success"]
//	"$[0].login_success"    -> ["0","login_success"]   (the classic alert show_if form)
//	"$.foo.bar"             -> ["foo","bar"]
func parseReportPlanPathSegments(path string) []string {
	p := strings.TrimPrefix(strings.TrimSpace(path), "$")
	var segments []string
	var token strings.Builder
	flush := func() {
		if token.Len() > 0 {
			segments = append(segments, token.String())
			token.Reset()
		}
	}
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '.', '[', ']':
			flush()
		default:
			token.WriteByte(p[i])
		}
	}
	flush()
	return segments
}

func resolveReportPlanPath(data interface{}, path string) (interface{}, bool) {
	if data == nil {
		return nil, false
	}
	if path == "" {
		return data, true
	}
	current := data
	for _, seg := range parseReportPlanPathSegments(path) {
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

// reportPlanWidgetSourcePaths returns the file path a file/file-list/markdown
// widget points at. Data widgets bind via db+sql and have no file source.
func reportPlanWidgetSourcePaths(w *reportPlanWidget) []string {
	if w == nil {
		return nil
	}
	if src := strings.TrimSpace(w.Source); src != "" {
		return []string{src}
	}
	return nil
}

// reportPlanIsDataWidget reports whether the widget binds to db/db.sqlite via SQL.
func reportPlanIsDataWidget(w *reportPlanWidget) bool {
	return w != nil && (strings.TrimSpace(w.DB) != "" || strings.TrimSpace(w.SQL) != "")
}

// reportPlanRowsToValue converts SQL result rows to the generic []interface{}
// shape the path/filter/shape validators operate on.
func reportPlanRowsToValue(rows []map[string]interface{}) interface{} {
	out := make([]interface{}, len(rows))
	for i, r := range rows {
		out[i] = r
	}
	return out
}

func reportPlanIsFileWidgetKind(kind string) bool {
	return kind == "file" || kind == "file-list"
}

func reportPlanValidFileWidgetSource(source string) bool {
	source = strings.TrimSpace(strings.ReplaceAll(source, "\\", "/"))
	if source == "" || strings.HasPrefix(source, "/") || strings.Contains(source, "\x00") {
		return false
	}
	for _, part := range strings.Split(source, "/") {
		if part == ".." {
			return false
		}
	}
	cleaned := filepath.Clean(source)
	if cleaned == "." || strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return false
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	return strings.HasPrefix(cleaned, "db/") ||
		strings.HasPrefix(cleaned, "knowledgebase/") ||
		strings.HasPrefix(cleaned, "docs/")
}

func reportPlanWidgetSourceLabel(w *reportPlanWidget) string {
	if w == nil {
		return ""
	}
	if reportPlanIsDataWidget(w) {
		if sql := strings.TrimSpace(w.SQL); sql != "" {
			return sql
		}
		return w.DB
	}
	return w.Source
}

// reportPlanQueryFunc runs a read-only SQL query against a workflow's SQLite DB
// and returns the result rows as objects keyed by column name. Supplied by the
// workshop controller (→ workspace /api/query). nil when no DB access is wired,
// in which case data widgets are validated structurally only.
type reportPlanQueryFunc func(ctx context.Context, dbPath, sql string) ([]map[string]interface{}, error)

// loadReportPlanWidgetData runs a data widget's SQL against its db and returns the
// result rows as a generic []interface{}. Results are cached per (db, sql) so the
// same query isn't re-run across widgets in one validation pass.
func loadReportPlanWidgetData(
	ctx context.Context,
	workspacePath string,
	queryDB reportPlanQueryFunc,
	w *reportPlanWidget,
	sourceCache map[string]interface{},
	sourceMissing map[string]bool,
	section string,
	locator string,
	result *reportPlanValidationResult,
) (interface{}, bool) {
	if queryDB == nil {
		// No DB access wired (e.g. unit context) — skip data validation.
		return nil, false
	}
	dbPath := normalizePathForWorkspaceAPI(w.DB, workspacePath)
	cacheKey := dbPath + "\n" + strings.TrimSpace(w.SQL)
	if data, ok := sourceCache[cacheKey]; ok {
		return data, true
	}
	if sourceMissing[cacheKey] {
		return nil, false
	}
	rows, err := queryDB(ctx, dbPath, w.SQL)
	if err != nil {
		sourceMissing[cacheKey] = true
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("SQL query failed against %s: %v", w.DB, err),
			Hint:    "Fix the `sql` (test it with sqlite3 against db/db.sqlite) — or confirm a workflow step has populated the table.",
		})
		return nil, false
	}
	data := reportPlanRowsToValue(rows)
	sourceCache[cacheKey] = data
	return data, true
}

// validateReportPlan parses the canonical JSON report plan (with markdown fallback)
// and checks each widget against its referenced source file. Returns a structured
// result for the LLM to act on.
func validateReportPlan(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	queryDB reportPlanQueryFunc,
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
	sourceRequirements := map[string]*reportPlanSourceRequirement{}

	for _, section := range sections {
		for _, w := range section.Widgets {
			result.Widgets++
			sourceLabel := reportPlanWidgetSourceLabel(w)
			locator := fmt.Sprintf("%s@%s", w.Kind, sourceLabel)
			if w.InRow {
				locator = fmt.Sprintf("row[%d]:%s", w.RowIndex, locator)
			}

			// 1. Resolve the widget's data binding. File/file-list/markdown widgets
			// point `source` at a file; data widgets bind via `db` + `sql`.
			if !reportPlanIsDataWidget(w) {
				src := strings.TrimSpace(w.Source)
				if src == "" {
					result.Valid = false
					result.Errors = append(result.Errors, reportPlanDiagnostic{
						Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: "widget has no data binding.",
						Hint:    "Data widgets need `db` + `sql`; file/file-list/markdown widgets need a `source` under db/, knowledgebase/, or docs/.",
					})
					continue
				}
				if !reportPlanValidFileWidgetSource(src) {
					result.Valid = false
					result.Errors = append(result.Errors, reportPlanDiagnostic{
						Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
						Message: fmt.Sprintf("source %q must be a path under db/, knowledgebase/, or docs/.", src),
					})
					continue
				}
				validateReportPlanOptions(w, section.Heading, locator, result)
				continue
			}

			// Data widget: must declare a SQL query against the db.
			if strings.TrimSpace(w.SQL) == "" {
				result.Valid = false
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
					Message: "data widget sets `db` but has no `sql`.",
					Hint:    "Add a read-only `sql` query, e.g. \"SELECT ... FROM <table> ...\".",
				})
				continue
			}

			// 2. Run the SQL and get the result rows (array of row objects).
			data, hasData := loadReportPlanWidgetData(ctx, workspacePath, queryDB, w, sourceCache, sourceMissing, section.Heading, locator, result)
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
					Message: fmt.Sprintf("path %q does not resolve inside %s.", pathLabel, sourceLabel),
					Hint:    "Open the source JSON and pick a real key. Use dot-notation (e.g. entities.0.label); bare `$` means the whole document.",
				})
				continue
			}

			// 4. Filter eligibility — only meaningful on arrays. Stat / alert
			// renderers apply filter to the source array first (then unwrap to
			// a single row before path runs), so the post-path-value array
			// check below doesn't apply to them — their shape validators do
			// the renderer-order check instead.
			if w.Filter != "" {
				if _, isArr := resolved.([]interface{}); !isArr {
					if w.Kind != "stat" && w.Kind != "alert" {
						result.Warnings = append(result.Warnings, reportPlanDiagnostic{
							Severity: "warning", Section: section.Heading, Line: w.LineNum, Widget: locator,
							Message: fmt.Sprintf("filter %q is set but the resolved value is not an array; filter will be ignored.", w.Filter),
						})
					}
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
	validateReportPlanProducerContracts(ctx, workspacePath, readFile, sourceRequirements, result)

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

func trackReportPlanSourceRequirement(reqs map[string]*reportPlanSourceRequirement, w *reportPlanWidget, locator string) {
	if w == nil {
		return
	}
	for _, source := range reportPlanWidgetSourcePaths(w) {
		req := reqs[source]
		if req == nil {
			req = &reportPlanSourceRequirement{
				Source: source,
				Fields: map[string]struct{}{},
			}
			reqs[source] = req
		}
		req.Widgets = append(req.Widgets, locator)
	}
	if strings.TrimSpace(w.Fields["query"]) != "" {
		// JSONata can rename, derive, join, and aggregate fields. For queried
		// widgets the producer contract can deterministically guarantee the input
		// files, while validate_report_plan executes the query against live JSON to
		// verify the transformed output shape.
		return
	}

	addField := func(raw string) {
		for _, source := range reportPlanWidgetSourcePaths(w) {
			reportPlanAddFieldRefs(reqs[source].Fields, raw)
		}
	}
	addField(w.Path)
	if w.Filter != "" {
		if eq := strings.Index(w.Filter, "="); eq > 0 {
			addField(w.Filter[:eq])
		}
	}
	for _, key := range []string{
		"fields", "x_axis", "xaxis", "y_axis", "yaxis", "color_by", "colorby",
		"default_sort", "card_title_field", "card_subtitle_field", "card_description_field",
		"card_link_field", "card_image_field", "delta_path", "deltapath", "trend_path",
		"trendpath", "rows", "columns", "values",
	} {
		addField(w.Fields[key])
	}
	if rawSeries := reportPlanFirstNonEmpty(w.Fields["series"]); rawSeries != "" {
		addField(rawSeries)
	}
	if rawFormats := reportPlanFirstNonEmpty(w.Fields["formats"]); rawFormats != "" {
		for _, part := range strings.Split(rawFormats, ",") {
			if eq := strings.Index(part, "="); eq > 0 {
				addField(part[:eq])
			}
		}
	}
	if rawShowIf := reportPlanFirstNonEmpty(w.Fields["show_if"], w.Fields["showif"]); rawShowIf != "" {
		matches := reportPlanShowIfRE.FindStringSubmatch(rawShowIf)
		if len(matches) >= 3 {
			addField(matches[2])
		}
	}
}

func validateReportPlanProducerContracts(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	reqs map[string]*reportPlanSourceRequirement,
	result *reportPlanValidationResult,
) {
	if len(reqs) == 0 || result == nil {
		return
	}
	contracts, contextOutputs, err := collectReportPlanProducerContracts(ctx, workspacePath, readFile, reqs)
	if err != nil {
		// Missing planning/plan.json is common in isolated unit tests and partially
		// created workspaces; live workflow runs already require the plan.
		if !strings.Contains(err.Error(), "file not found") && !strings.Contains(err.Error(), "not found") {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning",
				Message:  fmt.Sprintf("could not cross-check report sources against planning/plan.json: %v", err),
				Hint:     "Fix planning/plan.json parsing, then rerun validate_report_plan to verify report producer contracts.",
			})
		}
		return
	}

	sources := make([]string, 0, len(reqs))
	for source := range reqs {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	for _, source := range sources {
		req := reqs[source]
		sourceContracts := contracts[source]
		if len(sourceContracts) == 0 {
			candidates := reportPlanContractStepLabels(contextOutputs[source])
			message := fmt.Sprintf("no step validation_schema file rule declares report source %s.", source)
			hint := fmt.Sprintf("Add a validation_schema file rule on the producing step: files: [{file_name: %q, must_exist: true, json_checks: [...]}].", source)
			if len(candidates) > 0 {
				message = fmt.Sprintf("report source %s appears in context_output for %s, but no validation_schema file rule declares it.", source, strings.Join(candidates, ", "))
				hint = fmt.Sprintf("Update that step's validation_schema (or planning/step_config.json override) with file_name: %q and JSON checks for the fields used by the report.", source)
			}
			result.Valid = false
			result.Errors = append(result.Errors, reportPlanDiagnostic{
				Severity: "error",
				Widget:   strings.Join(reportPlanUniqueStrings(req.Widgets), ", "),
				Message:  message,
				Hint:     hint,
			})
			continue
		}

		requiredFields := reportPlanSortedFieldRefs(req.Fields)
		if len(requiredFields) == 0 {
			continue
		}
		validationPaths := reportPlanContractValidationPaths(sourceContracts)
		if len(validationPaths) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, reportPlanDiagnostic{
				Severity: "error",
				Widget:   strings.Join(reportPlanUniqueStrings(req.Widgets), ", "),
				Message:  fmt.Sprintf("validation_schema declares %s but only checks file existence; report widgets also depend on field(s): %s.", source, strings.Join(requiredFields, ", ")),
				Hint:     "Add json_checks for the report-facing fields so pre-validation catches shape regressions before the report renders blank or misleading values.",
			})
			continue
		}
		var missing []string
		for _, field := range requiredFields {
			if !reportPlanValidationPathsMentionField(validationPaths, field) {
				missing = append(missing, field)
			}
		}
		if len(missing) > 0 {
			result.Valid = false
			result.Errors = append(result.Errors, reportPlanDiagnostic{
				Severity: "error",
				Widget:   strings.Join(reportPlanUniqueStrings(req.Widgets), ", "),
				Message:  fmt.Sprintf("validation_schema for %s does not mention report field(s): %s.", source, strings.Join(reportPlanTruncateStrings(missing, 8), ", ")),
				Hint:     "Add json_checks for those paths, or simplify the widget with JSONata/path settings that point at fields already guaranteed by validation.",
			})
		}
	}
}

func collectReportPlanProducerContracts(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
	reqs map[string]*reportPlanSourceRequirement,
) (map[string][]reportPlanProducerContract, map[string][]reportPlanProducerContract, error) {
	plan, err := readPlanFromFile(ctx, workspacePath, readFile)
	if err != nil {
		return nil, nil, err
	}
	stepConfigs, cfgErr := readStepConfigViaFileCallback(ctx, workspacePath, readFile)
	if cfgErr != nil {
		return nil, nil, cfgErr
	}
	configByID := map[string]StepConfig{}
	for _, cfg := range stepConfigs {
		if cfg.ID != "" {
			configByID[cfg.ID] = cfg
		}
	}

	allSteps := append(collectAllSteps(plan.Steps), collectAllSteps(plan.OrphanSteps)...)
	contracts := map[string][]reportPlanProducerContract{}
	contextOutputs := map[string][]reportPlanProducerContract{}
	for _, stepInfo := range allSteps {
		if stepInfo.Step == nil {
			continue
		}
		stepID := stepInfo.Step.GetID()
		stepTitle := stepInfo.Step.GetTitle()
		for _, output := range reportPlanContextOutputPaths(stepInfo.Step.GetContextOutput()) {
			if _, ok := reqs[output]; ok {
				contextOutputs[output] = append(contextOutputs[output], reportPlanProducerContract{
					StepID:         stepID,
					StepTitle:      stepTitle,
					Source:         output,
					ContextOutputs: []string{output},
				})
			}
		}

		schema := stepInfo.Step.GetValidationSchema()
		fromStepConfig := false
		if cfg, ok := configByID[stepID]; ok && cfg.ValidationSchema != nil {
			schema = cfg.ValidationSchema
			fromStepConfig = true
		}
		if schema == nil {
			continue
		}
		for _, fileRule := range schema.Files {
			source := reportPlanNormalizeContractFileName(fileRule.FileName)
			if _, ok := reqs[source]; !ok {
				continue
			}
			contracts[source] = append(contracts[source], reportPlanProducerContract{
				StepID:          stepID,
				StepTitle:       stepTitle,
				Source:          source,
				FromStepConfig:  fromStepConfig,
				HasFileRule:     true,
				ValidationPaths: reportPlanValidationPathsForFileRule(fileRule),
			})
		}
	}
	return contracts, contextOutputs, nil
}

func reportPlanContextOutputPaths(output FlexibleContextOutput) []string {
	raw := output.String()
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var paths []string
	for _, part := range strings.Split(raw, ",") {
		normalized := reportPlanNormalizeContractFileName(part)
		if normalized != "" {
			paths = append(paths, normalized)
		}
	}
	return reportPlanUniqueStrings(paths)
}

func reportPlanNormalizeContractFileName(raw string) string {
	s := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	s = strings.Trim(s, "`'\" ")
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimPrefix(s, "/")
	if idx := strings.LastIndex(s, "/db/"); idx >= 0 {
		s = s[idx+1:]
	}
	return s
}

func reportPlanValidationPathsForFileRule(rule FileValidationRule) []string {
	var paths []string
	for _, check := range rule.JSONChecks {
		if strings.TrimSpace(check.Path) != "" {
			paths = append(paths, check.Path)
		}
		if check.ConsistencyCheck != nil && strings.TrimSpace(check.ConsistencyCheck.CompareWithPath) != "" {
			paths = append(paths, check.ConsistencyCheck.CompareWithPath)
		}
	}
	return reportPlanUniqueStrings(paths)
}

func reportPlanContractValidationPaths(contracts []reportPlanProducerContract) []string {
	var paths []string
	for _, contract := range contracts {
		paths = append(paths, contract.ValidationPaths...)
	}
	return reportPlanUniqueStrings(paths)
}

func reportPlanContractStepLabels(contracts []reportPlanProducerContract) []string {
	var labels []string
	for _, contract := range contracts {
		label := strings.TrimSpace(contract.StepID)
		if contract.StepTitle != "" {
			label = fmt.Sprintf("%s (%s)", contract.StepTitle, contract.StepID)
		}
		if label != "" {
			labels = append(labels, label)
		}
	}
	return reportPlanUniqueStrings(labels)
}

func reportPlanAddFieldRefs(fields map[string]struct{}, raw string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "$" {
		return
	}
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		if field == "" || field == "$" {
			continue
		}
		for _, sep := range []string{"=", ":", " "} {
			if idx := strings.Index(field, sep); idx > 0 {
				field = field[:idx]
			}
		}
		field = strings.Trim(field, "`'\" ")
		field = strings.TrimPrefix(field, "$.")
		field = strings.TrimPrefix(field, ".")
		if field == "" || field == "$" {
			continue
		}
		if strings.Contains(field, ".") {
			fields[field] = struct{}{}
		}
		for _, segment := range strings.Split(field, ".") {
			segment = strings.TrimSpace(segment)
			segment = strings.Trim(segment, "[]`'\" ")
			if segment == "" || segment == "$" {
				continue
			}
			if _, err := strconv.Atoi(segment); err == nil {
				continue
			}
			fields[segment] = struct{}{}
		}
	}
}

func reportPlanSortedFieldRefs(fields map[string]struct{}) []string {
	out := make([]string, 0, len(fields))
	for field := range fields {
		out = append(out, field)
	}
	sort.Strings(out)
	return out
}

func reportPlanValidationPathsMentionField(paths []string, field string) bool {
	fieldTokens := reportPlanFieldTokens(field)
	if len(fieldTokens) == 0 {
		return true
	}
	for _, path := range paths {
		pathTokens := reportPlanFieldTokens(path)
		for fieldToken := range fieldTokens {
			if _, ok := pathTokens[fieldToken]; ok {
				return true
			}
		}
	}
	return false
}

func reportPlanFieldTokens(raw string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		token := strings.ToLower(strings.TrimSpace(b.String()))
		b.Reset()
		if token == "" || token == "$" {
			return
		}
		if _, err := strconv.Atoi(token); err == nil {
			return
		}
		tokens[token] = struct{}{}
	}
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func reportPlanUniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func reportPlanTruncateStrings(values []string, max int) []string {
	if max <= 0 || len(values) <= max {
		return values
	}
	out := append([]string{}, values[:max]...)
	out = append(out, fmt.Sprintf("and %d more", len(values)-max))
	return out
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

// narrowSingularWidgetTarget mirrors the stat / alert renderer's evaluation
// order: when a filter is set on an array source, narrow it to the matching
// row(s) and unwrap a single match to its row before path resolution. Returns
// the target that path: should resolve against, whether the renderer-order
// narrowing actually fired (used to tailor error hints), and a skip flag for
// "source has no rows for this filter yet" — the renderer surfaces that as a
// friendly notice, so we shouldn't error here on volatile data.
func narrowSingularWidgetTarget(
	w *reportPlanWidget, data interface{}, kind, section, locator string,
	result *reportPlanValidationResult,
) (target interface{}, filterApplied bool, skip bool) {
	target = data
	if w.Filter == "" {
		return target, false, false
	}
	arr, ok := data.([]interface{})
	if !ok {
		return target, false, false
	}
	filtered, _ := applyReportPlanFilter(arr, w.Filter).([]interface{})
	switch len(filtered) {
	case 0:
		// Source is empty for this filter at validation time. Data is
		// volatile (steps may not have run yet); the renderer shows a
		// "no rows match filter" notice. Don't synthesize a structural
		// error from a transient empty slice.
		return target, true, true
	case 1:
		return filtered[0], true, false
	default:
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("%s filter %q matches %d rows in current source — %s widgets need exactly one row. The renderer will show a multi-match error.", kind, w.Filter, len(filtered), kind),
			Hint:    "Tighten the filter to match a single row, or precompute a singleton record.",
		})
		return filtered[0], true, false
	}
}

// looksLikeArrayIndexedPath reports whether a path starts with a numeric
// segment (e.g. "0", "0.balance", "12.field") — the most common shape for a
// stat / alert path that ought to drop its index when filter narrows the
// source to a single row.
func looksLikeArrayIndexedPath(path string) bool {
	if path == "" {
		return false
	}
	first := path
	if dot := strings.Index(path, "."); dot >= 0 {
		first = path[:dot]
	}
	if first == "" {
		return false
	}
	for _, r := range first {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// widget:stat — `path:` must resolve to a scalar (string or number). When
// `filter:` is set on an array source, the renderer narrows to a single row
// first and resolves `path:` against that row, so `path:` should address a
// field of the row directly (no leading array index). delta_path and
// trend_path resolve against the same target as path.
func validateReportPlanStatShape(
	w *reportPlanWidget, data interface{},
	section, locator string, result *reportPlanValidationResult,
) {
	target, filterApplied, skip := narrowSingularWidgetTarget(w, data, "stat", section, locator, result)
	if skip {
		return
	}
	v, ok := resolveReportPlanPath(target, w.Path)
	if !ok {
		result.Valid = false
		pathLabel := w.Path
		if pathLabel == "" {
			pathLabel = "(root)"
		}
		sourceLabel := reportPlanWidgetSourceLabel(w)
		hint := "Point `path:` at a scalar field (number or short string)."
		if filterApplied && looksLikeArrayIndexedPath(w.Path) {
			hint = "With `filter:` set, the source narrows to a single row before `path:` resolves — drop the leading array index from `path:` and use the bare field name (e.g. `balance` instead of `0.balance`)."
		}
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("stat `path:` %q does not resolve in %s.", pathLabel, sourceLabel),
			Hint:    hint,
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
		if _, ok := resolveReportPlanPath(target, dp); !ok {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("delta_path %q does not resolve in %s — the delta arrow won't render.", dp, reportPlanWidgetSourceLabel(w)),
			})
		}
	}
	if tp := reportPlanFirstNonEmpty(w.Fields["trend_path"], w.Fields["trendpath"]); tp != "" {
		resolved, okTP := resolveReportPlanPath(target, tp)
		if !okTP {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("trend_path %q does not resolve in %s — sparkline won't render.", tp, reportPlanWidgetSourceLabel(w)),
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
	// Mirror the alert renderer's filter → unwrap → path order so a stale
	// `0.field` path with a filter is flagged at validation time rather than
	// silently swallowed.
	if w.Path != "" {
		target, filterApplied, skip := narrowSingularWidgetTarget(w, data, "alert", section, locator, result)
		if !skip {
			if _, ok := resolveReportPlanPath(target, w.Path); !ok {
				hint := ""
				if filterApplied && looksLikeArrayIndexedPath(w.Path) {
					hint = "With `filter:` set, the source narrows to a single row before `path:` resolves — drop the leading array index from `path:` and use the bare field name."
				}
				result.Warnings = append(result.Warnings, reportPlanDiagnostic{
					Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
					Message: fmt.Sprintf("alert path %q does not resolve — {value} interpolation will render empty.", w.Path),
					Hint:    hint,
				})
			}
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

// Small helper mirroring parseBool in reportPlanParser.ts. Only treats 'true'
// / 'yes' / '1' as true; everything else is false.
func parseReportPlanBool(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "true" || s == "yes" || s == "1"
}

func validateReportPlanOptions(
	w *reportPlanWidget, section, locator string, result *reportPlanValidationResult,
) {
	if raw := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["render_format"], w.Fields["renderformat"])); raw != "" {
		switch raw {
		case "auto", "markdown", "html", "text", "code", "json", "image", "video", "audio", "pdf", "link":
		default:
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown render_format %q — file widget will fall back to auto.", raw),
				Hint:    "Use one of: auto, markdown, html, text, code, json, image, video, audio, pdf, link.",
			})
		}
		if !reportPlanIsFileWidgetKind(w.Kind) {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "render_format has no effect except on file widgets.",
			})
		}
	}
	if raw := strings.ToLower(reportPlanFirstNonEmpty(w.Fields["list_format"], w.Fields["listformat"])); raw != "" {
		switch raw {
		case "list", "cards", "table", "gallery":
		default:
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown list_format %q — file-list widget will fall back to list.", raw),
				Hint:    "Use one of: list, cards, table, gallery.",
			})
		}
		if w.Kind != "file-list" {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: "list_format has no effect except on file-list widgets.",
			})
		}
	}
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
		if w.Kind == "text" || reportPlanIsFileWidgetKind(w.Kind) {
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("color_by has no effect on widget:%s — only chart and table use it.", w.Kind),
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
	if w.Kind == "text" || reportPlanIsFileWidgetKind(w.Kind) {
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: fmt.Sprintf("%s has no effect on widget:%s.", fieldName, w.Kind),
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
		"source": {}, "db": {}, "sql": {}, "path": {}, "filter": {}, "show_if": {}, "showif": {},
	}
	out := make([]reportPlanParsedSection, 0, len(sections))
	for _, s := range sections {
		ps := reportPlanParsedSection{Heading: s.Heading}
		for _, w := range s.Widgets {
			pw := reportPlanParsedWidget{
				Kind:     w.Kind,
				Source:   w.Source,
				DB:       w.DB,
				SQL:      w.SQL,
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

// reportPlanLooksLikeFlattenArtifact heuristically flags filenames that are
// almost always pre-flattened helpers derived from a canonical source — the
// validator suggests collapsing them via an in-widget JSONata query.
func reportPlanLooksLikeFlattenArtifact(base string) bool {
	lower := strings.ToLower(base)
	if !strings.HasSuffix(lower, ".json") {
		return false
	}
	stem := strings.TrimSuffix(lower, ".json")
	if strings.HasSuffix(stem, "_rows") || strings.HasSuffix(stem, "_summary") || strings.HasSuffix(stem, "_summary_rows") {
		return true
	}
	if strings.HasPrefix(stem, "flat_") || strings.HasPrefix(stem, "flattened_") {
		return true
	}
	return false
}

// evalReportPlanQuery evaluates a JSONata expression against the raw source
// JSON, mirroring the browser pipeline (source → query → path → filter →
// render). Returns the transformed value or an error.
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
	queryDB reportPlanQueryFunc,
) (*reportPlanRenderPreviewResult, error) {
	planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return nil, err
	}

	validation, validationErr := validateReportPlan(ctx, workspacePath, readFile, queryDB)
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
				queryDB,
				sourceCache,
				sourceErrors,
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
	queryDB reportPlanQueryFunc,
	sourceCache map[string]interface{},
	sourceErrors map[string]string,
) reportPlanPreviewWidget {
	out := reportPlanPreviewWidget{
		Kind:        w.Kind,
		Title:       reportPlanFirstNonEmpty(w.Fields["title"]),
		Description: reportPlanFirstNonEmpty(w.Fields["description"]),
		Source:      w.Source,
		DB:          w.DB,
		SQL:         w.SQL,
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

	// File / file-list / markdown-from-file widgets render a file, not data.
	if !reportPlanIsDataWidget(w) {
		if !reportPlanValidFileWidgetSource(w.Source) {
			out.Visible = false
			out.RenderState = "error"
			out.Reason = "file widget source must be under db/, knowledgebase/, or docs/"
			out.Summary = out.Reason
			return out
		}
		if w.Kind == "file-list" {
			out.Summary = fmt.Sprintf("file list for folder %s", w.Source)
			if format := reportPlanFirstNonEmpty(w.Fields["list_format"], w.Fields["listformat"]); format != "" {
				out.Summary += fmt.Sprintf(" (%s)", format)
			}
		} else {
			out.Summary = fmt.Sprintf("file artifact %s", w.Source)
			if format := reportPlanFirstNonEmpty(w.Fields["render_format"], w.Fields["renderformat"]); format != "" {
				out.Summary += fmt.Sprintf(" (%s)", format)
			}
		}
		out.DataPreview = map[string]interface{}{
			"source":        w.Source,
			"render_format": reportPlanFirstNonEmpty(w.Fields["render_format"], w.Fields["renderformat"]),
			"list_format":   reportPlanFirstNonEmpty(w.Fields["list_format"], w.Fields["listformat"]),
		}
		return out
	}

	raw, readErr := reportPlanLoadWidgetPreviewData(ctx, workspacePath, w, queryDB, sourceCache, sourceErrors)
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

// reportPlanLoadWidgetPreviewData runs a data widget's SQL against its db and
// returns the result rows as []interface{}. Cached per (db, sql) so repeated
// widgets don't re-run the same query. Returns ("", data) on success or a
// non-empty error string on failure.
func reportPlanLoadWidgetPreviewData(
	ctx context.Context,
	workspacePath string,
	w *reportPlanWidget,
	queryDB reportPlanQueryFunc,
	sourceCache map[string]interface{},
	sourceErrors map[string]string,
) (interface{}, string) {
	if queryDB == nil {
		return nil, "no database access wired for preview"
	}
	if strings.TrimSpace(w.SQL) == "" {
		return nil, "data widget has no sql"
	}
	dbPath := normalizePathForWorkspaceAPI(w.DB, workspacePath)
	cacheKey := dbPath + "\n" + strings.TrimSpace(w.SQL)
	if errMsg, ok := sourceErrors[cacheKey]; ok {
		return nil, errMsg
	}
	if cached, ok := sourceCache[cacheKey]; ok {
		return cached, ""
	}
	rows, err := queryDB(ctx, dbPath, w.SQL)
	if err != nil {
		msg := fmt.Sprintf("SQL query failed against %s: %v", w.DB, err)
		sourceErrors[cacheKey] = msg
		return nil, msg
	}
	data := reportPlanRowsToValue(rows)
	sourceCache[cacheKey] = data
	return data, ""
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

func reportPlanInsertWidget(doc *reportPlanDocument, widget reportPlanDocumentWidget, sectionIndex int, rowID string, index int, tab string) error {
	tab = strings.TrimSpace(tab)
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
		if tab != "" {
			doc.Sections[rowSectionIndex].Entries[rowEntryIndex].Tab = tab
		}
		return nil
	}
	section := &doc.Sections[sectionIndex]
	entry := reportPlanDocumentEntry{
		Kind:   "single",
		Widget: &widget,
	}
	if tab != "" {
		entry.Tab = tab
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
	queryDB reportPlanQueryFunc,
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"validate_report_plan",
		"Validate reports/report_plan.json after editing it. It parses every widget, runs each data widget's `sql` against db/db.sqlite (read-only), and checks: file-source path allowlist, SQL executes, dot-path resolution against the result rows, widget/data shape compatibility, and option validity. Returns structured per-widget errors + warnings + suggestions plus a parsed dump showing exactly what the validator saw.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			res, err := validateReportPlan(ctx, workspacePath, readFile, queryDB)
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
			"source": { "type": "string", "description": "Single source path. JSON widgets use db/<file>.json. file/file-list widgets use db/, knowledgebase/, or docs/ paths." },
			"sources": { "type": "object", "additionalProperties": { "type": "string" }, "description": "Named db sources for JSONata joins, e.g. {\"runs\":\"db/runs.json\",\"costs\":\"db/costs.json\"}. The JSONata input becomes {runs: <runs JSON>, costs: <costs JSON>}. Every source must be covered by step validation_schema." },
			"path": { "type": "string", "description": "Dot-notation path into the source JSON. For collection widgets (table, chart, cards, pivot) this should resolve to an array. For stat / alert widgets the renderer applies filter first and then resolves path against the matched row, so when filter is set, path must be a bare field name on the row (e.g. \"balance\"), NOT \"0.balance\". Use a leading numeric index only when there is no filter and you intend to pick by position." },
			"filter": { "type": "string", "description": "Narrows an array source to matching rows. Format: \"key=value\" (string equality). For stat / alert widgets, filter is the right way to pick one row by name — it narrows the array to a single row that path resolves against. For collection widgets, filter narrows the array passed to the renderer (table rows, chart points, etc.)." },
			"query": { "type": "string", "description": "JSONata expression evaluated before path/filter/widget rendering. With source, it runs against that source JSON. With sources, it runs against an object keyed by alias, e.g. {runs: ..., costs: ...}, so joins/aggregates can read runs.rows and costs.rows together. Use for report-time transforms like $sum(rows.amount), rows[status='paid'], or costs.rows.{run_id: run_id, amount: amount}. If query returns the final scalar/array, leave path empty or '$'." },
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
			"fields": { "type": "array", "items": { "type": "string" } },
			"cardTitleField": { "type": "string" },
			"cardSubtitleField": { "type": "string" },
			"cardDescriptionField": { "type": "string" },
			"cardLinkField": { "type": "string" },
			"cardImageField": { "type": "string" },
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
			"renderFormat": { "type": "string", "enum": ["auto", "markdown", "html", "text", "code", "json", "image", "video", "audio", "pdf", "link"], "description": "file widget only. auto infers from extension; explicit formats render markdown/html/text/code/json/image/video/audio/pdf or a link tile." },
			"listFormat": { "type": "string", "enum": ["list", "cards", "table", "gallery"], "description": "file-list widget only. gallery is best for image/video evidence; table is best for dense inventories." },
			"recursive": { "type": "boolean", "description": "file-list widget only. Whether to include nested folder files." },
			"extensions": { "type": "array", "items": { "type": "string" }, "description": "file-list widget only. Optional extension allowlist, e.g. [\"png\", \"jpg\", \"pdf\"]." },
			"maxItems": { "type": "integer", "description": "file-list widget only. Caps displayed files." },
			"layout": {
				"type": "object",
				"properties": {
					"span": { "type": "integer", "minimum": 1, "maximum": 24 },
					"minWidth": { "type": "integer", "minimum": 1 }
				},
				"additionalProperties": false
			}
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
			"tab": { "type": "string", "description": "Optional tab label for this widget entry when the section layout mode is tabs. Use the user-facing route name, e.g. \"Happy path\" or \"Fallback route\". Prefer setting this for widgets tied to todo_task/orchestration/routing outputs so each route has its own tab. Updating an existing widget with tab sets or clears the containing entry tab." },
			"kind": { "type": "string", "enum": ["text", "markdown", "table", "cards", "chart", "stat", "alert", "pivot", "file", "file-list"] },
			"index": { "type": "integer" },
			"config": %s
		},
		"required": ["kind"],
		"additionalProperties": false
	}`, widgetConfigSchema)
	upsertParams, _ := parseSchemaForToolParameters(upsertSchema)

	mcpAgent.RegisterCustomTool(
		"upsert_report_widget",
		"Create or update one report widget in reports/report_plan.json. If widget_id exists, this merges the provided config into the existing widget. If widget_id is omitted, it creates a new widget in the target section; pass row_id to insert into an existing row entry.\n\n"+
			"Supported widget kinds: text, markdown (formatted text/markdown body), table, cards (record tiles with title/subtitle/description/image fields — set cardTitleField etc.), chart (bar/line/area/pie), stat (KPI tile + delta + sparkline), alert (severity callout), pivot (rows × cols × aggregate), file (render one stored artifact), file-list (list a folder of artifacts).\n\n"+
			"Data binding: use `source: \"db/file.json\"` for one JSON file. Use `sources: {\"alias\":\"db/file.json\", ...}` plus `query` for JSONata joins across multiple db files; the query input is an object keyed by alias. Do not create helper db files only to join/filter/sum report data when JSONata can do it in the widget. For artifacts, use `kind:\"file\"` with source under db/, knowledgebase/, or docs/ and optional `renderFormat`; for multiple images/videos/PDFs/etc use `kind:\"file-list\"` with source folder plus `listFormat`, `recursive`, `extensions`, and `maxItems`.\n\n"+
			"Chart configuration: single-series uses xAxis + yAxis (or relies on canonical {label,value} keys). For multi-series — overlaying multiple lines/bars on the same axes — set `series: [\"field_a\", \"field_b\", ...]` and `xAxis` (each row in the source contributes one x-tick; each series field becomes one plotted line/bar). Optional: `seriesColors` (hex parallel to series), `stacked: true` for bar/area to stack instead of group. Tooltip and legend render automatically.\n\n"+
			"Per-widget grid layout: when the parent section has section.layout.columns set, pass `layout: { span: N, minWidth: 320 }` in config to span N grid columns. For route dashboards, prefer set_section_layout(mode=\"tabs\") on one shared conceptual section and pass `tab: \"Route name\"` so entries with the same route label render together. Use tabs by default for todo_task predefined routes, orchestration/routing branches, or any plan where route outputs would otherwise be mixed in one long section; use a combined table only when the user explicitly wants cross-route comparison or the route outputs share one schema. Use set_report_theme to swap the chart palette report-wide.",
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
			tab, hasTab := args["tab"].(string)
			tab = strings.TrimSpace(tab)
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
				if hasTab {
					doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Tab = tab
				}
				if loc.InRow {
					doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Row.Widgets[loc.WidgetIndex] = *widget
				} else {
					doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Widget = widget
				}
			} else {
				sectionIndex := reportPlanEnsureSection(doc, sectionID, sectionHeading)
				if err := reportPlanInsertWidget(doc, *widget, sectionIndex, rowID, index, tab); err != nil {
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
			sourceTab := ""
			if loc.SectionIndex >= 0 && loc.SectionIndex < len(doc.Sections) &&
				loc.EntryIndex >= 0 && loc.EntryIndex < len(doc.Sections[loc.SectionIndex].Entries) {
				sourceTab = doc.Sections[loc.SectionIndex].Entries[loc.EntryIndex].Tab
			}
			widget, err := reportPlanRemoveWidgetAt(doc, loc)
			if err != nil {
				return "", err
			}
			sectionIndex := 0
			if targetRowID == "" {
				sectionIndex = reportPlanEnsureSection(doc, targetSectionID, targetSectionHeading)
			}
			if err := reportPlanInsertWidget(doc, widget, sectionIndex, targetRowID, targetIndex, sourceTab); err != nil {
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

	themeSchema := `{
		"type": "object",
		"properties": {
			"theme": { "type": ["string", "null"] },
			"colors": {
				"type": ["object", "null"],
				"properties": {
					"primary":  { "type": "string" },
					"accent":   { "type": "string" },
					"card":     { "type": "string" },
					"muted":    { "type": "string" },
					"border":   { "type": "string" },
					"chart":    { "type": "array", "items": { "type": "string" }, "maxItems": 5 }
				},
				"additionalProperties": false
			}
		},
		"additionalProperties": false
	}`
	themeParams, _ := parseSchemaForToolParameters(themeSchema)
	mcpAgent.RegisterCustomTool(
		"set_report_theme",
		"Set the plan-level theme on reports/report_plan.json. Two ways to use this:\n\n"+
			"1. **Named theme** — pass theme: \"anthropic\" / \"brand\" / \"warm\" / \"cool\". The bundled CSS blocks override --chart-1..5, --primary, --accent, and surface tints across the report. Quickest path; no color authoring needed. \"anthropic\" is the recommended default: a warm editorial \"paper\" palette (ivory surfaces, warm near-black text, a single clay/terracotta accent, muted earthy charts) that overrides the full surface + semantic token set.\n\n"+
			"2. **Inline custom palette** — pass colors: { primary, accent, card, muted, border, chart: [c1,c2,c3,c4,c5] } with hex strings (e.g. \"#cc0000\"). The renderer converts each hex to HSL and injects them as CSS variables on the report root, overriding any named theme. Use this for brand-specific palettes (e.g. \"HDFC red\", \"Citi blue\") that no bundled theme matches. All fields are optional — anything you omit falls through to the named theme (if set) or the workspace default.\n\n"+
			"You can pass both — theme provides the baseline, colors fine-tune individual variables on top. Pass theme: null and colors: null to clear everything and revert to workspace defaults. Themes scope to the report subtree only; surrounding app chrome is unaffected.",
		themeParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			// Theme name — accept the field even if not provided so the agent can
			// clear it explicitly with theme: null.
			if raw, ok := args["theme"]; ok {
				if str, isStr := raw.(string); isStr {
					doc.Theme = strings.TrimSpace(str)
				} else if raw == nil {
					doc.Theme = ""
				}
			}
			// Inline colors — same semantics: explicit null clears, missing key
			// leaves whatever the plan already had untouched (set_report_theme
			// shouldn't be a footgun that wipes a colors block when the agent
			// only meant to change the theme name).
			if raw, ok := args["colors"]; ok {
				if raw == nil {
					doc.ThemeColors = nil
				} else if obj, isObj := raw.(map[string]interface{}); isObj {
					colors := &reportPlanDocumentThemeColors{}
					if v, ok := obj["primary"].(string); ok {
						colors.Primary = strings.TrimSpace(v)
					}
					if v, ok := obj["accent"].(string); ok {
						colors.Accent = strings.TrimSpace(v)
					}
					if v, ok := obj["card"].(string); ok {
						colors.Card = strings.TrimSpace(v)
					}
					if v, ok := obj["muted"].(string); ok {
						colors.Muted = strings.TrimSpace(v)
					}
					if v, ok := obj["border"].(string); ok {
						colors.Border = strings.TrimSpace(v)
					}
					if arr, ok := obj["chart"].([]interface{}); ok {
						for _, item := range arr {
							if s, isStr := item.(string); isStr {
								colors.Chart = append(colors.Chart, strings.TrimSpace(s))
							}
						}
					}
					// Don't store an empty palette — it's noise on disk.
					empty := colors.Primary == "" && colors.Accent == "" && colors.Card == "" &&
						colors.Muted == "" && colors.Border == "" && len(colors.Chart) == 0
					if empty {
						doc.ThemeColors = nil
					} else {
						doc.ThemeColors = colors
					}
				}
			}
			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(map[string]interface{}{
				"theme":       doc.Theme,
				"themeColors": doc.ThemeColors,
				"plan":        doc,
			}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("report theme set: theme=%q, colors=%v\n", doc.Theme, doc.ThemeColors != nil) + string(out), nil
		},
		"workflow",
	)

	sectionLayoutSchema := `{
		"type": "object",
		"properties": {
			"section_id": { "type": "string" },
			"section_heading": { "type": "string" },
			"mode": { "type": ["string", "null"], "enum": ["grid", "tabs", null], "description": "Use grid for normal grid layout, tabs to group entries by their tab label, or null/omit with columns null to clear layout." },
			"columns": { "type": ["integer", "null"], "minimum": 1, "maximum": 24 },
			"gap": { "type": ["integer", "null"], "minimum": 0, "maximum": 64 }
		},
		"additionalProperties": false
	}`
	sectionLayoutParams, _ := parseSchemaForToolParameters(sectionLayoutSchema)
	mcpAgent.RegisterCustomTool(
		"set_section_layout",
		"Set or clear a section's layout. mode=grid keeps the normal grid layout; mode=tabs renders entries grouped by their entry tab label, useful for workflow routes. For routed workflows, prefer one shared conceptual section in mode=tabs with each route's widgets assigned the route name via upsert_report_widget(tab=...). When columns is set (1–24), the active tab/grid entries flow into a CSS Grid of that width and individual widgets honor layout.span. Pass mode:null and columns:null (or omit both) to clear layout. Identify the section via section_id (preferred — call get_report_plan first) or section_heading.",
		sectionLayoutParams,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
			if err != nil {
				return "", err
			}
			doc := cleanupReportPlanDocument(planRead.Document)
			sectionID, _ := args["section_id"].(string)
			sectionHeading, _ := args["section_heading"].(string)
			idx := reportPlanFindSection(doc, sectionID, sectionHeading)
			if idx < 0 {
				return "", fmt.Errorf("section not found (section_id=%q, section_heading=%q) — call get_report_plan first", sectionID, sectionHeading)
			}
			columns := 0
			if raw, ok := args["columns"].(float64); ok {
				columns = int(raw)
			}
			gap := 0
			if raw, ok := args["gap"].(float64); ok {
				gap = int(raw)
			}
			mode := ""
			if raw, ok := args["mode"].(string); ok {
				raw = strings.ToLower(strings.TrimSpace(raw))
				if raw == "grid" || raw == "tabs" {
					mode = raw
				}
			}
			if columns <= 0 && mode == "" {
				doc.Sections[idx].Layout = nil
			} else {
				doc.Sections[idx].Layout = &reportPlanDocumentSectionLayout{
					Mode:    mode,
					Columns: columns,
					Gap:     gap,
				}
			}
			doc = cleanupReportPlanDocument(doc)
			if err := writeReportPlanDocument(ctx, workspacePath, writeFile, doc); err != nil {
				return "", err
			}
			out, err := json.MarshalIndent(map[string]interface{}{"section_id": doc.Sections[idx].ID, "layout": doc.Sections[idx].Layout, "plan": doc}, "", "  ")
			if err != nil {
				return "", fmt.Errorf("failed to marshal updated report plan: %w", err)
			}
			return fmt.Sprintf("section layout set: %s\n", doc.Sections[idx].ID) + string(out), nil
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
	queryDB reportPlanQueryFunc,
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"preview_report_render",
		"Preview how reports/report_plan.json resolves against current workspace data. Runs each data widget's `sql` against db/db.sqlite (read-only). Returns validation output, a human-readable preview markdown, and per-widget resolved data previews so you can inspect what the final report would show before asking the user to open the Report tab.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			res, err := previewReportRender(ctx, workspacePath, readFile, queryDB)
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
