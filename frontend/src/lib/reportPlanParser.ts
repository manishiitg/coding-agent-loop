// Parser for reports/report_plan.md — widget definitions driving the dynamic report.
// See docs/workflow/persistent_stores_design.md section 2.
//
// Input: raw markdown string (contents of report_plan.md).
// Output: ParsedReportPlan — sections + widget entries, ready for ReportViewer to render.
//
// Grammar recap:
//   ## Heading              — starts a section
//   ```widget:{kind}        — a full-width widget block; body is `key: value` lines
//     source: ...
//     path: ...
//     filter: ...           (optional, for table widgets with array sources)
//   ```
//   ```widget:row           — groups widgets side by side; body is one widget per line
//     - {kind} | source: {path} | path: {key} [ | filter: {expr} ]
//   ```
//
// The parser is intentionally forgiving: unknown widget kinds, malformed lines, and
// extra whitespace are skipped silently rather than raising. Widget definitions the
// user is editing shouldn't break the whole report; bad widgets should just not render.

import type {
  ReportCostsMetric,
  ReportCostsScope,
  ReportCostsView,
  ReportEvalsMetric,
  ReportEvalsView,
  ReportRunsView,
  ParsedReportPlan,
  ReportAlertSeverity,
  ReportChartType,
  ReportDefaultSort,
  ReportEntry,
  ReportFormatterName,
  ReportPivotAggregate,
  ReportSection,
  ReportSortDirection,
  ReportWidget,
  ReportWidgetKind,
  ReportWidgetRow,
} from '../services/api-types'

const KNOWN_FORMATTERS: ReportFormatterName[] = [
  'currency-inr', 'currency-usd', 'percent', 'percent-1dp',
  'short-date', 'long-date', 'datetime',
  'number', 'number-1dp', 'number-2dp', 'bytes', 'boolean-icon',
]
const KNOWN_FORMATTER_SET = new Set<string>(KNOWN_FORMATTERS)
const KNOWN_CHART_TYPES: ReportChartType[] = ['bar', 'line', 'area', 'pie']
const KNOWN_CHART_TYPE_SET = new Set<string>(KNOWN_CHART_TYPES)
const KNOWN_ALERT_SEVERITIES: ReportAlertSeverity[] = ['info', 'warning', 'error', 'success']
const KNOWN_ALERT_SEVERITY_SET = new Set<string>(KNOWN_ALERT_SEVERITIES)
const KNOWN_PIVOT_AGGREGATES: ReportPivotAggregate[] = ['sum', 'avg', 'count', 'min', 'max', 'first']
const KNOWN_PIVOT_AGGREGATE_SET = new Set<string>(KNOWN_PIVOT_AGGREGATES)
const KNOWN_COSTS_SCOPES: ReportCostsScope[] = ['phase', 'execution', 'evaluation', 'all']
const KNOWN_COSTS_SCOPE_SET = new Set<string>(KNOWN_COSTS_SCOPES)
const KNOWN_COSTS_VIEWS: ReportCostsView[] = ['summary', 'stage-breakdown', 'run-table', 'step-table', 'model-table']
const KNOWN_COSTS_VIEW_SET = new Set<string>(KNOWN_COSTS_VIEWS)
const KNOWN_COSTS_METRICS: ReportCostsMetric[] = ['cost', 'total_tokens', 'input_tokens', 'output_tokens', 'llm_calls']
const KNOWN_COSTS_METRIC_SET = new Set<string>(KNOWN_COSTS_METRICS)
const KNOWN_EVALS_VIEWS: ReportEvalsView[] = ['summary', 'run-chart', 'run-table', 'step-table']
const KNOWN_EVALS_VIEW_SET = new Set<string>(KNOWN_EVALS_VIEWS)
const KNOWN_EVALS_METRICS: ReportEvalsMetric[] = ['score_percentage', 'total_score']
const KNOWN_EVALS_METRIC_SET = new Set<string>(KNOWN_EVALS_METRICS)
const KNOWN_RUNS_VIEWS: ReportRunsView[] = ['summary', 'duration-chart', 'status-chart', 'table']
const KNOWN_RUNS_VIEW_SET = new Set<string>(KNOWN_RUNS_VIEWS)

