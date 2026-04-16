// Dynamic report viewer — parses reports/report_plan.md, fetches each widget's JSON
// source, renders widgets. See docs/workflow/persistent_stores_design.md.

import { useEffect, useMemo, useState } from 'react'
import { agentApi } from '../../services/api'
import {
  applyWidgetFilter,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import type {
  ParsedReportPlan,
  ReportEntry,
  ReportWidget,
} from '../../services/api-types'

async function readWorkspaceText(filepath: string): Promise<string | null> {
  try {
    const resp = await agentApi.getPlannerFileContent(filepath)
    if (resp && resp.success && resp.data && typeof resp.data.content === 'string') {
      return resp.data.content
    }
    return null
  } catch {
    // 404 / network — missing source files are expected when a widget points at a db/
    // file that hasn't been written yet. Callers distinguish missing from fetched by
    // the `null` vs `undefined` cache entry below.
    return null
  }
}

interface ReportViewerProps {
  workspacePath: string
  isOpen: boolean
  onClose: () => void
}

interface ReportViewProps {
  workspacePath: string
  /** Optional close/back handler; when omitted, no close button is rendered (used for canvas-mode). */
  onClose?: () => void
}

// Source content cached per workspace-relative path. `undefined` = not yet fetched;
// `null` = fetched and missing/malformed; otherwise the parsed JSON value.
type SourceCache = Record<string, unknown>

// Modal wrapper — overlay + panel + close-on-backdrop. Used by scheduler runs panel.
export function ReportViewer({ workspacePath, isOpen, onClose }: ReportViewerProps) {
  if (!isOpen) return null
  return (
    <div style={overlayStyle} onClick={onClose}>
      <div style={panelStyle} onClick={e => e.stopPropagation()}>
        <ReportView workspacePath={workspacePath} onClose={onClose} />
      </div>
    </div>
  )
}

// Inline content — renders the report plan directly without modal chrome. Used by the
// workflow canvas when canvasViewMode === 'report'.
export function ReportView({ workspacePath, onClose }: ReportViewProps) {
  const [loading, setLoading] = useState(false)
  const [planMarkdown, setPlanMarkdown] = useState<string | null>(null)
  const [sources, setSources] = useState<SourceCache>({})
  const [error, setError] = useState<string | null>(null)

  const plan: ParsedReportPlan = useMemo(() => {
    if (!planMarkdown) return { sections: [] }
    return parseReportPlan(planMarkdown)
  }, [planMarkdown])

  // Stable key: same set of paths → same string → effect below doesn't re-run.
  // Using the array identity directly would recompute every render because useMemo
  // returns a fresh Array.from each time the plan parses.
  const referencedSourcesKey = useMemo(() => {
    const set = new Set<string>()
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        if (entry.kind === 'single') set.add(entry.widget.source)
        else for (const w of entry.row.widgets) set.add(w.source)
      }
    }
    return Array.from(set).sort().join('|')
  }, [plan])

  useEffect(() => {
    if (!workspacePath) return
    let cancelled = false
    setLoading(true)
    setError(null)

    readWorkspaceText(`${workspacePath}/reports/report_plan.md`)
      .then(content => {
        if (cancelled) return
        setPlanMarkdown(content)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
    }
  }, [workspacePath])

  // Fetch sources referenced by the plan. `sources` is intentionally NOT a dep — we
  // read it fresh via setSources' functional form and return `prev` unchanged when
  // every referenced path is already cached, which is the no-op guard that prevents
  // the effect from looping on its own setState.
  useEffect(() => {
    if (!workspacePath || referencedSourcesKey === '') return
    const paths = referencedSourcesKey.split('|')
    let cancelled = false

    Promise.all(
      paths.map(async (path): Promise<readonly [string, unknown]> => {
        const content = await readWorkspaceText(`${workspacePath}/${path}`)
        if (content === null || content.trim() === '') return [path, null] as const
        try {
          return [path, JSON.parse(content)] as const
        } catch {
          return [path, null] as const
        }
      })
    ).then(results => {
      if (cancelled) return
      setSources(prev => {
        let changed = false
        const next = { ...prev }
        for (const [path, data] of results) {
          if (!(path in prev)) {
            next[path] = data
            changed = true
          }
        }
        return changed ? next : prev
      })
    })

    return () => {
      cancelled = true
    }
  }, [workspacePath, referencedSourcesKey])

  const planExists = planMarkdown !== null
  const hasAnyContent = useMemo(() => {
    if (!planExists) return false
    for (const section of plan.sections) {
      for (const entry of section.entries) {
        const widgets = entry.kind === 'single' ? [entry.widget] : entry.row.widgets
        for (const w of widgets) {
          const raw = sources[w.source]
          if (raw === undefined) return true
          if (raw == null) continue
          const resolved = applyWidgetFilter(resolveJSONPath(raw, w.path), w.filter)
          if (resolved != null && !(Array.isArray(resolved) && resolved.length === 0)) {
            return true
          }
        }
      }
    }
    return false
  }, [planExists, plan, sources])

  return (
    <div className="h-full w-full flex flex-col overflow-hidden bg-background text-foreground">
      <div className="flex items-center justify-between px-5 py-3 border-b border-border flex-shrink-0">
        <h2 className="m-0 text-base font-semibold">Report</h2>
        {onClose && (
          <button
            onClick={onClose}
            className="text-2xl leading-none bg-transparent border-none cursor-pointer text-muted-foreground hover:text-foreground transition-colors"
            title="Close"
          >
            ×
          </button>
        )}
      </div>

      <div className="flex-1 overflow-y-auto px-5 py-4">
        {loading && <div className="italic text-muted-foreground">Loading…</div>}
        {error && <div className="text-destructive">Failed to load report: {error}</div>}

        {!loading && !error && !hasAnyContent && (
          <div className="italic text-muted-foreground">
            No report yet. The builder writes reports/report_plan.md to configure this
            view, and widgets render once db/ or knowledgebase/graph.json has data.
          </div>
        )}

        {!loading && !error && hasAnyContent &&
          plan.sections.map((section, i) => (
            <section key={i} className="mb-6">
              <h3 className="m-0 mb-2 text-sm font-semibold opacity-85">{section.heading}</h3>
              {section.entries.map((entry, j) => (
                <EntryRenderer key={j} entry={entry} sources={sources} />
              ))}
            </section>
          ))}
      </div>
    </div>
  )
}

