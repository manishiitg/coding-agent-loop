import { describe, expect, it, vi } from 'vitest'
import { createHydrationGate } from './hydrationGate'

describe('createHydrationGate', () => {
  it('shares one pending promise across all waiters', async () => {
    const gate = createHydrationGate()
    const first = gate.wait()
    const second = gate.wait()

    expect(first).toBe(second)
    expect(gate.snapshot()).toEqual({ status: 'pending', error: null })

    gate.settle()
    await expect(first).resolves.toEqual({ status: 'hydrated', error: null })
    await expect(second).resolves.toEqual({ status: 'hydrated', error: null })
  })

  it('settles once and returns immediately to later callers', async () => {
    const gate = createHydrationGate()
    gate.settle()

    await expect(gate.wait()).resolves.toEqual({ status: 'hydrated', error: null })
    expect(gate.settle(new Error('late error'))).toEqual({ status: 'hydrated', error: null })
  })

  it('surfaces hydration failures without leaving callers blocked', async () => {
    const gate = createHydrationGate()
    const waiter = gate.wait()
    const error = new Error('storage unavailable')

    gate.settle(error)

    await expect(waiter).resolves.toEqual({ status: 'failed', error })
    expect(gate.snapshot()).toEqual({ status: 'failed', error })
  })

  it('does not require a timeout to release concurrent callers', async () => {
    vi.useFakeTimers()
    const gate = createHydrationGate()
    const waiter = gate.wait()

    gate.settle()
    await expect(waiter).resolves.toMatchObject({ status: 'hydrated' })
    expect(vi.getTimerCount()).toBe(0)
    vi.useRealTimers()
  })
})
