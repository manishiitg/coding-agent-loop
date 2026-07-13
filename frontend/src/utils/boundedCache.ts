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
      const value = entries.get(key)
      if (value === undefined) return undefined
      entries.delete(key)
      entries.set(key, value)
      return value
    },
    set: (key, value) => {
      entries.delete(key)
      entries.set(key, value)
      while (entries.size > limit) {
        const oldestKey = entries.keys().next().value as K | undefined
        if (oldestKey === undefined) break
        entries.delete(oldestKey)
      }
    },
    delete: key => entries.delete(key),
    size: () => entries.size,
  }
}
