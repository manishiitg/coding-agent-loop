import React from 'react'
import { deliveryTickState, deliveryTickTitle } from './deliveryTickState'

// WhatsApp-style delivery receipt for a live-input user row. Shared by the
// terminal transcript and the event-card view; renders nothing without
// delivery metadata so plain query rows are untouched.
export const DeliveryTick: React.FC<{ metadata: Record<string, unknown> | undefined }> = ({ metadata }) => {
  const state = deliveryTickState(metadata)
  if (!state) return null
  const glyph = state === 'confirmed' ? '✓✓' : state === 'unflushed' ? '✓…' : state === 'failed' ? '!' : '✓'
  // Debug-level visibility: near-invisible grays, the glyph alone carries
  // the state (✓ sent, ✓✓ confirmed, ✓… queued, ! failed). Failed keeps
  // only a faint red whisper.
  const tone = state === 'confirmed'
    ? 'text-slate-300 dark:text-slate-500'
    : state === 'unflushed'
      ? 'text-stone-300 dark:text-stone-600'
      : state === 'failed'
        ? 'text-red-300/70 dark:text-red-400/50'
        : 'text-slate-300 dark:text-slate-600'
  return (
    <span data-testid="delivery-tick" data-state={state} title={deliveryTickTitle(metadata, state)} className={`shrink-0 select-none text-[11px] leading-none ${tone}`} aria-label={deliveryTickTitle(metadata, state)}>
      {glyph}
    </span>
  )
}
