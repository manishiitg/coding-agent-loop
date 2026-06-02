import type { ReportWidget } from '../../../services/api-types'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import { useEmbeddedWidgetRenderer } from './reportEmbedContext'

export function MarkdownWidget({ value, widget }: { value: unknown; widget: ReportWidget }) {
  const renderEmbeddedWidget = useEmbeddedWidgetRenderer() ?? undefined
  const markdown =
    typeof value === 'string'
      ? value
      : typeof value === 'number' || typeof value === 'boolean'
        ? String(value)
        : `\`\`\`json\n${JSON.stringify(value, null, 2)}\n\`\`\``
  // Enable workspace-file path linking so a markdown report (e.g. an in-depth
  // per-PAN write-up) can link to PDFs/other workspace files and have them open
  // in the in-app viewer on click. basePath is the markdown's own source file, so
  // relative links (e.g. "26AS_AAAFX.pdf" or "../Downloads/x.pdf") resolve against
  // its folder. Only hrefs ending in a real file extension are auto-linked, so
  // ordinary prose is untouched.
  const basePath = typeof widget.source === 'string' && widget.source.trim() !== '' ? widget.source : undefined
  return (
    <div className="flex flex-col gap-1.5">
      <div className="text-sm text-foreground">
        <MarkdownRenderer content={markdown} className="max-w-none" maxHeight="none" basePath={basePath} renderEmbeddedWidget={renderEmbeddedWidget} />
      </div>
    </div>
  )
}
