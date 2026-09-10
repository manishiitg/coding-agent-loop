import { lazy, Suspense, useEffect, useId, useRef, useState } from 'react'
import { FileText, Loader2, X } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { PulseReviewReport } from '../../services/api-types'
import ModalPortal from '../ui/ModalPortal'
import { pulseReviewDate } from './pulseReviewCoverage'

const MarkdownRenderer = lazy(() => import('../ui/MarkdownRenderer').then(module => ({ default: module.MarkdownRenderer })))

export function PulseReviewReportReader({ report, title, onClose }: {
  report: PulseReviewReport; title: string; onClose: () => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const titleId = useId()
  const [content, setContent] = useState<string | null>(null)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const element = dialog.current
    element?.showModal()
    return () => element?.close()
  }, [])
  useEffect(() => {
    let current = true
    setError(''); setContent(null)
    if (report.source === 'review_note') {
      setContent(report.content || 'No additional review note recorded.')
      return () => { current = false }
    }
    void agentApi.getPlannerFileContent(report.path).then(result => {
      if (!current) return
      if (!result.success || result.data?.content == null) throw new Error('This report could not be read.')
      setContent(result.data.content)
    }).catch(err => { if (current) setError(err instanceof Error ? err.message : 'This report could not be read.') })
    return () => { current = false }
  }, [report.path, report.source, report.content, retry])

  return <ModalPortal>
    <dialog ref={dialog} aria-labelledby={titleId} aria-modal="true" data-workspace-collapse-ignore="true"
      onCancel={event => { event.preventDefault(); onClose() }}
      className="fixed inset-0 m-0 h-[100dvh] max-h-none w-screen max-w-none overflow-hidden border-0 bg-background p-0 text-foreground">
      <div className="flex h-full min-h-0 flex-col">
        <header className="flex shrink-0 items-center justify-between gap-4 border-b px-4 py-3 sm:px-8">
          <div className="min-w-0"><h2 id={titleId} className="flex items-center gap-2 text-base font-semibold"><FileText className="h-4 w-4 shrink-0" />{title}</h2>
            <p className="mt-1 text-xs text-muted-foreground">Updated {pulseReviewDate(report.updated_at)}{report.result && ` · ${report.result === 'incomplete' ? 'No completion recorded' : report.result}`}</p>
          </div>
          <button type="button" autoFocus onClick={onClose} aria-label="Close report" className="inline-flex shrink-0 items-center gap-2 rounded-lg border px-3 py-2 text-sm hover:bg-muted focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary"><X className="h-4 w-4" />Close</button>
        </header>
        <div className="min-h-0 flex-1 overflow-auto overscroll-contain px-4 py-6 sm:px-8 sm:py-8" aria-label="Report content">
          <div className="mx-auto w-full max-w-6xl">
            {error ? <div role="alert" className="rounded-lg border border-destructive/30 p-4 text-sm"><p>{error}</p><button type="button" className="mt-3 rounded-md border px-3 py-2 hover:bg-muted" onClick={() => setRetry(value => value + 1)}>Retry</button></div>
              : content === null ? <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Loader2 className="h-4 w-4 animate-spin" />Loading report…</p>
                : <Suspense fallback={<p role="status">Opening report…</p>}><MarkdownRenderer content={content} className="text-base leading-7 [&_p]:text-base [&_p]:leading-7 [&_li]:text-base [&_li]:leading-7" basePath={report.path.substring(0, report.path.lastIndexOf('/'))} /></Suspense>}
            {report.source !== 'review_note' && <details className="mt-8 border-t pt-3 text-xs text-muted-foreground"><summary className="cursor-pointer">Report file</summary><p className="mt-2 break-all">{report.path}</p></details>}
          </div>
        </div>
      </div>
    </dialog>
  </ModalPortal>
}
