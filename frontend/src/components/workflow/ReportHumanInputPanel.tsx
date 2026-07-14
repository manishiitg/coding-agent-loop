import { useCallback, useEffect, useMemo, useState } from 'react'
import { CheckCircle2, ChevronDown, ChevronRight, Clock3, Loader2, MessageSquareText, RefreshCw, Send, X } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { ReportHumanInput } from '../../services/api-types'
import { useChatStore } from '../../stores/useChatStore'
import {
  parseReportHumanInputContext,
  reportHumanInputHistory,
  reportHumanInputStatusLabel,
} from '../../utils/reportHumanInputFormatting'
import { useContainerSizeTier } from './reportWidgets/tableHelpers'
import { WORKFLOW_LOG_REFRESH_EVENT } from './workflowEvents'

type ReportHumanInputDraft = {
  selectedOptionId: string
  note: string
  submitting?: boolean
}

function sourceLabel(source: string): string {
  if (source === 'goal_advisor') return 'Goal Advisor'
  if (source === 'chief_of_staff') return 'Chief of Staff'
  return 'Pulse'
}

function inputTime(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' })
}

function priorityTone(priority: string): string {
  if (priority === 'high') return 'border-rose-500/40 bg-rose-500/10 text-rose-200'
  if (priority === 'low') return 'border-slate-500/30 bg-slate-500/10 text-slate-300'
  return 'border-amber-500/35 bg-amber-500/10 text-amber-200'
}

function selectedOptionTitle(input: ReportHumanInput): string {
  if (!input.selected_option_id) return ''
  return input.options.find(option => option.id === input.selected_option_id)?.title || input.selected_option_id
}

function consumedActorLabel(input: ReportHumanInput): string {
  const actor = input.consumed_by?.trim()
  if (actor && actor.toLowerCase() !== 'agent') return actor
  return sourceLabel(input.source)
}

function answerHandlerLabel(input: ReportHumanInput): string {
  return input.source === 'chief_of_staff' ? 'Chief of Staff' : 'Pulse'
}

function statusTone(input: ReportHumanInput): string {
	if (input.status === 'consumed') return 'text-emerald-300'
	if (input.status === 'answered' || input.status === 'claimed') return 'text-amber-200'
  return 'text-muted-foreground'
}

function HumanInputContext({ value }: { value: string }) {
  const sections = parseReportHumanInputContext(value)
  if (sections.length === 0) return null

  return (
    <div className="mt-3 space-y-3 border-l border-cyan-400/20 pl-3 text-xs leading-5 text-muted-foreground">
      {sections.map((section, index) => (
        <div key={`${section.label || 'context'}-${index}`}>
          {section.label && (
            <div className="mb-0.5 font-semibold text-foreground">{section.label}</div>
          )}
          {section.body && <p className="whitespace-pre-line">{section.body}</p>}
          {section.items.length > 0 && (
            <ol className="mt-1.5 list-decimal space-y-1.5 pl-4 marker:font-semibold marker:text-cyan-300">
              {section.items.map((item, itemIndex) => <li key={itemIndex} className="pl-1">{item}</li>)}
            </ol>
          )}
        </div>
      ))}
    </div>
  )
}

interface ReportHumanInputPanelProps {
	workspacePath: string
	className?: string
	source?: string
	workspaceLabel?: string
	providedInputs?: ReportHumanInput[]
	providedLoading?: boolean
	providedError?: string | null
	onRequestRefresh?: () => void
}

