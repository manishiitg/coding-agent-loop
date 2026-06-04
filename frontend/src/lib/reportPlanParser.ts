// Parser for reports/report_plan.json.
// See docs/workflow/persistent_stores_design.md section 2.
//
// Input: raw file contents from report_plan.json.
// Output: ParsedReportPlan — sections + widget entries, ready for ReportViewer to render.
// Supports both the current JSON format and the legacy fenced-widget markdown
// format still used by some builder helpers/docs.
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
  ParsedReportPlan,
  ReportFileListFormat,
  ReportFileRenderFormat,
  ReportEntry,
  ReportSection,
  ReportWidget,
  ReportWidgetKind,
  ReportWidgetRow,
} from '../services/api-types'

const KNOWN_FILE_RENDER_FORMATS: ReportFileRenderFormat[] = ['auto', 'markdown', 'html', 'text', 'code', 'json', 'image', 'video', 'audio', 'pdf', 'link']
const KNOWN_FILE_RENDER_FORMAT_SET = new Set<string>(KNOWN_FILE_RENDER_FORMATS)
const KNOWN_FILE_LIST_FORMATS: ReportFileListFormat[] = ['list', 'cards', 'table', 'gallery']
const KNOWN_FILE_LIST_FORMAT_SET = new Set<string>(KNOWN_FILE_LIST_FORMATS)
const LEGACY_EMPTY_WIDGET_KIND_SET = new Set<string>(['costs', 'evals', 'runs'])

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

export function parseReportPlan(rawPlan: string): ParsedReportPlan {
  if (!rawPlan || rawPlan.trim() === '') {
    return { sections: [] }
  }

  const trimmed = rawPlan.trim()
  if (trimmed.startsWith('{')) {
    const parsedJSON = parseReportPlanJSON(trimmed)
    if (parsedJSON) return parsedJSON
  }
  return parseReportPlanMarkdown(rawPlan)
}

function parseReportPlanMarkdown(raw: string): ParsedReportPlan {
  const sections: ReportSection[] = []
  const lines = raw.split(/\r?\n/)
  let currentSection: ReportSection | null = null

  const ensureSection = (): ReportSection => {
    if (!currentSection) {
      currentSection = { heading: 'Report', entries: [] }
      sections.push(currentSection)
    }
    return currentSection
  }

  for (let i = 0; i < lines.length; i += 1) {
    const trimmed = lines[i].trim()
    if (!trimmed) continue

    const headingMatch = /^##\s+(.+)$/.exec(trimmed)
    if (headingMatch) {
      currentSection = { heading: headingMatch[1].trim(), entries: [] }
      sections.push(currentSection)
      continue
    }

    const widgetMatch = /^```widget:([a-z_/-]+)\s*$/.exec(trimmed)
    if (!widgetMatch) continue

    const kind = widgetMatch[1].trim().toLowerCase()
    const body: string[] = []
    i += 1
    for (; i < lines.length; i += 1) {
      if (lines[i].trim() === '```') break
      body.push(lines[i])
    }

    const entry = parseWidgetBlock(kind, body)
    if (entry) ensureSection().entries.push(entry)
  }

  return { sections: sections.filter(section => section.entries.length > 0) }
}

