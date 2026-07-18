import type { PollingEvent } from '../services/api-types'

const FALLBACK_REQUEST_LIFETIME_MS = 30 * 60 * 1000
const EXPIRY_GRACE_MS = 5 * 1000

export interface HumanFeedbackEventData {
  question?: string
  allow_feedback?: boolean
  context?: string
  session_id?: string
  workflow_id?: string
  request_id?: string
  yes_no_only?: boolean
  yes_label?: string
  no_label?: string
  options?: string[]
  routed_to_parent_chat?: boolean
}

export interface BlockingHumanFeedbackDetails {
  requestId: string
  question: string
  sessionId: string
  timestampMs: number
  expiresAtMs: number
  displayEvent: {
    type: string
    data: HumanFeedbackEventData
    timestamp: string
  }
}

function requestLifetimeMs(context: string): number {
  const match = context.match(/expires\s+in\s+(\d+)\s+seconds?/i)
  if (!match) return FALLBACK_REQUEST_LIFETIME_MS
  const seconds = Number(match[1])
  return Number.isFinite(seconds) && seconds > 0
    ? seconds * 1000
    : FALLBACK_REQUEST_LIFETIME_MS
}

export function getBlockingHumanFeedbackDetails(
  event: PollingEvent,
): BlockingHumanFeedbackDetails | null {
  if (event.type !== 'blocking_human_feedback') return null

  const agentEvent = event.data as Record<string, unknown> | undefined
  const data = agentEvent?.data as HumanFeedbackEventData | undefined
  const requestId = typeof data?.request_id === 'string' ? data.request_id.trim() : ''
  if (!requestId) return null

  const timestamp = event.timestamp || new Date().toISOString()
  const parsedTimestamp = Date.parse(timestamp)
  const timestampMs = Number.isFinite(parsedTimestamp) ? parsedTimestamp : Date.now()
  const context = typeof data?.context === 'string' ? data.context : ''
  const normalizedData: HumanFeedbackEventData = {
    ...data,
    request_id: requestId,
    question: typeof data?.question === 'string' ? data.question : '',
    context,
  }

  return {
    requestId,
    question: normalizedData.question || '',
    sessionId: normalizedData.session_id || event.session_id || '',
    timestampMs,
    expiresAtMs: timestampMs + requestLifetimeMs(context),
    displayEvent: {
      type: 'blocking_human_feedback',
      data: normalizedData,
      timestamp,
    },
  }
}

export function collectPendingHumanFeedback(
  tabEvents: Record<string, PollingEvent[]>,
  isSubmitted: (requestId: string) => boolean,
  nowMs = Date.now(),
): BlockingHumanFeedbackDetails[] {
  const byRequestId = new Map<string, BlockingHumanFeedbackDetails>()

  for (const [sessionId, events] of Object.entries(tabEvents)) {
    for (const event of events) {
      const details = getBlockingHumanFeedbackDetails(event)
      if (!details || isSubmitted(details.requestId)) continue
      if (nowMs > details.expiresAtMs + EXPIRY_GRACE_MS) continue

      const normalized = details.sessionId ? details : { ...details, sessionId }
      const existing = byRequestId.get(details.requestId)
      if (!existing || normalized.timestampMs > existing.timestampMs) {
        byRequestId.set(details.requestId, normalized)
      }
    }
  }

  return Array.from(byRequestId.values()).sort((a, b) => a.timestampMs - b.timestampMs)
}