// Parses a comma-separated list of field names into a trimmed non-empty array.
// Returns undefined when the resulting list is empty so callers can drop the key.
function parseFieldList(raw: string): string[] | undefined {
  const out: string[] = []
  for (const part of raw.split(',')) {
    const p = part.trim()
    if (p) out.push(p)
  }
  return out.length > 0 ? out : undefined
}

// Parses `formats` value: comma-separated `field=preset` pairs.
// Example: `balance=currency-inr, eval_score=percent-1dp, updated=datetime`
// Unknown presets are silently dropped.
function parseFormatsField(raw: string): Record<string, ReportFormatterName> | undefined {
  const out: Record<string, ReportFormatterName> = {}
  for (const part of raw.split(',')) {
    const eq = part.indexOf('=')
    if (eq <= 0) continue
    const field = part.slice(0, eq).trim()
    const preset = part.slice(eq + 1).trim()
    if (!field || !preset) continue
    if (!KNOWN_FORMATTER_SET.has(preset)) continue
    out[field] = preset as ReportFormatterName
  }
  return Object.keys(out).length > 0 ? out : undefined
}

// Parses `colors` / `colors_dark` — comma-separated list of hex or CSS color names.
// Invalid entries are dropped but don't invalidate the whole list.
function parseColorsField(raw: string): string[] | undefined {
  const out: string[] = []
  for (const part of raw.split(',')) {
    const c = part.trim()
    if (c && isPlausibleColor(c)) out.push(c)
  }
  return out.length > 0 ? out : undefined
}

// Parses `color_map` — comma-separated `value=color` pairs.
// Example: `ok=#10b981, warning=#f59e0b, failed=#ef4444`
function parseColorMapField(raw: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const part of raw.split(',')) {
    const eq = part.indexOf('=')
    if (eq <= 0) continue
    const value = part.slice(0, eq).trim()
    const color = part.slice(eq + 1).trim()
    if (!value || !color) continue
    if (!isPlausibleColor(color)) continue
    out[value] = color
  }
  return Object.keys(out).length > 0 ? out : undefined
}

// Accepts #rgb, #rrggbb, #rrggbbaa, or CSS named colors. Named-color validation
// is loose — anything that looks like a word is passed through and the browser
// decides. Prevents obvious junk like multi-word strings.
const HEX_COLOR_RE = /^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/
const CSS_NAMED_RE = /^[a-zA-Z]+$/
function isPlausibleColor(value: string): boolean {
  return HEX_COLOR_RE.test(value) || CSS_NAMED_RE.test(value)
}

function parsePositiveInt(raw: string): number | undefined {
  const n = Number.parseInt(raw, 10)
  return Number.isFinite(n) && n > 0 ? n : undefined
}

function parseBool(raw: string): boolean | undefined {
  const lower = raw.toLowerCase()
  if (lower === 'true' || lower === 'yes' || lower === '1') return true
  if (lower === 'false' || lower === 'no' || lower === '0') return false
  return undefined
}

export function parseReportPlan(markdown: string): ParsedReportPlan {
  if (!markdown || markdown.trim() === '') {
    return { sections: [] }
  }

  const lines = markdown.split('\n')
  const sections: ReportSection[] = []
  let current: ReportSection | null = null
  let i = 0

  while (i < lines.length) {
    const raw = lines[i]
    const trimmed = raw.trim()

    // H2 heading → start new section. H1 (single #) and H3+ are ignored — only ##
    // delimits sections by the design doc's spec.
    if (/^##\s+/.test(trimmed) && !/^###/.test(trimmed)) {
      if (current) sections.push(current)
      current = { heading: trimmed.replace(/^##\s+/, '').trim(), entries: [] }
      i++
      continue
    }

    // Fenced code block? Check for widget language tag.
    const fenceMatch = /^```\s*widget:([\w-]+)\s*$/.exec(trimmed)
    if (fenceMatch) {
      const kind = fenceMatch[1]
      const body: string[] = []
      i++
      while (i < lines.length && lines[i].trim() !== '```') {
        body.push(lines[i])
        i++
      }
      // Skip the closing ``` if present
      if (i < lines.length) i++

      // Only attach widgets to a section — drop widgets that appear before any heading.
      if (!current) continue

      const entry = parseWidgetBlock(kind, body)
      if (entry) current.entries.push(entry)
      continue
    }

    // Any other line — narrative markdown between widgets, ignored for now. If we want
    // to support prose inside sections later, attach it to the current section here.
    i++
  }

  if (current) sections.push(current)
  return { sections }
}

