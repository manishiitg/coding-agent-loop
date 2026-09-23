import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchCompleteSessionEvents, SessionEventsHTTPError } from './events'

const cfg = { baseUrl: 'http://agent.test', token: () => 'tok', durableChat: true }

function page(ids: number[], hasMore: boolean) {
  return {
    events: ids.map(seq => ({ id: `e${seq}`, type: 'user_message', sequence: seq })),
    has_more: hasMore,
    oldest_sequence: ids[0],
    latest_sequence: ids[ids.length - 1],
  }
}

function mockFetch(responses: Array<{ status?: number; body?: unknown }>) {
  const calls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string) => {
    calls.push(url)
    const next = responses.shift() ?? { status: 500 }
    const status = next.status ?? 200
    return { ok: status < 400, status, json: async () => next.body } as Response
  }))
  return calls
}

afterEach(() => vi.unstubAllGlobals())

describe('fetchCompleteSessionEvents', () => {
  it('pages backwards with before_sequence and returns the whole history oldest-first', async () => {
    const calls = mockFetch([
      { body: page([5, 6], true) },
      { body: page([3, 4], true) },
      { body: page([1, 2], false) },
    ])
    const result = await fetchCompleteSessionEvents(cfg, 'sess-1', { pageSize: 2 })

    expect(result.truncated).toBe(false)
    expect(result.events.map(e => e.id)).toEqual(['e1', 'e2', 'e3', 'e4', 'e5', 'e6'])
    expect(calls[0]).not.toContain('before_sequence')
    expect(calls[1]).toContain('before_sequence=5')
    expect(calls[2]).toContain('before_sequence=3')
  })

  it('stops at the page cap and reports truncation', async () => {
    mockFetch([{ body: page([9, 10], true) }, { body: page([7, 8], true) }])
    const result = await fetchCompleteSessionEvents(cfg, 'sess-1', { pageSize: 2, maxPages: 2 })

    expect(result.truncated).toBe(true)
    expect(result.events.map(e => e.id)).toEqual(['e7', 'e8', 'e9', 'e10'])
  })

  it('surfaces the HTTP status so callers can treat 404 as no conversation', async () => {
    mockFetch([{ status: 404 }])
    const err = await fetchCompleteSessionEvents(cfg, 'missing').catch(e => e)

    expect(err).toBeInstanceOf(SessionEventsHTTPError)
    expect((err as SessionEventsHTTPError).status).toBe(404)
  })
})
