import { useCallback, useEffect, useState, type ReactNode } from 'react'
import {
  AlertCircle,
  Ban,
  BellRing,
  CheckCircle2,
  Loader2,
  Mail,
  RefreshCw,
  Webhook,
} from 'lucide-react'
import {
  loadWorkflowNotificationInfo,
  type WorkflowNotificationInfo,
  type WorkflowNotificationState,
} from '../../services/workflow-notifications'
import { formatNotificationStateLabel } from './notificationStatus'
import NotificationInstructions from './NotificationInstructions'

interface WorkflowNotificationViewProps {
  workspacePath: string | null
  onStateLoaded?: (state: WorkflowNotificationState) => void
  loadInfo?: () => Promise<WorkflowNotificationInfo>
  onSetup?: () => void
  headerAction?: ReactNode
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

export default function WorkflowNotificationView({
  workspacePath,
  onStateLoaded,
  loadInfo,
  onSetup,
  headerAction,
}: WorkflowNotificationViewProps) {
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
    void load()
  }, [load])

  // Gmail auth resolves in a background subprocess on the server (~5s), so the
  // first read returns state="checking" rather than making the popup wait. Poll
  // until it settles; this is the only field that arrives late.
  useEffect(() => {
    if (info?.gmail?.state !== 'checking') return
    const timer = setTimeout(() => { void load() }, 1500)
    return () => clearTimeout(timer)
  }, [info?.gmail?.state, load])

  const state = info?.effectiveState || 'not_configured'
  const StateIcon = state === 'ready' ? CheckCircle2 : state === 'missing_secret' || state === 'invalid_secret' ? AlertCircle : BellRing
  const gmailReady = info?.gmail?.state === 'ready'
  const gmailChecking = info?.gmail?.state === 'checking'
  const gmailDefault = info?.gmail?.default_recipient?.trim() || ''
  const gmailBlocked = info?.gmail?.blocked_recipients || []
  const gmailSender = info?.gmail?.default_sender?.trim() || ''
  const gmailSenderChoices = info?.gmail?.sender_choices || []
  const scopeName = info?.scopeLabel || workspacePath?.split('/').filter(Boolean).pop() || 'Workflow'
  const senderLabel = (id: string) => {
    const choice = gmailSenderChoices.find(entry => entry.id === id)
    return choice ? (choice.email || choice.display_name || id) : id
  }
  // Two summaries go out per run: the run result, then Pulse's review of it.
  // Read-only: /notify in chat is the one place these are set (user decision
  // 2026-09-03 -- the editable form here duplicated it and read as clutter).
  const summaries = info ? [
    {
      key: 'run',
      title: 'Run summary',
      instructions: info.runSummaryInstructions.trim(),
      channels: info.runSummaryChannels,
      senders: info.runSummaryGmailConnectionIds.map(senderLabel),
      recipients: info.runSummaryRecipients,
      slackWebhooks: info.runSummarySlackWebhooks,
    },
    {
      key: 'pulse',
      title: 'Pulse review',
      instructions: info.pulseSummaryInstructions.trim(),
      channels: info.pulseSummaryChannels,
      senders: info.pulseSummaryGmailConnectionIds.map(senderLabel),
      recipients: info.pulseSummaryRecipients,
      slackWebhooks: info.pulseSummarySlackWebhooks,
    },
  ] : []
  const overrides = info ? [
    ...info.excludeChannels.map(channel => ({ key: `x-${channel}`, text: `No ${channel}` })),
    ...[...gmailBlocked, ...info.blockRecipients].map(email => ({ key: `b-${email}`, text: `Never to ${email}` })),
  ] : []
  // Colour carries meaning, sparingly: channels sky, sender violet, recipients
  // green, warnings amber, everything else neutral -- so a row can be read by
  // shape before its labels (user: "white on light white", 2026-09-03).
  const chipBase = 'inline-flex max-w-full items-center truncate rounded-full border px-2 py-0.5 text-xs'
  const chipMuted = `${chipBase} border-dashed border-border bg-background text-muted-foreground`
  const chipWarn = `${chipBase} border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300`
  const chipChannel = `${chipBase} border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300`
  const chipFrom = `${chipBase} border-violet-500/30 bg-violet-500/10 text-violet-700 dark:text-violet-300`
  const chipTo = `${chipBase} border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300`
  const rowClass = 'flex flex-wrap items-center gap-x-3 gap-y-1.5 px-4 py-2.5'
  const labelClass = 'flex w-28 shrink-0 items-center gap-1.5 text-xs font-medium text-foreground'
  const sectionHeadClass = 'flex items-center gap-2 border-b border-border bg-muted/50 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground'
  const stateLine = state === 'ready'
    ? 'Agentic notification delivery is on: the agent decides when, the server delivers.'
    : state === 'missing_secret'
      ? 'Slack webhook secret is missing.'
      : state === 'invalid_secret'
        ? 'Slack webhook secret is not a valid Incoming Webhook URL.'
        : 'No channel is set up for this workflow yet.'

  return (
        <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
          <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
            <div className="min-w-0">
              <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
                <BellRing className="h-4 w-4 text-primary" />
                Notify
                <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${stateBadgeClass(state)}`}>
                  <StateIcon className="mr-1 h-3 w-3" />{formatNotificationStateLabel(state)}
                </span>
              </h2>
              <p className="mt-0.5 truncate text-xs text-muted-foreground">{scopeName} · {stateLine}</p>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              <button onClick={() => { void load() }} disabled={loading} className={iconButtonClass} aria-label="Refresh notification status">
                <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
              </button>
              {headerAction}
              {onSetup ? (
                <button type="button" onClick={onSetup} className={`${setupClass} transition-colors hover:bg-muted hover:text-foreground`}>Change with <code className="rounded bg-background px-1 font-medium text-foreground">/notify</code></button>
              ) : (
                <span className={setupClass}>Change with <code className="rounded bg-background px-1 font-medium text-foreground">/notify</code></span>
              )}
            </div>
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
                <section className="rounded-md border border-border">
                  <h3 className={sectionHeadClass}><span className="h-2 w-2 rounded-full bg-sky-500" />Channels</h3>
                  <div className="divide-y divide-border">
                    <div className={rowClass}>
                      <span className={labelClass}><Webhook className="h-3.5 w-3.5 text-sky-500" />Workflow Slack webhook</span>
                      {info.slackWebhook.secret_name
                        ? <span className={`${chipChannel} font-mono`}>{info.slackWebhook.secret_name}</span>
                        : <span className={chipMuted}>None</span>}
                      <span className={`ml-auto rounded-full border px-2 py-0.5 text-xs ${stateBadgeClass(state)}`}>{formatNotificationStateLabel(state)}</span>
                    </div>
                    <div className={rowClass}>
                      <span className={labelClass} title="Gmail account channel (inherited from the account)"><Mail className="h-3.5 w-3.5 text-violet-500" />Gmail</span>
                      {gmailSender ? <span className={`${chipFrom} font-mono`} title="Sends from">{gmailSender}</span> : <span className={chipMuted}>No sending account</span>}
                      {gmailDefault
                        ? <span className={`${chipTo} font-mono`} title="Default recipients">→ {gmailDefault}</span>
                        : gmailReady && <span className={chipMuted} title="Mail that names no recipient has nowhere to go until one is set (Bots panel → Default recipients)">no default recipient</span>}
                      {!gmailReady && !gmailChecking && info.gmail?.summary && <span className={chipWarn}>{info.gmail.summary}</span>}
                      <span className={`ml-auto rounded-full border px-2 py-0.5 text-xs ${gmailReady ? 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'border-border bg-background text-muted-foreground'}`}>
                        {gmailChecking ? 'Checking…' : gmailReady ? 'Connected' : 'Not connected'}
                      </span>
                    </div>
                    {overrides.length > 0 && (
                      <div className={rowClass}>
                        <span className={labelClass}><Ban className="h-3.5 w-3.5 text-amber-500" />Overrides</span>
                        {overrides.map(item => <span key={item.key} className={chipWarn}>{item.text}</span>)}
                      </div>
                    )}
                  </div>
                </section>

                <section className="rounded-md border border-border">
                  <h3 className={sectionHeadClass}><span className="h-2 w-2 rounded-full bg-violet-500" />What gets sent</h3>
                  <div className="divide-y divide-border">
                    {summaries.map(summary => (
                      <div key={summary.key} className={`space-y-1.5 border-l-2 px-4 py-2.5 ${summary.key === 'run' ? 'border-sky-500/60' : 'border-violet-500/60'}`}>
                        <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                          <span className="w-28 shrink-0 text-xs font-medium text-foreground">{summary.title}</span>
                          {summary.channels.length > 0
                            ? summary.channels.map(channel => <span key={channel} className={`${chipChannel} capitalize`}>{channel}</span>)
                            : <span className={chipChannel}>All channels</span>}
                          {summary.senders.map(sender => <span key={sender} className={`${chipFrom} font-mono`} title="From">from {sender}</span>)}
                          {summary.recipients.length > 0
                            ? summary.recipients.map(email => <span key={email} className={`${chipTo} font-mono`} title="To">to {email}</span>)
                            : gmailDefault && <span className={chipMuted}>to {gmailDefault}</span>}
                          {summary.slackWebhooks.map(secret => <span key={secret} className={`${chipChannel} font-mono`} title="Slack webhook secret">#{secret}</span>)}
                        </div>
                        {summary.instructions && (
                          <NotificationInstructions target={summary.key === 'run' ? 'run_summary' : 'pulse_review'} title={summary.title} instructions={summary.instructions} />
                        )}
                      </div>
                    ))}
                  </div>
                </section>
              </div>
            ) : null}
          </div>
        </div>
  )
}
