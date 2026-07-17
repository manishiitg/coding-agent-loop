import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { agentApi } from '../../services/api'
import { useTheme } from '../../hooks/useTheme'
import { HtmlRenderer } from '../ui/HtmlRenderer'
import { applyThemeToLog } from './LogViewer'
import { buildPulseTimelineHtml } from './pulseTimelineHtml'
import { WORKFLOW_LOG_REFRESH_EVENT } from './workflowEvents'

type PulseTimelineWindow = Window & {
  __runloopRenderPulseModule?: (module: string) => void
}

export function PulseHtmlSectionViewer({
  workspacePath,
  module,
  label,
  className = '',
}: {
  workspacePath: string
  module: string
  label: string
  className?: string
}) {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const iframeRef = useRef<HTMLIFrameElement>(null)
  const { theme } = useTheme()

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const result = await agentApi.getBuilderDoc(workspacePath, 'improve')
      if (!result.success && result.error) throw new Error(result.error)
      setContent(result.exists ? result.content || '' : '')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load Pulse history.')
    } finally {
      setLoading(false)
    }
  }, [workspacePath])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const refresh = () => { void load() }
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, refresh)
    return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, refresh)
  }, [load])

  const isDark = theme === 'dark'
  const html = useMemo(() => buildPulseTimelineHtml(applyThemeToLog(content, isDark)), [content, isDark])
  const showModule = useCallback((frame: HTMLIFrameElement | null) => {
    const frameWindow = frame?.contentWindow as PulseTimelineWindow | null
    frameWindow?.__runloopRenderPulseModule?.(module)
  }, [module])

  useEffect(() => { showModule(iframeRef.current) }, [showModule])

  if (loading && !content) {
    return <div className={`flex min-h-28 items-center justify-center gap-2 text-xs text-muted-foreground ${className}`}><Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading section history…</div>
  }
  if (error) return <div className={`rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive ${className}`}>{error}</div>
  if (!content.trim()) return null

  return (
    <section className={`min-w-0 w-full ${className}`} aria-label={`${label} Pulse history`}>
      <div className="min-w-0 overflow-hidden bg-muted/20">
        <HtmlRenderer
          content={html}
          autoHeight
          initialHeight={180}
          iframeRef={iframeRef}
          onFrameLoad={showModule}
        />
      </div>
    </section>
  )
}

export default PulseHtmlSectionViewer
