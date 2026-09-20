import React from 'react'
import { Check, CheckCheck, CircleAlert, Clock } from 'lucide-react'
import { deliveryTickState, deliveryTickTitle } from './deliveryTickState'

const TICK_ICON = {
  confirmed: CheckCheck,
  unflushed: Clock,
  failed: CircleAlert,
  fast: Check,
} as const

// Icon delivery receipt for a live-input user row. Shared by the terminal
// transcript and the event-card view; renders nothing without delivery
// metadata so plain query rows are untouched. Receipts stay visible.
export const DeliveryTick: React.FC<{ metadata: Record<string, unknown> | undefined }> = ({ metadata }) => {
  const state = deliveryTickState(metadata)
  if (!state) return null
  const Icon = TICK_ICON[state]
  // Debug-level visibility: near-invisible grays. Failed keeps only a
  // faint red whisper.
  const tone = state === 'confirmed'
    ? 'text-slate-300 dark:text-slate-500'
    : state === 'unflushed'
      ? 'text-stone-300 dark:text-stone-600'
      : state === 'failed'
        ? 'text-red-300/70 dark:text-red-400/50'
        : 'text-slate-300 dark:text-slate-600'
  return (
    <span data-testid="delivery-tick" data-state={state} title={deliveryTickTitle(metadata, state)} className={`shrink-0 select-none leading-none ${tone}`} aria-label={deliveryTickTitle(metadata, state)}>
      <Icon className="h-3 w-3" aria-hidden="true" />
    </span>
  )
}
