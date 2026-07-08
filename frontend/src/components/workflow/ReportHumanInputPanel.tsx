import { useCallback, useEffect, useState } from 'react'
import { Loader2, MessageSquareText, RefreshCw, Send, X } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { ReportHumanInput } from '../../services/api-types'
import { useChatStore } from '../../stores/useChatStore'
import { useContainerSizeTier } from './reportWidgets/tableHelpers'

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

function inputStatusLabel(input: ReportHumanInput): string {
  if (input.status === 'answered') return 'Answered'
  if (input.status === 'consumed') return 'Used by agent'
  if (input.status === 'dismissed') return 'Dismissed'
  return 'Needs answer'
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

export function ReportHumanInputPanel({ workspacePath, className = '' }: { workspacePath: string; className?: string }) {
  const [inputs, setInputs] = useState<ReportHumanInput[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [drafts, setDrafts] = useState<Record<string, ReportHumanInputDraft>>({})
  const [refreshNonce, setRefreshNonce] = useState(0)
  const [panelRef, sizeTier] = useContainerSizeTier(560, 900)
  const compactOptions = sizeTier === 'phone'

  const loadInputs = useCallback(async (cancelled?: () => boolean) => {
    if (!workspacePath) return
    setLoading(true)
    setError(null)
    try {
      const res = await agentApi.listReportHumanInputs(workspacePath)
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
  }, [workspacePath])

  useEffect(() => {
    let cancelled = false
    void loadInputs(() => cancelled)
    return () => { cancelled = true }
  }, [loadInputs, refreshNonce])

  useEffect(() => {
    setDrafts({})
  }, [workspacePath])

  const pending = inputs.filter(input => input.status === 'pending')
  const history = inputs.filter(input => input.status !== 'pending').slice(0, 4)
  if (!loading && !error && pending.length === 0 && history.length === 0) return null

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
    if (input.options.length > 0 && !selectedOptionId) {
      useChatStore.getState().addToast('Choose an option before answering.', 'error')
      return
    }
    if (input.options.length === 0 && !note) {
      useChatStore.getState().addToast('Write an answer before submitting.', 'error')
      return
    }
    updateDraft(input.id, { submitting: true })
    try {
      await agentApi.answerReportHumanInput(workspacePath, input.id, {
        selected_option_id: selectedOptionId,
        note,
      })
      useChatStore.getState().addToast('Answer saved for the next Pulse pass.', 'success')
      setRefreshNonce(prev => prev + 1)
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
      setRefreshNonce(prev => prev + 1)
    } catch (err) {
      useChatStore.getState().addToast(err instanceof Error ? err.message : 'Failed to dismiss question.', 'error')
    } finally {
      updateDraft(input.id, { submitting: false })
    }
  }

  return (
    <section ref={panelRef} className={`rounded-lg border border-cyan-500/25 bg-cyan-500/[0.06] p-3 shadow-sm ${className}`}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-cyan-400/30 bg-cyan-400/10 text-cyan-200">
            <MessageSquareText className="h-4 w-4" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-semibold text-foreground">Questions for you</div>
            <div className="text-xs text-muted-foreground">
              Answers are saved in this workflow's <code className="rounded bg-background/70 px-1">db/db.sqlite</code>.
            </div>
          </div>
        </div>
        <button
          type="button"
          onClick={() => setRefreshNonce(prev => prev + 1)}
          className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs font-medium text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          {loading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}
          Refresh
        </button>
      </div>
      {error && <div className="mt-2 rounded-md border border-destructive/30 bg-destructive/10 px-2 py-1.5 text-xs text-destructive">{error}</div>}
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
              {input.context && <p className="mt-1 text-xs leading-5 text-muted-foreground">{input.context}</p>}
              {input.options.length > 0 && (
                <div className={compactOptions ? 'mt-3 overflow-hidden rounded-md border border-border/70 bg-background/45' : 'mt-3 grid grid-cols-2 gap-2'}>
                  {input.options.map(option => {
                    const checked = draft.selectedOptionId === option.id
                    return (
                      <label
                        key={option.id}
                        className={compactOptions
                          ? `flex cursor-pointer items-start gap-2 border-b border-border/60 p-2.5 last:border-b-0 transition-colors ${checked ? 'bg-cyan-400/10' : 'hover:bg-muted/40'}`
                          : `cursor-pointer rounded-md border p-2 transition-colors ${checked ? 'border-cyan-400 bg-cyan-400/10' : 'border-border bg-card/50 hover:border-cyan-400/50'}`
                        }
                      >
                        <input
                          type="radio"
                          name={`report-human-input-${input.id}`}
                          className={compactOptions ? 'mt-0.5 h-3.5 w-3.5 shrink-0 accent-cyan-400' : 'sr-only'}
                          checked={checked}
                          onChange={() => updateDraft(input.id, { selectedOptionId: option.id })}
                        />
                        <span className="min-w-0">
                          <span className="block break-words text-xs font-semibold text-foreground">{option.title}</span>
                          {option.description && <span className="mt-0.5 block break-words text-xs leading-5 text-muted-foreground">{option.description}</span>}
                        </span>
                      </label>
                    )
                  })}
                </div>
              )}
              {input.allow_free_text && (
                <textarea
                  value={draft.note}
                  onChange={event => updateDraft(input.id, { note: event.target.value })}
                  placeholder={input.options.length > 0 ? 'Optional note' : 'Write your answer'}
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
          <div className="mb-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-muted-foreground">Recent answers</div>
          <div className="grid gap-1.5">
            {history.map(input => (
              <div key={input.id} className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md bg-background/50 px-2 py-1.5 text-xs">
                <span className="font-medium text-foreground">{inputStatusLabel(input)}</span>
                <span className="text-muted-foreground">{input.question}</span>
                {input.selected_option_id && <span className="rounded bg-muted px-1 text-muted-foreground">{input.selected_option_id}</span>}
                {input.outcome_summary && <span className="text-muted-foreground">Outcome: {input.outcome_summary}</span>}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}

export default ReportHumanInputPanel
