import type { PollingEvent } from '../services/api-types'

export interface EventMemoryStats {
  eventCount: number
  sizeBytes: number
  largestEventBytes: number
  largestEventType: string
}

export const EMPTY_EVENT_MEMORY_STATS: EventMemoryStats = {
  eventCount: 0,
  sizeBytes: 0,
  largestEventBytes: 0,
  largestEventType: '',
}

const estimateEventBytes = (event: PollingEvent): number => {
  try {
    return JSON.stringify(event).length
  } catch {
    return 0
  }
}

export const getEventMemoryStats = (events: PollingEvent[] | undefined): EventMemoryStats => {
  if (!events || events.length === 0) return EMPTY_EVENT_MEMORY_STATS

  let sizeBytes = 0
  let largestEventBytes = 0
  let largestEventType = ''

  for (const event of events) {
    const eventBytes = estimateEventBytes(event)
    sizeBytes += eventBytes
    if (eventBytes > largestEventBytes) {
      largestEventBytes = eventBytes
      largestEventType = event.type || '(unknown)'
    }
  }

  return {
    eventCount: events.length,
    sizeBytes,
    largestEventBytes,
    largestEventType,
  }
}

export const formatEventMemoryBytes = (bytes: number): string => {
  if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`
  return `${bytes} B`
}

export const hasEventMemoryPressure = (stats: EventMemoryStats): boolean => {
  return stats.eventCount >= 1000 || stats.sizeBytes >= 1024 * 1024
}
