import type { PollingEvent } from '../services/api-types'

export function withLiveInputReceipt(event: PollingEvent, status: string, provider?: string): PollingEvent {
  const outer = event.data as unknown as Record<string, unknown>
  const inner = (outer.data ?? {}) as Record<string, unknown>
  return { ...event, data: { ...outer, data: { ...inner, metadata: {
    ...(inner.metadata as Record<string, unknown> ?? {}),
    source: 'coding_agent_live_input', delivery_status: status, provider,
  } } } as PollingEvent['data'] }
}

export function liveInputReceiptText(metadata: Record<string, unknown> | undefined): string {
  switch (metadata?.delivery_status) {
    case 'sending': return 'Submitting…'
    case 'sent_to_cli':
    case 'queued_for_injection': return 'Queued'
    case 'next_turn_started': return 'Follow-up started'
    default: return ''
  }
}
