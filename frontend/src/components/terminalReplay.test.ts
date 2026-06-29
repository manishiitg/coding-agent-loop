import { describe, it, expect } from 'vitest'
import { computeXtermWrite } from './terminalReplay'

// NOTE on isolation: cross-terminal isolation (no overlap/accumulation/stale
// bytes across a switch) is handled STRUCTURALLY by remounting the pane with
// key={terminal_id} — a switch disposes the old xterm and creates a fresh one,
// so there is nothing to unit-test at the decision layer for switches. These
// tests cover the SAME-terminal streaming decision (computeXtermWrite), which is
// the only replay logic that runs within one terminal instance.

describe('computeXtermWrite', () => {
  it('first write (empty prev) → reset + full content', () => {
    expect(computeXtermWrite('', 'hello')).toEqual({ reset: true, data: 'hello' })
  })

  it('same-terminal clean append (prefix-extension) → delta only, no reset', () => {
    expect(computeXtermWrite('line1\n', 'line1\nline2\n')).toEqual({ reset: false, data: 'line2\n' })
  })

  it('repeated clean appends keep emitting only the newest delta', () => {
    expect(computeXtermWrite('a', 'ab')).toEqual({ reset: false, data: 'b' })
    expect(computeXtermWrite('ab', 'abc')).toEqual({ reset: false, data: 'c' })
    expect(computeXtermWrite('abc', 'abcde')).toEqual({ reset: false, data: 'de' })
  })

  it('continuity break (not a prefix-extension) → reset + full content', () => {
    expect(computeXtermWrite('abc', 'xyz')).toEqual({ reset: true, data: 'xyz' })
  })

  it('resize / single-width re-seed (narrow not a prefix of wide) → reset + full (no multi-width stack)', () => {
    const narrow = 'Done — you\'re now\npublishing to\nsurge.sh'
    const wide = 'Done — you\'re now publishing to surge.sh'
    const decision = computeXtermWrite(narrow, wide)
    expect(decision.reset).toBe(true)
    expect(decision.data).toBe(wide)
    // The narrow bytes are NOT carried into the rebuild data.
    expect(decision.data).not.toContain(narrow)
  })

  it('empty prev is never treated as a prefix (avoids a spurious zero-length append)', () => {
    expect(computeXtermWrite('', '')).toEqual({ reset: true, data: '' })
  })

  it('shrink / trim (new content shorter, not a prefix) → reset + full', () => {
    expect(computeXtermWrite('line1\nline2\nline3\n', 'line2\nline3\n')).toEqual({
      reset: true,
      data: 'line2\nline3\n',
    })
  })
})
