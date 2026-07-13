export interface BoundedCache<K, V> {
  get: (key: K) => V | undefined
  set: (key: K, value: V) => void
  delete: (key: K) => boolean
  size: () => number
}

/** Small LRU cache for module-level UI data that must not grow for app life. */
export function createBoundedCache<K, V>(limit: number): BoundedCache<K, V> {
  if (!Number.isInteger(limit) || limit < 1) throw new Error('Cache limit must be a positive integer')
  const entries = new Map<K, V>()

  return {
    get: (key) => {
      if (!entries.has(key)) return undefined
      const value = entries.get(key)
      entries.delete(key)
      entries.set(key, value as V)
      return value
    },
    set: (key, value) => {
      entries.delete(key)
      entries.set(key, value)
      while (entries.size > limit) {
        const oldest = entries.keys().next()
        if (oldest.done) break
        entries.delete(oldest.value)
      }
    },
    delete: key => entries.delete(key),
    size: () => entries.size,
  }
}
