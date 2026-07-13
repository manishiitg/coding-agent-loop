import { describe, expect, it, vi } from 'vitest'
import { createRequestCoalescer } from './requestCoalescer'

describe('createRequestCoalescer', () => {
  it('shares concurrent reads with the same key', async () => {
    const coalesce = createRequestCoalescer()
    let resolve!: (value: string) => void
    const request = vi.fn(() => new Promise<string>(done => { resolve = done }))

    const first = coalesce('terminals:all', request)
    const second = coalesce('terminals:all', request)
    expect(first).toBe(second)
    expect(request).toHaveBeenCalledTimes(1)

    resolve('ok')
    await expect(first).resolves.toBe('ok')
  })

  it('does not combine different request keys', async () => {
    const coalesce = createRequestCoalescer()
    const request = vi.fn(async (value: string) => value)

    await Promise.all([
      coalesce('terminals:one', () => request('one')),
      coalesce('terminals:two', () => request('two')),
    ])

    expect(request).toHaveBeenCalledTimes(2)
  })

  it('allows a retry after a failed request', async () => {
    const coalesce = createRequestCoalescer()
    const request = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce('recovered')

    await expect(coalesce('active-sessions', request)).rejects.toThrow('offline')
    await expect(coalesce('active-sessions', request)).resolves.toBe('recovered')
    expect(request).toHaveBeenCalledTimes(2)
  })
})
