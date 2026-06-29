// Pure, testable core of the xterm terminal-replay contract used by
// TerminalCenter / XtermTerminalPane. Extracted (additively) so the replay
// decisions can be unit-tested without a DOM or a real xterm. The component
// imports and calls these functions; it does NOT duplicate the logic.
//
// REPLAY CONTRACT (established in 674bacca, unchanged here — see XtermTerminalPane):
//   - Terminal switch / mount: the component hard-resets the xterm (term.reset(),
//     which clears buffer + scrollback), bumps the write generation, and clears
//     the tracked previous content. The next write therefore starts from offset 0.
//   - Same-terminal streaming: when the new content is a prefix-extension of the
//     previous content, write ONLY the new byte delta (no reset) — see
//     computeXtermWrite({reset:false}).
//   - Continuity break: when the new content is NOT a prefix-extension of the
//     previous content (recorder trimmed, re-seeded at a new width, or a switch
//     reset prev to ''), reset and write the full content — computeXtermWrite
//     ({reset:true}). The component realises the reset via an inline RIS ("\x1bc")
//     prefix on the write so the clear is parsed in-order.
// There is exactly ONE write path (applyContent); nothing writes to the xterm
// outside it except the resize carry-over repaint, which is also generation-gated.

/**
 * Minimal slice of the xterm Terminal API the generation-gated writer needs.
 * Lets tests pass a mock that records reset()/write() calls.
 */
export interface XtermWritable {
  write(data: string, callback?: () => void): void
}

/**
 * computeXtermWrite is the SINGLE replay decision shared by every content write
 * into the xterm:
 *   - continuity (newContent is a prefix-extension of a NON-empty prevContent)
 *     → append ONLY the new byte delta, no reset.
 *   - otherwise (first write with empty prev, or a continuity break where
 *     newContent is not a prefix-extension) → reset and write the full newContent.
 *
 * This mirrors the logic established in 674bacca; it does not change it. The
 * component decides HOW to realise a reset (inline RIS for same-terminal rebuilds,
 * a hard term.reset() for a terminal switch) — this function only decides WHAT to
 * write and whether the buffer must be cleared first.
 */
export function computeXtermWrite(
  prevContent: string,
  newContent: string,
): { reset: boolean; data: string } {
  const appendOnlyPrefix = prevContent !== '' && newContent.startsWith(prevContent)
  if (appendOnlyPrefix) {
    return { reset: false, data: newContent.slice(prevContent.length) }
  }
  return { reset: true, data: newContent }
}

/**
 * writeWithGeneration drops a write whose generation is stale. The generation is
 * bumped on every terminal SWITCH (alongside term.reset()). A write captures the
 * generation when it is scheduled and is discarded here if a switch has happened
 * since (currentGeneration !== capturedGeneration), so a deferred / still-pending
 * write from the PREVIOUS terminal cannot land on the new one and corrupt it.
 * Current-terminal writes (captured generation still current) are unaffected.
 * Returns true iff the write was performed.
 */
export function writeWithGeneration(
  term: XtermWritable,
  data: string,
  capturedGeneration: number,
  currentGeneration: number,
  callback?: () => void,
): boolean {
  if (capturedGeneration !== currentGeneration) {
    return false
  }
  term.write(data, callback)
  return true
}
