// Structural equality for the manifest capabilities: a flat object of
// primitives, string arrays, and one nested plain-JSON `llm_config`.
export function capabilitiesEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== typeof b || a === null || b === null || typeof a !== 'object') return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((item, index) => capabilitiesEqual(item, b[index]))
  }
  const left = a as Record<string, unknown>
  const right = b as Record<string, unknown>
  const keys = new Set([...Object.keys(left), ...Object.keys(right)])
  for (const key of keys) {
    if (!capabilitiesEqual(left[key], right[key])) return false
  }
  return true
}

// Keep unsaved fields the user changed; refresh every untouched field from
// the authoritative manifest so agent edits do not linger as stale selections.
export function mergeRemoteCapabilities<T extends object>(draft: T, loaded: T, remote: T): T {
  const next = { ...remote }
  for (const key of Object.keys(draft) as (keyof T)[]) {
    if (!capabilitiesEqual(draft[key], loaded[key])) next[key] = draft[key]
  }
  return next
}
