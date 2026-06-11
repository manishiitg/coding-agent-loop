import React, { useCallback, useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import { HtmlRenderer } from '../ui/HtmlRenderer'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

// Fired by the preview-pane controls' refresh button when the Log view is active.
export const WORKFLOW_LOG_REFRESH_EVENT = 'workflow-log-refresh'

interface LogViewerProps {
  workspacePath: string
}

// LogViewer renders the workflow's single agent-curated log (builder/improve.html)
// as a first-class right-panel view, alongside Plan and Report. The log HTML is
// authored by the improve/review/monitor agents and renders in the same kind of
// sandboxed iframe the report uses, so it gets full CSS/theme fidelity.
export function LogViewer({ workspacePath }: LogViewerProps) {
  const [content, setContent] = useState<string>('')
  const [exists, setExists] = useState<boolean | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, 'improve')
      setExists(!!res.exists)
      setContent(res.content || '')
      if (!res.success && res.error) setError(res.error)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => { void load() }, [load])

  // Refresh when the workflow finishes a run (the log just gained entries) and
  // when the user hits the pane's refresh control.
  useEffect(() => {
    const onRefresh = () => { void load() }
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
    return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
  }, [load])

  const head = content.trimStart().toLowerCase()
  const isHtml = head.startsWith('<!doctype') || head.startsWith('<html')

  if (loading && !content) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading log…
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center p-6">
        <div className="max-w-md rounded-md border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-800 dark:bg-red-900/20 dark:text-red-300">
          {error}
        </div>
      </div>
    )
  }

  if (exists === false || !content.trim()) {
    return (
      <div className="flex h-full items-center justify-center p-6 text-center">
        <div className="max-w-md text-sm text-muted-foreground">
          No workflow log yet. Run <code className="rounded bg-muted px-1">/define-success</code> to bootstrap it, then
          improvements, reviews, and the post-run monitor will record entries here.
        </div>
      </div>
    )
  }

  return (
    <div className="h-full w-full">
      {isHtml ? (
        <HtmlRenderer content={content} />
      ) : (
        <div className="h-full overflow-y-auto p-4">
          <MarkdownRenderer content={content} disablePathLinking />
        </div>
      )}
    </div>
  )
}

export default LogViewer
