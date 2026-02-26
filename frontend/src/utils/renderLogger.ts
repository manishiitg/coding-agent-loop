import { useRef } from 'react'

// Throttle: only log once per component per interval (ms)
const THROTTLE_MS = 500
const lastLogTime: Record<string, number> = {}

function shouldLog(key: string): boolean {
  const now = Date.now()
  if (now - (lastLogTime[key] || 0) < THROTTLE_MS) return false
  lastLogTime[key] = now
  return true
}

/**
 * Lightweight render tracker for debugging performance.
 * Throttled to max 2 logs/sec per component to avoid flooding.
 *
 * Usage:
 *   useRenderLogger('MyComponent', { events: events.length, isStreaming, autoScroll })
 *
 * Output in console:
 *   [Render] MyComponent #14  changed: events (500→501), isStreaming (true→false)
 */
export function useRenderLogger(
  name: string,
  deps: Record<string, unknown> = {}
) {
  const renderCount = useRef(0)
  const prevDeps = useRef<Record<string, unknown>>({})

  renderCount.current++

  const changed: string[] = []
  for (const key of Object.keys(deps)) {
    const prev = prevDeps.current[key]
    const curr = deps[key]
    if (!Object.is(prev, curr)) {
      const fmt = (v: unknown) => {
        if (v === undefined) return 'undefined'
        if (v === null) return 'null'
        if (typeof v === 'object') {
          if (Array.isArray(v)) return `Array(${v.length})`
          return `{…}`
        }
        if (typeof v === 'string' && v.length > 30) return `"${v.slice(0, 27)}…"`
        return String(v)
      }
      changed.push(`${key} (${fmt(prev)}→${fmt(curr)})`)
    }
  }
  prevDeps.current = { ...deps }

  if (!shouldLog(`render:${name}`)) return

  if (changed.length > 0) {
    console.log(`[Render] ${name} #${renderCount.current}  changed: ${changed.join(', ')}`)
  } else {
    console.log(`[Render] ${name} #${renderCount.current}  (no dep change)`)
  }
}

/**
 * Log when a useMemo value is recomputed.
 * Throttled to avoid flooding.
 */
export function useMemoLogger(label: string, value: unknown, summary?: string | number) {
  const prevRef = useRef<unknown>(undefined)
  if (!Object.is(prevRef.current, value)) {
    prevRef.current = value
    if (shouldLog(`memo:${label}`)) {
      const info = summary !== undefined ? ` (${summary})` : ''
      console.log(`[Memo] ${label} recomputed${info}`)
    }
  }
}
