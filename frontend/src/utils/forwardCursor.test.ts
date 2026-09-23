import { describe, expect, it } from 'vitest'
import { nextForwardCursor } from './forwardCursor'

describe('nextForwardCursor', () => {
  it('advances on newer rows', () => {
    expect(nextForwardCursor(5, 9, 4, true)).toBe(9)
  })

  it('never resets a durable cursor to zero on an empty forward page', () => {
    expect(nextForwardCursor(120, 0, 0, true)).toBe(120)
  })

  it('ignores a backward cursor that arrives with rows', () => {
    expect(nextForwardCursor(120, 80, 3, true)).toBe(120)
  })

  it('accepts the server clamping a stale cursor to a non-empty journal tip', () => {
    expect(nextForwardCursor(5000, 140, 0, true)).toBe(140)
  })

  it('lets execution tabs follow in-memory indices that restart after a restart', () => {
    expect(nextForwardCursor(5000, 3, 3, false)).toBe(3)
  })
})
