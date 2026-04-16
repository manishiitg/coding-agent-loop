// Dynamic report viewer — parses reports/report_plan.md, fetches each widget's JSON
// source, renders widgets. See docs/workflow/persistent_stores_design.md.

import { useEffect, useMemo, useState } from 'react'
import {
  Bar, BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Line, LineChart,
  Area, AreaChart,
  Pie, PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis, YAxis,
} from 'recharts'
import { ArrowDown, ArrowUp, ArrowUpDown, Download, Search } from 'lucide-react'
import { agentApi } from '../../services/api'
import {
  applyWidgetFilter,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import { compareValues, formatAuto, formatNamed, rowsToCSV } from '../../lib/reportFormatters'
import type {
  ParsedReportPlan,
  ReportEntry,
  ReportFormatterName,
  ReportWidget,
} from '../../services/api-types'

// Default rows-per-page for tables; overridable per-widget via `page_size:`.
const DEFAULT_TABLE_PAGE_SIZE = 25
// Recharts categorical palette; cycles for pie slices and multi-series bars.
const CHART_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16']

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
  if (raw === undefined) {
    // Source still loading — show a small placeholder so empty widgets aren't silent.
    return (
      <div className="text-xs italic text-muted-foreground py-2">
        Loading {widget.source}…
      </div>
    )
  }
  if (raw === null) {
    return (
      <div className="text-xs italic text-muted-foreground py-2">
        Source not available: <code className="px-1 rounded bg-muted">{widget.source}</code>
      </div>
    )
  }
  const resolved = applyWidgetFilter(resolveJSONPath(raw, widget.path), widget.filter)
  if (resolved == null) return null
  if (Array.isArray(resolved) && resolved.length === 0) return null

  if (widget.kind === 'text') return <TextWidget value={resolved} widget={widget} />
  if (widget.kind === 'table') return <TableWidget value={resolved} widget={widget} />
  if (widget.kind === 'chart') return <ChartWidget value={resolved} widget={widget} />
  return null
}

function TextWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const text =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : JSON.stringify(value, null, 2)
  return (
    <div className="flex flex-col gap-1">
      {(widget.title || widget.description) && (
        <div className="flex flex-col gap-0.5">
          {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
          {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
        </div>
      )}
      <div className="whitespace-pre-wrap leading-6 text-foreground">{text}</div>
    </div>
  )
}

type SortDirection = 'asc' | 'desc'

function TableWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false // default true
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])

  const [sortField, setSortField] = useState<string | null>(widget.defaultSort?.field ?? null)
  const [sortDir, setSortDir] = useState<SortDirection>(widget.defaultSort?.direction ?? 'asc')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)

  const columns = useMemo(() => {
    if (!Array.isArray(value) || value.length === 0) return []
    const rows = value as Record<string, unknown>[]
    const cols: string[] = []
    const seen = new Set<string>()
    for (const row of rows) {
      if (!row || typeof row !== 'object') continue
      for (const k of Object.keys(row)) {
        if (!seen.has(k) && !hidden.has(k)) {
          seen.add(k)
          cols.push(k)
        }
      }
    }
    return cols
  }, [value, hidden])

  // Auto-detect numeric columns (used for right-alignment when no formatter declared).
  const numericColumns = useMemo(() => {
    const out = new Set<string>()
    if (!Array.isArray(value)) return out
    const rows = value as Record<string, unknown>[]
    for (const col of columns) {
      // Treat a column as numeric when at least one non-null value parses as a finite number
      // AND no value parses as a non-empty non-numeric string.
      let sawNumeric = false
      let sawNonNumeric = false
      for (const row of rows) {
        const v = row?.[col]
        if (v == null || v === '') continue
        if (typeof v === 'number' && Number.isFinite(v)) {
          sawNumeric = true
        } else if (typeof v === 'string' && v.trim() !== '' && Number.isFinite(Number(v))) {
          sawNumeric = true
        } else {
          sawNonNumeric = true
          break
        }
      }
      if (sawNumeric && !sawNonNumeric) out.add(col)
    }
    return out
  }, [value, columns])

  const filtered = useMemo(() => {
    if (!Array.isArray(value)) return []
    const rows = value as Record<string, unknown>[]
    const needle = search.trim().toLowerCase()
    if (!needle) return rows
    return rows.filter(row => {
      if (!row) return false
      for (const col of columns) {
        const v = row[col]
        if (v == null) continue
        const s = typeof v === 'object' ? JSON.stringify(v) : String(v)
        if (s.toLowerCase().includes(needle)) return true
      }
      return false
    })
  }, [value, columns, search])

  const sorted = useMemo(() => {
    if (!sortField) return filtered
    const copy = [...filtered]
    copy.sort((a, b) => {
      const cmp = compareValues(a?.[sortField], b?.[sortField])
      return sortDir === 'asc' ? cmp : -cmp
    })
    return copy
  }, [filtered, sortField, sortDir])

  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))
  const safePage = Math.min(page, totalPages - 1)
  const pageRows = useMemo(
    () => sorted.slice(safePage * pageSize, (safePage + 1) * pageSize),
    [sorted, safePage, pageSize],
  )

  if (!Array.isArray(value) || value.length === 0 || columns.length === 0) return null
  const rowCountText =
    sorted.length === (value as unknown[]).length
      ? `${sorted.length} row${sorted.length === 1 ? '' : 's'}`
      : `${sorted.length} of ${(value as unknown[]).length}`

  const onSortClick = (col: string) => {
    if (sortField === col) {
      setSortDir(prev => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(col)
      setSortDir('asc')
    }
    setPage(0)
  }

  const handleExport = () => {
    const csv = rowsToCSV(sorted as Record<string, unknown>[], columns)
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `report-table-${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  return (
    <div className="flex flex-col gap-2">
      {(widget.title || widget.description) && (
        <div className="flex flex-col gap-0.5">
          {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
          {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
        </div>
      )}
      <div className="flex items-center gap-2 text-xs">
        {enableSearch && (
          <div className="relative flex-1 max-w-xs">
            <Search className="absolute left-2 top-1.5 w-3.5 h-3.5 text-muted-foreground" />
            <input
              type="text"
              placeholder="Search…"
              value={search}
              onChange={e => {
                setSearch(e.target.value)
                setPage(0)
              }}
              className="w-full pl-7 pr-2 py-1 text-xs bg-muted/30 border border-input rounded focus:outline-none focus:ring-1 focus:ring-primary"
            />
          </div>
        )}
        <div className="text-muted-foreground">{rowCountText}</div>
        <button
          onClick={handleExport}
          className="ml-auto p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          title="Export CSV"
        >
          <Download className="w-3.5 h-3.5" />
        </button>
      </div>

      <div className="overflow-auto max-h-[60vh] border border-border/60 rounded-md">
        <table className="w-full border-collapse text-sm">
          <thead className="sticky top-0 bg-background z-10 shadow-[0_1px_0_0_var(--border)]">
            <tr>
              {columns.map(c => {
                const isSorted = sortField === c
                const align = numericColumns.has(c) || c in formats ? 'text-right' : 'text-left'
                return (
                  <th
                    key={c}
                    className={`px-2.5 py-1.5 border-b-2 border-border font-semibold text-foreground select-none cursor-pointer hover:bg-muted/40 transition-colors ${align}`}
                    onClick={() => onSortClick(c)}
                  >
                    <span className="inline-flex items-center gap-1">
                      <span>{c}</span>
                      {isSorted ? (
                        sortDir === 'asc' ? (
                          <ArrowUp className="w-3 h-3" />
                        ) : (
                          <ArrowDown className="w-3 h-3" />
                        )
                      ) : (
                        <ArrowUpDown className="w-3 h-3 opacity-30" />
                      )}
                    </span>
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody>
            {pageRows.map((row, i) => (
              <tr
                key={safePage * pageSize + i}
                className="even:bg-muted/20 hover:bg-muted/40 transition-colors"
              >
                {columns.map(c => {
                  const preset = formats[c] as ReportFormatterName | undefined
                  const formatted = preset ? formatNamed(row?.[c], preset) : formatAuto(row?.[c])
                  const align = formatted.isNumeric ? 'text-right tabular-nums' : 'text-left'
                  return (
                    <td
                      key={c}
                      className={`px-2.5 py-1.5 border-b border-border/40 align-top text-foreground ${align}`}
                    >
                      {formatted.text}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages > 1 && (
        <div className="flex items-center justify-end gap-2 text-xs text-muted-foreground">
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={safePage === 0}
            className="px-2 py-0.5 rounded border border-border disabled:opacity-30 hover:bg-muted transition-colors"
          >
            Prev
          </button>
          <span>
            Page {safePage + 1} of {totalPages}
          </span>
          <button
            onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
            disabled={safePage >= totalPages - 1}
            className="px-2 py-0.5 rounded border border-border disabled:opacity-30 hover:bg-muted transition-colors"
          >
            Next
          </button>
        </div>
      )}
    </div>
  )
}

// Normalises chart data to {label, value, ...rest} pairs. When `xAxis`/`yAxis` are
// declared in the widget, those field names take precedence; otherwise we accept
// either the canonical {label,value} shape or fall back to the first two object keys.
function normaliseChartPoints(value: unknown, widget: ReportWidget): Array<Record<string, unknown> & { _label: string; _value: number }> {
  if (!Array.isArray(value) || value.length === 0) return []
  const xField = widget.xAxis
  const yField = widget.yAxis
  const out: Array<Record<string, unknown> & { _label: string; _value: number }> = []
  for (const item of value as Record<string, unknown>[]) {
    if (!item || typeof item !== 'object') continue
    let label: string | undefined
    let num: number | undefined
    if (xField && yField) {
      label = item[xField] != null ? String(item[xField]) : undefined
      num = Number(item[yField])
    } else if ('label' in item && 'value' in item) {
      label = String(item.label ?? '')
      num = Number(item.value)
    } else {
      const keys = Object.keys(item)
      if (keys.length < 2) continue
      label = String(item[keys[0]] ?? '')
      num = Number(item[keys[1]])
    }
    if (label === undefined || !Number.isFinite(num)) continue
    out.push({ ...item, _label: label, _value: num as number })
  }
  return out
}

function ChartWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const points = useMemo(() => {
    let pts = normaliseChartPoints(value, widget)
    // Sort first (so topN takes the right slice), then truncate.
    if (widget.sort === 'asc') pts = [...pts].sort((a, b) => a._value - b._value)
    else if (widget.sort === 'desc') pts = [...pts].sort((a, b) => b._value - a._value)
    if (widget.topN && widget.topN > 0) pts = pts.slice(0, widget.topN)
    return pts
  }, [value, widget])

  if (points.length === 0) return null

  const chartType = widget.chartType ?? 'bar'
  const showValues = widget.showValues === true
  const heightPx = widget.height ?? 288 // h-72 default

  const header = (widget.title || widget.description) ? (
    <div className="flex flex-col gap-0.5 mb-2">
      {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
      {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
    </div>
  ) : null

  // Pie chart — special layout.
  if (chartType === 'pie') {
    return (
      <div className="flex flex-col">
        {header}
        <div style={{ width: '100%', height: heightPx }}>
          <ResponsiveContainer>
            <PieChart>
              <Pie
                data={points}
                dataKey="_value"
                nameKey="_label"
                outerRadius={Math.min(120, heightPx * 0.4)}
                label={showValues ? (entry: { _label: string; _value: number }) => `${entry._label}: ${entry._value}` : undefined}
              >
                {points.map((_, i) => (
                  <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                ))}
              </Pie>
              <Tooltip />
              <Legend />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>
    )
  }

  // Bar / line / area share the same XY layout.
  const ChartContainer =
    chartType === 'line' ? LineChart : chartType === 'area' ? AreaChart : BarChart
  const ValueLabel = showValues
    ? { dataKey: '_value', position: chartType === 'bar' ? 'top' as const : 'top' as const, fontSize: 10 }
    : undefined

  return (
    <div className="flex flex-col">
      {header}
      <div style={{ width: '100%', height: heightPx }}>
        <ResponsiveContainer>
          <ChartContainer data={points} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
            <CartesianGrid strokeDasharray="3 3" opacity={0.3} />
            <XAxis dataKey="_label" tick={{ fontSize: 11 }} interval={0} angle={points.length > 8 ? -30 : 0} textAnchor={points.length > 8 ? 'end' : 'middle'} height={points.length > 8 ? 60 : 30} />
            <YAxis tick={{ fontSize: 11 }} />
            <Tooltip />
            {chartType === 'bar' && <Bar dataKey="_value" fill={CHART_COLORS[0]} radius={[3, 3, 0, 0]} label={ValueLabel} />}
            {chartType === 'line' && <Line type="monotone" dataKey="_value" stroke={CHART_COLORS[0]} strokeWidth={2} dot={{ r: 3 }} label={ValueLabel} />}
            {chartType === 'area' && <Area type="monotone" dataKey="_value" stroke={CHART_COLORS[0]} fill={CHART_COLORS[0]} fillOpacity={0.25} label={ValueLabel} />}
          </ChartContainer>
        </ResponsiveContainer>
      </div>
    </div>
  )
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
