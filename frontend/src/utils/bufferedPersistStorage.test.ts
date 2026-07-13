import { afterEach, describe, expect, it, vi } from 'vitest'
import { createBufferedPersistStorage } from './bufferedPersistStorage'

const createMemoryStorage = () => {
  const values = new Map<string, string>()
  const storage: Storage = {
    get length() {
      return values.size
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(key)
    },
    setItem: (key, value) => {
      values.set(key, value)
    },
  }
  return { storage, values }
}

describe('createBufferedPersistStorage', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('coalesces durable changes and writes only the latest value', () => {
    vi.useFakeTimers()
    const { storage, values } = createMemoryStorage()
    const setItem = vi.spyOn(storage, 'setItem')
    const buffered = createBufferedPersistStorage<{ count: number }>(storage, { delayMs: 250 })

    buffered.setItem('chat-store', { state: { count: 1 }, version: 0 })
    buffered.setItem('chat-store', { state: { count: 2 }, version: 0 })

    expect(setItem).not.toHaveBeenCalled()
    vi.advanceTimersByTime(250)
    expect(setItem).toHaveBeenCalledTimes(1)
    expect(JSON.parse(values.get('chat-store') || '{}').state.count).toBe(2)
  })

  it('skips repeated state references and structurally identical payloads', () => {
    vi.useFakeTimers()
    const { storage } = createMemoryStorage()
    const setItem = vi.spyOn(storage, 'setItem')
    const buffered = createBufferedPersistStorage<{ count: number }>(storage)
    const state = { count: 1 }

    buffered.setItem('chat-store', { state, version: 0 })
    buffered.flush()
    buffered.setItem('chat-store', { state, version: 0 })
    buffered.setItem('chat-store', { state: { count: 1 }, version: 0 })
    vi.runAllTimers()

    expect(setItem).toHaveBeenCalledTimes(1)
  })

  it('flushes pending state before disposal', () => {
    vi.useFakeTimers()
    const { storage, values } = createMemoryStorage()
    const buffered = createBufferedPersistStorage<{ ready: boolean }>(storage)

    buffered.setItem('chat-store', { state: { ready: true }, version: 0 })
    buffered.dispose()

    expect(JSON.parse(values.get('chat-store') || '{}').state.ready).toBe(true)
    expect(vi.getTimerCount()).toBe(0)
  })
})