function parseWidgetBlock(kind: string, body: string[]): ReportEntry | null {
  if (kind === 'row') {
    const row = parseRowBlock(body)
    if (row.widgets.length === 0) return null
    return { kind: 'row', row }
  }
  if (!isWidgetKind(kind)) return null
  const widget = parseKeyValueWidget(kind, body)
  if (!widget) return null
  return { kind: 'single', widget }
}

function isWidgetKind(kind: string): kind is ReportWidgetKind {
  return (
    kind === 'text' ||
    kind === 'chart' ||
    kind === 'table' ||
    kind === 'stat' ||
    kind === 'alert' ||
    kind === 'pivot' ||
    kind === 'costs' ||
    kind === 'evals' ||
    kind === 'runs'
  )
}

// Parses `key: value` lines inside a widget block. Unknown keys are ignored. `source` is
// required; `path` is optional and defaults to empty (→ the whole source, useful when
// the source file is itself the array/value a widget wants to render).
//
// Optional rich-rendering fields:
//   formats: <field>=<preset>, ...           (table only)
//   page_size: <int>                          (table only)
//   enable_search: true|false                 (table only)
//   chart_type: bar|line|area|pie             (chart only)
//   x_axis: <field>                           (chart only)
//   y_axis: <field>                           (chart only)
function parseKeyValueWidget(kind: ReportWidgetKind, body: string[]): ReportWidget | null {
  const fields: Record<string, string> = {}
  for (const line of body) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    const sepIdx = trimmed.indexOf(':')
    if (sepIdx <= 0) continue
    const key = trimmed.slice(0, sepIdx).trim().toLowerCase()
    const value = trimmed.slice(sepIdx + 1).trim()
    if (value) fields[key] = value
  }
  if (kind !== 'costs' && kind !== 'evals' && kind !== 'runs' && !fields.source) return null
  const widget: ReportWidget = {
    kind,
    source: kind === 'costs' || kind === 'evals' || kind === 'runs' ? '' : fields.source,
    path: normalizePath(fields.path),
  }
  if (fields.filter) widget.filter = fields.filter
  if (fields.show_if || fields.showif) widget.showIf = fields.show_if || fields.showif
  applyOptionalFields(widget, fields)
  return widget
}

