import { useEffect, useMemo, useState } from 'react'
import { MessageCircleQuestion, X } from 'lucide-react'
import { agentApi } from '../services/api'
import type { PendingHumanFeedbackRequest } from '../services/api-types'
import { useChatStore } from '../stores'
import { collectPendingHumanFeedback } from '../utils/humanFeedbackAttention'
import {
  hasSubmittedFeedback,
  HUMAN_FEEDBACK_SUBMITTED_EVENT,
} from '../utils/notificationDedup'
import { BlockingHumanFeedbackDisplay } from './events/BlockingHumanFeedbackDisplay'

/**
 * App-level response surface for short-lived human_feedback requests.
 *
 * This intentionally lives outside ChatArea: Tree and Terminal are alternative
 * views of the same run, and neither should control whether urgent human input
 * is visible. The original event card remains in Tree as conversation history.
 */
export function GlobalHumanFeedbackPrompt() {
  const tabEvents = useChatStore((state) => state.tabEvents)
  const chatTabs = useChatStore((state) => state.chatTabs)
  const [submittedRequestIds, setSubmittedRequestIds] = useState<Set<string>>(() => new Set())
  const [serverPending, setServerPending] = useState<PendingHumanFeedbackRequest[]>([])
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [minimized, setMinimized] = useState(false)

  useEffect(() => {
    const recordSubmission = (event: Event) => {
      const requestId = (event as CustomEvent<{ requestId?: string }>).detail?.requestId
      if (!requestId) return
      setSubmittedRequestIds((current) => new Set(current).add(requestId))
    }
    window.addEventListener(HUMAN_FEEDBACK_SUBMITTED_EVENT, recordSubmission)
    return () => window.removeEventListener(HUMAN_FEEDBACK_SUBMITTED_EVENT, recordSubmission)
  }, [])

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 15_000)
    return () => window.clearInterval(timer)
  }, [])

  useEffect(() => {
    let cancelled = false
    const refresh = async () => {
      try {
        const response = await agentApi.getPendingHumanFeedback()
        if (!cancelled) setServerPending(response.requests || [])
      } catch {
        // Event-backed discovery below remains available during transient API errors.
      }
    }
    void refresh()
    const timer = window.setInterval(() => { void refresh() }, 2_000)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  const pending = useMemo(() => {
    const eventPending = collectPendingHumanFeedback(
      tabEvents,
      (requestId) => submittedRequestIds.has(requestId) || hasSubmittedFeedback(requestId),
      nowMs,
    )
    const byRequestId = new Map(eventPending.map((request) => [request.requestId, request]))
    for (const request of serverPending) {
      if (submittedRequestIds.has(request.unique_id) || hasSubmittedFeedback(request.unique_id)) continue
      const expiresAtMs = Date.parse(request.expires_at)
      if (Number.isFinite(expiresAtMs) && nowMs > expiresAtMs) continue
      const timestampMs = Date.parse(request.created_at)
      byRequestId.set(request.unique_id, {
        requestId: request.unique_id,
        question: request.message_for_user,
        sessionId: request.session_id || '',
        timestampMs: Number.isFinite(timestampMs) ? timestampMs : nowMs,
        expiresAtMs: Number.isFinite(expiresAtMs) ? expiresAtMs : nowMs + 30 * 60 * 1000,
        displayEvent: {
          type: 'blocking_human_feedback',
          timestamp: request.created_at,
          data: {
            request_id: request.unique_id,
            question: request.message_for_user,
            context: request.context,
            session_id: request.session_id,
            options: request.options,
            allow_feedback: request.allow_feedback,
          },
        },
      })
    }
    return Array.from(byRequestId.values()).sort((a, b) => a.timestampMs - b.timestampMs)
  }, [tabEvents, nowMs, serverPending, submittedRequestIds])
  const current = pending[0]
  const pendingSignature = pending.map((request) => request.requestId).join('|')

  useEffect(() => {
    setMinimized(false)
  }, [pendingSignature])

  if (!current) return null

  const originTab = Object.values(chatTabs).find((tab) => tab.sessionId === current.sessionId)
  const originLabel = originTab?.name || 'Agent run'
  const waitingCount = pending.length

  if (minimized) {
    return (
      <button
        type="button"
        onClick={() => setMinimized(false)}
        className="fixed bottom-4 right-4 z-[10000] flex items-center gap-2 rounded-lg border border-indigo-300 bg-white px-3 py-2.5 text-left shadow-xl transition-colors hover:bg-indigo-50 dark:border-indigo-700 dark:bg-gray-900 dark:hover:bg-indigo-950"
        aria-label="Open pending human input"
      >
        <span className="relative text-indigo-600 dark:text-indigo-400">
          <MessageCircleQuestion className="h-5 w-5" />
          <span className="absolute -right-1 -top-1 h-2 w-2 rounded-full bg-red-500" />
        </span>
        <span>
          <span className="block text-sm font-medium text-gray-900 dark:text-gray-100">Input required</span>
          <span className="block max-w-64 truncate text-xs text-gray-500 dark:text-gray-400">
            {originLabel}{waitingCount > 1 ? ` · ${waitingCount} waiting` : ''}
          </span>
        </span>
      </button>
    )
  }

  return (
    <section
      className="fixed bottom-4 right-4 z-[10000] w-[min(28rem,calc(100vw-2rem))] rounded-xl border border-indigo-200 bg-white shadow-2xl dark:border-indigo-800 dark:bg-gray-900"
      role="dialog"
      aria-modal="false"
      aria-labelledby="global-human-feedback-title"
    >
      <header className="flex items-start gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <div className="mt-0.5 rounded-lg bg-indigo-100 p-2 text-indigo-600 dark:bg-indigo-950 dark:text-indigo-300">
          <MessageCircleQuestion className="h-5 w-5" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 id="global-human-feedback-title" className="text-sm font-semibold text-gray-900 dark:text-gray-100">
            AgentWorks needs your input
          </h2>
          <p className="mt-0.5 truncate text-xs text-gray-500 dark:text-gray-400">
            {originLabel} is waiting for your response
            {waitingCount > 1 ? ` · ${waitingCount} requests pending` : ''}
          </p>
        </div>
        <button
          type="button"
          onClick={() => setMinimized(true)}
          className="shrink-0 rounded p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-800 dark:hover:text-gray-200"
          aria-label="Respond later"
          title="Later"
        >
          <X className="h-4 w-4" />
        </button>
      </header>

      <div className="max-h-[min(34rem,70vh)] overflow-y-auto px-3 pb-3">
        <BlockingHumanFeedbackDisplay
          key={current.requestId}
          event={current.displayEvent}
          onApprove={(requestId) => { void agentApi.submitHumanFeedback(requestId, 'Approve') }}
          onSubmitFeedback={(requestId, feedback) => agentApi.submitHumanFeedback(requestId, feedback)}
        />
      </div>
    </section>
  )
}
