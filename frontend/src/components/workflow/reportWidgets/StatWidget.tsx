// Stat widget — KPI tile: big number with optional label, delta and sparkline.
// `path` resolves to a scalar. `delta_path` and `trend_path` resolve against
// the same source; delta is a signed number, trend is an array of numbers.

import type { ReportWidget } from '../../../services/api-types'
import { formatAuto, formatNamed } from '../../../lib/reportFormatters'
import { resolveJSONPath } from '../../../lib/reportPlanParser'
import { trendArrow, trendDirection, trendTones } from '../../../lib/reportTokens'
import {
  type SingularWidgetSourceResolution,
  Sparkline,
  StandaloneWidgetNotice,
  WidgetError,
  WidgetHeader,
  WidgetVisibilityButton,
  normalizeSingularWidgetPath,
} from './shared'

export function StatWidget({
  source,
  resolution,
  widget,
  onToggleHidden,
}: {
  source: unknown
  resolution: SingularWidgetSourceResolution
  widget: ReportWidget
  onToggleHidden?: () => void
}) {
  if (resolution.status === 'no-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`No rows in ${widget.source}${widget.filter ? ` matching filter "${widget.filter}"` : ''}.`}
          hint="Stat widgets backed by array sources need one matching row."
          severity="info"
        />
      </StandaloneWidgetNotice>
    )
  }
  if (resolution.status === 'multi-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Stat widget matched ${resolution.value.length} rows in ${widget.source}${widget.filter ? ` for filter "${widget.filter}"` : ''}.`}
          hint="Point the widget at a single row, or narrow the filter so exactly one record remains."
        />
      </StandaloneWidgetNotice>
    )
  }

  const scalarPath = normalizeSingularWidgetPath(widget.path)
  const rawValue = resolveJSONPath(source, scalarPath)
  if (rawValue === undefined) {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Path "${widget.path || '(root)'}" doesn't resolve in ${widget.source}.`}
          hint="Point `path:` at a scalar (number or string) the stat should display."
        />
      </StandaloneWidgetNotice>
    )
  }
  const formatted = widget.format ? formatNamed(rawValue, widget.format) : formatAuto(rawValue)
  const delta = widget.deltaPath != null ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.deltaPath)) : undefined
  const deltaNum = typeof delta === 'number' ? delta : Number(delta)
  const deltaFormatted =
    Number.isFinite(deltaNum) && widget.deltaPath
      ? widget.deltaFormat
        ? formatNamed(deltaNum, widget.deltaFormat).text
        : formatAuto(deltaNum).text
      : undefined
  const trend = widget.trendPath != null ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.trendPath)) : undefined
  const trendNumbers = Array.isArray(trend)
    ? (trend as unknown[]).map(v => Number(v)).filter(n => Number.isFinite(n))
    : []

  const direction = trendDirection(Number.isFinite(deltaNum) ? deltaNum : null)
  const deltaTone = trendTones[direction]
  const deltaArrow = trendArrow[direction]

  return (
    <div className="relative flex h-full flex-col gap-2 overflow-hidden rounded-xl bg-card/75 px-3 py-3 transition-shadow sm:border sm:border-border/60 sm:bg-gradient-to-br sm:from-card sm:via-card sm:to-muted/25 sm:shadow-sm sm:hover:shadow-md">
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <span className="absolute inset-x-0 top-0 hidden h-[2px] bg-gradient-to-r from-primary/0 via-primary/60 to-primary/0 sm:block" aria-hidden />
      <WidgetHeader widget={widget} mode="metric" />
      <div className="flex items-baseline gap-2">
        <div className="text-2xl font-semibold tabular-nums tracking-tight text-foreground sm:text-3xl">
          {widget.prefix ?? ''}{formatted.text}{widget.suffix ?? ''}
        </div>
        {deltaFormatted !== undefined && (
          <div className={`inline-flex items-center gap-1 text-[10px] font-semibold px-1.5 py-0.5 rounded-full ${deltaTone}`}>
            <span aria-hidden>{deltaArrow}</span>
            <span className="tabular-nums">{deltaFormatted}</span>
          </div>
        )}
      </div>
      {widget.label && <div className="text-xs text-muted-foreground">{widget.label}</div>}
      {trendNumbers.length >= 2 && <Sparkline points={trendNumbers} />}
    </div>
  )
}
