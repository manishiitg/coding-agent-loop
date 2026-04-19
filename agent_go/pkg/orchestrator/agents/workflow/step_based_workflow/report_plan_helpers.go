package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// Mirrors the subset of frontend/src/lib/reportPlanParser.ts needed to validate
// reports/report_plan.md. Kept narrow: validation only, no rendering concerns.

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
	Kind   string            // "text" | "table" | "chart"
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

type reportPlanParsedSection struct {
	Heading string                   `json:"heading"`
	Widgets []reportPlanParsedWidget `json:"widgets"`
}

type reportPlanParsedWidget struct {
	Kind     string            `json:"kind"`
	Source   string            `json:"source"`
	Path     string            `json:"path,omitempty"`
	Filter   string            `json:"filter,omitempty"`
	ShowIf   string            `json:"show_if,omitempty"`
	Line     int               `json:"line,omitempty"`
	InRow    bool              `json:"in_row,omitempty"`
	RowIndex int               `json:"row_index,omitempty"`
	// Options captures all non-standard fields the parser saw — so the builder
	// can spot e.g. `type: stat_row` (unrecognised key) or `chrt_type: bar`
	// (typo) being silently ignored.
	Options map[string]string `json:"options,omitempty"`
}

// parseReportPlan walks the markdown and returns sections+widgets. Intentionally
// forgiving — matches the frontend parser behaviour so we flag what would silently
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
				kind != "stat" && kind != "alert" && kind != "pivot" {
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
	if fields["source"] == "" {
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
		if len(cleaned) < 3 {
			continue
		}
		kind := strings.ToLower(cleaned[0])
		if kind != "text" && kind != "table" && kind != "chart" &&
			kind != "stat" && kind != "alert" && kind != "pivot" {
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
		if fields["source"] == "" {
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

// validateReportPlanMarkdown parses report_plan.md and checks each widget against
// its referenced source file. Returns a structured result for the LLM to act on.
func validateReportPlanMarkdown(
	ctx context.Context,
	workspacePath string,
	readFile func(context.Context, string) (string, error),
) (*reportPlanValidationResult, error) {
	// The workspace read API resolves paths from the workspace-docs root, so workflow-
	// relative paths must be normalized via normalizePathForWorkspaceAPI — the same
	// pattern readPlanFromFile and readEvaluationPlanFromFile use.
	planPath := normalizePathForWorkspaceAPI(filepath.Join("reports", "report_plan.md"), workspacePath)
	markdown, err := readFile(ctx, planPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", planPath, err)
	}

	sections := parseReportPlan(markdown)
	result := &reportPlanValidationResult{Valid: true, Sections: len(sections)}

	if len(sections) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, reportPlanDiagnostic{
			Severity: "error",
			Message:  "report_plan.md has no `## Heading` sections — widgets placed before any H2 are silently dropped by the renderer.",
			Hint:     "Add an H2 heading (e.g. `## Overview`) above each widget block.",
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
	// instead of the recognised keys.
	result.Parsed = buildReportPlanParsedDump(sections)

	// Global advice for builder on how to read the result.
	if len(result.Errors) == 0 && len(result.Warnings) == 0 && result.Widgets > 0 {
		result.Suggestions = append(result.Suggestions, "All widgets resolved against real data. Open the Report tab to preview layout.")
	} else {
		if len(result.Errors) > 0 {
			result.Valid = false
		}
		result.Suggestions = append(result.Suggestions, "Fix errors first (they cause silent blank widgets). Warnings are safe to ignore if intentional.")
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
		result.Warnings = append(result.Warnings, reportPlanDiagnostic{
			Severity: "warning", Section: section, Line: w.LineNum, Widget: locator,
			Message: "stat `path:` resolves to an object or array — stat renders scalars best.",
			Hint:    "Pick a leaf field; nested values get JSON.stringify'd, which reads poorly in a KPI tile.",
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
// `result.parsed`. Fields captured in the dump mirror the parser's recognised
// keys; anything else the builder wrote in the block lands in `options` so
// typos like `chrt_type` or `show-if` surface visibly.
func buildReportPlanParsedDump(sections []reportPlanSection) []reportPlanParsedSection {
	if len(sections) == 0 {
		return nil
	}
	// Keys the parser consumes into named widget fields. Everything outside this
	// set is surfaced under `options` — that's where typos and unrecognised
	// keys become visible.
	recognised := map[string]struct{}{
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
				if _, known := recognised[k]; known {
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
		"Validate reports/report_plan.md after editing it. Parses every widget block, reads the JSON sources they point to, and checks: source path is allowed (db/*.json or knowledgebase/{graph,index}.json), source file exists and is valid JSON, the dot-path resolves, the resolved shape matches the widget kind (array-of-objects for table/chart/pivot; scalar for stat), and options like chart_type/formats/sort/aggregate/severity are known values. Returns structured per-widget errors + warnings + suggestions AND a `parsed` dump showing exactly what the parser saw (so you can spot silent-drop bugs: widgets missing from `parsed` had a malformed fence tag; unknown keys land in the per-widget `options` bag where typos like `chrt_type` are visible). Run this every time you edit report_plan.md — the renderer drops bad widgets silently, so without this tool the user sees a blank Report tab with no indication why.",
		params,
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			res, err := validateReportPlanMarkdown(ctx, workspacePath, readFile)
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
