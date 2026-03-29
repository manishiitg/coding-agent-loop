import type { PollingEvent } from '../services/api-types'

function getParsedTimestamp(timestamp?: string): number | null {
  if (!timestamp) return null
  const parsed = Date.parse(timestamp)
  return Number.isFinite(parsed) ? parsed : null
}

function getEventIndex(event: PollingEvent): number | null {
  return typeof event.event_index === 'number' ? event.event_index : null
}

export function compareEventsChronologically(
  a: PollingEvent,
  b: PollingEvent,
  sourceOrder?: Map<string, number>
): number {
  if (sourceOrder) {
    const orderA = sourceOrder.get(a.id)
    const orderB = sourceOrder.get(b.id)
    if (orderA !== undefined && orderB !== undefined && orderA !== orderB) {
      return orderA - orderB
    }
  }

  const timeA = getParsedTimestamp(a.timestamp)
  const timeB = getParsedTimestamp(b.timestamp)

  if (timeA !== null && timeB !== null && timeA !== timeB) {
    return timeA - timeB
  }

  const indexA = getEventIndex(a)
  const indexB = getEventIndex(b)
  if (indexA !== null && indexB !== null && indexA !== indexB) {
    return indexA - indexB
  }

  if (timeA !== null && timeB === null) return 1
  if (timeA === null && timeB !== null) return -1

  if (indexA !== null && indexB === null) return 1
  if (indexA === null && indexB !== null) return -1

  return a.id.localeCompare(b.id)
}

export function compareEventsReverseChronologically(a: PollingEvent, b: PollingEvent): number {
  return compareEventsChronologically(b, a)
}

export function summarizeEventForDebug(event: PollingEvent) {
  return {
    id: event.id,
    type: event.type,
    ts: event.timestamp,
    idx: event.event_index,
    cid: event.correlation_id,
    pid: event.parent_id,
  }
}