// Applies optional table/chart/common fields from a parsed key-value bag onto the widget.
// Silently ignores unknown values (e.g. unknown chart_type) so a typo in the markdown
// degrades gracefully to default rendering instead of breaking the whole report.
function applyOptionalFields(widget: ReportWidget, fields: Record<string, string>): void {
  // Common to every widget kind
  if (fields.title) widget.title = fields.title
  if (fields.description) widget.description = fields.description
  if (fields.height) {
    const n = parsePositiveInt(fields.height)
    if (n !== undefined) widget.height = n
  }

  if (widget.kind === 'table') {
    if (fields.formats) {
      const fm = parseFormatsField(fields.formats)
      if (fm) widget.formats = fm
    }
    if (fields.page_size || fields.pagesize) {
      const n = parsePositiveInt(fields.page_size || fields.pagesize)
      if (n !== undefined) widget.pageSize = n
    }
    if (fields.enable_search || fields.enablesearch) {
      const b = parseBool(fields.enable_search || fields.enablesearch)
      if (b !== undefined) widget.enableSearch = b
    }
    if (fields.default_sort || fields.defaultsort) {
      const s = parseDefaultSort(fields.default_sort || fields.defaultsort)
      if (s) widget.defaultSort = s
    }
    if (fields.hide_columns || fields.hidecolumns) {
      const list = (fields.hide_columns || fields.hidecolumns)
        .split(',')
        .map(s => s.trim())
        .filter(Boolean)
      if (list.length > 0) widget.hideColumns = list
    }
  } else if (widget.kind === 'chart') {
    if (fields.chart_type || fields.charttype) {
      const t = (fields.chart_type || fields.charttype).toLowerCase()
      if (KNOWN_CHART_TYPE_SET.has(t)) widget.chartType = t as ReportChartType
    }
    if (fields.x_axis || fields.xaxis) widget.xAxis = (fields.x_axis || fields.xaxis)
    if (fields.y_axis || fields.yaxis) widget.yAxis = (fields.y_axis || fields.yaxis)
    if (fields.top_n || fields.topn) {
      const n = parsePositiveInt(fields.top_n || fields.topn)
      if (n !== undefined) widget.topN = n
    }
    if (fields.sort) {
      const s = fields.sort.toLowerCase()
      if (s === 'asc' || s === 'desc' || s === 'none') {
        widget.sort = s as ReportSortDirection | 'none'
      }
    }
    if (fields.show_values || fields.showvalues) {
      const b = parseBool(fields.show_values || fields.showvalues)
      if (b !== undefined) widget.showValues = b
    }
    // Multi-series: `series: a, b, c` becomes [a, b, c]. When set, each field
    // is plotted as its own series using x_axis as the shared category key.
    if (fields.series) {
      const list = parseFieldList(fields.series)
      if (list) widget.series = list
    }
    if (fields.series_colors || fields.seriescolors) {
      const list = parseColorsField(fields.series_colors || fields.seriescolors)
      if (list) widget.seriesColors = list
    }
    if (fields.stacked) {
      const b = parseBool(fields.stacked)
      if (b !== undefined) widget.stacked = b
    }
  } else if (widget.kind === 'stat') {
    if (fields.label) widget.label = fields.label
    if (fields.prefix) widget.prefix = fields.prefix
    if (fields.suffix) widget.suffix = fields.suffix
    if (fields.format) {
      const f = fields.format
      if (KNOWN_FORMATTER_SET.has(f)) widget.format = f as ReportFormatterName
    }
    if (fields.delta_path || fields.deltapath) widget.deltaPath = fields.delta_path || fields.deltapath
    if (fields.delta_format || fields.deltaformat) {
      const f = fields.delta_format || fields.deltaformat
      if (KNOWN_FORMATTER_SET.has(f)) widget.deltaFormat = f as ReportFormatterName
    }
    if (fields.trend_path || fields.trendpath) widget.trendPath = fields.trend_path || fields.trendpath
  } else if (widget.kind === 'alert') {
    if (fields.severity) {
      const s = fields.severity.toLowerCase()
      if (KNOWN_ALERT_SEVERITY_SET.has(s)) widget.severity = s as ReportAlertSeverity
    }
    if (fields.message) widget.message = fields.message
  } else if (widget.kind === 'pivot') {
    if (fields.rows) widget.rowsField = fields.rows
    if (fields.columns) widget.columnsField = fields.columns
    if (fields.values) widget.valuesField = fields.values
    if (fields.aggregate) {
      const a = fields.aggregate.toLowerCase()
      if (KNOWN_PIVOT_AGGREGATE_SET.has(a)) widget.aggregate = a as ReportPivotAggregate
    }
    if (fields.format) {
      const f = fields.format
      if (KNOWN_FORMATTER_SET.has(f)) widget.format = f as ReportFormatterName
    }
    if (fields.heatmap) {
      const b = parseBool(fields.heatmap)
      if (b !== undefined) widget.heatmap = b
    }
    if (fields.heatmap_colors || fields.heatmapcolors) {
      const list = parseColorsField(fields.heatmap_colors || fields.heatmapcolors)
      if (list && list.length >= 2) widget.heatmapColors = [list[0], list[1]]
    }
  } else if (widget.kind === 'costs') {
    if (fields.scope) {
      const scope = fields.scope.toLowerCase()
      if (KNOWN_COSTS_SCOPE_SET.has(scope)) widget.costsScope = scope as ReportCostsScope
    }
    if (fields.view) {
      const view = fields.view.toLowerCase()
      if (KNOWN_COSTS_VIEW_SET.has(view)) widget.costsView = view as ReportCostsView
    }
    if (fields.metric) {
      const metric = fields.metric.toLowerCase()
      if (KNOWN_COSTS_METRIC_SET.has(metric)) widget.costsMetric = metric as ReportCostsMetric
    }
    if (fields.run_folder || fields.runfolder) widget.runFolder = fields.run_folder || fields.runfolder
    if (fields.group) widget.group = fields.group
  } else if (widget.kind === 'evals') {
    if (fields.view) {
      const view = fields.view.toLowerCase()
      if (KNOWN_EVALS_VIEW_SET.has(view)) widget.evalsView = view as ReportEvalsView
    }
    if (fields.metric) {
      const metric = fields.metric.toLowerCase()
      if (KNOWN_EVALS_METRIC_SET.has(metric)) widget.evalsMetric = metric as ReportEvalsMetric
    }
    if (fields.run_folder || fields.runfolder) widget.runFolder = fields.run_folder || fields.runfolder
    if (fields.group) widget.group = fields.group
  } else if (widget.kind === 'runs') {
    if (fields.view) {
      const view = fields.view.toLowerCase()
      if (KNOWN_RUNS_VIEW_SET.has(view)) widget.runsView = view as ReportRunsView
    }
    if (fields.run_folder || fields.runfolder) widget.runFolder = fields.run_folder || fields.runfolder
    if (fields.group) widget.group = fields.group
  }

  // Color options — apply to chart and table; ignored for text widgets.
  if (widget.kind === 'chart' || widget.kind === 'table') {
    if (fields.colors) {
      const c = parseColorsField(fields.colors)
      if (c) widget.colors = c
    }
    if (fields.colors_dark || fields.colorsdark) {
      const c = parseColorsField(fields.colors_dark || fields.colorsdark)
      if (c) widget.colorsDark = c
    }
    if (fields.color_by || fields.colorby) {
      widget.colorBy = fields.color_by || fields.colorby
    }
    if (fields.color_map || fields.colormap) {
      const m = parseColorMapField(fields.color_map || fields.colormap)
      if (m) widget.colorMap = m
    }
  }
}

