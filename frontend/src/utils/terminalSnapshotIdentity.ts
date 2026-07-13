import type { TerminalSnapshot } from '../services/api-types'

const isRecord = (value: unknown): value is Record<string, unknown> => (
  value !== null && typeof value === 'object' && !Array.isArray(value)
)

const jsonValueEqual = (left: unknown, right: unknown): boolean => {
  if (Object.is(left, right)) return true
  if (Array.isArray(left) || Array.isArray(right)) {
    if (!Array.isArray(left) || !Array.isArray(right) || left.length !== right.length) return false
    return left.every((value, index) => jsonValueEqual(value, right[index]))
  }
  if (!isRecord(left) || !isRecord(right)) return false

  const leftKeys = Object.keys(left)
  const rightKeys = Object.keys(right)
  if (leftKeys.length !== rightKeys.length) return false
  return leftKeys.every(key => (
    Object.prototype.hasOwnProperty.call(right, key) && jsonValueEqual(left[key], right[key])
  ))
}

/**
 * Reuses terminal snapshots from the previous poll when their JSON payload is
 * unchanged. Poll ordering is not treated as a change because the terminal
 * rail applies its own stable sort before rendering.
 */
export function reconcileTerminalSnapshots(
  current: TerminalSnapshot[],
  incoming: TerminalSnapshot[],
): TerminalSnapshot[] {
  if (current.length === 0) return incoming.length === 0 ? current : incoming
  if (incoming.length === 0) return incoming

  const currentByID = new Map(current.map(terminal => [terminal.terminal_id, terminal]))
  const incomingIDs = new Set(incoming.map(terminal => terminal.terminal_id))
  let changed = current.length !== incoming.length
  const reconciled = incoming.map(terminal => {
    const existing = currentByID.get(terminal.terminal_id)
    if (existing && jsonValueEqual(existing, terminal)) return existing
    changed = true
    return terminal
  })

  if (!changed && current.every(terminal => incomingIDs.has(terminal.terminal_id))) {
    return current
  }
  return reconciled
}
