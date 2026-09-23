import { useEffect, useState } from 'react'
import type { CodingAgentQuestionPrompt } from '../utils/cleanConversation'

type ChoiceAnswer = { id: string; selectedLabels: string[] }

export function CodingAgentQuestionCard({ prompt, onAnswer }: {
  prompt: CodingAgentQuestionPrompt
  onAnswer?: (provider: string, promptId: string, answers: ChoiceAnswer[]) => Promise<void>
}) {
  const [selected, setSelected] = useState<Record<string, string[]>>({})
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { setSelected({}); setSubmitting(false); setError('') }, [prompt.provider, prompt.promptId])
  const pending = prompt.state === 'pending'
  const ready = prompt.questions.every((question) => (selected[question.id]?.length || 0) > 0)
  const providerName = prompt.provider === 'muse-cli' ? 'Muse' : prompt.provider === 'claude-code' ? 'Claude' : 'Coding agent'
  const submit = async () => {
    if (!pending || !ready || !onAnswer || submitting) return
    setSubmitting(true)
    setError('')
    try {
      await onAnswer(prompt.provider, prompt.promptId, prompt.questions.map((question) => ({
        id: question.id,
        selectedLabels: question.options.filter((option) => selected[question.id].includes(option.label)).map((option) => option.label),
      })))
      // The native settled event is the acknowledgement; keep controls
      // disabled until that durable event reaches the conversation.
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Could not submit this choice. Refresh and try again.')
      setSubmitting(false)
    }
  }
  return <section className="w-full rounded-2xl border border-violet-300 bg-violet-50 p-4 text-slate-900 dark:border-violet-800 dark:bg-slate-900 dark:text-slate-100" data-testid="coding-agent-question-card">
    <div className="mb-3 text-xs font-semibold uppercase tracking-wide text-violet-700 dark:text-violet-300">{providerName} needs your choice</div>
    {prompt.questions.map((question) => <fieldset key={question.id} className="mb-4 space-y-2" disabled={!pending || submitting || !onAnswer}>
      <legend className="mb-2 text-sm font-medium">{question.question}</legend>
      {question.multiSelect && <p className="mb-2 text-xs text-slate-600 dark:text-slate-400">Select all that apply.</p>}
      {question.options.map((option) => {
        const checked = pending ? (selected[question.id] || []).includes(option.label) : prompt.answers.some((answer) => answer.id === question.id && answer.selectedLabels.includes(option.label))
        return <label key={option.label} className={`flex cursor-pointer gap-2 rounded-lg border px-3 py-2 text-sm ${checked ? 'border-violet-500 bg-violet-100 dark:bg-violet-950' : 'border-slate-200 dark:border-slate-700'}`}>
          <input type={question.multiSelect ? 'checkbox' : 'radio'} name={`${prompt.provider}:${prompt.promptId}:${question.id}`} value={option.label} checked={checked} onChange={() => setSelected((current) => {
            const prior = current[question.id] || []
            const next = question.multiSelect ? (prior.includes(option.label) ? prior.filter((value) => value !== option.label) : [...prior, option.label]) : [option.label]
            return { ...current, [question.id]: next }
          })} />
          <span><span className="block font-medium">{option.label}</span>{option.description && <span className="block text-xs text-slate-600 dark:text-slate-400">{option.description}</span>}</span>
        </label>
      })}
    </fieldset>)}
    {pending ? <button type="button" onClick={submit} disabled={!ready || submitting || !onAnswer} className="rounded-lg bg-violet-600 px-4 py-2 text-sm font-medium text-white disabled:cursor-not-allowed disabled:opacity-50">{submitting ? 'Submitting…' : 'Send choice'}</button>
      : <p className="text-xs text-slate-600 dark:text-slate-400">{prompt.state === 'answered' ? 'Choice submitted' : 'Question interrupted'}</p>}
    {error && <p role="alert" className="mt-2 text-xs text-red-600">{error}</p>}
  </section>
}
