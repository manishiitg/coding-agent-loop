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