function EntryRenderer({
  entry,
  sources,
}: {
  entry: ReportEntry
  sources: SourceCache
}) {
  if (entry.kind === 'single') {
    return <WidgetRenderer widget={entry.widget} sources={sources} />
  }
  return (
    <div className="flex flex-wrap gap-4 mb-3">
      {entry.row.widgets.map((w, i) => (
        <div key={i} className="flex-1 min-w-[240px]">
          <WidgetRenderer widget={w} sources={sources} />
        </div>
      ))}
    </div>
  )
}

function WidgetRenderer({
  widget,
  sources,
}: {
  widget: ReportWidget
  sources: SourceCache
}) {
  const raw = sources[widget.source]
  if (raw === undefined || raw === null) return null
  const resolved = applyWidgetFilter(resolveJSONPath(raw, widget.path), widget.filter)
  if (resolved == null) return null
  if (Array.isArray(resolved) && resolved.length === 0) return null

  if (widget.kind === 'text') return <TextWidget value={resolved} />
  if (widget.kind === 'table') return <TableWidget value={resolved} />
  if (widget.kind === 'chart') return <ChartWidget value={resolved} />
  return null
}

function TextWidget({ value }: { value: unknown }) {
  const text =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : JSON.stringify(value, null, 2)
  return <div className="whitespace-pre-wrap leading-6 text-foreground">{text}</div>
}

