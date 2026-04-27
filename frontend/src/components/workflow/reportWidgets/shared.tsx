// Shared primitives for report widgets. Pulled out of ReportViewer.tsx so
// individual widget files can be moved to this directory without dragging
// the orchestration code along. Anything used by 2+ widgets — or by both a
// widget and ReportViewer's WidgetCard dispatcher — lives here.
//
// The widget renderers themselves live in sibling files
// (TextWidget.tsx, StatWidget.tsx, etc.) and import from this module.

import React from 'react'
import { ChevronDown, ChevronUp } from 'lucide-react'
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
      {widget.title && <div className="text-sm font-semibold text-foreground">{widget.title}</div>}
      {widget.description && <div className="text-xs leading-5 text-muted-foreground">{widget.description}</div>}
    </div>
  )
}

export function WidgetVisibilityButton({
  hidden = false,
  onToggle,
  className = '',
}: {
  hidden?: boolean
  onToggle: () => void
  className?: string
}) {
  const Icon = hidden ? ChevronDown : ChevronUp
  return (
    <button
      type="button"
      onClick={onToggle}
      className={`absolute right-2 top-2 z-20 inline-flex h-7 w-7 items-center justify-center rounded-full border border-border/70 bg-background/90 text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-muted hover:text-foreground ${className}`}
      title={hidden ? 'Show widget' : 'Hide widget'}
      aria-label={hidden ? 'Show widget' : 'Hide widget'}
    >
      <Icon className="h-3.5 w-3.5" />
    </button>
  )
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
    <div className="relative rounded-xl bg-card/70 px-4 py-3 sm:border sm:border-border/60 sm:bg-card/80 sm:shadow-sm">
      {onToggleHidden && <WidgetVisibilityButton hidden={hidden} onToggle={onToggleHidden} />}
      {children}
    </div>
  )
}

// Adds a soft card around table/chart/pivot widgets. Hover lifts the shadow
// slightly so the dashboard feels interactive even when the content is static.
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
  const shellClassName =
    widget.kind === 'text'
      ? 'group relative px-0 py-0 transition-all duration-200 sm:rounded-xl sm:border sm:border-border/60 sm:bg-background/85 sm:px-3 sm:py-3 sm:shadow-sm sm:hover:border-border sm:hover:shadow-md'
      : 'group relative px-0 py-0 transition-all duration-200 sm:overflow-hidden sm:rounded-xl sm:border sm:border-border/60 sm:bg-gradient-to-b sm:from-card sm:to-muted/15 sm:px-3 sm:py-3 sm:shadow-sm sm:hover:border-border sm:hover:shadow-md'
  return (
    <div className={shellClassName}>
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <span className="absolute inset-x-0 top-0 hidden h-[2px] bg-gradient-to-r from-primary/0 via-primary/60 to-primary/0 opacity-60 transition-opacity group-hover:opacity-100 sm:block" aria-hidden />
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
