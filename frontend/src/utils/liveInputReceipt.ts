import type { PollingEvent } from '../services/api-types'

export function withLiveInputReceipt(event: PollingEvent, status: string, provider?: string, messageId?: string): PollingEvent {
  const outer = event.data as unknown as Record<string, unknown>
  const inner = (outer.data ?? {}) as Record<string, unknown>
  const existing = (inner.metadata ?? {}) as Record<string, unknown>
  // An accepted HTTP ack is the fast pane confirmation (single tick).
  // Never downgrade: a durability verdict that already landed (for
  // example via a replayed event racing the ack) always wins.
  const accepted = status === 'sent_to_cli' || status === 'next_turn_started' || status === 'queued_for_injection'
  const confirmation = existing.confirmation ?? (accepted ? 'fast' : undefined)
  return { ...event, data: { ...outer, data: { ...inner, metadata: {
    ...existing,
    source: 'coding_agent_live_input', delivery_status: status, provider,
    ...(messageId?.trim() ? { message_id: messageId.trim() } : {}),
    ...(confirmation !== undefined ? { confirmation } : {}),
  } } } as PollingEvent['data'] }
}

export type LiveInputConfirmationOutcome = 'confirmed' | 'accepted_but_unflushed' | 'failed'

export interface LiveInputConfirmationUpdate {
  messageId: string
  outcome: LiveInputConfirmationOutcome
  proofSource?: string
  latencyMs?: number
  provider?: string
}

// withDeliveryConfirmation stamps the durability half of the receipt
// (double tick) onto a live-input row. It keeps delivery_status and
// message_id intact and never downgrades a confirmed row: a stale
// duplicate arriving after confirmation must not un-confirm it.
export function withDeliveryConfirmation(event: PollingEvent, update: LiveInputConfirmationUpdate): PollingEvent {
  const outer = event.data as unknown as Record<string, unknown>
  const inner = (outer.data ?? {}) as Record<string, unknown>
  const existing = (inner.metadata ?? {}) as Record<string, unknown>
  if (existing.confirmation === 'confirmed') return event
  return { ...event, data: { ...outer, data: { ...inner, metadata: {
    ...existing,
    confirmation: update.outcome,
    ...(update.proofSource ? { proof_source: update.proofSource } : {}),
    ...(update.latencyMs !== undefined ? { latency_ms: update.latencyMs } : {}),
    ...(update.provider ? { provider: update.provider } : {}),
    confirmed_at: new Date().toISOString(),
  } } } as PollingEvent['data'] }
}

// applyLiveInputConfirmation upgrades every row carrying update.messageId.
// Rows without the id (other turns, other sessions' echoes) pass through
// untouched, so replayed confirm events are safe to re-apply.
export function applyLiveInputConfirmation(events: PollingEvent[], update: LiveInputConfirmationUpdate): PollingEvent[] {
  if (!update.messageId) return events
  return events.map(event => {
    const outer = event.data as unknown as Record<string, unknown> | undefined
    const inner = (outer?.data ?? {}) as Record<string, unknown>
    const metadata = (inner.metadata ?? {}) as Record<string, unknown>
    if (metadata.message_id !== update.messageId) return event
    return withDeliveryConfirmation(event, update)
  })
}

// stampLiveInputIdentity transfers a suppressed backend echo's identity
// onto the surviving optimistic row WITHOUT flipping its source: a query
// that the backend steered live keeps its normal bubble, and the
// message_id lets the later durability event upgrade it. Rows that
// already carry an id are left alone.
export function stampLiveInputIdentity(event: PollingEvent, messageId: string, deliveryStatus: string, provider?: string): PollingEvent {
  const outer = event.data as unknown as Record<string, unknown>
  const inner = (outer.data ?? {}) as Record<string, unknown>
  const existing = (inner.metadata ?? {}) as Record<string, unknown>
  if (typeof existing.message_id === 'string' && existing.message_id) return event
  if (!messageId?.trim()) return event
  return { ...event, data: { ...outer, data: { ...inner, metadata: {
    ...existing,
    message_id: messageId.trim(),
    delivery_status: deliveryStatus,
    ...(provider ? { provider } : {}),
    ...(existing.confirmation === undefined ? { confirmation: 'fast' } : {}),
  } } } as PollingEvent['data'] }
}

// readLiveInputConfirmation parses a live_input_confirmed wire event.
// It accepts both the top-level fields and the metadata mirror the
// backend sets, since SSE and polling serialize the same envelope.
export function readLiveInputConfirmation(event: PollingEvent): LiveInputConfirmationUpdate | null {
  if (event.type !== 'live_input_confirmed') return null
  const outer = event.data as unknown as Record<string, unknown> | undefined
  const inner = (outer?.data ?? {}) as Record<string, unknown>
  const metadata = (inner.metadata ?? {}) as Record<string, unknown>
  const messageId = (inner.message_id ?? metadata.message_id) as string | undefined
  const outcome = (inner.outcome ?? inner.confirmation ?? metadata.confirmation ?? metadata.outcome) as string | undefined
  if (!messageId || (outcome !== 'confirmed' && outcome !== 'accepted_but_unflushed' && outcome !== 'failed')) return null
  const latencyRaw = (inner.latency_ms ?? metadata.latency_ms) as number | undefined
  return {
    messageId,
    outcome,
    proofSource: ((inner.proof_source ?? metadata.proof_source) as string | undefined) ?? undefined,
    latencyMs: typeof latencyRaw === 'number' ? latencyRaw : undefined,
    provider: ((inner.provider ?? metadata.provider) as string | undefined) ?? undefined,
  }
}
