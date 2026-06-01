import { useEffect } from 'react'
import { X } from 'lucide-react'
import { useReportFilePreviewStore } from '../../../stores/useReportFilePreviewStore'
import { FilePreviewByPath } from './FileWidget'

// FilePreviewModal renders the file selected via useReportFilePreviewStore in a
// fixed-position overlay scoped to the report. Mounted once inside ReportView so
// report file links (file-list rows, table/cards links) can preview PDFs, images,
// markdown, etc. inline without depending on the chat workspace's file viewer.
export function FilePreviewModal() {
  const path = useReportFilePreviewStore((s) => s.path)
  const name = useReportFilePreviewStore((s) => s.name)
  const close = useReportFilePreviewStore((s) => s.close)

  useEffect(() => {
    if (!path) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [path, close])

  if (!path) return null

  return (
    <div
      className="fixed inset-0 z-[60] flex flex-col bg-black/60 backdrop-blur-sm"
      role="dialog"
      aria-modal="true"
      onClick={close}
    >
      <div
        className="m-auto flex h-[90vh] w-[min(1100px,94vw)] flex-col overflow-hidden rounded-xl border border-border bg-background shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex flex-shrink-0 items-center justify-between gap-3 border-b border-border bg-muted/30 px-4 py-2.5">
          <div className="min-w-0">
            <div className="truncate text-sm font-medium text-foreground">{name || 'Preview'}</div>
            <div className="truncate text-[11px] text-muted-foreground">{path}</div>
          </div>
          <button
            type="button"
            onClick={close}
            title="Close (Esc)"
            aria-label="Close preview"
            className="inline-flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-lg border border-border bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-hidden">
          <FilePreviewByPath path={path} name={name || undefined} />
        </div>
      </div>
    </div>
  )
}
