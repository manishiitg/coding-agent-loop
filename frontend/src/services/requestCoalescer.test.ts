import { describe, expect, it, vi } from 'vitest'
import { createRequestCoalescer } from './requestCoalescer'

describe('createRequestCoalescer', () => {
  it('shares concurrent reads with the same key', async () => {
    const coalesce = createRequestCoalescer()
    let resolve!: (value: string) => void
    const request = vi.fn(() => new Promise<string>(done => { resolve = done }))

    const requests = Array.from({ length: 20 }, () => coalesce('terminals:all', request))
    const first = requests[0]
    expect(requests.every(pending => pending === first)).toBe(true)
    expect(request).toHaveBeenCalledTimes(1)

    resolve('ok')
    await expect(Promise.all(requests)).resolves.toEqual(Array(20).fill('ok'))
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
