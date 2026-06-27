import React, { useCallback, useEffect, useMemo, useState } from 'react'
import { Activity, Brain, Cloud, Globe, Laptop, Smartphone, TabletSmartphone, X, ExternalLink, RefreshCw, Target } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import { agentApi } from '../services/api'
import type { ScheduledJob } from '../services/api-types'
import { useAppStore } from '../stores/useAppStore'
import { HtmlRenderer } from './ui/HtmlRenderer'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import ModalPortal from './ui/ModalPortal'
import { useAuthStore } from '../stores/useAuthStore'
import { useTheme } from '../hooks/useTheme'
import { formatBackupStateLabel, getBackupDotClass } from './workflow/backupStatus'
import { formatPublishStateLabel, getPublishDotClass } from './workflow/publishStatus'

// The Org Pulse daily pass is a built-in multi-agent schedule; the toggle just
// enables/disables it (same as the Scheduled Tasks popup), and "Open log" shows
// its single HTML output on the right. Mirrors the workflow Pulse toggle.
const ORG_PULSE_JOB_ID = 'builtin-org-pulse'
const ORG_PULSE_LOG_PATH = 'pulse/org-pulse.html'
const ORG_GOALS_PATH = 'pulse/goals.html'
const ORG_BACKUP_COMMAND_MESSAGE = `Help me set up or run org-level backup.

Call get_reference_doc(kind="backup-strategy") and follow its org-level workflow-style contract. Read pulse/backup.json and pulse/backup/status.json if they exist.

Scope:
- pulse/goals.html
- pulse/org-pulse.html
- Chief of Staff memory files
- employee/org config files
- multi-agent schedules/config

If org backup is NOT configured yet: set up the zero-config local-git default, write pulse/backup.json, write pulse/backup/status.json with state "configured_not_verified" or "healthy" if you can complete the local backup now, and ask me before adding any remote destination or credentials.

If org backup IS configured: run a backup now, skip only if pulse/backup/status.json proves the current source hash is unchanged, and report the result.

Always write pulse/backup/status.json. Never write org backup state into any workflow.json or content HTML file, and never back up secrets.`
const ORG_PUBLISH_COMMAND_MESSAGE = `Help me set up or run org-level publish.

Call get_reference_doc(kind="publish-strategy") and follow its org-level workflow-style contract. Read pulse/publish.json and pulse/publish/status.json if they exist.

Publish scope:
- pulse/goals.html as goals.html
- pulse/org-pulse.html as pulse.html
- an index.html wrapper with Goals | Pulse navigation

If org publish is NOT configured: ask me which static host to use, default to private visibility with a PUBLISH_PASSWORD secret, write pulse/publish.json, and write pulse/publish/status.json with state "configured_not_verified". Do not do the first/verifying publish until I confirm the destination and visibility.

If org publish IS configured and verified: publish now only if the org HTML changed since the last publish. Stage files outside the workspace, force dark mode, deploy, then come back and update pulse/publish/status.json with state "published", the url, and last_source_hash.

Always write pulse/publish/status.json. Never publish secrets or raw memory files. Never write org publish state into any workflow.json or content HTML file.`
export const ORG_HTML_PREVIEW_PREFERENCE_KEY = 'org_html_preview_device'
export const ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT = 'org-html-preview-device-changed'
export type OrgHtmlPreviewDevice = 'mobile' | 'tablet' | 'desktop'

const ORG_HTML_PREVIEW_DEVICE_OPTS = [
  { mode: 'mobile' as const, Icon: Smartphone, label: 'Mobile preview' },
  { mode: 'tablet' as const, Icon: TabletSmartphone, label: 'Tablet preview' },
  { mode: 'desktop' as const, Icon: Laptop, label: 'Laptop preview' },
]

export function getOrgHtmlPreviewDevice(): OrgHtmlPreviewDevice {
  try {
    const saved = localStorage.getItem(ORG_HTML_PREVIEW_PREFERENCE_KEY)
    return saved === 'mobile' || saved === 'tablet' || saved === 'desktop' ? saved : 'mobile'
  } catch {
    return 'mobile'
  }
}

function setOrgHtmlPreviewDevice(device: OrgHtmlPreviewDevice) {
  try { localStorage.setItem(ORG_HTML_PREVIEW_PREFERENCE_KEY, device) } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, { detail: { preference: device } }))
}

function orgHtmlPreviewShellClass(device: OrgHtmlPreviewDevice): string {
  return device === 'mobile'
    ? 'mx-auto h-full w-full max-w-[480px]'
    : device === 'tablet'
      ? 'mx-auto h-full w-full max-w-[880px]'
      : 'h-full w-full'
}

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

