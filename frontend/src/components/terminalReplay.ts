// Pure, testable core of the xterm terminal-replay contract used by
// TerminalCenter / XtermTerminalPane. Extracted so the replay decision can be
// unit-tested without a DOM or a real xterm. The component imports and calls it;
// it does NOT duplicate the logic.
//
// CROSS-TERMINAL ISOLATION is handled structurally, NOT here: the pane is
// rendered with key={terminal_id}, so React remounts (disposes + recreates) the
// xterm on every terminal switch. A fresh instance has an empty buffer and no
// stale parse queue, so this function only ever compares content WITHIN one
// terminal's stream.
//
// SAME-TERMINAL REPLAY CONTRACT (established in 674bacca, unchanged):
//   - continuity (newContent is a prefix-extension of a NON-empty prevContent)
//     → append ONLY the new byte delta, no reset.
//   - otherwise (first write with empty prev, or a continuity break where
//     newContent is not a prefix-extension — e.g. the recorder trimmed / re-seeded
//     at a new width) → reset and write the full newContent. The component
//     realises the reset via an inline RIS ("\x1bc") prefix on the write so the
//     clear is parsed in-order.

/**
 * computeXtermWrite is the single replay decision for same-terminal content
 * updates: it decides WHAT to write and whether the buffer must be cleared first.
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
