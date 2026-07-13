import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const createMemoryStorage = (): Storage => {
  const values = new Map<string, string>()
  return {
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
}

describe('useChatStore hydration bootstrap', () => {
  beforeEach(() => {
    vi.resetModules()
    vi.stubGlobal('localStorage', createMemoryStorage())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('finishes synchronous storage hydration without a timeout fallback', async () => {
    const timeoutSpy = vi.spyOn(globalThis, 'setTimeout')
    const chatStore = await import('./useChatStore')

    await chatStore.waitForChatStoreHydration()

    expect(chatStore.getChatStoreHydrationSnapshot()).toEqual({
      status: 'hydrated',
      error: null,
    })
    expect(timeoutSpy.mock.calls.some(([, delay]) => delay === 3000)).toBe(false)
  })
})
