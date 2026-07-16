import { useCallback, useEffect, useState } from 'react'
import {
  AlertCircle,
  BellRing,
  Bot,
  CheckCircle2,
  Loader2,
  Mail,
  RefreshCw,
  ServerCog,
  Webhook,
  X,
} from 'lucide-react'
import {
  loadWorkflowNotificationInfo,
  type WorkflowNotificationInfo,
  type WorkflowNotificationState,
} from '../../services/workflow-notifications'
import ModalPortal from '../ui/ModalPortal'
import { formatNotificationStateLabel } from './notificationStatus'

interface WorkflowNotificationPopupProps {
  isOpen: boolean
  onClose: () => void
  workspacePath: string | null
  onStateLoaded?: (state: WorkflowNotificationState) => void
  loadInfo?: () => Promise<WorkflowNotificationInfo>
  scopeKind?: 'workflow' | 'chief-of-staff'
  onSetup?: () => void
}

const iconButtonClass = 'inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50'
const setupClass = 'inline-flex items-center gap-1 rounded-md border border-border bg-muted/40 px-2.5 py-1.5 text-[11px] text-muted-foreground'

const stateBadgeClass = (state: WorkflowNotificationState): string => {
  switch (state) {
    case 'ready':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'missing_secret':
    case 'invalid_secret':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
    default:
      return 'border-border bg-background text-muted-foreground'
  }
}

const summaryFor = (info: WorkflowNotificationInfo, scopeKind: 'workflow' | 'chief-of-staff'): string => {
  const destination = scopeKind === 'chief-of-staff' ? 'Chief of Staff’s Slack webhook' : 'this workflow’s Slack webhook'
  const unconfigured = scopeKind === 'chief-of-staff' ? 'Chief of Staff Slack destination' : 'workflow-specific Slack destination'
  switch (info.effectiveState) {
    case 'ready':
      return `The agent can decide when a notification is useful. Every notify_user call is delivered to ${destination} by the backend.`
    case 'missing_secret':
      return 'A Slack webhook is referenced, but its selected encrypted secret is missing. Use /notify to repair or replace it.'
    case 'invalid_secret':
      return 'The referenced encrypted secret is not a valid Slack Incoming Webhook URL. Use /notify to replace it safely.'
    default:
      return `No ${unconfigured} is configured. Use /notify to choose notification behavior and connect one if needed.`
  }
}

