import React, { useEffect, useState, useCallback } from 'react'
import {
  X,
  Loader2,
  AlertCircle,
  Globe,
  RefreshCw,
  Info,
  ExternalLink,
  Copy,
  Check
} from 'lucide-react'
import { agentApi } from '../../services/api'
import type {
  WorkflowPublishDestination,
  WorkflowPublishDestinationStatus,
  WorkflowPublishInfoResponse,
  WorkflowPublishStrategyInfo
} from '../../services/api-types'
import ModalPortal from '../ui/ModalPortal'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { formatPublishStateLabel, getPublishStateVisual } from './publishStatus'

interface WorkflowPublishPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  onStateLoaded?: (state: string) => void
}

const FALLBACK_SUPPORTED: WorkflowPublishStrategyInfo[] = [
  { id: 'netlify', label: 'Netlify', method: 'cli', description: 'netlify deploy --prod; default URL *.netlify.app.' },
  { id: 'vercel', label: 'Vercel', method: 'cli', description: 'vercel deploy --prod; default URL *.vercel.app.' },
  { id: 'cloudflare-pages', label: 'Cloudflare Pages', method: 'cli', description: 'wrangler pages deploy; default URL *.pages.dev.' },
  { id: 'github-pages', label: 'GitHub Pages', method: 'git', description: 'Push static files to the gh-pages branch.' },
  { id: 's3', label: 'S3 / object store', method: 'sync', description: 'aws s3 sync / rclone to a static bucket — the any-host catch-all.' }
]

const formatRelativeTime = (dateStr?: string): string => {
  if (!dateStr) return 'Never'
  const date = new Date(dateStr)
  if (Number.isNaN(date.getTime())) return 'Unknown'
  const diffMs = Date.now() - date.getTime()
  const diffMin = Math.floor(diffMs / 60000)
  if (diffMin < 1) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  const diffDay = Math.floor(diffHr / 24)
  if (diffDay < 30) return `${diffDay}d ago`
  return date.toLocaleDateString()
}

const extractErrorMessage = (err: unknown, fallback: string): string => {
  const maybe = err as { response?: { data?: { error?: string; message?: string } | string }; message?: string }
  const data = maybe.response?.data
  if (typeof data === 'string') return data
  return data?.message || data?.error || maybe.message || fallback
}

const destinationTitle = (d: WorkflowPublishDestination): string =>
  d.site || d.public_base_url || d.id || d.provider

const findDestStatus = (
  statuses: WorkflowPublishDestinationStatus[] | undefined,
  d: WorkflowPublishDestination
): WorkflowPublishDestinationStatus | undefined => statuses?.find(s => s.id === d.id)

