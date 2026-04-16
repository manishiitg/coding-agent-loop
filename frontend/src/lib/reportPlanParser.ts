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
  ParsedReportPlan,
  ReportEntry,
  ReportSection,
  ReportWidget,
  ReportWidgetKind,
  ReportWidgetRow,
} from '../services/api-types'

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
  return kind === 'text' || kind === 'chart' || kind === 'table'
}

// Parses `key: value` lines inside a widget block. Unknown keys are ignored. `source` is
// required; `path` is optional and defaults to empty (→ the whole source, useful when
// the source file is itself the array/value a widget wants to render).
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
  if (!fields.source) return null
  const widget: ReportWidget = { kind, source: fields.source, path: normalizePath(fields.path) }
  if (fields.filter) widget.filter = fields.filter
  return widget
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
    if (parts.length < 3) continue
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
    if (!fields.source) continue
    const widget: ReportWidget = { kind, source: fields.source, path: normalizePath(fields.path) }
    if (fields.filter) widget.filter = fields.filter
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