function TableWidget({ value }: { value: unknown }) {
  const columns = useMemo(() => {
    if (!Array.isArray(value) || value.length === 0) return []
    const rows = value as Record<string, unknown>[]
    const cols: string[] = []
    const seen = new Set<string>()
    for (const row of rows) {
      if (!row || typeof row !== 'object') continue
      for (const k of Object.keys(row)) {
        if (!seen.has(k)) {
          seen.add(k)
          cols.push(k)
        }
      }
    }
    return cols
  }, [value])

  if (!Array.isArray(value) || value.length === 0 || columns.length === 0) return null
  const rows = value as Record<string, unknown>[]

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr>
            {columns.map(c => (
              <th
                key={c}
                className="text-left px-2.5 py-1.5 border-b-2 border-border font-semibold text-foreground"
              >
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i}>
              {columns.map(c => (
                <td
                  key={c}
                  className="px-2.5 py-1.5 border-b border-border/60 align-top text-foreground"
                >
                  {formatCell(row[c])}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function ChartWidget({ value }: { value: unknown }) {
  // Expected shape per the design doc: [{ label: string, value: number }]. We tolerate
  // arrays of other object shapes by falling back to (keys[0], keys[1]) as (label, value).
  if (!Array.isArray(value) || value.length === 0) return null
  const points: { label: string; value: number }[] = []
  for (const item of value as Record<string, unknown>[]) {
    if (!item || typeof item !== 'object') continue
    let label: string | undefined
    let num: number | undefined
    if ('label' in item && 'value' in item) {
      label = String(item.label ?? '')
      num = Number(item.value)
    } else {
      const keys = Object.keys(item)
      if (keys.length < 2) continue
      label = String(item[keys[0]] ?? '')
      num = Number(item[keys[1]])
    }
    if (label === undefined || !Number.isFinite(num)) continue
    points.push({ label, value: num as number })
  }
  if (points.length === 0) return null

  const max = Math.max(...points.map(p => p.value), 1)
  return (
    <div className="flex flex-col gap-1.5">
      {points.map((p, i) => (
        <div key={i} className="flex items-center gap-2 text-xs text-foreground">
          <div className="w-36 whitespace-nowrap overflow-hidden text-ellipsis" title={p.label}>
            {p.label}
          </div>
          <div className="flex-1 h-3.5 rounded-sm overflow-hidden bg-muted">
            <div
              className="h-full bg-primary"
              style={{ width: `${(p.value / max) * 100}%` }}
            />
          </div>
          <div className="min-w-[40px] text-right tabular-nums">{p.value}</div>
        </div>
      ))}
    </div>
  )
}

function formatCell(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') return String(v)
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

const overlayStyle: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  background: 'rgba(0, 0, 0, 0.55)',
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  zIndex: 1000,
}

const panelStyle: React.CSSProperties = {
  background: 'var(--color-bg, #fff)',
  color: 'var(--color-fg, #111)',
  width: 'min(960px, 92vw)',
  maxHeight: '90vh',
  borderRadius: 8,
  boxShadow: '0 20px 60px rgba(0,0,0,0.4)',
  display: 'flex',
  flexDirection: 'column',
  overflow: 'hidden',
}

// Used when ReportView renders inline (e.g. as a canvas mode). Fills its parent and
// provides its own column layout so the header stays pinned and the content scrolls.
const innerWrapStyle: React.CSSProperties = {
  background: 'var(--color-bg, #fff)',
  color: 'var(--color-fg, #111)',
  height: '100%',
  width: '100%',
  display: 'flex',
  flexDirection: 'column',
  overflow: 'hidden',
}

const headerStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  padding: '12px 20px',
  borderBottom: '1px solid rgba(0,0,0,0.1)',
}

const closeButtonStyle: React.CSSProperties = {
  background: 'transparent',
  border: 'none',
  fontSize: 28,
  lineHeight: 1,
  cursor: 'pointer',
  color: 'inherit',
}

const contentStyle: React.CSSProperties = {
  padding: 20,
  overflowY: 'auto',
  flex: 1,
}

const mutedStyle: React.CSSProperties = { opacity: 0.7, fontStyle: 'italic' }
const errorStyle: React.CSSProperties = { color: '#b00020' }

const sectionStyle: React.CSSProperties = { marginBottom: 24 }
const sectionHeadingStyle: React.CSSProperties = {
  margin: '0 0 8px 0',
  fontSize: 16,
  fontWeight: 600,
  opacity: 0.85,
}

const rowStyle: React.CSSProperties = {
  display: 'flex',
  gap: 16,
  flexWrap: 'wrap',
  marginBottom: 12,
}
const rowCellStyle: React.CSSProperties = { flex: 1, minWidth: 240 }

const tableStyle: React.CSSProperties = {
  width: '100%',
  borderCollapse: 'collapse',
  fontSize: 14,
}
const thStyle: React.CSSProperties = {
  textAlign: 'left',
  padding: '6px 10px',
  borderBottom: '2px solid rgba(0,0,0,0.15)',
  fontWeight: 600,
}
const tdStyle: React.CSSProperties = {
  padding: '6px 10px',
  borderBottom: '1px solid rgba(0,0,0,0.08)',
  verticalAlign: 'top',
}

const chartWrapStyle: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 6,
}
const chartRowStyle: React.CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  fontSize: 13,
}
const chartLabelStyle: React.CSSProperties = {
  width: 140,
  whiteSpace: 'nowrap',
  overflow: 'hidden',
  textOverflow: 'ellipsis',
}
const chartBarTrackStyle: React.CSSProperties = {
  flex: 1,
  background: 'rgba(0,0,0,0.08)',
  height: 14,
  borderRadius: 3,
  overflow: 'hidden',
}
const chartBarFillStyle: React.CSSProperties = {
  height: '100%',
  background: 'var(--color-accent, #3b82f6)',
}
const chartValueStyle: React.CSSProperties = {
  minWidth: 40,
  textAlign: 'right',
  fontVariantNumeric: 'tabular-nums',
}