// Parses `default_sort: <field>:<direction>` (e.g. `balance:desc`) or just `<field>` (asc).
function parseDefaultSort(raw: string): ReportDefaultSort | undefined {
  const parts = raw.split(':').map(s => s.trim())
  if (parts.length === 0 || !parts[0]) return undefined
  const direction = parts[1]?.toLowerCase()
  return {
    field: parts[0],
    direction: direction === 'desc' ? 'desc' : 'asc',
  }
}

// Treats JSONPath-style root selectors as "whole source" so users can write
// `path: $[*]` or `path: $` when the source is a root-level array/value.
function normalizePath(raw: string | undefined): string {
  if (!raw) return ''
  const t = raw.trim()
  if (t === '$' || t === '$[*]' || t === '.' || t === '*') return ''
  return t
}

// Parses a `widget:row` body. Each non-blank non-comment line is expected to be:
//   - {kind} | source: {path} | path: {key} [ | filter: {expr} ]
// Costs / evals / runs widgets are the exception: they read from dedicated APIs, so
// `source` is omitted and the row can be as small as `costs | view: summary`.
// The leading `-` is optional to tolerate agent-edited variants.
function parseRowBlock(body: string[]): ReportWidgetRow {
  const widgets: ReportWidget[] = []
  for (const rawLine of body) {
    const line = rawLine.trim().replace(/^-\s*/, '')
    if (!line || line.startsWith('#')) continue

    const parts = line.split('|').map(p => p.trim()).filter(Boolean)
    if (parts.length < 2) continue
    const kind = parts[0].toLowerCase()
    if (!isWidgetKind(kind)) continue

    const fields: Record<string, string> = {}
    for (let p = 1; p < parts.length; p++) {
      const segment = parts[p]
      const sepIdx = segment.indexOf(':')
      if (sepIdx <= 0) continue
      const key = segment.slice(0, sepIdx).trim().toLowerCase()
      const value = segment.slice(sepIdx + 1).trim()
      if (value) fields[key] = value
    }
    if (kind !== 'costs' && kind !== 'evals' && kind !== 'runs' && !fields.source) continue
    const widget: ReportWidget = {
      kind,
      source: kind === 'costs' || kind === 'evals' || kind === 'runs' ? '' : fields.source,
      path: normalizePath(fields.path),
    }
    if (fields.filter) widget.filter = fields.filter
    if (fields.show_if || fields.showif) widget.showIf = fields.show_if || fields.showif
    applyOptionalFields(widget, fields)
    widgets.push(widget)
  }
  return { widgets }
}

