import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, Brain, Laptop, PanelRightClose, RefreshCw, Smartphone, TabletSmartphone, Target } from 'lucide-react'
import { agentApi } from '../../services/api'
import { useAuthStore } from '../../stores/useAuthStore'
import { useTheme } from '../../hooks/useTheme'
import { HtmlRenderer } from '../ui/HtmlRenderer'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import {
  ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT,
  getOrgHtmlPreviewDevice,
  orgHtmlPreviewShellClass,
  setOrgHtmlPreviewDevice,
  type OrgHtmlPreviewDevice,
} from './orgHtmlPreview'

const ORG_PULSE_LOG_PATH = 'pulse/org-pulse.html'
const ORG_GOALS_PATH = 'pulse/goals.html'

const ORG_HTML_PREVIEW_DEVICE_OPTS = [
  { mode: 'mobile' as const, Icon: Smartphone, label: 'Mobile preview' },
  { mode: 'tablet' as const, Icon: TabletSmartphone, label: 'Tablet preview' },
  { mode: 'desktop' as const, Icon: Laptop, label: 'Laptop preview' },
]

function applyThemeToOrgHtml(content: string, isDark: boolean): string {
  const themeAttr = isDark ? 'dark' : 'light'
  const trimmed = content.trimStart()

  if (/^<(!doctype|html)/i.test(trimmed)) {
    if (!/<html[\s>]/i.test(content)) return content
    return content.replace(/<html\b([^>]*)>/i, (_m, attrs: string) => {
      let next = attrs
      if (/\sdata-theme=(["']).*?\1/i.test(next)) {
        next = next.replace(/\sdata-theme=(["']).*?\1/i, ` data-theme="${themeAttr}"`)
      } else {
        next = ` data-theme="${themeAttr}"${next}`
      }

      const classMatch = next.match(/\sclass=(["'])(.*?)\1/i)
      if (classMatch) {
        const classes = classMatch[2]
          .split(/\s+/)
          .filter(cls => cls && cls !== 'dark' && cls !== 'dark-plus')
        if (isDark) classes.push('dark')
        const classAttr = classes.length > 0 ? ` class="${classes.join(' ')}"` : ''
        next = next.replace(/\sclass=(["']).*?\1/i, classAttr)
      } else if (isDark) {
        next = ` class="dark"${next}`
      }

      return `<html${next}>`
    })
  }

  return `<!doctype html><html data-theme="${themeAttr}"${isDark ? ' class="dark"' : ''}><head><meta charset="utf-8"><style>
    :root{color-scheme:light;--bg:#f7f7f5;--fg:#191917;--muted:#686760;--line:#e6e3dc;--card:#fff}
    html[data-theme="dark"]{color-scheme:dark;--bg:#0f0f12;--fg:#f1f0f4;--muted:#a3a2aa;--line:#2b2b33;--card:#17171c}
    html,body{margin:0;background:var(--bg);color:var(--fg);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif;line-height:1.55}
    body{padding:24px;max-width:920px}
    a{color:inherit} code{background:rgba(127,127,127,.16);padding:1px 5px;border-radius:4px}
    table{width:100%;border-collapse:collapse} th,td{border-bottom:1px solid var(--line);padding:8px;text-align:left}
  </style></head><body>${content}</body></html>`
}

interface OrgHtmlPanelProps {
  title: string
  path: string
  loadingText: string
  emptyText: string
  Icon: React.ComponentType<{ className?: string }>
  toolbarLeading?: React.ReactNode
  onClosePanel?: () => void
  // When set, the panel renders at this device width and hides the device toggle
  // (used when embedded in a narrow column, e.g. the org page goals column).
  fixedDevice?: OrgHtmlPreviewDevice
}

const toolbarIconBtnClass = 'inline-flex h-7 w-7 flex-none items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50'

const OrgHtmlPanel: React.FC<OrgHtmlPanelProps> = ({ title, path, loadingText, emptyText, Icon, toolbarLeading, onClosePanel, fixedDevice }) => {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [device, setDevice] = useState<OrgHtmlPreviewDevice>(() => fixedDevice ?? getOrgHtmlPreviewDevice())
  const { theme } = useTheme()
  const isDark = useMemo(() => {
    if (typeof document === 'undefined') return false
    const classes = document.documentElement.classList
    return theme === 'dark' || classes.contains('dark') || classes.contains('dark-plus')
  }, [theme])
  const themedContent = useMemo(() => applyThemeToOrgHtml(content, isDark), [content, isDark])

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const response = await agentApi.getPlannerFileContent(path)
      const rawContent = response.success && response.data ? response.data.content ?? '' : ''
      if (!response.success || !rawContent) {
        setContent('')
        setError(response.message || emptyText)
        return
      }
      setContent(typeof rawContent === 'string' ? rawContent : String(rawContent))
    } catch {
      setContent('')
      setError(emptyText)
    } finally {
      setLoading(false)
    }
  }, [emptyText, path])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    if (fixedDevice) return // locked to fixedDevice — ignore the global preview preference
    const handler = (event: Event) => {
      const preference = (event as CustomEvent).detail?.preference
      if (preference === 'mobile' || preference === 'tablet' || preference === 'desktop') {
        setDevice(preference)
      }
    }
    window.addEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
    return () => window.removeEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
  }, [fixedDevice])

  const refresh = useCallback(() => {
    void load()
  }, [load])

  const selectDevice = useCallback((next: OrgHtmlPreviewDevice) => {
    setDevice(next)
    setOrgHtmlPreviewDevice(next)
  }, [])

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex flex-wrap items-center justify-between gap-1 border-b border-border bg-muted/40 px-2 py-2">
        <div className="min-w-0 flex-none">
          {toolbarLeading || (
            <div className="flex min-w-0 items-center gap-2 px-1">
              <Icon className="h-4 w-4 flex-none text-primary" />
              <div className="min-w-0">
                <h2 className="truncate text-sm font-semibold text-foreground">{title}</h2>
                <p className="truncate text-xs text-muted-foreground">{path}</p>
              </div>
            </div>
          )}
        </div>
        <div className="flex flex-none items-center gap-1">
          {!fixedDevice && (
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/70 p-0.5 shadow-sm">
            {ORG_HTML_PREVIEW_DEVICE_OPTS.map(({ mode, Icon: DeviceIcon, label }) => (
              <button
                key={mode}
                type="button"
                onClick={() => selectDevice(mode)}
                title={label}
                aria-label={label}
                className={`inline-flex h-6 w-6 items-center justify-center rounded transition-colors ${
                  device === mode ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <DeviceIcon className="h-3.5 w-3.5" />
              </button>
            ))}
          </div>
          )}
          <button type="button" onClick={refresh} disabled={loading} title="Refresh" aria-label="Refresh" className={toolbarIconBtnClass}>
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          {onClosePanel && (
            <button type="button" onClick={onClosePanel} title="Hide panel" aria-label="Hide panel" className={toolbarIconBtnClass}>
              <PanelRightClose className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden bg-muted/20">
        {loading && !content ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{loadingText}</div>
        ) : content ? (
          <div className={orgHtmlPreviewShellClass(device)}>
            <HtmlRenderer content={themedContent} />
          </div>
        ) : (
          <div className="flex h-full items-center justify-center p-4 text-center">
            <div className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
              {error || emptyText}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

export const OrgGoalsPanel: React.FC<{ toolbarLeading?: React.ReactNode; onClosePanel?: () => void; fixedDevice?: OrgHtmlPreviewDevice }> = ({ toolbarLeading, onClosePanel, fixedDevice }) => (
  <OrgHtmlPanel
    title="Org Goals"
    path={ORG_GOALS_PATH}
    loadingText="Loading Org Goals..."
    emptyText="No org goals yet. Ask the Chief of Staff to set org goals."
    Icon={Target}
    toolbarLeading={toolbarLeading}
    onClosePanel={onClosePanel}
    fixedDevice={fixedDevice}
  />
)

export const OrgPulsePanel: React.FC<{ toolbarLeading?: React.ReactNode; onClosePanel?: () => void; fixedDevice?: OrgHtmlPreviewDevice }> = ({ toolbarLeading, onClosePanel, fixedDevice }) => (
  <OrgHtmlPanel
    title="Org Pulse"
    path={ORG_PULSE_LOG_PATH}
    loadingText="Loading Org Pulse..."
    emptyText="No Org Pulse log yet. Use /pulse-setup in Chief of Staff to configure Daily Org Pulse."
    Icon={Activity}
    toolbarLeading={toolbarLeading}
    onClosePanel={onClosePanel}
    fixedDevice={fixedDevice}
  />
)

export const MemoryPanel: React.FC<{ toolbarLeading?: React.ReactNode; onClosePanel?: () => void }> = ({ toolbarLeading, onClosePanel }) => {
  const authUser = useAuthStore(state => state.user)
  const userId = authUser?.id || 'default'
  const primaryPath = `_users/${userId}/memories/index.md`
  const legacyPath = 'memories/index.md'
  const [content, setContent] = useState('')
  const [path, setPath] = useState(primaryPath)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const primary = await agentApi.getPlannerFileContent(primaryPath)
      const primaryContent = primary.success && primary.data ? primary.data.content ?? '' : ''
      if (primaryContent) {
        setContent(typeof primaryContent === 'string' ? primaryContent : String(primaryContent))
        setPath(primaryPath)
        return
      }

      const legacy = await agentApi.getPlannerFileContent(legacyPath)
      const legacyContent = legacy.success && legacy.data ? legacy.data.content ?? '' : ''
      if (legacyContent) {
        setContent(typeof legacyContent === 'string' ? legacyContent : String(legacyContent))
        setPath(legacyPath)
        return
      }

      setContent('')
      setPath(primaryPath)
      setError(primary.message || legacy.message || 'No memory index yet.')
    } catch {
      setContent('')
      setPath(primaryPath)
      setError('No memory index yet.')
    } finally {
      setLoading(false)
    }
  }, [primaryPath])

  useEffect(() => { void load() }, [load])

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex flex-wrap items-center justify-between gap-1 border-b border-border bg-muted/40 px-2 py-2">
        <div className="min-w-0 flex-none">
          {toolbarLeading || (
            <div className="flex min-w-0 items-center gap-2 px-1">
              <Brain className="h-4 w-4 flex-none text-primary" />
              <div className="min-w-0">
                <h2 className="truncate text-sm font-semibold text-foreground">Memory</h2>
                <p className="truncate text-xs text-muted-foreground">{path}</p>
              </div>
            </div>
          )}
        </div>
        <div className="flex flex-none items-center gap-1">
          <button type="button" onClick={() => { void load() }} disabled={loading} title="Refresh memory" aria-label="Refresh memory" className={toolbarIconBtnClass}>
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
          {onClosePanel && (
            <button type="button" onClick={onClosePanel} title="Hide panel" aria-label="Hide panel" className={toolbarIconBtnClass}>
              <PanelRightClose className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {loading && !content ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading Memory...</div>
        ) : content ? (
          <MarkdownRenderer
            content={content}
            basePath={path.includes('/') ? path.split('/').slice(0, -1).join('/') : undefined}
            className="text-sm [&_h1]:text-base [&_h2]:text-sm [&_h3]:text-sm [&_p]:leading-6 [&_li]:leading-6"
          />
        ) : (
          <div className="flex h-full items-center justify-center p-4 text-center">
            <div className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
              {error || 'No memory index yet. Ask the Chief of Staff to remember something or run memory enrichment.'}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