function parseReportPlanJSON(raw: string): ParsedReportPlan | null {
  try {
    const parsed = JSON.parse(raw) as {
      theme?: unknown
      themeColors?: unknown
      sections?: Array<{
        heading?: unknown
        entries?: Array<{
          kind?: unknown
          tab?: unknown
          widget?: unknown
          row?: { widgets?: unknown }
        }>
      }>
    }
    if (!parsed || !Array.isArray(parsed.sections)) return null

    const sections: ReportSection[] = []
    for (const section of parsed.sections) {
      if (!section || typeof section.heading !== 'string' || section.heading.trim() === '') continue
      const entries: ReportEntry[] = []
      const rawEntries = Array.isArray(section.entries) ? section.entries : []
      for (const entry of rawEntries) {
        if (!entry || typeof entry.kind !== 'string') continue
        const tab = typeof entry.tab === 'string' && entry.tab.trim() !== '' ? entry.tab.trim() : undefined
        if (entry.kind === 'single') {
          const widget = parseReportPlanJSONWidget(entry.widget)
          if (widget) entries.push({ kind: 'single', widget, tab })
          continue
        }
        if (entry.kind === 'row') {
          const rawWidgets = Array.isArray(entry.row?.widgets) ? entry.row?.widgets : []
          const widgets = rawWidgets
            .map(widget => parseReportPlanJSONWidget(widget))
            .filter((widget): widget is ReportWidget => widget !== null)
          if (widgets.length > 0) entries.push({ kind: 'row', row: { widgets }, tab })
        }
      }
      const layout = parseSectionLayout((section as Record<string, unknown>).layout)
      const built: ReportSection = { heading: section.heading.trim(), entries }
      if (layout) built.layout = layout
      sections.push(built)
    }
    const theme = typeof parsed.theme === 'string' && parsed.theme.trim() !== ''
      ? parsed.theme.trim()
      : undefined
    const themeColors = parseThemeColors(parsed.themeColors)
    const out: ParsedReportPlan = { sections }
    if (theme) out.theme = theme
    if (themeColors) out.themeColors = themeColors
    return out
  } catch {
    return null
  }
}

function parseThemeColors(raw: unknown): ParsedReportPlan['themeColors'] | undefined {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return undefined
  const obj = raw as Record<string, unknown>
  const out: NonNullable<ParsedReportPlan['themeColors']> = {}
  const stringFields = ['primary', 'accent', 'card', 'muted', 'border'] as const
  for (const k of stringFields) {
    const v = obj[k]
    if (typeof v === 'string' && v.trim() !== '') out[k] = v.trim()
  }
  if (Array.isArray(obj.chart)) {
    const chart = obj.chart
      .filter((v): v is string => typeof v === 'string' && v.trim() !== '')
      .map(v => v.trim())
    if (chart.length > 0) out.chart = chart
  }
  return Object.keys(out).length > 0 ? out : undefined
}

function parseReportPlanJSONWidget(raw: unknown): ReportWidget | null {
  if (!raw || typeof raw !== 'object') return null
  const source = raw as Record<string, unknown>
  const kind = typeof source.kind === 'string' ? source.kind.toLowerCase() : ''
  if (isLegacyEmptyWidgetKind(kind)) return parseLegacyEmptyWidget(source)
  if (!isWidgetKind(kind)) return null

  const widget: ReportWidget = {
    kind,
    hidden: source.hidden === true ? true : undefined,
    path: normalizePath(typeof source.path === 'string' ? source.path : ''),
  }
  if (typeof source.source === 'string' && source.source) widget.source = source.source
  if (typeof source.db === 'string' && source.db) widget.db = source.db
  if (typeof source.sql === 'string' && source.sql) widget.sql = source.sql
  // A widget needs either a file `source` (file/file-list/markdown) or a
  // `db` + `sql` data binding. Anything else is unrenderable.
  if (!widget.source && !(widget.db && widget.sql)) return null

  if (typeof source.filter === 'string') widget.filter = source.filter
  if (typeof source.title === 'string') widget.title = source.title
  if (typeof source.description === 'string') widget.description = source.description
  if (typeof source.height === 'number' && Number.isFinite(source.height) && source.height > 0) widget.height = Math.trunc(source.height)
  if (typeof source.showIf === 'string') widget.showIf = source.showIf

  if (widget.kind === 'file' || widget.kind === 'file-list') {
    if (typeof source.renderFormat === 'string' && KNOWN_FILE_RENDER_FORMAT_SET.has(source.renderFormat.toLowerCase())) {
      widget.renderFormat = source.renderFormat.toLowerCase() as ReportFileRenderFormat
    }
    if (typeof source.listFormat === 'string' && KNOWN_FILE_LIST_FORMAT_SET.has(source.listFormat.toLowerCase())) {
      widget.listFormat = source.listFormat.toLowerCase() as ReportFileListFormat
    }
    if (typeof source.recursive === 'boolean') widget.recursive = source.recursive
    if (Array.isArray(source.extensions)) {
      const extensions = source.extensions
        .filter((value): value is string => typeof value === 'string' && value.trim().length > 0)
        .map(value => value.trim().replace(/^\./, '').toLowerCase())
      if (extensions.length > 0) widget.extensions = extensions
    }
    if (typeof source.maxItems === 'number' && Number.isFinite(source.maxItems) && source.maxItems > 0) widget.maxItems = Math.trunc(source.maxItems)
  }

  const widgetLayout = parseWidgetLayout(source.layout)
  if (widgetLayout) widget.layout = widgetLayout

  return widget
}