const WorkflowPublishPopup: React.FC<WorkflowPublishPopupProps> = ({ isOpen, onClose, workspacePath, onStateLoaded }) => {
  const [loading, setLoading] = useState(false)
  const [info, setInfo] = useState<WorkflowPublishInfoResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const load = useCallback(async () => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const resp = await agentApi.getWorkflowPublish(workspacePath)
      setInfo(resp)
      onStateLoaded?.(resp.effective_state || 'not_configured')
    } catch (err) {
      setError(extractErrorMessage(err, 'Failed to load publish status'))
    } finally {
      setLoading(false)
    }
  }, [workspacePath, onStateLoaded])

  useEffect(() => {
    if (isOpen && workspacePath) {
      setNotice(null)
      load()
    }
  }, [isOpen, workspacePath, load])

  const copyUrl = async (url: string) => {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch { /* clipboard unavailable */ }
  }

  if (!isOpen) return null

  const state = info?.effective_state || 'not_configured'
  const visual = getPublishStateVisual(state)
  const StateIcon = visual.Icon
  const configEnabled = Boolean(info?.config?.enabled)
  const url = info?.url || info?.status?.url || ''
  const destinations = info?.config?.destinations || []
  const targets = info?.config?.targets || []
  const targetLabel = targets.length
    ? targets.map(t => (typeof t === 'string' ? t : (t.id || t.artifact || 'artifact'))).join(', ')
    : 'pulse, report'
  const supported = info?.supported?.length ? info.supported : FALLBACK_SUPPORTED

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4">
        <div className="relative flex max-h-[calc(100dvh-1rem)] w-full max-w-3xl flex-col rounded-lg border border-border bg-background shadow-xl sm:max-h-[86vh]">
          <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
            <div className="min-w-0">
              <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
                <Globe className="h-4.5 w-4.5 text-primary" />
                Publish
              </h2>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">
                Share this automation's Pulse log &amp; report dashboard at a public URL
              </p>
            </div>
            <button onClick={onClose} className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label="Close">
              <X className="h-4 w-4" />
            </button>
          </div>

          {error && (
            <div className="flex items-center gap-2 bg-destructive/10 px-5 py-2 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 flex-shrink-0" />
              <span className="min-w-0 flex-1">{error}</span>
              <button onClick={() => setError(null)} className="text-destructive/70 hover:text-destructive" aria-label="Dismiss error">
                <X className="h-3 w-3" />
              </button>
            </div>
          )}

          <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-5">
            {loading && !info ? (
              <div className="flex items-center justify-center py-12">
                <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
              </div>
            ) : (
              <div className="space-y-4">
                <section className="rounded-md border border-border overflow-hidden">
                  <div className="flex flex-col gap-3 bg-muted/30 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <StateIcon className={`h-4.5 w-4.5 ${visual.icon}`} />
                        <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${visual.badge}`}>
                          {formatPublishStateLabel(state)}
                        </span>
                      </div>
                      <h3 className="mt-2 text-base font-semibold text-foreground">Public site</h3>
                      <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
                        {info?.status?.summary
                          || (!configEnabled ? 'No publish destination is configured yet.' : 'Publish status is waiting for the builder to update publish/status.json.')}
                      </p>
                      {info?.status?.last_error && <p className="mt-2 text-xs text-destructive">{info.status.last_error}</p>}
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <TooltipProvider delayDuration={150}>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button onClick={load} disabled={loading} className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50" aria-label="Refresh publish status">
                              <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom"><p>Refresh publish status</p></TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                      <span className="inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 px-2.5 py-1.5 text-[11px] text-muted-foreground">
                        Set up · publish in chat with <code className="rounded bg-background px-1 font-medium text-foreground">/publish</code>
                      </span>
                    </div>
                  </div>

                  {url ? (
                    <div className="flex items-center gap-2 border-t border-border px-4 py-3">
                      <Globe className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                      <a href={url} target="_blank" rel="noopener noreferrer" className="min-w-0 flex-1 truncate text-sm font-medium text-primary hover:underline" title={url}>
                        {url}
                      </a>
                      <button onClick={() => copyUrl(url)} className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Copy URL">
                        {copied ? <Check className="h-3.5 w-3.5 text-emerald-500" /> : <Copy className="h-3.5 w-3.5" />}
                      </button>
                      <a href={url} target="_blank" rel="noopener noreferrer" className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted hover:text-foreground" aria-label="Open URL">
                        <ExternalLink className="h-3.5 w-3.5" />
                      </a>
                    </div>
                  ) : null}

                  <div className="grid border-t border-border text-sm sm:grid-cols-3">
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="text-xs text-muted-foreground">Last published</div>
                      <div className="mt-1 font-medium text-foreground">{formatRelativeTime(info?.status?.last_published_at)}</div>
                    </div>
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="text-xs text-muted-foreground">Last attempt</div>
                      <div className="mt-1 font-medium text-foreground">{formatRelativeTime(info?.status?.last_attempt_at)}</div>
                    </div>
                    <div className="px-4 py-3">
                      <div className="text-xs text-muted-foreground">Publishing</div>
                      <div className="mt-1 font-medium text-foreground">{targetLabel}</div>
                    </div>
                  </div>
                </section>

                {notice && (
                  <div className="flex items-start gap-2 rounded-md border border-sky-500/30 bg-sky-500/10 px-3 py-2 text-xs text-sky-700 dark:text-sky-300">
                    <Info className="mt-0.5 h-3.5 w-3.5 flex-shrink-0" />
                    <span>{notice}</span>
                  </div>
                )}

                <section className="rounded-md border border-border">
                  <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
                    <div>
                      <h3 className="text-sm font-semibold text-foreground">Destinations</h3>
                      <p className="mt-0.5 text-xs text-muted-foreground">The builder deploys to these and writes the URL.</p>
                    </div>
                    <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{destinations.length}</span>
                  </div>
                  {destinations.length === 0 ? (
                    <div className="px-4 py-5 text-sm text-muted-foreground">
                      Use setup to pick a static host (Netlify, Vercel, Cloudflare Pages, GitHub Pages, S3, …) — any static host works.
                    </div>
                  ) : (
                    <div className="divide-y divide-border">
                      {destinations.map((d) => {
                        const status = findDestStatus(info?.status?.destinations, d)
                        const dState = status?.state || 'configured_not_verified'
                        const dVisual = getPublishStateVisual(dState)
                        const DIcon = dVisual.Icon
                        return (
                          <div key={d.id || destinationTitle(d)} className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                            <div className="min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <DIcon className={`h-3.5 w-3.5 ${dVisual.icon}`} />
                                <span className="text-sm font-medium text-foreground">{d.id || 'Destination'}</span>
                                <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] uppercase tracking-wide text-muted-foreground">{d.provider}</span>
                                {d.method && <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">{d.method}</span>}
                              </div>
                              <div className="mt-1 truncate text-xs text-muted-foreground">{destinationTitle(d)}</div>
                              {status?.url && (
                                <a href={status.url} target="_blank" rel="noopener noreferrer" className="mt-1 inline-block truncate text-xs text-primary hover:underline">{status.url}</a>
                              )}
                              {status?.error && <div className="mt-1 text-xs text-destructive">{status.error}</div>}
                            </div>
                            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground sm:justify-end">
                              <span className={`rounded-full border px-2 py-0.5 ${dVisual.badge}`}>{formatPublishStateLabel(dState)}</span>
                              {status?.last_success_at && <span>{formatRelativeTime(status.last_success_at)}</span>}
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}
                </section>

                <section className="rounded-md border border-border">
                  <div className="border-b border-border px-4 py-3">
                    <h3 className="text-sm font-semibold text-foreground">Common hosts</h3>
                    <p className="mt-0.5 text-xs text-muted-foreground">Suggestions, not a limit — any static host works.</p>
                  </div>
                  <div className="grid divide-y divide-border md:grid-cols-2 md:divide-x md:divide-y-0">
                    {supported.map((s) => (
                      <div key={s.id} className="px-4 py-3">
                        <div className="flex items-center gap-2 text-sm font-medium text-foreground">
                          <Globe className="h-3.5 w-3.5 text-muted-foreground" />
                          {s.label}
                          <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">{s.method}</span>
                        </div>
                        <p className="mt-1 text-xs leading-5 text-muted-foreground">{s.description}</p>
                      </div>
                    ))}
                  </div>
                </section>

                <p className="px-1 text-[11px] text-muted-foreground">
                  Publishing puts the data on a public URL — the builder confirms what becomes public before the first publish. Status: {info?.status_path || 'publish/status.json'}
                </p>
              </div>
            )}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}

export default WorkflowPublishPopup
