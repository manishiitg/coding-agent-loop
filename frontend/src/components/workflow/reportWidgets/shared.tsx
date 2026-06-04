// Shared primitives for report widgets. Pulled out of ReportViewer.tsx so
// individual widget files can be moved to this directory without dragging
// the orchestration code along. Anything used by 2+ widgets — or by both a
// widget and ReportViewer's WidgetCard dispatcher — lives here.
//
// The widget renderers themselves live in sibling files
// (TextWidget.tsx, StatWidget.tsx, etc.) and import from this module.

import React from 'react'
import type { ReportWidget } from '../../../services/api-types'
import { applyWidgetFilter } from '../../../lib/reportPlanParser'

// Result of narrowing a multi-row source to a single value for stat / alert
// widgets. The parser/dispatcher uses the status to decide whether to
// render the widget, an error, or skip it.
export type SingularWidgetSourceResolution =
  | { status: 'ok'; value: unknown }
  | { status: 'no-match'; value: undefined }
  | { status: 'multi-match'; value: unknown[] }

// Strips JSONPath-style array selectors from a path so stat / alert widgets
// can resolve scalars against a singular source. The dispatcher already
// narrowed the array to one row; the path should treat that row as the root.
export function normalizeSingularWidgetPath(path: string): string {
  const trimmed = path.trim()
  if (!trimmed || trimmed === '$' || trimmed === '$[*]' || trimmed === '.' || trimmed === '*') return ''
  if (trimmed.startsWith('$[*].')) return trimmed.slice(5)
  if (trimmed.startsWith('$.')) return trimmed.slice(2)
  return trimmed
}

export function resolveSingularWidgetSource(source: unknown, widget: ReportWidget): SingularWidgetSourceResolution {
  const filtered = applyWidgetFilter(source, widget.filter)
  if (!Array.isArray(filtered)) return { status: 'ok', value: filtered }
  if (filtered.length === 0) return { status: 'no-match', value: undefined }
  if (filtered.length === 1) return { status: 'ok', value: filtered[0] }
  return { status: 'multi-match', value: filtered }
}

export function WidgetHeader({
  widget,
  mode = 'default',
  className = '',
}: {
  widget: ReportWidget
  mode?: 'default' | 'metric'
  className?: string
}) {
  if (!widget.title && !widget.description) return null
  if (mode === 'metric') {
    return (
      <div className={`flex flex-col gap-1 ${className}`}>
        {widget.title && <div className="text-[11px] font-semibold uppercase tracking-[0.18em] text-muted-foreground">{widget.title}</div>}
        {widget.description && <div className="text-xs leading-5 text-muted-foreground">{widget.description}</div>}
      </div>
    )
  }
  return (
    <div className={`flex flex-col gap-1 ${className}`}>
      {widget.title && <div className="report-heading text-[15px] font-semibold leading-snug text-foreground">{widget.title}</div>}
      {widget.description && <div className="text-xs leading-5 text-muted-foreground">{widget.description}</div>}
    </div>
  )
}

// The per-widget collapse/hide control was removed per product decision — report
// widgets no longer show a compress/hide icon. Kept as a no-op so the existing
// call sites (WidgetShell, StandaloneWidgetNotice, Stat/Alert) need no changes;
// `onToggle`/`hidden` are accepted but ignored.
export function WidgetVisibilityButton(_props: {
  hidden?: boolean
  onToggle: () => void
  className?: string
}) {
  return null
}

export function StandaloneWidgetNotice({
  children,
  hidden = false,
  onToggleHidden,
}: {
  children: React.ReactNode
  hidden?: boolean
  onToggleHidden?: () => void
}) {
  return (
    <div className="relative rounded-xl bg-card px-3 py-2.5 sm:border sm:border-border">
      {onToggleHidden && <WidgetVisibilityButton hidden={hidden} onToggle={onToggleHidden} />}
      {children}
    </div>
  )
}

// Wraps table/chart/pivot widgets in a calm, flat "paper" card — a single
// hairline border, generous padding, no gradient fill or hover shadow-lift.
// Editorial restraint over dashboard flourish.
// A "document" widget renders a self-contained .md/.html document (which carries
// its own heading/structure). These render bare — no card box, no widget title —
// so the document isn't double-framed.
export function isDocumentWidget(widget: ReportWidget): boolean {
  if (widget.kind === 'markdown') return true
  if (widget.kind === 'file') {
    const fmt = widget.renderFormat || 'auto'
    if (fmt === 'markdown' || fmt === 'html') return true
    if (fmt === 'auto') {
      const ext = (widget.source || '').split('.').pop()?.toLowerCase()
      return ext === 'md' || ext === 'markdown' || ext === 'html' || ext === 'htm'
    }
  }
  return false
}

