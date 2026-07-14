package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/reportinteraction"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// Mirrors the subset of frontend/src/lib/reportPlanParser.ts needed to validate
// reports/report_plan.json.

var (
	reportPlanHexColorRE = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	reportPlanCSSNamedRE = regexp.MustCompile(`^[a-zA-Z]+$`)
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
	ID             string
	Kind           string // "file" | "file-list" | "interaction"
	Source         string // file/file-list widgets: a file path under db/, knowledgebase/, docs/
	Path           string
	Filter         string
	Question       string
	ResponseKind   string
	Options        []reportPlanDocumentInteractionOption
	AllowFreeText  bool
	Placeholder    string
	InstanceKey    string
	SubjectID      string
	SubjectVersion string
	SubjectHash    string
	Fields         map[string]string // raw optional fields, lowercase keys
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

type reportPlanDocumentInteractionOption struct {
	ID          string `json:"id" jsonschema:"required"`
	Title       string `json:"title" jsonschema:"required"`
	Description string `json:"description,omitempty"`
}

// Reports use HTML `file` widgets for their authored documents and may add
// native `interaction` widgets for user-configured, database-backed inputs.
// `file-list` remains available for durable evidence/assets folders. Legacy
// markdown and data-viz widget kinds are intentionally not supported.
type reportPlanDocumentWidget struct {
	ID             string                                `json:"id,omitempty"`
	Hidden         bool                                  `json:"hidden,omitempty"`
	Kind           string                                `json:"kind" jsonschema:"required,enum=file,enum=file-list,enum=interaction"`
	Source         string                                `json:"source,omitempty"`
	DB             string                                `json:"db,omitempty"`
	SQL            string                                `json:"sql,omitempty"`
	Path           string                                `json:"path,omitempty"`
	Filter         string                                `json:"filter,omitempty"`
	Title          string                                `json:"title,omitempty"`
	Description    string                                `json:"description,omitempty"`
	Height         int                                   `json:"height,omitempty"`
	ShowIf         string                                `json:"showIf,omitempty"`
	RenderFormat   string                                `json:"renderFormat,omitempty" jsonschema:"enum=auto,enum=html,enum=text,enum=code,enum=json,enum=image,enum=video,enum=audio,enum=pdf,enum=link"`
	ListFormat     string                                `json:"listFormat,omitempty" jsonschema:"enum=list,enum=cards,enum=table,enum=gallery"`
	Recursive      *bool                                 `json:"recursive,omitempty"`
	Extensions     []string                              `json:"extensions,omitempty"`
	MaxItems       int                                   `json:"maxItems,omitempty"`
	Question       string                                `json:"question,omitempty"`
	ResponseKind   string                                `json:"responseKind,omitempty" jsonschema:"enum=choice,enum=text,enum=choice-with-text"`
	Options        []reportPlanDocumentInteractionOption `json:"options,omitempty"`
	AllowFreeText  bool                                  `json:"allowFreeText,omitempty"`
	Placeholder    string                                `json:"placeholder,omitempty"`
	InstanceKey    string                                `json:"instanceKey,omitempty"`
	SubjectID      string                                `json:"subjectId,omitempty"`
	SubjectVersion string                                `json:"subjectVersion,omitempty"`
	SubjectHash    string                                `json:"subjectHash,omitempty"`
	Layout         *reportPlanDocumentWidgetLayout       `json:"layout,omitempty"`
}

// Public aliases for schema generation. The package-private types above are
// the canonical definitions; these aliases just expose them under stable
// names that the schema-gen command (and any other downstream tooling) can
// reference without the package having to broaden the surface of every
// callsite. Type aliases are zero-cost — they refer to the same underlying
// type — so no runtime or memory implications.
type (
	ReportPlanDocument                  = reportPlanDocument
	ReportPlanDocumentSection           = reportPlanDocumentSection
	ReportPlanDocumentSectionLayout     = reportPlanDocumentSectionLayout
	ReportPlanDocumentEntry             = reportPlanDocumentEntry
	ReportPlanDocumentRow               = reportPlanDocumentRow
	ReportPlanDocumentWidget            = reportPlanDocumentWidget
	ReportPlanDocumentInteractionOption = reportPlanDocumentInteractionOption
	ReportPlanDocumentWidgetLayout      = reportPlanDocumentWidgetLayout
	ReportPlanDocumentDefaultSort       = reportPlanDocumentDefaultSort
	ReportPlanDocumentThemeColors       = reportPlanDocumentThemeColors
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
	// Parsed shows what the validator actually extracted. Useful for debugging
	// silent-blank widgets after normalization drops unsupported legacy kinds.
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

type reportPlanRenderPreviewResult struct {
	Valid           bool                        `json:"valid"`
	Sections        int                         `json:"sections"`
	Widgets         int                         `json:"widgets"`
	PlanFilePath    string                      `json:"plan_file_path,omitempty"`
	PlanFormat      string                      `json:"plan_format,omitempty"`
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
	return kind == "file" || kind == "file-list" || kind == "interaction"
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
	if widget.Kind == "interaction" {
		widget.ResponseKind = strings.ToLower(strings.TrimSpace(widget.ResponseKind))
		if widget.ResponseKind == "" {
			if len(widget.Options) > 0 {
				widget.ResponseKind = "choice"
			} else {
				widget.ResponseKind = "text"
			}
		}
		if strings.TrimSpace(widget.InstanceKey) == "" {
			widget.InstanceKey = "default"
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
	add("show_if", widget.ShowIf)
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
	add("question", widget.Question)
	add("response_kind", widget.ResponseKind)
	add("placeholder", widget.Placeholder)
	add("instance_key", widget.InstanceKey)
	add("subject_id", widget.SubjectID)
	add("subject_version", widget.SubjectVersion)
	add("subject_hash", widget.SubjectHash)
	if widget.AllowFreeText {
		fields["allow_free_text"] = "true"
	}
	return &reportPlanWidget{
		ID:             widget.ID,
		Kind:           widget.Kind,
		Source:         widget.Source,
		Path:           widget.Path,
		Filter:         widget.Filter,
		Question:       widget.Question,
		ResponseKind:   widget.ResponseKind,
		Options:        widget.Options,
		AllowFreeText:  widget.AllowFreeText,
		Placeholder:    widget.Placeholder,
		InstanceKey:    widget.InstanceKey,
		SubjectID:      widget.SubjectID,
		SubjectVersion: widget.SubjectVersion,
		SubjectHash:    widget.SubjectHash,
		Fields:         fields,
		Section:        section,
		InRow:          inRow,
		RowIndex:       rowIndex,
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

func normalizeReportPlanPath(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" || t == "$" || t == "$[*]" || t == "." || t == "*" {
		return ""
	}
	return t
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
	if w.Kind == "interaction" {
		return w.ID
	}
	return w.Source
}

// validateReportPlan parses the canonical JSON report plan and checks each
// widget against its referenced source file. Returns a structured result for the
// LLM to act on.
func validateReportPlan(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (*reportPlanValidationResult, error) {
	planRead, err := readReportPlanDocument(ctx, workspacePath, readFile)
	if err != nil {
		return nil, err
	}

	sections := flattenReportPlanDocument(planRead.Document)
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

	// Reports are HTML documents registered as file widgets. Native interaction
	// widgets are user-configured input surfaces backed by db/db.sqlite.
	for _, section := range sections {
		for _, w := range section.Widgets {
			result.Widgets++
			sourceLabel := reportPlanWidgetSourceLabel(w)
			locator := fmt.Sprintf("%s@%s", w.Kind, sourceLabel)
			if w.InRow {
				locator = fmt.Sprintf("row[%d]:%s", w.RowIndex, locator)
			}

			if w.Kind == "interaction" {
				validateReportPlanInteractionWidget(w, section.Heading, locator, result)
				continue
			}

			// File/file-list widgets point `source` at a file under db/,
			// knowledgebase/, or docs/.
			src := strings.TrimSpace(w.Source)
			if src == "" {
				result.Valid = false
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section.Heading, Line: w.LineNum, Widget: locator,
					Message: "widget has no data binding.",
					Hint:    "file/file-list widgets need a `source` under db/, knowledgebase/, or docs/.",
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
		}
	}

	// Dump the parsed plan so the LLM sees exactly what the parser extracted —
	// surfaces the common failure mode where a widget block "looks right" but
	// the fence tag didn't match and every field silently lands in `options`
	// instead of the recognized keys.
	result.Parsed = buildReportPlanParsedDump(sections)

	// Global advice for builder on how to read the result.
	if len(result.Errors) == 0 && len(result.Warnings) == 0 && result.Widgets > 0 {
		result.Suggestions = append(result.Suggestions, "All widgets are valid. Open the Report tab to preview layout and any saved interaction state.")
	} else {
		if len(result.Errors) > 0 {
			result.Valid = false
		}
		result.Suggestions = append(result.Suggestions, "Fix errors first. Review warnings too — some still degrade rendering even when the report remains technically valid.")
	}
	return result, nil
}

func validateReportPlanInteractionWidget(
	w *reportPlanWidget, section, locator string, result *reportPlanValidationResult,
) {
	question := strings.TrimSpace(w.Question)
	if question == "" {
		question = strings.TrimSpace(w.Fields["title"])
	}
	if question == "" {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Widget: locator,
			Message: "interaction widget requires a question.",
			Hint:    "Set config.question to the exact user-facing prompt.",
		})
	}
	responseKind := strings.ToLower(strings.TrimSpace(w.ResponseKind))
	switch responseKind {
	case "choice", "text", "choice-with-text":
	default:
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error", Section: section, Widget: locator,
			Message: fmt.Sprintf("interaction widget has unsupported responseKind %q.", w.ResponseKind),
			Hint:    "Use choice, text, or choice-with-text.",
		})
	}
	if responseKind == "choice" || responseKind == "choice-with-text" {
		if len(w.Options) == 0 {
			result.Valid = false
			result.Errors = append(result.Errors, reportPlanDiagnostic{
				Severity: "error", Section: section, Widget: locator,
				Message: "choice interaction widget requires at least one option.",
			})
			return
		}
		seen := map[string]bool{}
		for _, option := range w.Options {
			id := strings.TrimSpace(option.ID)
			if id == "" || strings.TrimSpace(option.Title) == "" {
				result.Valid = false
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section, Widget: locator,
					Message: "each interaction option requires id and title.",
				})
				continue
			}
			if seen[id] {
				result.Valid = false
				result.Errors = append(result.Errors, reportPlanDiagnostic{
					Severity: "error", Section: section, Widget: locator,
					Message: fmt.Sprintf("interaction option id %q is duplicated.", id),
				})
			}
			seen[id] = true
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
		case "auto", "html", "text", "code", "json", "image", "video", "audio", "pdf", "link":
		default:
			result.Warnings = append(result.Warnings, reportPlanDiagnostic{
				Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
				Message: fmt.Sprintf("unknown render_format %q — file widget will fall back to auto.", raw),
				Hint:    "Use one of: auto, html, text, code, json, image, video, audio, pdf, link.",
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

	sections := flattenReportPlanDocument(planRead.Document)
	result := &reportPlanRenderPreviewResult{
		Valid:      validation.Valid,
		Sections:   len(sections),
		Validation: validation,
	}
	result.PlanFormat = planRead.Format
	result.PlanFilePath = planRead.FilePath
	result.PlanJSON = planRead.Document

	var rendered strings.Builder
	for _, section := range sections {
		previewSection := reportPlanPreviewSection{Heading: section.Heading}
		rendered.WriteString("## " + section.Heading + "\n\n")
		for _, widget := range section.Widgets {
			result.Widgets++
			pw := buildReportPlanWidgetPreview(widget)
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

func buildReportPlanWidgetPreview(w *reportPlanWidget) reportPlanPreviewWidget {
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
	if w.Kind == "interaction" {
		out.Summary = fmt.Sprintf("configured %s response stored in db/db.sqlite", w.ResponseKind)
		out.DataPreview = map[string]interface{}{
			"widget_id":       w.ID,
			"question":        w.Question,
			"response_kind":   w.ResponseKind,
			"instance_key":    w.InstanceKey,
			"allow_free_text": w.AllowFreeText,
			"options":         w.Options,
		}
		return out
	}

	// File / file-list widgets render durable files.
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
	if reportPlanDocumentHasInteraction(doc) {
		dbPath := filepath.Join(GetPromptDocsRoot(), filepath.FromSlash(workspacePath), DBFolderName, "db.sqlite")
		db, err := reportinteraction.OpenDatabase(ctx, dbPath)
		if err != nil {
			return fmt.Errorf("initialize report interaction storage: %w", err)
		}
		if err := db.Close(); err != nil {
			return fmt.Errorf("close report interaction storage: %w", err)
		}
	}
	content, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report plan: %w", err)
	}
	path := normalizePathForWorkspaceAPI(filepath.Join("reports", "report_plan.json"), workspacePath)
	return writeFile(ctx, path, string(content)+"\n")
}

func reportPlanDocumentHasInteraction(doc *reportPlanDocument) bool {
	for _, section := range doc.Sections {
		for _, entry := range section.Entries {
			if entry.Widget != nil && entry.Widget.Kind == "interaction" {
				return true
			}
			if entry.Row != nil {
				for _, widget := range entry.Row.Widgets {
					if widget.Kind == "interaction" {
						return true
					}
				}
			}
		}
	}
	return false
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
	// The tool schema already constrains these enums at the call boundary;
	// this re-check guards non-tool callers and keeps a bad value from being
	// persisted to report_plan.json where only validate_report_plan would
	// catch it after the fact.
	if rf := strings.ToLower(strings.TrimSpace(widget.RenderFormat)); rf != "" {
		switch rf {
		case "auto", "html", "text", "code", "json", "image", "video", "audio", "pdf", "link":
			widget.RenderFormat = rf
		default:
			return nil, fmt.Errorf("unsupported renderFormat %q (use auto, html, text, code, json, image, video, audio, pdf, or link)", widget.RenderFormat)
		}
	}
	if lf := strings.ToLower(strings.TrimSpace(widget.ListFormat)); lf != "" {
		switch lf {
		case "list", "cards", "table", "gallery":
			widget.ListFormat = lf
		default:
			return nil, fmt.Errorf("unsupported listFormat %q (use list, cards, table, or gallery)", widget.ListFormat)
		}
	}
	if widget.Kind == "interaction" {
		widget.ResponseKind = strings.ToLower(strings.TrimSpace(widget.ResponseKind))
		if widget.ResponseKind == "" {
			if len(widget.Options) > 0 {
				widget.ResponseKind = "choice"
			} else {
				widget.ResponseKind = "text"
			}
		}
		switch widget.ResponseKind {
		case "choice", "text", "choice-with-text":
		default:
			return nil, fmt.Errorf("unsupported responseKind %q (use choice, text, or choice-with-text)", widget.ResponseKind)
		}
		if strings.TrimSpace(widget.Question) == "" && strings.TrimSpace(widget.Title) == "" {
			return nil, fmt.Errorf("interaction widget requires question")
		}
		if (widget.ResponseKind == "choice" || widget.ResponseKind == "choice-with-text") && len(widget.Options) == 0 {
			return nil, fmt.Errorf("interaction widget responseKind %q requires options", widget.ResponseKind)
		}
		seen := map[string]bool{}
		for index := range widget.Options {
			widget.Options[index].ID = strings.TrimSpace(widget.Options[index].ID)
			widget.Options[index].Title = strings.TrimSpace(widget.Options[index].Title)
			widget.Options[index].Description = strings.TrimSpace(widget.Options[index].Description)
			if widget.Options[index].ID == "" || widget.Options[index].Title == "" {
				return nil, fmt.Errorf("each interaction option requires id and title")
			}
			if seen[widget.Options[index].ID] {
				return nil, fmt.Errorf("duplicate interaction option id %q", widget.Options[index].ID)
			}
			seen[widget.Options[index].ID] = true
		}
		if widget.ResponseKind == "text" || widget.ResponseKind == "choice-with-text" {
			widget.AllowFreeText = true
		}
		if strings.TrimSpace(widget.InstanceKey) == "" {
			widget.InstanceKey = "default"
		}
	}
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
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"validate_report_plan",
		"Validate reports/report_plan.json after editing it. It checks file-source path allowlists and formats plus interaction questions, response kinds, and option IDs. Returns structured per-widget errors + warnings + suggestions plus a parsed dump showing exactly what the validator saw.",
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
			"source": { "type": "string", "description": "file/file-list only. File path under db/, knowledgebase/, or docs/. For reports use a file widget pointing at an HTML document under db/reports/; file-list points at a durable assets/evidence folder." },
			"title": { "type": "string" },
			"description": { "type": "string" },
			"height": { "type": "integer" },
			"showIf": { "type": "string" },
			"renderFormat": { "type": "string", "enum": ["auto", "html", "text", "code", "json", "image", "video", "audio", "pdf", "link"], "description": "file widget only. Report documents must use html. Other formats are attachment compatibility paths." },
			"listFormat": { "type": "string", "enum": ["list", "cards", "table", "gallery"], "description": "file-list widget only. gallery is best for image/video evidence; table is best for dense inventories." },
			"recursive": { "type": "boolean", "description": "file-list widget only. Whether to include nested folder files." },
			"extensions": { "type": "array", "items": { "type": "string" }, "description": "file-list widget only. Optional extension allowlist, e.g. [\"png\", \"jpg\", \"pdf\"]." },
			"maxItems": { "type": "integer", "description": "file-list widget only. Caps displayed files." },
			"question": { "type": "string", "description": "interaction only. Exact persistent question shown to the user in the Report page." },
			"responseKind": { "type": "string", "enum": ["choice", "text", "choice-with-text"], "description": "interaction only. Native response control to render." },
			"options": {
				"type": "array",
				"description": "interaction choice options. IDs are the durable values workflow steps read from report_widget_responses.selected_option_id.",
				"items": {
					"type": "object",
					"properties": {
						"id": { "type": "string" },
						"title": { "type": "string" },
						"description": { "type": "string" }
					},
					"required": ["id", "title"],
					"additionalProperties": false
				}
			},
			"allowFreeText": { "type": "boolean", "description": "interaction choice only. Allow a note or a note-only custom response." },
			"placeholder": { "type": "string", "description": "interaction text box placeholder." },
			"instanceKey": { "type": "string", "description": "interaction response instance. Defaults to default; change it when the configured widget intentionally represents a different durable subject." },
			"subjectId": { "type": "string", "description": "interaction optional subject/artifact id stored with the response." },
			"subjectVersion": { "type": "string", "description": "interaction optional subject/artifact version stored with the response." },
			"subjectHash": { "type": "string", "description": "interaction optional immutable artifact hash stored with the response." },
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
			"tab": { "type": "string", "description": "Optional tab label for this widget entry when the section layout mode is tabs. Use the user-facing route name, e.g. \"Happy path\" or \"Fallback route\". Updating an existing widget with tab sets or clears the containing entry tab." },
			"kind": { "type": "string", "enum": ["file", "file-list", "interaction"], "description": "Use file for HTML reports, file-list for durable assets/evidence, and interaction for a user-configured database-backed input widget." },
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
			"Report documents remain HTML: create an HTML document under `db/reports/` and register it with `kind:\"file\"`, `renderFormat:\"html\"`, and `source` set to that file. Add `kind:\"interaction\"` only when the user explicitly asks to configure a durable input/decision control in the Report page. Interaction widgets are native app controls, not agent-authored JavaScript; their answers are stored in db/db.sqlite table report_widget_responses for later workflow steps.\n\n"+
			"Binding: file/file-list widgets point `source` at a path under db/, knowledgebase/, or docs/. For multiple images/videos/PDFs use `kind:\"file-list\"` with a source folder plus `listFormat`, `recursive`, `extensions`, and `maxItems`. Interaction widgets use question/responseKind/options and do not need source.\n\n"+
			"Per-widget grid layout: when the parent section has section.layout.columns set, pass `layout: { span: N, minWidth: 320 }` in config to span N grid columns. For route dashboards, prefer set_section_layout(mode=\"tabs\") on one shared conceptual section and pass `tab: \"Route name\"` so entries with the same route label render together.",
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
				// Collision-checked against the current document: UnixNano is
				// unique enough for sequential tool calls, but the check makes
				// the invariant explicit and costs nothing.
				for i := int64(0); ; i++ {
					candidate := fmt.Sprintf("widget-%d", time.Now().UnixNano()+i)
					if _, exists := reportPlanFindWidget(doc, candidate); !exists {
						widget.ID = candidate
						break
					}
				}
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
					// Reject implausible colors at write time — otherwise a bad
					// value sits in report_plan.json until validate_report_plan
					// happens to run, and the renderer silently falls back.
					for field, value := range map[string]string{
						"primary": colors.Primary, "accent": colors.Accent, "card": colors.Card,
						"muted": colors.Muted, "border": colors.Border,
					} {
						if value != "" && !reportPlanIsPlausibleColor(value) {
							return "", fmt.Errorf("colors.%s %q is not a recognizable color (use #RGB / #RRGGBB / #RRGGBBAA hex or a CSS named color)", field, value)
						}
					}
					for i, c := range colors.Chart {
						if c != "" && !reportPlanIsPlausibleColor(c) {
							return "", fmt.Errorf("colors.chart[%d] %q is not a recognizable color (use #RGB / #RRGGBB / #RRGGBBAA hex or a CSS named color)", i, c)
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
) error {
	schema := `{
		"type": "object",
		"properties": {},
		"additionalProperties": false
	}`
	params, _ := parseSchemaForToolParameters(schema)

	mcpAgent.RegisterCustomTool(
		"preview_report_render",
		"Preview how reports/report_plan.json resolves against the current workspace. Returns validation output, a human-readable preview markdown, and per-widget previews so you can inspect what the final report would show before asking the user to open the Report tab.",
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
