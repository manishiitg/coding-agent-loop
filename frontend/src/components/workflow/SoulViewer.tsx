import { useCallback, useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'

interface SoulViewerProps {
  workspacePath: string
}

// SoulViewer renders the workflow's north star (soul/soul.md — ## Objective +
// ## Success Criteria) as a first-class right-panel view. soul.md stays markdown
// (it's parsed by the framework-health signal and run-time objective injection),
// so we render it with the theme-aware MarkdownRenderer rather than an iframe.
export function SoulViewer({ workspacePath }: SoulViewerProps) {
  const [content, setContent] = useState('')
  const [exists, setExists] = useState<boolean | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const res = await agentApi.getBuilderDoc(workspacePath, 'soul')
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

  if (loading && !content) {
    return (
      <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Loading soul…
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
          No soul yet — the workflow's north star. Run <code className="rounded bg-muted px-1">/define-success</code> to
          write <code className="rounded bg-muted px-1">## Objective</code> and <code className="rounded bg-muted px-1">## Success Criteria</code>.
        </div>
      </div>
    )
  }

  return (
    <div className="h-full overflow-y-auto px-6 py-5">
      <div className="mx-auto max-w-3xl">
        <MarkdownRenderer content={content} disablePathLinking />
      </div>
    </div>
  )
}

export default SoulViewer
