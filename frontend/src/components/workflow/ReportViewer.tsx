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
  evaluateShowIf,
  parseReportPlan,
  resolveJSONPath,
} from '../../lib/reportPlanParser'
import { compareValues, formatAuto, formatNamed, rowsToCSV } from '../../lib/reportFormatters'
import { useTheme } from '../../hooks/useTheme'
import type {
  ParsedReportPlan,
  ReportAlertSeverity,
  ReportEntry,
  ReportFormatterName,
  ReportWidget,
} from '../../services/api-types'

// Default rows-per-page for tables; overridable per-widget via `page_size:`.
const DEFAULT_TABLE_PAGE_SIZE = 25
// Default categorical palette. Widgets override via `colors:` / `colors_dark:`.
const CHART_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#ec4899', '#06b6d4', '#84cc16']

// Resolves the effective color palette for a widget given the current theme.
// Precedence: colorsDark (when dark) > colors > CHART_COLORS.
function resolvePalette(widget: ReportWidget, theme: 'light' | 'dark'): string[] {
  if (theme === 'dark' && widget.colorsDark && widget.colorsDark.length > 0) return widget.colorsDark
  if (widget.colors && widget.colors.length > 0) return widget.colors
  return CHART_COLORS
}

// Resolves a per-row color from widget semantic coloring. Returns undefined when
// no match exists (caller falls back to cycled palette / default).
function resolveSemanticColor(
  widget: ReportWidget,
  row: Record<string, unknown> | null | undefined,
  palette: string[],
  index: number,
): string | undefined {
  if (!widget.colorBy || !row) return undefined
  const rawValue = row[widget.colorBy]
  if (rawValue === undefined || rawValue === null) return undefined
  const key = String(rawValue)
  if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
  // No map entry — cycle palette deterministically by the distinct-value index.
  if (palette.length > 0) return palette[index % palette.length]
  return undefined
}

