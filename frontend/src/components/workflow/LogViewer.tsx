import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import { HtmlRenderer } from '../ui/HtmlRenderer'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { useTheme } from '../../hooks/useTheme'

// Fired by the preview-pane controls' refresh button when the Log view is active.
export const WORKFLOW_LOG_REFRESH_EVENT = 'workflow-log-refresh'

// The log renders in an isolated iframe, so it can't inherit the app's theme.
// We push the app's light/dark choice into the log content: a full <!doctype>
// document gets a data-theme attribute on <html> (the skeleton's CSS keys its
// dark palette on it); a legacy bare fragment gets wrapped in a minimal themed
// shell so it's at least readable in dark mode.
function applyThemeToLog(content: string, isDark: boolean): string {
  const themeAttr = isDark ? 'dark' : 'light'
  const trimmed = content.trimStart()
  if (/^<(!doctype|html)/i.test(trimmed)) {
    if (/<html[\s>]/i.test(content)) {
      return content.replace(/<html\b([^>]*)>/i, (_m, attrs) => `<html data-theme="${themeAttr}"${attrs}>`)
    }
    return content
  }
  // Legacy fragment — wrap with a theme-aware base shell.
  return `<!doctype html><html data-theme="${themeAttr}"><head><meta charset="utf-8"><style>
    :root{color-scheme:light;--bg:#f6f6f4;--fg:#1a1a18;--muted:#5b5b54;--line:#e7e6e1;--link:#3a5a40}
    html[data-theme="dark"]{color-scheme:dark;--bg:#141413;--fg:#e8e7e2;--muted:#a3a299;--line:#2c2b28;--link:#7fb086}
    html,body{margin:0;background:var(--bg);color:var(--fg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;line-height:1.55}
    body{padding:24px 28px;max-width:880px}
    h1,h2,h3,h4{line-height:1.25} h3{margin-top:1.6em} a{color:var(--link)}
    code{background:rgba(127,127,127,.16);padding:1px 5px;border-radius:4px;font-size:.92em}
    hr{border:none;border-top:1px solid var(--line);margin:1.4em 0}
    [class*="entry"]{border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin:10px 0}
  </style></head><body>${content}</body></html>`
}

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

  // The log is HTML by convention — a full <!doctype> document or (legacy) bare
  // fragments like <div class="improvement-entry">. Either renders in the iframe;
  // anything starting with a tag is HTML. (soul.md etc. are handled elsewhere.)
  const isHtml = content.trimStart().startsWith('<')

  const { theme } = useTheme()
  const isDark = useMemo(() => {
    if (typeof document === 'undefined') return false
    const c = document.documentElement.classList
    return c.contains('dark') || c.contains('dark-plus')
  }, [theme])
  const themedContent = useMemo(() => applyThemeToLog(content, isDark), [content, isDark])

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
          No Pulse log yet. Run <code className="rounded bg-muted px-1">/auto-improve</code> to set
          up recurring runs + self-improvement — its scheduled runs and improvement passes record
          entries here, and it seeds the success criteria along the way. (The post-run monitor also
          records here after any scheduled run;{' '}
          <code className="rounded bg-muted px-1">/define-success</code> just seeds the criteria on
          its own.)
        </div>
      </div>
    )
  }

  // Frame the log like a document on a page rather than a raw edge-to-edge iframe:
  // a subtle backdrop with the content centered and width-capped, so a wide panel
  // reads as a framed page (the way the report does) instead of a stretched column.
  return (
    <div className="h-full w-full overflow-hidden bg-muted/30 dark:bg-black/20">
      <div className="mx-auto h-full w-full max-w-[1040px]">
        {isHtml ? (
          <HtmlRenderer content={themedContent} />
        ) : (
          <div className="h-full overflow-y-auto p-4">
            <MarkdownRenderer content={content} disablePathLinking />
          </div>
        )}
      </div>
    </div>
  )
}

export default LogViewer
