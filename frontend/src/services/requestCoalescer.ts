export type RequestCoalescer = <T>(key: string, request: () => Promise<T>) => Promise<T>

/**
 * Shares one in-flight promise for identical reads. The entry is removed after
 * either success or failure so later polling intervals still fetch fresh data.
 */
export function createRequestCoalescer(): RequestCoalescer {
  const inFlight = new Map<string, Promise<unknown>>()

  return <T>(key: string, request: () => Promise<T>): Promise<T> => {
    const existing = inFlight.get(key)
    if (existing) return existing as Promise<T>

    let requestResult: Promise<T>
    try {
      requestResult = request()
    } catch (error) {
      requestResult = Promise.reject(error)
    }

    let pending: Promise<T>
    pending = requestResult
      .finally(() => {
        if (inFlight.get(key) === pending) inFlight.delete(key)
      })
    inFlight.set(key, pending)
    return pending
  }
}