function parseSectionLayout(raw: unknown): ReportSection['layout'] | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  const obj = raw as Record<string, unknown>
  const layout: NonNullable<ReportSection['layout']> = {}
  if (typeof obj.mode === 'string') {
    const mode = obj.mode.toLowerCase()
    if (mode === 'grid' || mode === 'tabs') layout.mode = mode
  }
  if (typeof obj.columns === 'number' && Number.isFinite(obj.columns) && obj.columns > 0) {
    layout.columns = Math.min(24, Math.trunc(obj.columns))
  }
  if (typeof obj.gap === 'number' && Number.isFinite(obj.gap) && obj.gap >= 0) {
    layout.gap = Math.min(64, Math.trunc(obj.gap))
  }
  return Object.keys(layout).length > 0 ? layout : null
}

function parseWidgetLayout(raw: unknown): { span?: number; minWidth?: number } | null {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return null
  const obj = raw as Record<string, unknown>
  const layout: { span?: number; minWidth?: number } = {}
  if (typeof obj.span === 'number' && Number.isFinite(obj.span) && obj.span > 0) {
    layout.span = Math.min(24, Math.trunc(obj.span))
  }
  if (typeof obj.minWidth === 'number' && Number.isFinite(obj.minWidth) && obj.minWidth > 0) {
    layout.minWidth = Math.min(2000, Math.trunc(obj.minWidth))
  }
  return Object.keys(layout).length > 0 ? layout : null
}

function parseWidgetBlock(kind: string, body: string[]): ReportEntry | null {
  if (kind === 'row') {
    const row = parseRowBlock(body)
    if (row.widgets.length === 0) return null
    return { kind: 'row', row }
  }
  if (isLegacyEmptyWidgetKind(kind)) {
    const widget = parseLegacyEmptyWidget(parseKeyValueFields(body))
    return { kind: 'single', widget }
  }
  if (!isWidgetKind(kind)) return null
  const widget = parseKeyValueWidget(kind, body)
  if (!widget) return null
  return { kind: 'single', widget }
}

function isWidgetKind(kind: string): kind is ReportWidgetKind {
  return kind === 'markdown' || kind === 'file' || kind === 'file-list'
}

function isLegacyEmptyWidgetKind(kind: string): boolean {
  return LEGACY_EMPTY_WIDGET_KIND_SET.has(kind)
}

function parseLegacyEmptyWidget(source: Record<string, unknown>): ReportWidget {
  const widget: ReportWidget = {
    kind: 'markdown',
    hidden: source.hidden === true ? true : undefined,
    source: '',
    path: '',
  }
  if (typeof source.title === 'string') widget.title = source.title
  if (typeof source.description === 'string') widget.description = source.description
  if (typeof source.height === 'number' && Number.isFinite(source.height) && source.height > 0) {
    widget.height = Math.trunc(source.height)
  } else if (typeof source.height === 'string') {
    const height = parsePositiveInt(source.height)
    if (height !== undefined) widget.height = height
  }
  const widgetLayout = parseWidgetLayout(source.layout)
  if (widgetLayout) widget.layout = widgetLayout
  return widget
}

function parseKeyValueFields(body: string[]): Record<string, string> {
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
  return fields
}