// Shifts a hex/named color toward a subtle tint — used for table row backgrounds
// so semantic coloring stays legible against the app theme. Hex shortcuts only;
// named colors pass through at low opacity via rgba-ish CSS.
function toRowTint(color: string): string {
  // #rgb / #rrggbb → 14% alpha; named colors → rely on color-mix-ish fallback.
  if (color.startsWith('#')) {
    if (color.length === 4) {
      const r = color[1], g = color[2], b = color[3]
      return `#${r}${r}${g}${g}${b}${b}24` // ~14% alpha
    }
    if (color.length === 7) return `${color}24`
    if (color.length === 9) return color // already has alpha
  }
  return color
}

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
          // Honor show_if when deciding whether this widget counts as "content" —
          // a hidden widget shouldn't keep the empty-state banner suppressed.
          if (!evaluateShowIf(raw, w.showIf)) continue
          // Widget kinds that render from a declared scalar/config rather than
          // a resolved array/value (stat / alert / pivot) count as content as
          // long as their source resolves to anything — their own renderer
          // surfaces per-widget empty states with better messaging.
          if (w.kind === 'stat' || w.kind === 'alert' || w.kind === 'pivot') {
            return true
          }
          const resolved = applyWidgetFilter(resolveJSONPath(raw, w.path), w.filter)
          if (resolved != null && !(Array.isArray(resolved) && resolved.length === 0)) {
            return true
          }
        }
      }
    }
    return false
  }, [planExists, plan, sources])

  const totalWidgets = useMemo(() => {
    let n = 0
    for (const s of plan.sections) {
      for (const e of s.entries) {
        n += e.kind === 'single' ? 1 : e.row.widgets.length
      }
    }
    return n
  }, [plan])

  return (
    <div className="h-full w-full flex flex-col overflow-hidden bg-background text-foreground">
      <div className="flex items-center justify-between px-5 py-3 border-b border-border flex-shrink-0 bg-background/80 backdrop-blur-sm">
        <div className="flex items-baseline gap-3">
          <h2 className="m-0 text-base font-semibold">Report</h2>
          {planExists && totalWidgets > 0 && (
            <span className="text-xs text-muted-foreground">
              {plan.sections.length} section{plan.sections.length === 1 ? '' : 's'} · {totalWidgets} widget{totalWidgets === 1 ? '' : 's'}
            </span>
          )}
        </div>
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
        {loading && <ReportSkeleton />}
        {error && <div className="text-destructive">Failed to load report: {error}</div>}

        {!loading && !error && !hasAnyContent && (
          <div className="flex flex-col items-center justify-center py-16 gap-2">
            <div className="text-3xl opacity-40" aria-hidden>📊</div>
            <div className="text-sm font-medium text-foreground">No report yet</div>
            <div className="max-w-md text-center text-xs text-muted-foreground">
              The builder writes <code className="px-1 rounded bg-muted">reports/report_plan.md</code> to configure
              this view; widgets render once <code className="px-1 rounded bg-muted">db/</code> or{' '}
              <code className="px-1 rounded bg-muted">knowledgebase/graph.json</code> has data.
            </div>
          </div>
        )}

        {!loading && !error && hasAnyContent && (
          <div className="flex flex-col gap-6 animate-in fade-in duration-200">
            {plan.sections.map((section, i) => (
              <section key={i} className="flex flex-col gap-3">
                <div className="flex items-baseline gap-2 border-b border-border/40 pb-1">
                  <h3 className="m-0 text-sm font-semibold tracking-wide uppercase text-foreground">
                    {section.heading}
                  </h3>
                  <div className="h-px flex-1" />
                </div>
                <div className="flex flex-col gap-3">
                  {section.entries.map((entry, j) => (
                    <EntryRenderer key={j} entry={entry} sources={sources} />
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

// Loading skeleton — three light-gray placeholder blocks so the layout doesn't
// jump when widgets resolve. Matches typical section + widget heights roughly.
function ReportSkeleton() {
  return (
    <div className="flex flex-col gap-6 animate-pulse">
      {[0, 1, 2].map(i => (
        <div key={i} className="flex flex-col gap-2">
          <div className="h-4 w-24 rounded bg-muted" />
          <div className="h-24 w-full rounded-md border border-border/50 bg-muted/30" />
        </div>
      ))}
    </div>
  )
}

// Heavy widgets (table/chart/pivot) get a subtle card shell for visual grouping.
// Light widgets (text/stat/alert) already carry their own chrome — wrapping
// would produce a distracting double-border.
const WIDGET_KINDS_WITH_CARD = new Set(['table', 'chart', 'pivot'])

function EntryRenderer({
  entry,
  sources,
}: {
  entry: ReportEntry
  sources: SourceCache
}) {
  if (entry.kind === 'single') {
    return <WidgetShell widget={entry.widget}><WidgetRenderer widget={entry.widget} sources={sources} /></WidgetShell>
  }
  return (
    <div className="flex flex-wrap gap-3">
      {entry.row.widgets.map((w, i) => (
        <div key={i} className="flex-1 min-w-[240px]">
          <WidgetShell widget={w}>
            <WidgetRenderer widget={w} sources={sources} />
          </WidgetShell>
        </div>
      ))}
    </div>
  )
}

// Adds a soft card around table/chart/pivot widgets. Hover lifts the shadow
// slightly so the dashboard feels interactive even when the content is static.
function WidgetShell({ widget, children }: { widget: ReportWidget; children: React.ReactNode }) {
  if (!WIDGET_KINDS_WITH_CARD.has(widget.kind)) return <>{children}</>
  return (
    <div className="rounded-lg border border-border/60 bg-card px-4 py-3 shadow-sm transition-shadow hover:shadow-md">
      {children}
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
  // Universal conditional — drop the widget entirely when show_if is set and
  // evaluates to false. Happens BEFORE path resolution so "hide when count=0"
  // doesn't flash a spinner / empty-state first.
  if (!evaluateShowIf(raw, widget.showIf)) return null

  // Stat / alert / pivot have their own shape expectations and surface their
  // own errors — delegate so we don't try to force them through the generic
  // array-or-scalar pipeline below.
  if (widget.kind === 'stat') return <StatWidget source={raw} widget={widget} />
  if (widget.kind === 'alert') return <AlertWidget source={raw} widget={widget} />
  if (widget.kind === 'pivot') return <PivotWidget source={raw} widget={widget} />

  const resolvedRaw = resolveJSONPath(raw, widget.path)
  if (resolvedRaw === undefined) {
    return (
      <WidgetError
        widget={widget}
        message={`Path "${widget.path || '(root)'}" doesn't resolve in ${widget.source}.`}
        hint="Check the source JSON for a matching key. Run validate_report_plan in builder chat for specifics."
      />
    )
  }
  const resolved = applyWidgetFilter(resolvedRaw, widget.filter)
  if (resolved == null) return null
  if (Array.isArray(resolved) && resolved.length === 0) {
    return (
      <WidgetError
        widget={widget}
        message={`No rows in ${widget.source}${widget.filter ? ` matching filter "${widget.filter}"` : ''}.`}
        hint="The source is valid but empty for this widget; this usually clears after the workflow runs."
        severity="info"
      />
    )
  }

  if (widget.kind === 'text') return <TextWidget value={resolved} widget={widget} />
  if (widget.kind === 'table') return <TableWidget value={resolved} widget={widget} />
  if (widget.kind === 'chart') return <ChartWidget value={resolved} widget={widget} />
  return null
}

// Inline per-widget diagnostic — surfaces silent-failure cases (unresolved path,
// empty filter result) so the builder doesn't see a mystery blank space.
function WidgetError({
  widget,
  message,
  hint,
  severity = 'error',
}: {
  widget: ReportWidget
  message: string
  hint?: string
  severity?: 'error' | 'info'
}) {
  const tone = severity === 'error'
    ? 'border-destructive/30 bg-destructive/5 text-destructive'
    : 'border-muted bg-muted/30 text-muted-foreground'
  return (
    <div className={`text-xs rounded border px-2 py-1.5 my-1 ${tone}`}>
      <div className="flex items-center gap-2">
        {widget.title && <span className="font-semibold">{widget.title}</span>}
        <span className="opacity-70">({widget.kind})</span>
      </div>
      <div className="mt-0.5">{message}</div>
      {hint && <div className="mt-0.5 opacity-75">{hint}</div>}
    </div>
  )
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

// ---------------------------------------------------------------------------
// Stat widget — KPI tile: big number with optional label, delta and sparkline.
// `path` resolves to a scalar. `delta_path` and `trend_path` resolve against
// the same source; delta is a signed number, trend is an array of numbers.
// ---------------------------------------------------------------------------
function StatWidget({ source, widget }: { source: unknown; widget: ReportWidget }) {
  const rawValue = resolveJSONPath(source, widget.path)
  if (rawValue === undefined) {
    return (
      <WidgetError
        widget={widget}
        message={`Path "${widget.path || '(root)'}" doesn't resolve in ${widget.source}.`}
        hint="Point `path:` at a scalar (number or string) the stat should display."
      />
    )
  }
  const formatted = widget.format ? formatNamed(rawValue, widget.format) : formatAuto(rawValue)
  const delta = widget.deltaPath != null ? resolveJSONPath(source, widget.deltaPath) : undefined
  const deltaNum = typeof delta === 'number' ? delta : Number(delta)
  const deltaFormatted =
    Number.isFinite(deltaNum) && widget.deltaPath
      ? widget.deltaFormat
        ? formatNamed(deltaNum, widget.deltaFormat).text
        : formatAuto(deltaNum).text
      : undefined
  const trend = widget.trendPath != null ? resolveJSONPath(source, widget.trendPath) : undefined
  const trendNumbers = Array.isArray(trend)
    ? (trend as unknown[]).map(v => Number(v)).filter(n => Number.isFinite(n))
    : []

  const deltaTone =
    Number.isFinite(deltaNum) && deltaNum > 0
      ? 'text-emerald-600 dark:text-emerald-400'
      : Number.isFinite(deltaNum) && deltaNum < 0
        ? 'text-red-600 dark:text-red-400'
        : 'text-muted-foreground'
  const deltaArrow = Number.isFinite(deltaNum) && deltaNum > 0 ? '▲' : Number.isFinite(deltaNum) && deltaNum < 0 ? '▼' : '·'

  return (
    <div className="flex flex-col gap-1 rounded-md border border-border/60 bg-muted/20 px-4 py-3">
      {(widget.title || widget.description) && (
        <div className="flex flex-col gap-0.5">
          {widget.title && <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{widget.title}</div>}
          {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
        </div>
      )}
      <div className="flex items-baseline gap-2">
        <div className="text-2xl font-semibold tabular-nums text-foreground">
          {widget.prefix ?? ''}{formatted.text}{widget.suffix ?? ''}
        </div>
        {deltaFormatted !== undefined && (
          <div className={`text-xs font-medium ${deltaTone}`}>
            {deltaArrow} {deltaFormatted}
          </div>
        )}
      </div>
      {widget.label && <div className="text-xs text-muted-foreground">{widget.label}</div>}
      {trendNumbers.length >= 2 && <Sparkline points={trendNumbers} />}
    </div>
  )
}

// Inline sparkline SVG — minimal: a single path scaled to fit, 1px stroke.
// No axes, no points, no tooltip. Fits inside stat widgets + table cells.
function Sparkline({ points, width = 120, height = 28 }: { points: number[]; width?: number; height?: number }) {
  const { theme } = useTheme()
  const stroke = theme === 'dark' ? '#60a5fa' : '#2563eb'
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const stepX = points.length > 1 ? width / (points.length - 1) : 0
  const d = points
    .map((v, i) => {
      const x = i * stepX
      const y = height - ((v - min) / span) * height
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="mt-1">
      <path d={d} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

// ---------------------------------------------------------------------------
// Alert widget — colored callout banner. Typically used with show_if so the
// widget only renders when the underlying condition is true ("unassigned > 0",
// "last_sync_days > 7", etc.). `path` is optional — if set, its resolved
// value is available via `{value}` interpolation in message/title.
// ---------------------------------------------------------------------------
function AlertWidget({ source, widget }: { source: unknown; widget: ReportWidget }) {
  const severity: ReportAlertSeverity = widget.severity ?? 'info'
  const tone = {
    info: 'border-blue-500/30 bg-blue-500/5 text-foreground',
    warning: 'border-amber-500/40 bg-amber-500/10 text-foreground',
    error: 'border-red-500/40 bg-red-500/10 text-foreground',
    success: 'border-emerald-500/40 bg-emerald-500/10 text-foreground',
  }[severity]
  const icon = { info: 'ℹ', warning: '⚠', error: '✕', success: '✓' }[severity]
  const resolvedValue = widget.path ? resolveJSONPath(source, widget.path) : undefined
  const valueText =
    resolvedValue === undefined || resolvedValue === null
      ? ''
      : widget.format
        ? formatNamed(resolvedValue, widget.format).text
        : formatAuto(resolvedValue).text
  const interpolate = (s: string | undefined): string | undefined =>
    s == null ? s : s.replace(/\{value\}/g, valueText)
  const title = interpolate(widget.title)
  const message = interpolate(widget.message)
  return (
    <div className={`flex items-start gap-2 rounded-md border px-3 py-2 ${tone}`}>
      <div className="text-base leading-5" aria-hidden>{icon}</div>
      <div className="flex flex-col gap-0.5">
        {title && <div className="text-sm font-semibold">{title}</div>}
        {message && <div className="text-sm">{message}</div>}
        {!title && !message && (
          <div className="text-sm">
            {valueText || <span className="italic text-muted-foreground">(no message)</span>}
          </div>
        )}
      </div>
    </div>
  )
}

type SortDirection = 'asc' | 'desc'

function TableWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const formats = widget.formats ?? {}
  const enableSearch = widget.enableSearch !== false // default true
  const pageSize = widget.pageSize ?? DEFAULT_TABLE_PAGE_SIZE
  const hidden = useMemo(() => new Set(widget.hideColumns ?? []), [widget.hideColumns])
  const palette = useMemo(() => resolvePalette(widget, theme), [widget, theme])
  // Distinct-value → palette-index map so unmapped colorBy values get stable colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy || !Array.isArray(value)) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const row of value as Record<string, unknown>[]) {
      const raw: unknown = row?.[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [value, widget.colorBy])

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
            {pageRows.map((row, i) => {
              const rowColor = resolveSemanticColor(widget, row, palette, distinctIndex[String(row?.[widget.colorBy ?? ''] ?? '')] ?? i)
              const rowStyle = rowColor ? { backgroundColor: toRowTint(rowColor) } : undefined
              return (
              <tr
                key={safePage * pageSize + i}
                className={rowColor ? 'hover:bg-muted/40 transition-colors' : 'even:bg-muted/20 hover:bg-muted/40 transition-colors'}
                style={rowStyle}
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
              )
            })}
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

// ---------------------------------------------------------------------------
// Pivot widget — rows × columns × aggregate(values). Reads an array of records
// (source + path + optional filter), buckets by rowsField/columnsField, and
// applies the aggregator to valuesField for each cell.
// ---------------------------------------------------------------------------
function PivotWidget({ source, widget }: { source: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
  const resolvedRaw = resolveJSONPath(source, widget.path)
  const resolved = applyWidgetFilter(resolvedRaw, widget.filter)

  if (!widget.rowsField || !widget.columnsField || !widget.valuesField) {
    return (
      <WidgetError
        widget={widget}
        message="pivot requires rows, columns, and values fields."
        hint="Set `rows: <field>`, `columns: <field>`, `values: <field>`; optionally `aggregate: sum|avg|count|min|max|first`."
      />
    )
  }
  if (!Array.isArray(resolved) || resolved.length === 0) {
    return (
      <WidgetError
        widget={widget}
        message={`pivot source resolves to ${resolvedRaw == null ? 'nothing' : 'an empty or non-array value'}.`}
        hint="Point `path:` at a non-empty array of objects."
        severity="info"
      />
    )
  }

  const rowsField = widget.rowsField
  const columnsField = widget.columnsField
  const valuesField = widget.valuesField
  const aggregate = widget.aggregate ?? 'sum'

  // Build the cell grid — Map<rowKey, Map<colKey, aggregated>> with column-order
  // and row-order tracked in insertion-order arrays so rendering is stable.
  const rowKeys: string[] = []
  const colKeys: string[] = []
  const seenRow = new Set<string>()
  const seenCol = new Set<string>()
  const buckets = new Map<string, Map<string, number[]>>() // rowKey -> colKey -> raw numeric values
  const stringBuckets = new Map<string, Map<string, unknown>>() // for 'first' aggregate
  for (const row of resolved as Record<string, unknown>[]) {
    if (!row || typeof row !== 'object') continue
    const rk = row[rowsField]
    const ck = row[columnsField]
    if (rk == null || ck == null) continue
    const rKey = String(rk)
    const cKey = String(ck)
    if (!seenRow.has(rKey)) { seenRow.add(rKey); rowKeys.push(rKey) }
    if (!seenCol.has(cKey)) { seenCol.add(cKey); colKeys.push(cKey) }
    if (!buckets.has(rKey)) buckets.set(rKey, new Map())
    const inner = buckets.get(rKey)!
    if (!inner.has(cKey)) inner.set(cKey, [])
    const v = row[valuesField]
    if (aggregate === 'first') {
      if (!stringBuckets.has(rKey)) stringBuckets.set(rKey, new Map())
      const sInner = stringBuckets.get(rKey)!
      if (!sInner.has(cKey)) sInner.set(cKey, v)
    } else if (aggregate === 'count') {
      inner.get(cKey)!.push(1)
    } else {
      const num = typeof v === 'number' ? v : Number(v)
      if (Number.isFinite(num)) inner.get(cKey)!.push(num)
    }
  }

  // Reduce each cell's array via the aggregator.
  const cell = (rKey: string, cKey: string): { raw: unknown; num: number | undefined } => {
    if (aggregate === 'first') {
      const raw = stringBuckets.get(rKey)?.get(cKey)
      const num = typeof raw === 'number' ? raw : Number(raw)
      return { raw, num: Number.isFinite(num) ? num : undefined }
    }
    const vals = buckets.get(rKey)?.get(cKey) ?? []
    if (vals.length === 0) return { raw: undefined, num: undefined }
    let num: number
    switch (aggregate) {
      case 'avg': num = vals.reduce((a, b) => a + b, 0) / vals.length; break
      case 'min': num = Math.min(...vals); break
      case 'max': num = Math.max(...vals); break
      case 'count': num = vals.length; break
      case 'sum':
      default: num = vals.reduce((a, b) => a + b, 0); break
    }
    return { raw: num, num }
  }

  const allNums: number[] = []
  for (const rk of rowKeys) for (const ck of colKeys) {
    const { num } = cell(rk, ck)
    if (num !== undefined) allNums.push(num)
  }
  const min = allNums.length > 0 ? Math.min(...allNums) : 0
  const max = allNums.length > 0 ? Math.max(...allNums) : 0
  const span = max - min || 1

  const heatmap = widget.heatmap === true
  const [heatLow, heatHigh] = widget.heatmapColors ?? (
    theme === 'dark' ? ['#1e293b', '#60a5fa'] : ['#eff6ff', '#2563eb']
  )

  const tintFor = (n: number | undefined): string | undefined => {
    if (!heatmap || n === undefined) return undefined
    const t = (n - min) / span
    return lerpHex(heatLow, heatHigh, Math.max(0, Math.min(1, t)))
  }

  return (
    <div className="flex flex-col gap-2">
      {(widget.title || widget.description) && (
        <div className="flex flex-col gap-0.5">
          {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
          {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
        </div>
      )}
      <div className="overflow-auto max-h-[60vh] border border-border/60 rounded-md">
        <table className="border-collapse text-sm">
          <thead className="sticky top-0 bg-background z-10 shadow-[0_1px_0_0_var(--border)]">
            <tr>
              <th className="px-2.5 py-1.5 border-b-2 border-border font-semibold text-left text-muted-foreground">
                {rowsField} ╲ {columnsField}
              </th>
              {colKeys.map(ck => (
                <th key={ck} className="px-2.5 py-1.5 border-b-2 border-border font-semibold text-right text-foreground">
                  {ck}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rowKeys.map(rk => (
              <tr key={rk} className="hover:bg-muted/30 transition-colors">
                <th className="px-2.5 py-1.5 border-b border-border/40 font-medium text-left text-foreground bg-muted/20 sticky left-0">
                  {rk}
                </th>
                {colKeys.map(ck => {
                  const { raw, num } = cell(rk, ck)
                  const text = raw == null
                    ? ''
                    : widget.format
                      ? formatNamed(raw, widget.format).text
                      : formatAuto(raw).text
                  const bg = tintFor(num)
                  return (
                    <td
                      key={ck}
                      className="px-2.5 py-1.5 border-b border-border/40 text-right tabular-nums text-foreground"
                      style={bg ? { backgroundColor: bg } : undefined}
                    >
                      {text}
                    </td>
                  )
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="text-xs text-muted-foreground">
        {rowKeys.length} row{rowKeys.length === 1 ? '' : 's'} × {colKeys.length} col{colKeys.length === 1 ? '' : 's'} · aggregate: {aggregate}
      </div>
    </div>
  )
}

// Linear-interpolates between two hex colors (#rrggbb). Used by the pivot
// heatmap tint calculation. Gracefully falls back to the low color for bad
// inputs so a malformed `heatmap_colors` never crashes rendering.
function lerpHex(lo: string, hi: string, t: number): string {
  const parse = (c: string): [number, number, number] | null => {
    if (!c.startsWith('#')) return null
    const hex = c.length === 4
      ? `#${c[1]}${c[1]}${c[2]}${c[2]}${c[3]}${c[3]}`
      : c.length === 7 ? c : null
    if (!hex) return null
    return [
      parseInt(hex.slice(1, 3), 16),
      parseInt(hex.slice(3, 5), 16),
      parseInt(hex.slice(5, 7), 16),
    ]
  }
  const a = parse(lo)
  const b = parse(hi)
  if (!a || !b) return lo
  const mix = (x: number, y: number) => Math.round(x + (y - x) * t)
  const r = mix(a[0], b[0]).toString(16).padStart(2, '0')
  const g = mix(a[1], b[1]).toString(16).padStart(2, '0')
  const bl = mix(a[2], b[2]).toString(16).padStart(2, '0')
  return `#${r}${g}${bl}`
}

// Normalises chart data to {label, value, ...rest} pairs. When `xAxis`/`yAxis` are
// declared in the widget, those field names take precedence; otherwise we accept
// either the canonical {label,value} shape or fall back to the first two object keys.
//
// Multi-series mode: when `widget.series` is non-empty, every field in `series`
// becomes a numeric key on each point. `_value` is kept as the FIRST series'
// value so single-series code paths (sort, topN) continue to work intuitively.
function normaliseChartPoints(value: unknown, widget: ReportWidget): Array<Record<string, unknown> & { _label: string; _value: number }> {
  if (!Array.isArray(value) || value.length === 0) return []
  const xField = widget.xAxis
  const yField = widget.yAxis
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const out: Array<Record<string, unknown> & { _label: string; _value: number }> = []
  for (const item of value as Record<string, unknown>[]) {
    if (!item || typeof item !== 'object') continue
    let label: string | undefined
    let num: number | undefined
    const extra: Record<string, number> = {}
    if (series) {
      // Multi-series: require an explicit xField (or fall back to first key).
      const xKey = xField ?? Object.keys(item)[0]
      label = item[xKey] != null ? String(item[xKey]) : undefined
      for (const s of series) {
        const v = Number(item[s])
        if (Number.isFinite(v)) extra[s] = v
      }
      // `_value` mirrors the first series value so sort/topN still mean something.
      num = series.length > 0 ? extra[series[0]] : undefined
    } else if (xField && yField) {
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
    if (label === undefined) continue
    if (!series && !Number.isFinite(num)) continue
    out.push({ ...item, ...extra, _label: label, _value: Number.isFinite(num) ? (num as number) : 0 })
  }
  return out
}

function ChartWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const { theme } = useTheme()
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

  const palette = resolvePalette(widget, theme)
  // For semantic coloring, assign each distinct colorBy value a stable index into
  // the palette so unmapped values still get consistent (not random) colors.
  const distinctIndex = useMemo(() => {
    if (!widget.colorBy) return {}
    const out: Record<string, number> = {}
    let next = 0
    for (const p of points) {
      const raw: unknown = (p as unknown as Record<string, unknown>)[widget.colorBy]
      if (raw === undefined || raw === null) continue
      const key = String(raw)
      if (!(key in out)) out[key] = next++
    }
    return out
  }, [points, widget.colorBy])
  const colorForPoint = (p: (typeof points)[number], fallbackIndex: number): string => {
    if (widget.colorBy) {
      const raw = (p as unknown as Record<string, unknown>)[widget.colorBy]
      const key = raw === undefined || raw === null ? '' : String(raw)
      if (widget.colorMap && widget.colorMap[key]) return widget.colorMap[key]
      if (key in distinctIndex) return palette[distinctIndex[key] % palette.length]
    }
    return palette[fallbackIndex % palette.length]
  }

  const header = (widget.title || widget.description) ? (
    <div className="flex flex-col gap-0.5 mb-2">
      {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
      {widget.description && <div className="text-xs text-muted-foreground">{widget.description}</div>}
    </div>
  ) : null

  // Pie chart — always colors per slice.
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
                label={showValues ? ((entry: unknown) => {
                  const e = entry as { _label?: string; _value?: number }
                  return `${e._label ?? ''}: ${e._value ?? ''}`
                }) as never : undefined}
              >
                {points.map((p, i) => (
                  <Cell key={i} fill={colorForPoint(p, i)} />
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

  // Multi-series mode: one series per field in `widget.series`. Colors come
  // from `seriesColors` (parallel) with fallback to the palette. Stacked bars
  // and areas share a stackId; lines never stack.
  const series = widget.series && widget.series.length > 0 ? widget.series : null
  const seriesColorFor = (idx: number): string => {
    if (widget.seriesColors && widget.seriesColors[idx]) return widget.seriesColors[idx]
    return palette[idx % palette.length]
  }
  const stackId = widget.stacked ? 'stack-a' : undefined

  // Single-series fallback — palette[0] for line/area; bar supports per-Cell.
  const singleSeriesColor = palette[0]

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
            {series && <Legend wrapperStyle={{ fontSize: 11 }} />}
            {series
              ? series.map((field, i) => {
                  const color = seriesColorFor(i)
                  if (chartType === 'bar') {
                    return <Bar key={field} dataKey={field} fill={color} stackId={stackId} radius={widget.stacked ? undefined : [3, 3, 0, 0]} />
                  }
                  if (chartType === 'line') {
                    return <Line key={field} type="monotone" dataKey={field} stroke={color} strokeWidth={2} dot={{ r: 3 }} />
                  }
                  if (chartType === 'area') {
                    return <Area key={field} type="monotone" dataKey={field} stroke={color} fill={color} fillOpacity={0.3} stackId={stackId} />
                  }
                  return null
                })
              : <>
                  {chartType === 'bar' && (
                    <Bar dataKey="_value" radius={[3, 3, 0, 0]} label={ValueLabel}>
                      {points.map((p, i) => (
                        <Cell key={i} fill={colorForPoint(p, widget.colorBy ? i : 0)} />
                      ))}
                    </Bar>
                  )}
                  {chartType === 'line' && <Line type="monotone" dataKey="_value" stroke={singleSeriesColor} strokeWidth={2} dot={{ r: 3 }} label={ValueLabel} />}
                  {chartType === 'area' && <Area type="monotone" dataKey="_value" stroke={singleSeriesColor} fill={singleSeriesColor} fillOpacity={0.25} label={ValueLabel} />}
                </>}
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
