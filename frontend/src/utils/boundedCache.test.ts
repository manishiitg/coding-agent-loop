import { describe, expect, it } from 'vitest'
import { createBoundedCache } from './boundedCache'

describe('createBoundedCache', () => {
  it('evicts the least recently used entry', () => {
    const cache = createBoundedCache<string, number>(2)
    cache.set('first', 1)
    cache.set('second', 2)
    expect(cache.get('first')).toBe(1)

    cache.set('third', 3)

    expect(cache.get('second')).toBeUndefined()
    expect(cache.get('first')).toBe(1)
    expect(cache.get('third')).toBe(3)
    expect(cache.size()).toBe(2)
  })

  it('replaces values without growing', () => {
    const cache = createBoundedCache<string, number>(1)
    cache.set('key', 1)
    cache.set('key', 2)

    expect(cache.get('key')).toBe(2)
    expect(cache.size()).toBe(1)
  })

  it('refreshes an intentionally undefined value', () => {
    const cache = createBoundedCache<string, number | undefined>(2)
    cache.set('undefined', undefined)
    cache.set('second', 2)

    expect(cache.get('undefined')).toBeUndefined()
    cache.set('third', 3)

    expect(cache.get('second')).toBeUndefined()
    expect(cache.size()).toBe(2)
  })
})
