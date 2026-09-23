// Forward reads (SSE frames, `since` polls) never legitimately move a durable
// chat cursor backwards: the journal persists across restarts. The one
// exception is the server clamping a stale, too-high cursor to the tip of a
// non-empty journal, which it only does on an empty page. Execution tabs read
// the in-memory event window, whose indices restart after a server restart,
// so they keep whatever the server reports.
export function nextForwardCursor(prior: number, reported: number, eventCount: number, durable: boolean): number {
  if (!durable || reported >= prior) return reported
  return eventCount === 0 && reported > 0 ? reported : prior
}

// A durable chat's journal sequence is shared by every row the tab already
// holds, so a stream must never start below the newest one. Starting at 0
// makes the server replay the whole journal before live delivery.
export function durableStreamStartCursor(stored: number, events: ReadonlyArray<{ sequence?: number }>): number {
  let cursor = stored > 0 ? stored : 0
  for (const event of events) {
    if (typeof event.sequence === 'number' && event.sequence > cursor) cursor = event.sequence
  }
  return cursor
}