export function ReportHumanInputPanel({
  workspacePath,
	className = '',
	source,
	workspaceLabel,
	providedInputs,
	providedLoading,
	providedError,
	onRequestRefresh,
}: ReportHumanInputPanelProps) {
  const [inputs, setInputs] = useState<ReportHumanInput[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, ReportHumanInputDraft>>({})
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [expandedHistoryIds, setExpandedHistoryIds] = useState<Record<string, boolean>>({})
  const [panelRef, sizeTier] = useContainerSizeTier(560, 900)
	const compactOptions = sizeTier === 'phone'
	const externallyManaged = providedInputs !== undefined
	const visibleInputs = providedInputs ?? inputs
	const visibleLoading = externallyManaged ? Boolean(providedLoading) : loading
	const visibleError = externallyManaged ? (providedError || null) : error

  const loadInputs = useCallback(async (cancelled?: () => boolean) => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const res = await agentApi.listReportHumanInputs(workspacePath, undefined, source)
      if (cancelled?.()) return
      if (!res.success) throw new Error(res.error || 'Failed to load questions.')
      setInputs(res.inputs || [])
    } catch (err) {
      if (cancelled?.()) return
      setError(err instanceof Error ? err.message : 'Failed to load questions.')
      setInputs([])
    } finally {
      if (!cancelled?.()) setLoading(false)
    }
  }, [source, workspacePath])

	useEffect(() => {
		if (externallyManaged) return
		let cancelled = false
    void loadInputs(() => cancelled)
    return () => { cancelled = true }
	}, [externallyManaged, loadInputs, refreshNonce])

	useEffect(() => {
		if (externallyManaged) return
		const onRefresh = () => { void loadInputs() }
    window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
    return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
	}, [externallyManaged, loadInputs])

	const waitingForPulse = visibleInputs.some(input => input.status === 'answered' || input.status === 'claimed')
	useEffect(() => {
		if (externallyManaged || !waitingForPulse) return
    const timer = window.setInterval(() => { void loadInputs() }, 5000)
    return () => window.clearInterval(timer)
	}, [externallyManaged, loadInputs, waitingForPulse])

  useEffect(() => {
    setDrafts({})
    setHistoryOpen(false)
    setExpandedHistoryIds({})
  }, [workspacePath])

	const pending = visibleInputs.filter(input => input.status === 'pending')
	const history = reportHumanInputHistory(visibleInputs)
	if (!visibleLoading && !visibleError && pending.length === 0 && history.length === 0) return null

	const requestRefresh = () => {
		if (onRequestRefresh) {
			onRequestRefresh()
			return
		}
		setRefreshNonce(prev => prev + 1)
	}

  const updateDraft = (id: string, patch: Partial<ReportHumanInputDraft>) => {
    setDrafts(prev => {
      const current = prev[id] ?? { selectedOptionId: '', note: '' }
      return { ...prev, [id]: { ...current, ...patch } }
    })
  }

  const answerInput = async (input: ReportHumanInput) => {
    const draft = drafts[input.id] || { selectedOptionId: '', note: '' }
    const selectedOptionId = draft.selectedOptionId || ''
    const note = draft.note.trim()
    if (!selectedOptionId && !note) {
      const message = input.options.length > 0
        ? (input.allow_free_text ? 'Choose an option or write your own answer.' : 'Choose an option before answering.')
        : 'Write an answer before submitting.'
      useChatStore.getState().addToast(message, 'error')
      return
    }
    updateDraft(input.id, { submitting: true })
    try {
      await agentApi.answerReportHumanInput(workspacePath, input.id, {
        selected_option_id: selectedOptionId,
        note,
      })
      useChatStore.getState().addToast(`Answer saved for the next ${answerHandlerLabel(input)} run.`, 'success')
      setHistoryOpen(false)
		requestRefresh()
    } catch (err) {
      useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to save answer.', 'error')
    } finally {
      updateDraft(input.id, { submitting: false })
    }
  }

  const dismissInput = async (input: ReportHumanInput) => {
    updateDraft(input.id, { submitting: true })
    try {
      await agentApi.dismissReportHumanInput(workspacePath, input.id)
      useChatStore.getState().addToast('Question dismissed.', 'success')
      setHistoryOpen(false)
		requestRefresh()
    } catch (err) {
      useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to dismiss question.', 'error')
    } finally {
      updateDraft(input.id, { submitting: false })
    }
  }

  const renderHistoryRows = () => (
    <div className="grid gap-1.5">
      {history.map(input => {
        const expanded = Boolean(expandedHistoryIds[input.id])
        const answer = selectedOptionTitle(input)
        return (
          <div key={input.id} className="rounded-md bg-background/50 text-xs">
            <button
              type="button"
              onClick={() => setExpandedHistoryIds(prev => ({ ...prev, [input.id]: !expanded }))}
              aria-expanded={expanded}
              className="flex w-full min-w-0 items-center gap-2 px-2 py-1.5 text-left hover:bg-muted/40"
            >
              {expanded ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
              {input.status === 'consumed'
                ? <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-emerald-300" />
				: input.status === 'answered' || input.status === 'claimed'
                  ? <Clock3 className="h-3.5 w-3.5 shrink-0 text-amber-200" />
                  : null}
              <span className={`shrink-0 font-medium ${statusTone(input)}`}>{reportHumanInputStatusLabel(input)}</span>
              <span className="shrink-0 text-muted-foreground">{inputTime(input.consumed_at || input.answered_at || input.dismissed_at || input.updated_at)}</span>
              <span className="min-w-0 flex-1 truncate text-muted-foreground">{input.question}</span>
            </button>
            {expanded && (
              <div className="space-y-1.5 border-t border-border/50 px-3 py-2 text-muted-foreground">
                {answer && (
                  <div>
                    <span className="font-medium text-foreground">You answered: </span>
                    <span>{answer}</span>
                  </div>
                )}
                {input.note && (
                  <div>
                    <span className="font-medium text-foreground">Note: </span>
                    <span>{input.note}</span>
                  </div>
                )}
			{(input.status === 'answered' || input.status === 'claimed') && (
                  <div className="flex items-center gap-1.5 rounded-md border border-amber-400/20 bg-amber-400/[0.06] px-2 py-1.5 text-amber-100">
                    <Clock3 className="h-3.5 w-3.5 shrink-0" />
				<span>{input.status === 'claimed' ? `${answerHandlerLabel(input)} is working on this answer.` : `Answer received — waiting for ${answerHandlerLabel(input)} to act.`}</span>
                  </div>
                )}
                {input.outcome_summary && (
                  <div className="rounded-md border border-emerald-400/20 bg-emerald-400/[0.06] px-2 py-1.5 text-emerald-100">
                    <div>
                      <span className="font-medium">Handled by {consumedActorLabel(input)}: </span>
                      <span>{input.outcome_summary}</span>
                    </div>
                    {input.consumed_at && <div className="mt-1 text-[11px] text-emerald-200/70">Completed {inputTime(input.consumed_at)}</div>}
                  </div>
                )}
                {(input.run_id || input.evidence) && (
                  <div className="flex flex-wrap gap-x-3 gap-y-1 text-[11px]">
                    {input.run_id && <span>Run: <code className="rounded bg-muted px-1">{input.run_id}</code></span>}
                    {input.evidence && <span>Evidence: <code className="rounded bg-muted px-1">{input.evidence}</code></span>}
                  </div>
                )}
                {!answer && !input.note && !input.outcome_summary && <div>No saved answer details.</div>}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )

  if (pending.length === 0 && history.length > 0 && !historyOpen) {
    const latest = history[0]
    const latestSummary = latest?.status === 'consumed' && latest.outcome_summary
      ? latest.outcome_summary
      : latest?.question
    return (
      <section ref={panelRef} className={`rounded-lg border border-cyan-500/20 bg-cyan-500/[0.045] px-3 py-2 shadow-sm ${className}`}>
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            onClick={() => setHistoryOpen(true)}
            className="flex min-w-0 flex-1 items-center gap-2 text-left"
            aria-expanded={false}
          >
            <MessageSquareText className="h-4 w-4 shrink-0 text-cyan-200" />
            <span className="min-w-0">
              <span className="block text-xs font-semibold text-foreground">
                Recent decisions{workspaceLabel ? ` · ${workspaceLabel}` : ''}
              </span>
              <span className="block truncate text-[11px] text-muted-foreground">
                {history.length} saved{latest ? ` · ${reportHumanInputStatusLabel(latest)} · ${latestSummary}` : ''}
              </span>
            </span>
          </button>
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              type="button"
				onClick={requestRefresh}
              className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-label="Refresh questions"
            >
				{visibleLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
            </button>
            <button
              type="button"
              onClick={() => setHistoryOpen(true)}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-border bg-background px-2 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
              aria-expanded={false}
            >
              Show
              <ChevronDown className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>
      </section>
    )
  }

  return (
    <section ref={panelRef} className={`rounded-lg border border-cyan-500/25 bg-cyan-500/[0.06] p-3 shadow-sm ${className}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-cyan-400/30 bg-cyan-400/10 text-cyan-200">
            <MessageSquareText className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-foreground">
              {pending.length > 0
                ? `Needs your decision${workspaceLabel ? ` · ${workspaceLabel}` : ''}`
                : `Recent decisions${workspaceLabel ? ` · ${workspaceLabel}` : ''}`}
            </div>
            <div className="text-xs text-muted-foreground">
              {pending.length > 0
                ? `Your answer will be used by the next ${source === 'chief_of_staff' ? 'Chief of Staff' : 'Pulse'} run.`
                : 'Previous decisions and outcomes.'}
            </div>
          </div>
        </div>
        <button
          type="button"
			onClick={requestRefresh}
          className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
        >
			{visibleLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          Refresh
        </button>
      </div>
		{visibleError && <div className="mt-2 rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">{visibleError}</div>}
      <div className="mt-3 flex flex-col gap-2">
        {pending.map(input => {
          const draft = drafts[input.id] || { selectedOptionId: '', note: '' }
          const busy = Boolean(draft.submitting)
          return (
            <article key={input.id} className="rounded-md border border-border/70 bg-background/75 p-3">
              <div className="flex flex-wrap items-center gap-2 text-[11px]">
                <span className={`rounded-full border px-2 py-0.5 font-semibold uppercase tracking-[0.08em] ${priorityTone(input.priority)}`}>
                  {input.priority || 'medium'}
                </span>
                <span className="rounded-full border border-border bg-muted/40 px-2 py-0.5 text-muted-foreground">{sourceLabel(input.source)}</span>
                <span className="text-muted-foreground">{inputTime(input.created_at)}</span>
              </div>
              <h4 className="mt-2 text-sm font-semibold leading-snug text-foreground">{input.question}</h4>
              {input.context && <HumanInputContext value={input.context} />}
              {input.options.length > 0 && (
                <div className={compactOptions ? 'mt-3 overflow-hidden rounded-md border border-border/70 bg-background/45' : 'mt-3 grid grid-cols-2 gap-2'}>
                  {input.options.map(option => {
                    const checked = draft.selectedOptionId === option.id
                    return (
                      <button
                        key={option.id}
                        type="button"
                        role="radio"
                        aria-checked={checked}
                        onPointerDown={event => event.stopPropagation()}
                        onClick={event => {
                          event.stopPropagation()
                          updateDraft(input.id, { selectedOptionId: option.id })
                        }}
                        className={compactOptions
                          ? `flex w-full cursor-pointer items-start gap-2 border-b border-border/60 p-2.5 text-left last:border-b-0 transition-colors ${checked ? 'bg-cyan-400/10' : 'hover:bg-muted/40'}`
                          : `flex cursor-pointer items-start gap-2 rounded-md border p-2 text-left transition-colors ${checked ? 'border-cyan-400 bg-cyan-400/10' : 'border-border bg-card/50 hover:border-cyan-400/50'}`
                        }
                      >
                        <span
                          aria-hidden="true"
                          className={`mt-0.5 flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-full border ${checked ? 'border-cyan-300' : 'border-muted-foreground/60'}`}
                        >
                          {checked && <span className="h-1.5 w-1.5 rounded-full bg-cyan-300" />}
                        </span>
                        <span className="min-w-0 flex-1 text-left">
                          <span className="block break-words text-xs font-semibold text-foreground">{option.title}</span>
                          {option.description && <span className="mt-0.5 block break-words text-xs leading-5 text-muted-foreground">{option.description}</span>}
                        </span>
                      </button>
                    )
                  })}
                </div>
              )}
              {draft.selectedOptionId && (
                <div className="mt-2 text-xs text-cyan-200">
                  Selected: {input.options.find(option => option.id === draft.selectedOptionId)?.title || draft.selectedOptionId}. Save the answer to confirm it.
                </div>
              )}
              {input.allow_free_text && (
                <textarea
                  value={draft.note}
                  onChange={event => updateDraft(input.id, { note: event.target.value })}
                  placeholder={input.options.length > 0 ? 'Write a different answer or add a note' : 'Write your answer'}
                  className="mt-3 min-h-20 w-full resize-y rounded-md border border-border bg-background px-2.5 py-2 text-sm text-foreground outline-none transition-colors placeholder:text-muted-foreground focus:border-cyan-400"
                />
              )}
              {input.evidence && <div className="mt-2 text-[11px] text-muted-foreground">Evidence: <code className="rounded bg-muted px-1">{input.evidence}</code></div>}
              <div className="mt-3 flex flex-wrap justify-end gap-2">
                <button
                  type="button"
                  onClick={() => void dismissInput(input)}
                  disabled={busy}
                  className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-3 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
                >
                  <X className="h-3.5 w-3.5" />
                  Dismiss
                </button>
                <button
                  type="button"
                  onClick={() => void answerInput(input)}
                  disabled={busy}
                  className="inline-flex h-8 items-center gap-1.5 rounded-md border border-cyan-400/40 bg-cyan-400/15 px-3 text-xs font-semibold text-cyan-100 hover:bg-cyan-400/25 disabled:opacity-50"
                >
                  {busy ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
                  Save answer
                </button>
              </div>
            </article>
          )
        })}
      </div>
      {history.length > 0 && (
        <div className="mt-3 border-t border-border/60 pt-2">
          <button
            type="button"
            onClick={() => setHistoryOpen(prev => !prev)}
            className="mb-1 flex w-full items-center justify-between gap-2 text-left text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground hover:text-foreground"
            aria-expanded={historyOpen}
          >
            <span>Recent decisions</span>
            {historyOpen ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronRight className="h-3.5 w-3.5" />}
          </button>
          {historyOpen && renderHistoryRows()}
        </div>
      )}
    </section>
  )
}

export type ReportHumanInputScope = {
	workspacePath: string
	workspaceLabel: string
}

export function ReportHumanInputCollection({
	scopes,
	source,
	className = '',
}: {
	scopes: ReportHumanInputScope[]
	source?: string
	className?: string
}) {
	const scopeKey = scopes.map(scope => `${scope.workspacePath}\u0000${scope.workspaceLabel}`).join('\u0001')
	const stableScopes = useMemo(() => scopeKey.split('\u0001').filter(Boolean).map(value => {
		const [workspacePath, workspaceLabel] = value.split('\u0000')
		return { workspacePath, workspaceLabel }
	}), [scopeKey])
	const [inputs, setInputs] = useState<ReportHumanInput[]>([])
	const [loading, setLoading] = useState(false)
	const [error, setError] = useState<string | null>(null)
	const [refreshNonce, setRefreshNonce] = useState(0)

	const loadInputs = useCallback(async (cancelled?: () => boolean) => {
		const paths = stableScopes.map(scope => scope.workspacePath).filter(Boolean)
		if (paths.length === 0) {
			setInputs([])
			return
		}
		setLoading(true)
		setError(null)
		try {
			const result = await agentApi.listReportHumanInputsAggregate(paths, undefined, source)
			if (cancelled?.()) return
			if (!result.success) throw new Error(result.error || 'Failed to load questions.')
			setInputs(result.inputs || [])
		} catch (err) {
			if (cancelled?.()) return
			setError(err instanceof Error ? err.message : 'Failed to load questions.')
			setInputs([])
		} finally {
			if (!cancelled?.()) setLoading(false)
		}
	}, [source, stableScopes])

	useEffect(() => {
		let cancelled = false
		void loadInputs(() => cancelled)
		return () => { cancelled = true }
	}, [loadInputs, refreshNonce])

	useEffect(() => {
		const onRefresh = () => { void loadInputs() }
		window.addEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
		return () => window.removeEventListener(WORKFLOW_LOG_REFRESH_EVENT, onRefresh)
	}, [loadInputs])

	const waitingForAgent = inputs.some(input => input.status === 'answered' || input.status === 'claimed')
	useEffect(() => {
		if (!waitingForAgent) return
		const timer = window.setInterval(() => { void loadInputs() }, 5000)
		return () => window.clearInterval(timer)
	}, [loadInputs, waitingForAgent])

	if (loading && inputs.length === 0 && !error) return null

	return (
		<div className={className}>
			{stableScopes.map((scope, index) => (
				<ReportHumanInputPanel
					key={scope.workspacePath}
					workspacePath={scope.workspacePath}
					workspaceLabel={scope.workspaceLabel}
					source={source}
					providedInputs={inputs.filter(input => input.workspace_path === scope.workspacePath)}
					providedLoading={loading}
					providedError={index === 0 ? error : null}
					onRequestRefresh={() => setRefreshNonce(value => value + 1)}
				/>
			))}
		</div>
	)
}

export default ReportHumanInputPanel