// Parses `key: value` lines inside a widget block. Unknown keys are ignored. `source` is
// required; `path` is optional and defaults to empty (→ the whole source, useful when
// the source file is itself the array/value a widget wants to render).
//
// Optional rich-rendering fields:
//   formats: <field>=<preset>, ...           (table/cards only)
//   page_size: <int>                         (table/cards only)
//   enable_search: true|false                (table/cards only)
//   chart_type: bar|line|area|pie             (chart only)
//   x_axis: <field>                           (chart only)
//   y_axis: <field>                           (chart only)
function parseKeyValueWidget(kind: ReportWidgetKind, body: string[]): ReportWidget | null {
  const fields = parseKeyValueFields(body)
  if (!fields.source && !(fields.db && fields.sql)) return null
  const widget: ReportWidget = {
    kind,
    path: normalizePath(fields.path),
  }
  if (fields.source) widget.source = fields.source
  if (fields.db) widget.db = fields.db
  if (fields.sql) widget.sql = fields.sql
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

  if (widget.kind === 'file' || widget.kind === 'file-list') {
    const renderFormat = (fields.render_format || fields.renderformat || '').toLowerCase()
    if (KNOWN_FILE_RENDER_FORMAT_SET.has(renderFormat)) widget.renderFormat = renderFormat as ReportFileRenderFormat
    const listFormat = (fields.list_format || fields.listformat || '').toLowerCase()
    if (KNOWN_FILE_LIST_FORMAT_SET.has(listFormat)) widget.listFormat = listFormat as ReportFileListFormat
    if (fields.recursive) {
      const b = parseBool(fields.recursive)
      if (b !== undefined) widget.recursive = b
    }
    if (fields.extensions) {
      const extensions = fields.extensions
        .split(',')
        .map(value => value.trim().replace(/^\./, '').toLowerCase())
        .filter(Boolean)
      if (extensions.length > 0) widget.extensions = extensions
    }
    if (fields.max_items || fields.maxitems) {
      const n = parsePositiveInt(fields.max_items || fields.maxitems)
      if (n !== undefined) widget.maxItems = n
    }
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
// The leading `-` is optional to tolerate agent-edited variants.
function parseRowBlock(body: string[]): ReportWidgetRow {
  const widgets: ReportWidget[] = []
  for (const rawLine of body) {
    const line = rawLine.trim().replace(/^-\s*/, '')
    if (!line || line.startsWith('#')) continue

    const parts = line.split('|').map(p => p.trim()).filter(Boolean)
    if (parts.length < 1) continue
    const kind = parts[0].toLowerCase()
    const isLegacyEmpty = isLegacyEmptyWidgetKind(kind)
    if (!isLegacyEmpty && !isWidgetKind(kind)) continue

    const fields: Record<string, string> = {}
    for (let p = 1; p < parts.length; p++) {
      const segment = parts[p]
      const sepIdx = segment.indexOf(':')
      if (sepIdx <= 0) continue
      const key = segment.slice(0, sepIdx).trim().toLowerCase()
      const value = segment.slice(sepIdx + 1).trim()
      if (value) fields[key] = value
    }
    if (isLegacyEmpty) {
      widgets.push(parseLegacyEmptyWidget(fields))
      continue
    }
    if (!isWidgetKind(kind)) continue
    if (!fields.source && !(fields.db && fields.sql)) continue
    const widget: ReportWidget = {
      kind,
      path: normalizePath(fields.path),
    }
    if (fields.source) widget.source = fields.source
    if (fields.db) widget.db = fields.db
    if (fields.sql) widget.sql = fields.sql
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

// Splits a path into segments, understanding dot keys, bracket indices, and a
// leading `$` / `$.` document-root sigil (JSONPath/JSONata style). Examples:
//   "entities.0.label" -> ["entities","0","label"]
//   "rows[0].login_success" -> ["rows","0","login_success"]
//   "$[0].login_success" -> ["0","login_success"]   (the classic alert showIf form)
//   "$.foo.bar" -> ["foo","bar"]
function parsePathSegments(path: string): string[] {
  let p = path.trim()
  if (p.startsWith('$')) p = p.slice(1)
  const segments: string[] = []
  // Each token is either a bracketed numeric index [123] or a bare key.
  const re = /\[(\d+)\]|([^.[\]]+)/g
  let m: RegExpExecArray | null
  while ((m = re.exec(p)) !== null) {
    segments.push(m[1] !== undefined ? m[1] : m[2])
  }
  return segments
}

// Resolves `path` into `data`. Returns undefined for missing keys. Supports
// object keys, array indices as numeric dot segments ("entities.0.label") OR
// bracket notation ("entities[0].label"), and a leading `$`/`$.` root sigil.
export function resolveJSONPath(data: unknown, path: string): unknown {
  if (data == null || !path) return data
  const segments = parsePathSegments(path)
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
