export type DeliveryTickState = 'fast' | 'confirmed' | 'unflushed' | 'failed' | null

export function deliveryTickState(metadata: Record<string, unknown> | undefined): DeliveryTickState {
  if (!metadata) return null
  const confirmation = metadata.confirmation
  if (confirmation === 'confirmed') return 'confirmed'
  if (confirmation === 'accepted_but_unflushed') return 'unflushed'
  if (confirmation === 'failed') return 'failed'
  if (confirmation === 'fast') return 'fast'
  const status = metadata.delivery_status
  if (status === 'sent_to_cli' || status === 'next_turn_started' || status === 'queued_for_injection') return 'fast'
  return null
}

export function deliveryTickTitle(metadata: Record<string, unknown> | undefined, state: Exclude<DeliveryTickState, null>): string {
  const provider = typeof metadata?.provider === 'string' && metadata.provider ? ` ${metadata.provider}` : ''
  const latencyMs = typeof metadata?.latency_ms === 'number' ? metadata.latency_ms : null
  const latency = latencyMs === null ? '' : latencyMs >= 1000 ? ` in ${(latencyMs / 1000).toFixed(1)}s` : ` in ${Math.round(latencyMs)}ms`
  switch (state) {
    case 'confirmed': return `Confirmed in${provider} CLI record${latency}`
    case 'unflushed': return `Held in${provider} CLI queue, awaiting record`
    case 'failed': return `Delivery to${provider} CLI unconfirmed`
    case 'fast': return `Sent to${provider} CLI`
  }
}
