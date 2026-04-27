import type { ReportWidget } from '../../../services/api-types'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import { WidgetHeader } from './shared'

export function MarkdownWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const markdown =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : `\`\`\`json\n${JSON.stringify(value, null, 2)}\n\`\`\``
  return (
    <div className="flex flex-col gap-2.5">
      <WidgetHeader widget={widget} />
      <div className="rounded-lg bg-muted/20 px-3 py-3 text-sm text-foreground">
        <MarkdownRenderer content={markdown} className="max-w-none" maxHeight="none" disablePathLinking />
      </div>
    </div>
  )
}
