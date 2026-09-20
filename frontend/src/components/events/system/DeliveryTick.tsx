import React from 'react'
import { deliveryTickState, deliveryTickTitle } from './deliveryTickState'

// WhatsApp-style delivery receipt for a live-input user row. Shared by the
// terminal transcript and the event-card view; renders nothing without
// delivery metadata so plain query rows are untouched.
export const DeliveryTick: React.FC<{ metadata: Record<string, unknown> | undefined }> = ({ metadata }) => {
  const state = deliveryTickState(metadata)
  if (!state) return null
  const glyph = state === 'confirmed' ? '✓✓' : state === 'unflushed' ? '✓…' : state === 'failed' ? '!' : '✓'
  const tone = state === 'confirmed'
    ? 'text-cyan-700 dark:text-cyan-300'
    : state === 'unflushed'
      ? 'text-amber-600 dark:text-amber-400'
      : state === 'failed'
        ? 'text-red-500 dark:text-red-400'
        : 'text-slate-400 dark:text-slate-500'
  return (
    <span data-testid="delivery-tick" data-state={state} title={deliveryTickTitle(metadata, state)} className={`shrink-0 select-none text-[11px] leading-none ${tone}`} aria-label={deliveryTickTitle(metadata, state)}>
      {glyph}
    </span>
  )
}
