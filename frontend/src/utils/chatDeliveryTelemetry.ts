import type { PollingEvent } from '../services/api-types'

export type ChatDeliveryTelemetryPhase =
  | 'sse_received'
  | 'poll_received'
  | 'catchup_received'
  | 'processed'
  | 'painted'

export type ChatDeliveryTelemetryEvent = {
  sequence: number
  phase: ChatDeliveryTelemetryPhase
  session_id: string
  event_id?: string
  event_type?: string
  transport?: string
  tab_id?: string
  client_time: string
  performance_ms: number
  server_event_time?: string
}

export type ChatDeliveryTelemetryBatch = {
  page_id: string
  events: ChatDeliveryTelemetryEvent[]
}

type TelemetryTransport = (batch: ChatDeliveryTelemetryBatch) => Promise<void>

const CHAT_DELIVERY_EVENT_TYPES = new Set([
  'user_message',
  'conversation_thinking',
  'llm_generation_end',
  'unified_completion',
  'conversation_error',
  'agent_error',
])
const queue: ChatDeliveryTelemetryEvent[] = []
let timer: number | undefined
let sequence = 0
let pageID = ''
let telemetryTransport: TelemetryTransport | undefined

function getPageID(): string {
  if (!pageID) {
    pageID = typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID()
      : `page-${Date.now()}-${Math.random().toString(36).slice(2)}`
  }
  return pageID
}

export function configureChatDeliveryTelemetryTransport(transport: TelemetryTransport): void {
  telemetryTransport = transport
}

export function isChatDeliveryTelemetryEvent(event: Pick<PollingEvent, 'type'>): boolean {
  return typeof event.type === 'string' && CHAT_DELIVERY_EVENT_TYPES.has(event.type)
}

function flush(): void {
  timer = undefined
  const events = queue.splice(0, 100)
  if (events.length === 0) return
  const transport = telemetryTransport
  if (transport) {
    void transport({ page_id: getPageID(), events }).catch(() => {
      // Diagnostics must never delay, retry, or alter chat delivery.
    })
  }
  if (queue.length > 0) timer = window.setTimeout(flush, 100)
}

/** Record content-free browser milestones for visible chat events. */
export function recordChatDeliveryTelemetry(
  phase: ChatDeliveryTelemetryPhase,
  sessionId: string,
  events: PollingEvent[],
  transport: string,
  tabId?: string,
): void {
  if (typeof window === 'undefined' || !sessionId) return
  for (const event of events) {
    if (!isChatDeliveryTelemetryEvent(event)) continue
    queue.push({
      sequence: ++sequence,
      phase,
      session_id: sessionId,
      event_id: event.id || undefined,
      event_type: event.type || undefined,
      transport,
      tab_id: tabId,
      client_time: new Date().toISOString(),
      performance_ms: Math.round(performance.now() * 1000) / 1000,
      server_event_time: event.timestamp || undefined,
    })
  }
  if (queue.length === 0 || timer !== undefined) return
  timer = window.setTimeout(flush, 100)
}