export default function WorkflowNotificationPopup({
  isOpen,
  onClose,
  workspacePath,
  onStateLoaded,
  loadInfo,
  scopeKind = 'workflow',
  onSetup,
}: WorkflowNotificationPopupProps) {
  const [loading, setLoading] = useState(false)
  const [info, setInfo] = useState<WorkflowNotificationInfo | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!workspacePath && !loadInfo) return
    setLoading(true)
    setError(null)
    try {
      const next = loadInfo ? await loadInfo() : await loadWorkflowNotificationInfo(workspacePath as string)
      setInfo(next)
      onStateLoaded?.(next.effectiveState)
    } catch (loadError) {
      setError(loadError instanceof Error ? loadError.message : 'Failed to load notification status')
    } finally {
      setLoading(false)
    }
  }, [loadInfo, onStateLoaded, workspacePath])

  useEffect(() => {
    if (isOpen) void load()
  }, [isOpen, load])

  if (!isOpen) return null

  const state = info?.effectiveState || 'not_configured'
  const StateIcon = state === 'ready' ? CheckCircle2 : state === 'missing_secret' || state === 'invalid_secret' ? AlertCircle : BellRing
  const gmailReady = info?.gmail?.state === 'ready'
  const scopeName = info?.scopeLabel || workspacePath?.split('/').filter(Boolean).pop() || (scopeKind === 'chief-of-staff' ? 'Chief of Staff' : 'Workflow')
  const scopeLabel = scopeKind === 'chief-of-staff' ? 'Chief of Staff' : 'workflow'

  return (
    <ModalPortal>
      <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4">
        <div className="relative flex max-h-[calc(100dvh-1rem)] w-full max-w-3xl flex-col rounded-lg border border-border bg-background shadow-xl sm:max-h-[86vh]">
          <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
            <div className="min-w-0">
              <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
                <BellRing className="h-4 w-4 text-primary" />
                Notify
              </h2>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">Agentic, one-way notifications for {scopeName}</p>
            </div>
            <button onClick={onClose} className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label="Close">
              <X className="h-4 w-4" />
            </button>
          </div>

          {error && (
            <div className="flex items-center gap-2 bg-destructive/10 px-5 py-2 text-xs text-destructive">
              <AlertCircle className="h-3.5 w-3.5 flex-shrink-0" />
              <span className="min-w-0 flex-1">{error}</span>
            </div>
          )}

          <div className="flex-1 overflow-y-auto px-4 py-4 sm:px-5">
            {loading && !info ? (
              <div className="flex items-center justify-center py-12"><Loader2 className="h-5 w-5 animate-spin text-muted-foreground" /></div>
            ) : info ? (
              <div className="space-y-4">
                <section className="overflow-hidden rounded-md border border-border">
                  <div className="flex flex-col gap-3 bg-muted/30 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <StateIcon className={`h-4 w-4 ${state === 'ready' ? 'text-emerald-500' : state === 'missing_secret' || state === 'invalid_secret' ? 'text-amber-500' : 'text-muted-foreground'}`} />
                        <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${stateBadgeClass(state)}`}>
                          {formatNotificationStateLabel(state)}
                        </span>
                      </div>
                      <h3 className="mt-2 text-base font-semibold text-foreground">Agentic notification delivery</h3>
                      <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{summaryFor(info, scopeKind)}</p>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <button onClick={() => { void load() }} disabled={loading} className={iconButtonClass} aria-label="Refresh notification status">
                        <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
                      </button>
                      {onSetup ? (
                        <button type="button" onClick={onSetup} className={`${setupClass} transition-colors hover:bg-muted hover:text-foreground`}>Set up · test in chat with <code className="rounded bg-background px-1 font-medium text-foreground">/notify</code></button>
                      ) : (
                        <span className={setupClass}>Set up · test in chat with <code className="rounded bg-background px-1 font-medium text-foreground">/notify</code></span>
                      )}
                    </div>
                  </div>

                  <div className="grid border-t border-border text-sm sm:grid-cols-3">
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="flex items-center gap-1.5 text-xs text-muted-foreground"><Bot className="h-3.5 w-3.5" />Decision</div>
                      <div className="mt-1 font-medium text-foreground">Agent chooses when</div>
                    </div>
                    <div className="border-b border-border px-4 py-3 sm:border-b-0 sm:border-r">
                      <div className="flex items-center gap-1.5 text-xs text-muted-foreground"><ServerCog className="h-3.5 w-3.5" />Delivery</div>
                      <div className="mt-1 font-medium text-foreground">Backend resolves secrets</div>
                    </div>
                    <div className="px-4 py-3">
                      <div className="text-xs text-muted-foreground">Reply behavior</div>
                      <div className="mt-1 font-medium text-foreground">One-way · never blocks</div>
                    </div>
                  </div>
                </section>

                <section className="rounded-md border border-border">
                  <div className="border-b border-border px-4 py-3">
                    <h3 className="text-sm font-semibold text-foreground">Effective destinations</h3>
                    <p className="mt-0.5 text-xs text-muted-foreground">The agent never reads a webhook URL. It calls notify_user; the server applies these destinations.</p>
                  </div>
                  <div className="divide-y divide-border">
                    <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <Webhook className="h-3.5 w-3.5 text-muted-foreground" />
                          <span className="text-sm font-medium text-foreground">{scopeKind === 'chief-of-staff' ? 'Chief of Staff Slack webhook' : 'Workflow Slack webhook'}</span>
                        </div>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {info.slackWebhook.secret_name
                            ? <>Encrypted secret reference: <code>{info.slackWebhook.secret_name}</code></>
                            : info.slackWebhook.summary || `No ${scopeLabel}-specific webhook selected.`}
                        </p>
                      </div>
                      <span className={`w-fit rounded-full border px-2 py-0.5 text-xs ${stateBadgeClass(state)}`}>{formatNotificationStateLabel(state)}</span>
                    </div>

                    <div className="flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-start sm:justify-between">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <Mail className="h-3.5 w-3.5 text-muted-foreground" />
                          <span className="text-sm font-medium text-foreground">Gmail account channel</span>
                          <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Inherited</span>
                        </div>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {gmailReady
                            ? `Available to ${scopeKind === 'chief-of-staff' ? 'Chief of Staff' : 'this workflow'}${info.gmail?.default_recipient ? ` · default ${info.gmail.default_recipient}` : ''}. The agent may supply specific recipients when explicitly configured.`
                            : info.gmail?.summary || 'Not ready at account level. Configure and test Gmail from Notification channels.'}
                        </p>
                      </div>
                      <span className={`w-fit rounded-full border px-2 py-0.5 text-xs ${gmailReady ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'border-border bg-background text-muted-foreground'}`}>
                        {gmailReady ? 'Available' : 'Not ready'}
                      </span>
                    </div>
                  </div>
                </section>

                <div className="rounded-md border border-blue-500/20 bg-blue-500/5 px-4 py-3 text-xs text-muted-foreground">
                  Configure notification intent and the Slack destination through <code className="text-foreground">/notify</code>. This does not add a routing step. Short-lived questions that require an answer still use <code className="text-foreground">human_feedback</code> instead.
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </ModalPortal>
  )
}
