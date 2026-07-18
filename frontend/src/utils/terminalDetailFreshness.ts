import type { TerminalSnapshot } from '../services/api-types'

// A body is current only when it came from the latest list snapshot itself or
// from the cache entry keyed by that snapshot's exact revision. A body copied
// from an older revision is useful as a no-flicker fallback for rendering, but
// must never suppress the detail refresh for the new revision.
export function hasFreshTerminalDetailBody(
  listSnapshot: TerminalSnapshot,
  exactCachedDetail?: TerminalSnapshot,
): boolean {
  return Boolean(exactCachedDetail?.content?.trim() || listSnapshot.content?.trim())
}