const relTime = (iso?: string): string => {
  if (!iso) return 'never'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return 'unknown'
  const diff = Date.now() - d.getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return d.toLocaleDateString()
}

export const OrgPulseControl: React.FC = () => {
  const [job, setJob] = useState<ScheduledJob | null>(null)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [backupState, setBackupState] = useState('not_configured')
  const [publishState, setPublishState] = useState('not_configured')
  const setWorkspaceMinimized = useAppStore(s => s.setWorkspaceMinimized)
  const setMultiAgentRightPanelView = useAppStore(s => s.setMultiAgentRightPanelView)

  const load = useCallback(async () => {
    try {
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJob((resp.jobs || []).find(j => j.id === ORG_PULSE_JOB_ID) || null)
    } catch { /* leave last known state */ }
  }, [])

  useEffect(() => { void load() }, [load])

  const loadOrgOps = useCallback(async () => {
    const [backup, publish] = await Promise.allSettled([
      agentApi.getOrgBackup(),
      agentApi.getOrgPublish()
    ])
    if (backup.status === 'fulfilled') {
      setBackupState(backup.value.effective_state || 'not_configured')
    }
    if (publish.status === 'fulfilled') {
      setPublishState(publish.value.effective_state || 'not_configured')
    }
  }, [])

  useEffect(() => { void loadOrgOps() }, [loadOrgOps])
  useEffect(() => {
    if (open) void loadOrgOps()
  }, [open, loadOrgOps])

  const enabled = !!job?.enabled
  const hasRun = !!job?.last_run_at

  const toggle = useCallback(async () => {
    if (saving) return
    setSaving(true)
    try {
      const updated = enabled
        ? await schedulerApi.disableJob(ORG_PULSE_JOB_ID)
        : await schedulerApi.enableJob(ORG_PULSE_JOB_ID)
      setJob(updated)
    } catch {
      await load()
    } finally {
      setSaving(false)
    }
  }, [enabled, saving, load])

  const openLog = useCallback(() => {
    setMultiAgentRightPanelView('org-pulse')
    setWorkspaceMinimized(false)
    setOpen(false)
  }, [setMultiAgentRightPanelView, setWorkspaceMinimized])

  return (
    <>
      <button
        onClick={() => setOpen(true)}
        data-testid="org-pulse-button"
        className="flex items-center gap-1 px-2 py-1 text-xs rounded transition-colors text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700"
        title="Org Pulse — the Chief of Staff's daily org log"
      >
        <Activity className={`w-4 h-4 ${enabled ? 'text-primary' : ''}`} />
        <span className="hidden sm:inline">Org Pulse</span>
        <span className={`text-[10px] font-semibold tracking-wide ${enabled ? 'text-primary' : 'text-gray-400 dark:text-gray-500'}`}>
          {enabled ? 'ON' : 'OFF'}
        </span>
      </button>

      {open && (
        <ModalPortal>
          <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-4" onClick={() => setOpen(false)}>
            <div className="w-full max-w-md rounded-lg border border-border bg-background shadow-xl" onClick={e => e.stopPropagation()}>
              <div className="flex items-center justify-between border-b border-border px-5 py-3.5">
                <div className="flex items-center gap-2">
                  <Activity className="h-4 w-4 text-primary" />
                  <h2 className="text-sm font-semibold text-foreground">Org Pulse</h2>
                </div>
                <button onClick={() => setOpen(false)} className="rounded-md p-1 text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Close">
                  <X className="h-4 w-4" />
                </button>
              </div>

              <div className="space-y-3 px-5 py-4 text-sm text-muted-foreground">
                <p>When <span className="font-medium text-foreground">on</span>, your Chief of Staff reviews the whole org <span className="font-medium text-foreground">once a day</span> — rolls up each workflow's health, harvests reports &amp; learnings into memory, and records suggestions in a single log you open on the right.</p>
                <p>It only <span className="font-medium text-foreground">reviews and suggests</span> — it never changes a workflow. Acting on a suggestion (e.g. turning a repeated task into a workflow) is up to you.</p>
                <p>Use <code className="rounded bg-muted px-1 py-0.5 font-medium text-foreground">/org-backup</code> and <code className="rounded bg-muted px-1 py-0.5 font-medium text-foreground">/org-publish</code> to protect or share the org Goals/Pulse pages. They use the same config/status pattern as workflow backup and publish.</p>
                <div className="grid grid-cols-2 gap-2 pt-1">
                  <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
                    <div className="flex items-center gap-2 text-xs font-medium text-foreground">
                      <span className="relative inline-flex">
                        <Cloud className="h-3.5 w-3.5 text-muted-foreground" />
                        <span className={`absolute -right-1 -top-1 h-2 w-2 rounded-full border border-background ${getBackupDotClass(backupState)}`} />
                      </span>
                      Backup
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">{formatBackupStateLabel(backupState)}</div>
                    <code className="mt-1 inline-block rounded bg-background px-1 py-0.5 text-[10px] text-foreground">/org-backup</code>
                  </div>
                  <div className="rounded-md border border-border bg-muted/30 px-3 py-2">
                    <div className="flex items-center gap-2 text-xs font-medium text-foreground">
                      <span className="relative inline-flex">
                        <Globe className="h-3.5 w-3.5 text-muted-foreground" />
                        <span className={`absolute -right-1 -top-1 h-2 w-2 rounded-full border border-background ${getPublishDotClass(publishState)}`} />
                      </span>
                      Publish
                    </div>
                    <div className="mt-1 text-[11px] text-muted-foreground">{formatPublishStateLabel(publishState)}</div>
                    <code className="mt-1 inline-block rounded bg-background px-1 py-0.5 text-[10px] text-foreground">/org-publish</code>
                  </div>
                </div>
              </div>

              {/* enable / disable */}
              <div className="flex items-center justify-between border-t border-border px-5 py-3.5">
                <div>
                  <div className="text-sm font-medium text-foreground">Daily Org Pulse</div>
                  <div className="text-xs text-muted-foreground">
                    {enabled
                      ? `On · ${job?.next_run_at ? `next ${relTime(job.next_run_at)}` : 'scheduled daily'} · last run ${relTime(job?.last_run_at)}`
                      : 'Off — not reviewing the org'}
                  </div>
                </div>
                <button
                  type="button"
                  role="switch"
                  aria-checked={enabled}
                  onClick={() => { void toggle() }}
                  disabled={saving}
                  className={`relative inline-flex h-5 w-9 flex-none items-center rounded-full p-0 transition-colors disabled:opacity-50 ${enabled ? 'bg-primary' : 'bg-muted-foreground/30'}`}
                  aria-label="Toggle Org Pulse"
                >
                  <span className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${enabled ? 'translate-x-[18px]' : 'translate-x-[2px]'}`} />
                </button>
              </div>

              {/* view log / empty state */}
              <div className="border-t border-border px-5 py-4">
                {hasRun ? (
                  <button
                    onClick={openLog}
                    className="inline-flex w-full items-center justify-center gap-1.5 rounded-md border border-border px-3 py-1.5 text-xs font-medium text-foreground hover:bg-muted"
                  >
                    <ExternalLink className="h-3.5 w-3.5" />
                    Open today's log
                  </button>
                ) : (
                  <div className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
                    No Org Pulse log yet. Use <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/pulse-setup</code> in Chief of Staff to configure Daily Org Pulse.
                  </div>
                )}
              </div>
            </div>
          </div>
        </ModalPortal>
      )}
    </>
  )
}

interface OrgHtmlPanelProps {
  title: string
  path: string
  loadingText: string
  emptyText: string
  Icon: React.ComponentType<{ className?: string }>
  onSubmitCommand?: (query: string) => void
}

const OrgHtmlPanel: React.FC<OrgHtmlPanelProps> = ({ title, path, loadingText, emptyText, Icon, onSubmitCommand }) => {
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [device, setDevice] = useState<OrgHtmlPreviewDevice>(() => getOrgHtmlPreviewDevice())
  const [backupState, setBackupState] = useState('not_configured')
  const [publishState, setPublishState] = useState('not_configured')
  const [publishUrl, setPublishUrl] = useState('')
  const { theme } = useTheme()
  const isDark = useMemo(() => {
    if (typeof document === 'undefined') return false
    const classes = document.documentElement.classList
    return classes.contains('dark') || classes.contains('dark-plus')
  }, [theme])
  const themedContent = useMemo(() => applyThemeToOrgHtml(content, isDark), [content, isDark])

  const loadOrgOps = useCallback(async () => {
    const [backup, publish] = await Promise.allSettled([
      agentApi.getOrgBackup(),
      agentApi.getOrgPublish()
    ])
    if (backup.status === 'fulfilled') {
      setBackupState(backup.value.effective_state || 'not_configured')
    }
    if (publish.status === 'fulfilled') {
      setPublishState(publish.value.effective_state || 'not_configured')
      setPublishUrl(publish.value.url || publish.value.status?.url || '')
    }
  }, [])

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
  useEffect(() => { void loadOrgOps() }, [loadOrgOps])
  useEffect(() => {
    const handler = (event: Event) => {
      const preference = (event as CustomEvent).detail?.preference
      if (preference === 'mobile' || preference === 'tablet' || preference === 'desktop') {
        setDevice(preference)
      }
    }
    window.addEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
    return () => window.removeEventListener(ORG_HTML_PREVIEW_PREFERENCE_CHANGED_EVENT, handler)
  }, [])

  const refresh = useCallback(() => {
    void load()
    void loadOrgOps()
  }, [load, loadOrgOps])

  const selectDevice = useCallback((next: OrgHtmlPreviewDevice) => {
    setDevice(next)
    setOrgHtmlPreviewDevice(next)
  }, [])

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Icon className="h-4 w-4 flex-none text-primary" />
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-foreground">{title}</h2>
            <p className="truncate text-xs text-muted-foreground">{path}</p>
          </div>
        </div>
        <div className="flex flex-none items-center gap-1">
          <button
            type="button"
            onClick={() => onSubmitCommand?.(ORG_BACKUP_COMMAND_MESSAGE)}
            title={`Backup - ${formatBackupStateLabel(backupState)}`}
            aria-label="Org backup"
            className="relative inline-flex h-7 w-7 items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
          >
            <Cloud className="h-3.5 w-3.5" />
            <span className={`absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border border-background ${getBackupDotClass(backupState)}`} />
          </button>
          <button
            type="button"
            onClick={() => onSubmitCommand?.(ORG_PUBLISH_COMMAND_MESSAGE)}
            title={`Publish - ${formatPublishStateLabel(publishState)}`}
            aria-label="Org publish"
            className="relative inline-flex h-7 w-7 items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
          >
            <Globe className="h-3.5 w-3.5" />
            <span className={`absolute -right-0.5 -top-0.5 h-2 w-2 rounded-full border border-background ${getPublishDotClass(publishState)}`} />
          </button>
          {publishUrl && (
            <a
              href={publishUrl}
              target="_blank"
              rel="noopener noreferrer"
              title="Open published org page"
              aria-label="Open published org page"
              className="inline-flex h-7 w-7 items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground"
            >
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          )}
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
          <button
            type="button"
            onClick={refresh}
            disabled={loading}
            title="Refresh"
            aria-label="Refresh"
            className="inline-flex h-7 w-7 items-center justify-center rounded-lg border border-border bg-background/90 text-muted-foreground shadow-sm transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
          >
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-hidden bg-muted/20">
        {loading && !content ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            {loadingText}
          </div>
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

export const OrgGoalsPanel: React.FC<{ onSubmitCommand?: (query: string) => void }> = ({ onSubmitCommand }) => (
  <OrgHtmlPanel
    title="Org Goals"
    path={ORG_GOALS_PATH}
    loadingText="Loading Org Goals..."
    emptyText="No org goals yet. Ask the Chief of Staff to set org goals."
    Icon={Target}
    onSubmitCommand={onSubmitCommand}
  />
)

export const MemoryPanel: React.FC = () => {
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
      <div className="flex items-center justify-between gap-2 border-b border-border px-3 py-2">
        <div className="flex min-w-0 items-center gap-2">
          <Brain className="h-4 w-4 flex-none text-primary" />
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold text-foreground">Memory</h2>
            <p className="truncate text-xs text-muted-foreground">{path}</p>
          </div>
        </div>
        <button
          type="button"
          onClick={() => { void load() }}
          disabled={loading}
          title="Refresh memory"
          aria-label="Refresh memory"
          className="inline-flex h-7 w-7 flex-none items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
        >
          <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
        </button>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto p-3">
        {loading && !content ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            Loading Memory...
          </div>
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

export const OrgPulsePanel: React.FC<{ onSubmitCommand?: (query: string) => void }> = ({ onSubmitCommand }) => (
  <OrgHtmlPanel
    title="Org Pulse"
    path={ORG_PULSE_LOG_PATH}
    loadingText="Loading Org Pulse..."
    emptyText="No Org Pulse log yet. Use /pulse-setup in Chief of Staff to configure Daily Org Pulse."
    Icon={Activity}
    onSubmitCommand={onSubmitCommand}
  />
)

export default OrgPulseControl
