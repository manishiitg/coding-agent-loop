export type HydrationGateSnapshot =
  | { status: 'pending'; error: null }
  | { status: 'hydrated'; error: null }
  | { status: 'failed'; error: Error }

export interface HydrationGate {
  wait: () => Promise<HydrationGateSnapshot>
  settle: (error?: unknown) => HydrationGateSnapshot
  snapshot: () => HydrationGateSnapshot
}

const toError = (error: unknown): Error => {
  if (error instanceof Error) return error
  return new Error(typeof error === 'string' ? error : 'Unknown hydration error')
}

/**
 * One completion gate shared by every consumer of a persisted store. It is
 * intentionally independent from the store module so synchronous hydration
 * callbacks cannot touch bindings that are still in their initialization TDZ.
 */
export function createHydrationGate(): HydrationGate {
  let current: HydrationGateSnapshot = { status: 'pending', error: null }
  let resolve!: (snapshot: HydrationGateSnapshot) => void
  const completion = new Promise<HydrationGateSnapshot>((done) => {
    resolve = done
  })

  return {
    wait: () => current.status === 'pending' ? completion : Promise.resolve(current),
    settle: (error?: unknown) => {
      if (current.status !== 'pending') return current
      current = error === undefined
        ? { status: 'hydrated', error: null }
        : { status: 'failed', error: toError(error) }
      resolve(current)
      return current
    },
    snapshot: () => current,
  }
}
