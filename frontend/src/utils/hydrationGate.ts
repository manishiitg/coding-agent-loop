export type HydrationGateSnapshot =
  | { status: 'pending'; error: null }
  | { status: 'hydrated'; error: null }
  | { status: 'failed'; error: Error }

export interface HydrationGate {
  wait: () => Promise<HydrationGateSnapshot>
  settle: (error?: unknown) => HydrationGateSnapshot
  snapshot: () => HydrationGateSnapshot
}

export interface HydrationGateOptions {
  backstopMs?: number
  backstopMessage?: string
}

export class HydrationBackstopError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'HydrationBackstopError'
  }
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
export function createHydrationGate(options: HydrationGateOptions = {}): HydrationGate {
  let current: HydrationGateSnapshot = { status: 'pending', error: null }
  let backstopTimer: ReturnType<typeof setTimeout> | null = null
  let resolve!: (snapshot: HydrationGateSnapshot) => void
  const completion = new Promise<HydrationGateSnapshot>((done) => {
    resolve = done
  })

  const settle = (error?: unknown): HydrationGateSnapshot => {
    if (current.status !== 'pending') return current
    if (backstopTimer !== null) {
      clearTimeout(backstopTimer)
      backstopTimer = null
    }
    current = error === undefined
      ? { status: 'hydrated', error: null }
      : { status: 'failed', error: toError(error) }
    resolve(current)
    return current
  }

  const armBackstop = (): void => {
    if (!options.backstopMs || options.backstopMs <= 0 || backstopTimer !== null) return
    backstopTimer = setTimeout(() => {
      settle(new HydrationBackstopError(
        options.backstopMessage || `Store hydration did not finish within ${options.backstopMs}ms`,
      ))
    }, options.backstopMs)
  }

  return {
    wait: () => {
      if (current.status !== 'pending') return Promise.resolve(current)
      armBackstop()
      return completion
    },
    settle,
    snapshot: () => current,
  }
}
