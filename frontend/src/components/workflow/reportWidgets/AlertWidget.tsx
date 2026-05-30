// Alert widget — colored callout banner. Typically used with show_if so the
// widget only renders when the underlying condition is true ("unassigned > 0",
// "last_sync_days > 7", etc.). `path` is optional — if set, its resolved
// value is available via `{value}` interpolation in message/title.

import type { ReportAlertSeverity, ReportWidget } from '../../../services/api-types'
import { formatAuto, formatNamed } from '../../../lib/reportFormatters'
import { resolveJSONPath } from '../../../lib/reportPlanParser'
import { severityIcons, severityTones } from '../../../lib/reportTokens'
import {
  type SingularWidgetSourceResolution,
  StandaloneWidgetNotice,
  WidgetError,
  WidgetVisibilityButton,
  normalizeSingularWidgetPath,
} from './shared'

export function AlertWidget({
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
  if (resolution.status === 'no-match') return null
  if (resolution.status === 'multi-match') {
    return (
      <StandaloneWidgetNotice onToggleHidden={onToggleHidden}>
        <WidgetError
          widget={widget}
          message={`Alert widget matched ${resolution.value.length} rows in ${widget.source}${widget.filter ? ` for filter "${widget.filter}"` : ''}.`}
          hint="Alert widgets backed by array sources need a single row so `{value}` and `show_if` resolve consistently."
        />
      </StandaloneWidgetNotice>
    )
  }

  const severity: ReportAlertSeverity = widget.severity ?? 'info'
  const tone = severityTones[severity]
  const icon = severityIcons[severity]
  const resolvedValue = widget.path ? resolveJSONPath(source, normalizeSingularWidgetPath(widget.path)) : undefined
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
    <div className={`relative flex items-start gap-3 rounded-xl border border-l-[3px] px-4 py-3 ${tone}`}>
      {onToggleHidden && <WidgetVisibilityButton onToggle={onToggleHidden} />}
      <div className="mt-0.5 flex h-7 w-7 items-center justify-center rounded-full border border-current/15 bg-background/60 text-sm leading-5" aria-hidden>{icon}</div>
      <div className="flex flex-col gap-0.5">
        {title && <div className="report-heading text-[15px] font-semibold">{title}</div>}
        {message && <div className="text-sm leading-6">{message}</div>}
        {!title && !message && (
          <div className="text-sm leading-6">
            {valueText || <span className="italic text-muted-foreground">(no message)</span>}
          </div>
        )}
      </div>
    </div>
  )
}
