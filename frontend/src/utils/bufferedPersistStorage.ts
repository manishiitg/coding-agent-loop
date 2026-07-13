import type { PersistStorage, StorageValue } from 'zustand/middleware'

export interface BufferedPersistStorage<S> extends PersistStorage<S> {
  flush: () => void
  dispose: () => void
}

interface BufferedPersistStorageOptions {
  delayMs?: number
}

/**
 * JSON persistence that skips identical state references, deduplicates equal
 * payloads, and coalesces durable changes into one browser-storage write.
 */
export function createBufferedPersistStorage<S>(
  storage: Storage,
  options: BufferedPersistStorageOptions = {},
): BufferedPersistStorage<S> {
  const delayMs = options.delayMs ?? 250
  let lastInputState: S | undefined
  let lastPersistedValue: string | null = null
  let pending: { name: string; serialized: string } | null = null
  let timer: ReturnType<typeof setTimeout> | null = null

  const flush = () => {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
    if (!pending) return
    storage.setItem(pending.name, pending.serialized)
    lastPersistedValue = pending.serialized
    pending = null
  }

  return {
    getItem: (name) => {
      const stored = storage.getItem(name)
      lastPersistedValue = stored
      return stored ? JSON.parse(stored) as StorageValue<S> : null
    },
    setItem: (name, value) => {
      if (value.state === lastInputState) return
      lastInputState = value.state

      const serialized = JSON.stringify(value)
      if (serialized === lastPersistedValue) {
        if (timer !== null) {
          clearTimeout(timer)
          timer = null
        }
        pending = null
        return
      }
      if (serialized === pending?.serialized) return

      pending = { name, serialized }
      if (timer !== null) clearTimeout(timer)
      timer = setTimeout(flush, delayMs)
    },
    removeItem: (name) => {
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
      pending = null
      lastInputState = undefined
      lastPersistedValue = null
      storage.removeItem(name)
    },
    flush,
    dispose: () => {
      flush()
    },
  }
}
