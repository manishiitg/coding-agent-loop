import { useEffect, useMemo, useState } from 'react'
import { CheckCircle2, Loader2, Save } from 'lucide-react'
import { agentApi } from '../../../services/api'
import type { ReportWidget, ReportWidgetResponse } from '../../../services/api-types'
import { useChatStore } from '../../../stores/useChatStore'
import { WidgetHeader } from './shared'

function responseTime(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

function responseOptionTitle(widget: ReportWidget, optionID?: string): string {
  if (!optionID) return ''
  return widget.options?.find(option => option.id === optionID)?.title || optionID
}

export function InteractionWidget({
  widget,
  workspacePath,
}: {
  widget: ReportWidget
  workspacePath: string
}) {
  const widgetID = widget.id || ''
	const instanceKey = widget.instanceKey || 'default'
	const subjectID = widget.subjectId || ''
	const subjectVersion = widget.subjectVersion || ''
	const subjectHash = widget.subjectHash || ''
  const responseKind = widget.responseKind || (widget.options?.length ? 'choice' : 'text')
  const showsOptions = responseKind === 'choice' || responseKind === 'choice-with-text'
  const showsText = responseKind === 'text' || responseKind === 'choice-with-text' || widget.allowFreeText === true
  const [response, setResponse] = useState<ReportWidgetResponse | null>(null)
  const [selectedOptionID, setSelectedOptionID] = useState('')
  const [note, setNote] = useState('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [reloadNonce, setReloadNonce] = useState(0)

  useEffect(() => {
    const refresh = () => setReloadNonce(value => value + 1)
    const refreshIfCurrentWorkspace = (event: Event) => {
      const stalePath = (event as CustomEvent).detail?.workspacePath
      if (!stalePath || stalePath === workspacePath) refresh()
    }
    window.addEventListener('workflow-report-refresh-requested', refresh)
    window.addEventListener('workflow-report-data-stale', refreshIfCurrentWorkspace)
    return () => {
      window.removeEventListener('workflow-report-refresh-requested', refresh)
      window.removeEventListener('workflow-report-data-stale', refreshIfCurrentWorkspace)
    }
  }, [workspacePath])

  useEffect(() => {
    let cancelled = false
    if (!workspacePath || !widgetID) {
      setLoading(false)
      setError(widgetID ? null : 'This interaction widget needs a stable widget ID.')
      return () => { cancelled = true }
    }
    setLoading(true)
    setError(null)
    void agentApi.listReportWidgetResponses(workspacePath, widgetID, instanceKey)
      .then(result => {
        if (cancelled) return
        const next = result.responses?.[0] || null
        setResponse(next)
        setSelectedOptionID(next?.selected_option_id || '')
        setNote(next?.note || '')
      })
      .catch(err => {
        if (cancelled) return
        setError(err instanceof Error ? err.message : 'Failed to load the saved response.')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
	}, [instanceKey, reloadNonce, subjectHash, subjectID, subjectVersion, widgetID, workspacePath])

  const canSubmit = useMemo(() => {
    if (submitting || !widgetID) return false
    if (responseKind === 'text') return note.trim().length > 0
    if (responseKind === 'choice-with-text') return selectedOptionID.length > 0 || note.trim().length > 0
    if (widget.allowFreeText) return selectedOptionID.length > 0 || note.trim().length > 0
    return selectedOptionID.length > 0
  }, [note, responseKind, selectedOptionID, submitting, widget.allowFreeText, widgetID])

  const saveResponse = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    setError(null)
    try {
      const result = await agentApi.answerReportWidgetResponse(workspacePath, widgetID, {
			instance_key: instanceKey,
			selected_option_id: selectedOptionID,
			note: note.trim(),
			expected_subject_id: subjectID,
			expected_subject_version: subjectVersion,
			expected_subject_hash: subjectHash,
      })
      setResponse(result.response)
      setSelectedOptionID(result.response.selected_option_id || '')
      setNote(result.response.note || '')
      useChatStore.getState().addToast('Response saved for the next workflow run.', 'success')
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to save response.'
      setError(message)
      useChatStore.getState().addToast(message, 'error')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="space-y-3">
      <WidgetHeader widget={widget} />
      <div>
        <div className="text-sm font-medium leading-6 text-foreground">{widget.question || widget.title}</div>
        {(widget.subjectId || widget.subjectVersion) && (
          <div className="mt-1 text-[11px] text-muted-foreground">
            {[widget.subjectId, widget.subjectVersion ? `version ${widget.subjectVersion}` : ''].filter(Boolean).join(' · ')}
          </div>
        )}
      </div>

      {loading ? (
        <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          Loading saved response…
        </div>
      ) : (
        <>
          {showsOptions && (
            <div className="grid gap-2">
              {(widget.options || []).map(option => {
                const selected = selectedOptionID === option.id
                return (
                  <button
                    key={option.id}
                    type="button"
                    onClick={() => setSelectedOptionID(option.id)}
                    className={`rounded-lg border px-3 py-2.5 text-left transition-colors ${selected
                      ? 'border-primary/60 bg-primary/10 text-foreground'
                      : 'border-border bg-background/60 text-foreground hover:border-primary/35 hover:bg-muted/40'
                    }`}
                  >
                    <span className="flex items-center gap-2 text-sm font-medium">
                      <span className={`h-3 w-3 rounded-full border ${selected ? 'border-primary bg-primary' : 'border-muted-foreground/50'}`} />
                      {option.title}
                    </span>
                    {option.description && (
                      <span className="mt-1 block pl-5 text-xs leading-5 text-muted-foreground">{option.description}</span>
                    )}
                  </button>
                )
              })}
            </div>
          )}

          {showsText && (
            <textarea
              value={note}
              onChange={event => setNote(event.target.value)}
              rows={3}
              placeholder={widget.placeholder || (showsOptions ? 'Add an optional note…' : 'Enter your response…')}
              className="w-full resize-y rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground outline-none placeholder:text-muted-foreground focus:border-primary/60 focus:ring-1 focus:ring-primary/30"
            />
          )}

          {error && <div className="text-xs text-destructive">{error}</div>}

          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="min-w-0 text-xs text-muted-foreground">
				{response && response.status !== 'pending' ? (
					<span className="inline-flex items-center gap-1.5">
						<CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
						{response.status === 'completed'
							? 'Applied'
							: response.status === 'executing'
								? 'Applying'
								: response.status === 'failed'
									? 'Action failed'
									: 'Saved'}
                  {response.selected_option_id ? ` · ${responseOptionTitle(widget, response.selected_option_id)}` : ''}
                  {responseTime(response.updated_at) ? ` · ${responseTime(response.updated_at)}` : ''}
                </span>
              ) : (
                'No response yet. The workflow continues normally until you answer.'
              )}
				{response?.outcome_summary && <span className="mt-1 block">Outcome: {response.outcome_summary}</span>}
				{response?.failure_summary && <span className="mt-1 block text-destructive">Failure: {response.failure_summary}</span>}
            </div>
            <button
              type="button"
              onClick={() => { void saveResponse() }}
              disabled={!canSubmit}
              className="inline-flex h-9 shrink-0 items-center gap-2 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-opacity disabled:cursor-not-allowed disabled:opacity-45"
            >
              {submitting ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
				{response && response.status !== 'pending' ? 'Update response' : 'Save response'}
            </button>
          </div>
        </>
      )}
    </div>
  )
}
