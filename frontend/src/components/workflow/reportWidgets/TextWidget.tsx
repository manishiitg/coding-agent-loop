import type { ReportWidget } from '../../../services/api-types'
import { WidgetHeader } from './shared'

export function TextWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const text =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : JSON.stringify(value, null, 2)
  return (
    <div className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="whitespace-pre-wrap rounded-lg bg-muted/25 px-2.5 py-2.5 text-sm leading-7 text-foreground sm:px-3 sm:text-[15px]">
        {text}
      </div>
    </div>
  )
}
