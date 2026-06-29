import { describe, it, expect } from 'vitest'
import { computeXtermWrite, writeWithGeneration, type XtermWritable } from './terminalReplay'

// A mock xterm that records the ordered sequence of reset()/write() calls so we
// can assert ordering for switch / rapid-switch / stale-write scenarios.
class MockXterm implements XtermWritable {
  ops: string[] = []
  reset(): void {
    this.ops.push('reset')
  }
  write(data: string, callback?: () => void): void {
    this.ops.push('write:' + data)
    callback?.()
  }
}

// ReplayDriver mirrors XtermTerminalPane's switch-detect + applyContent DECISION
// (it uses the SAME extracted functions the component uses). It is the minimal
// model needed to assert the reset()/write() ordering and the generation gate:
//   - terminal switch (label change) → hard reset() + bump generation + clear prev
//     (the component's switch-detect block).
//   - then the replay decision (computeXtermWrite): continuity → delta write;
//     break/first → inline-RIS ("\x1bc") full write — generation-gated.
class ReplayDriver {
  prev = ''
  generation = 0
  private lastLabel: string | undefined = undefined
  readonly term: MockXterm
  constructor(term: MockXterm) {
    this.term = term
  }

  apply(label: string, content: string): void {
    if (label !== this.lastLabel) {
      this.term.reset()
      this.generation += 1
      this.prev = ''
      this.lastLabel = label
    }
    if (content === this.prev) return
    const gen = this.generation
    const { reset, data } = computeXtermWrite(this.prev, content)
    const payload = reset ? '\x1bc' + data : data
    if (writeWithGeneration(this.term, payload, gen, this.generation)) {
      this.prev = content
    }
  }
}

describe('computeXtermWrite', () => {
  it('first write (empty prev) → reset + full content', () => {
    expect(computeXtermWrite('', 'hello')).toEqual({ reset: true, data: 'hello' })
  })

  it('same-terminal clean append (prefix-extension) → delta only, no reset', () => {
    expect(computeXtermWrite('line1\n', 'line1\nline2\n')).toEqual({ reset: false, data: 'line2\n' })
  })

  it('continuity break (not a prefix-extension) → reset + full content', () => {
    expect(computeXtermWrite('abc', 'xyz')).toEqual({ reset: true, data: 'xyz' })
  })

  it('resize / single-width backfill (narrow not a prefix of wide) → reset + full (no multi-width stack)', () => {
    const narrow = 'Done — you\'re now\npublishing to\nsurge.sh'
    const wide = 'Done — you\'re now publishing to surge.sh'
    const decision = computeXtermWrite(narrow, wide)
    expect(decision.reset).toBe(true)
    expect(decision.data).toBe(wide)
    // The narrow bytes are NOT carried into the data — the rebuild replaces them.
    expect(decision.data).not.toContain(narrow)
  })

  it('empty prev is never treated as a prefix (avoids a spurious zero-length append)', () => {
    expect(computeXtermWrite('', '')).toEqual({ reset: true, data: '' })
  })
})

describe('writeWithGeneration', () => {
  it('current generation → performs the write', () => {
    const term = new MockXterm()
    const wrote = writeWithGeneration(term, 'payload', 5, 5)
    expect(wrote).toBe(true)
    expect(term.ops).toEqual(['write:payload'])
  })

  it('stale generation → drops the write (a switch bumped the generation)', () => {
    const term = new MockXterm()
    const wrote = writeWithGeneration(term, 'STALE', 4, 5)
    expect(wrote).toBe(false)
    expect(term.ops).toEqual([])
  })

  it('invokes the completion callback only when the write happens', () => {
    const term = new MockXterm()
    let called = 0
    writeWithGeneration(term, 'a', 1, 1, () => { called += 1 })
    writeWithGeneration(term, 'b', 1, 2, () => { called += 1 })
    expect(called).toBe(1)
  })
})

describe('terminal replay ordering (mock xterm)', () => {
  it('N consecutive switches each reset + full write — no accumulation', () => {
    const term = new MockXterm()
    const d = new ReplayDriver(term)
    d.apply('A', 'screenA')
    d.apply('B', 'screenB')
    d.apply('A', 'screenA2') // switch back to A (content changed meanwhile)
    d.apply('C', 'screenC')

    // One reset + one full (inline-RIS) write per switch, in order.
    expect(term.ops).toEqual([
      'reset', 'write:\x1bcscreenA',
      'reset', 'write:\x1bcscreenB',
      'reset', 'write:\x1bcscreenA2',
      'reset', 'write:\x1bcscreenC',
    ])
    // Every content write is a full inline-RIS rebuild — never a bare append across
    // a switch — so nothing from a previous terminal accumulates.
    const writes = term.ops.filter(o => o.startsWith('write:'))
    expect(writes).toHaveLength(4)
    expect(writes.every(w => w.startsWith('write:\x1bc'))).toBe(true)
  })

  it('same-terminal streaming appends only the delta (no reset, no RIS)', () => {
    const term = new MockXterm()
    const d = new ReplayDriver(term)
    d.apply('A', 'line1\n')
    d.apply('A', 'line1\nline2\n')
    d.apply('A', 'line1\nline2\nline3\n')

    expect(term.ops).toEqual([
      'reset', 'write:\x1bcline1\n', // switch-in: reset + full
      'write:line2\n',                // delta only
      'write:line3\n',                // delta only
    ])
  })

  it('same-terminal continuity break → inline-RIS full rebuild (no hard reset, single copy)', () => {
    const term = new MockXterm()
    const d = new ReplayDriver(term)
    d.apply('A', 'aaa')
    d.apply('A', 'XYZ') // not a prefix-extension → rebuild

    expect(term.ops).toEqual([
      'reset', 'write:\x1bcaaa',
      'write:\x1bcXYZ', // inline RIS clears, then full — no second hard reset
    ])
  })

  it('resize single-width backfill replaces, never stacks narrow + wide', () => {
    const term = new MockXterm()
    const d = new ReplayDriver(term)
    const narrow = 'Done\npublishing\nsurge.sh'
    const wide = 'Done publishing surge.sh'
    d.apply('A', narrow)
    d.apply('A', wide) // recorder re-seeded single-width; not a prefix → rebuild

    expect(term.ops).toEqual([
      'reset', 'write:\x1bc' + narrow,
      'write:\x1bc' + wide,
    ])
    // The wide rebuild is a single write; the narrow frame is not appended to it.
    const lastWrite = term.ops[term.ops.length - 1]
    expect(lastWrite).toBe('write:\x1bc' + wide)
    expect(lastWrite).not.toContain(narrow)
  })

  it('rapid switch: a deferred write from the PREVIOUS terminal is dropped after the switch', () => {
    const term = new MockXterm()
    const d = new ReplayDriver(term)
    d.apply('A', 'aaaa') // reset + full A

    // Simulate a write scheduled for terminal A (captures A's generation) that has
    // not executed yet — e.g. a still-queued async xterm write / deferred pager.
    const capturedGen = d.generation

    d.apply('B', 'bbbb') // switch to B → hard reset + generation bump + full B

    const opsBeforeStale = [...term.ops]
    // The deferred A write now lands AFTER the switch → must be dropped.
    const wrote = writeWithGeneration(term, 'STALE-A-DELTA', capturedGen, d.generation)
    expect(wrote).toBe(false)
    // No new op recorded; B's content is intact.
    expect(term.ops).toEqual(opsBeforeStale)
    expect(term.ops).toEqual([
      'reset', 'write:\x1bcaaaa',
      'reset', 'write:\x1bcbbbb',
    ])
  })
})