// An HTML document widget renders in a sandboxed iframe (HtmlReportFrame) that
// owns its full styling, so it goes edge-to-edge and full-width (no content-width
// cap, no reserved scrollbar gutter — the iframe self-scrolls past its height cap).
export function isHtmlDocumentWidget(widget: ReportWidget): boolean {
  if (widget.kind !== 'file') return false
  const fmt = widget.renderFormat || 'auto'
  if (fmt === 'html') return true
  if (fmt === 'auto') {
    const ext = (widget.source || '').split('.').pop()?.toLowerCase()
    return ext === 'html' || ext === 'htm'
  }
  return false
}

export function WidgetShell({
  widget,
  children,
  onToggleHidden,
}: {
  widget: ReportWidget
  children: React.ReactNode
  onToggleHidden?: () => void
}) {
  if (widget.kind === 'stat' || widget.kind === 'alert') return <>{children}</>
  // Document widgets (md/html) bring their own structure — render full-bleed.
  if (isDocumentWidget(widget)) return <>{children}</>
  const shellClassName =
    widget.kind === 'text'
      ? 'group relative px-0 py-0 transition-colors duration-200 sm:rounded-xl sm:border sm:border-border sm:bg-card sm:px-3.5 sm:py-2.5'
      : 'group relative px-0 py-0 transition-colors duration-200 sm:overflow-hidden sm:rounded-xl sm:border sm:border-border sm:bg-card sm:px-3.5 sm:py-2.5'
  return (
    <div className={shellClassName}>
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      {children}
    </div>
  )
}

// Inline per-widget diagnostic — surfaces silent-failure cases (unresolved path,
// empty filter result) so the builder doesn't see a mystery blank space.
export function WidgetError({
  widget,
  message,
  hint,
  severity = 'error',
  showWidgetMeta = true,
}: {
  widget: ReportWidget
  message: string
  hint?: string
  severity?: 'error' | 'info'
  showWidgetMeta?: boolean
}) {
  const tone = severity === 'error'
    ? 'border-destructive/30 bg-destructive/5 text-destructive'
    : 'border-border/70 bg-muted/30 text-muted-foreground'
  return (
    <div className={`rounded-xl border px-2.5 py-2 text-xs ${tone}`}>
      {showWidgetMeta && (
        <div className="flex items-center gap-2">
          {widget.title && <span className="font-semibold">{widget.title}</span>}
          <span className="opacity-70">({widget.kind})</span>
        </div>
      )}
      <div className="mt-0.5">{message}</div>
      {hint && <div className="mt-0.5 opacity-75">{hint}</div>}
    </div>
  )
}

// Inline sparkline SVG — minimal: a single path scaled to fit, 1px stroke.
// No axes, no points, no tooltip. Fits inside stat widgets + table cells.
export function Sparkline({ points, width = 120, height = 28 }: { points: number[]; width?: number; height?: number }) {
  const stroke = 'hsl(var(--chart-1))'
  const gradId = React.useId().replace(/:/g, '') + '-spark'
  const min = Math.min(...points)
  const max = Math.max(...points)
  const span = max - min || 1
  const stepX = points.length > 1 ? width / (points.length - 1) : 0
  const coords = points.map((v, i) => {
    const x = i * stepX
    const y = height - ((v - min) / span) * height
    return [x, y] as const
  })
  const lineD = coords.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(2)},${y.toFixed(2)}`).join(' ')
  const fillD = coords.length > 0
    ? `${lineD} L${(coords[coords.length - 1][0]).toFixed(2)},${height} L0,${height} Z`
    : ''
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} className="mt-1">
      <defs>
        <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={stroke} stopOpacity={0.35} />
          <stop offset="100%" stopColor={stroke} stopOpacity={0} />
        </linearGradient>
      </defs>
      {fillD && <path d={fillD} fill={`url(#${gradId})`} stroke="none" />}
      <path d={lineD} fill="none" stroke={stroke} strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}