// ---------------------------------------------------------------------------
// Data resolution helpers — walk a parsed JSON source by dot-path, apply filter.
// Kept next to the parser since both live in the "widget plumbing" layer.
// ---------------------------------------------------------------------------

// Resolves `path` (dot-separated) into `data`. Returns undefined for missing keys.
// Supports object keys; array indices can be written as numeric segments
// (e.g. "entities.0.label").
export function resolveJSONPath(data: unknown, path: string): unknown {
  if (data == null || !path) return data
  const segments = path.split('.').filter(s => s.length > 0)
  let current: unknown = data
  for (const segment of segments) {
    if (current == null) return undefined
    if (Array.isArray(current)) {
      const idx = Number(segment)
      if (!Number.isInteger(idx)) return undefined
      current = current[idx]
      continue
    }
    if (typeof current === 'object') {
      current = (current as Record<string, unknown>)[segment]
      continue
    }
    return undefined
  }
  return current
}

// Evaluates a `show_if:` expression against a source JSON value. Grammar:
//   <path>                  → truthy check on resolved value
//   !<path>                 → falsy check
//   <path> <op> <rhs>       → compare; op ∈ { >, <, >=, <=, ==, != }
// rhs is numeric if it parses as a finite number; otherwise treated as a string
// (quotes optional — `"yes"` and `yes` compare identically). Missing paths are
// treated as `null` for comparisons and `false` for truthy checks. Malformed
// expressions evaluate to `true` so a typo doesn't accidentally hide a whole
// section silently — validate_report_plan surfaces them as warnings.
const SHOW_IF_RE = /^\s*(!)?\s*([^\s!<>=]+)\s*(?:(>=|<=|==|!=|>|<)\s*(.+))?\s*$/
export function evaluateShowIf(data: unknown, expr: string | undefined): boolean {
  if (!expr) return true
  const match = SHOW_IF_RE.exec(expr)
  if (!match) return true
  const negate = match[1] === '!'
  const path = match[2]
  const op = match[3]
  const rhsRaw = match[4]
  const resolved = resolveJSONPath(data, path)
  if (!op) {
    const truthy = resolved !== undefined && resolved !== null && resolved !== false && resolved !== 0 && resolved !== ''
    return negate ? !truthy : truthy
  }
  const rhs = rhsRaw?.trim() ?? ''
  const rhsUnquoted = rhs.replace(/^['"]|['"]$/g, '')
  const rhsNum = Number(rhsUnquoted)
  const lhsNum = typeof resolved === 'number' ? resolved : Number(resolved)
  const numeric = Number.isFinite(rhsNum) && Number.isFinite(lhsNum)
  if (numeric) {
    switch (op) {
      case '>': return lhsNum > rhsNum
      case '<': return lhsNum < rhsNum
      case '>=': return lhsNum >= rhsNum
      case '<=': return lhsNum <= rhsNum
      case '==': return lhsNum === rhsNum
      case '!=': return lhsNum !== rhsNum
    }
  }
  // String compare — coerce resolved to string for equality/ordering.
  const lhsStr = resolved == null ? '' : String(resolved)
  switch (op) {
    case '==': return lhsStr === rhsUnquoted
    case '!=': return lhsStr !== rhsUnquoted
    case '>': return lhsStr > rhsUnquoted
    case '<': return lhsStr < rhsUnquoted
    case '>=': return lhsStr >= rhsUnquoted
    case '<=': return lhsStr <= rhsUnquoted
  }
  return true
}

// Applies a `key=value` filter to an array value. Non-arrays pass through untouched
// so the caller doesn't have to type-guard. String comparison only — widgets needing
// numeric or regex matching would need an extended filter grammar (future work).
export function applyWidgetFilter(value: unknown, filter: string | undefined): unknown {
  if (!filter || !Array.isArray(value)) return value
  const eqIdx = filter.indexOf('=')
  if (eqIdx <= 0) return value
  const key = filter.slice(0, eqIdx).trim()
  const match = filter.slice(eqIdx + 1).trim()
  if (!key) return value
  return (value as Array<Record<string, unknown>>).filter(item => {
    if (item == null || typeof item !== 'object') return false
    return String((item as Record<string, unknown>)[key]) === match
  })
}
