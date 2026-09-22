
/**
 * Workflow Event Processor Utility
 *
 * Small predicates over polled events (completion/error detection,
 * retention). The former extractWorkflowInfo batch/step aggregator was
 * removed on 2026-09-22 with the unemitted batch event family.
 */

import type { PollingEvent } from '../services/api-types'
import { EVENT_TYPES } from '../constants/runningWorkflows'

/**
 * Check if events contain workflow completion events.
 *
 * Note: Only workflow-level end events indicate true workflow completion.
 * agent_end and conversation_end are NOT completion events as workflows have multiple
 * agent calls and each agent has its own conversation.
 *
 * @param events - Array of events to check
 * @returns True if events contain workflow completion
 */
export function hasWorkflowCompletion(events: PollingEvent[]): boolean {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return events.some(e => e.type && EVENT_TYPES.COMPLETION.includes(e.type as any))
}

/**
 * Check if events contain workflow error events.
 *
 * Note: Only workflow_error indicates workflow failure (orchestrator_error was removed: never emitted).
 * agent_error and conversation_error are NOT treated as workflow failures as the
 * orchestrator handles these and may retry or continue execution.
 *
 * @param events - Array of events to check
 * @returns True if events contain workflow errors
 */
export function hasWorkflowError(events: PollingEvent[]): boolean {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return events.some(e => e.type && EVENT_TYPES.ERROR.includes(e.type as any))
}

/**
 * Check if an event should be retained during cleanup.
 *
 * Important events include completion, error, human feedback, and progress events
 * that are critical for understanding workflow state.
 *
 * @param event - Event to check
 * @returns True if event should be retained
 */
export function shouldRetainEvent(event: PollingEvent): boolean {
  if (!event.type) return false
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return EVENT_TYPES.IMPORTANT.includes(event.type as any)
}

/**
 * Clean up old events while retaining important ones.
 *
 * This function implements intelligent event cleanup that:
 * - Always retains important events (completion, errors, progress)
 * - Keeps recent regular events within the specified limit
 * - Sorts events by timestamp for proper ordering
 *
 * @param events - Array of events to clean up
 * @param maxEvents - Maximum number of events to retain
 * @returns Cleaned up events array
 */
export function cleanupEvents(events: PollingEvent[], maxEvents: number): PollingEvent[] {
  if (events.length <= maxEvents) return events

  // Separate important and regular events
  const important = events.filter(shouldRetainEvent)
  const regular = events.filter(e => !shouldRetainEvent(e))

  // Trim important events if they exceed maxEvents
  let trimmedImportant = important
  if (important.length > maxEvents) {
    // Keep only the newest maxEvents important events
    trimmedImportant = important
      .sort((a, b) => {
        const aTime = a.timestamp ? new Date(a.timestamp).getTime() : 0
        const bTime = b.timestamp ? new Date(b.timestamp).getTime() : 0
        return bTime - aTime // Sort newest first
      })
      .slice(0, maxEvents)
  }

  // Calculate budget for regular events (clamped to 0)
  const budget = Math.max(0, maxEvents - trimmedImportant.length)

  // Keep latest regular events within budget
  const keepRegular = budget > 0 ? regular.slice(-budget) : []

  // Combine and sort by timestamp
  return [...trimmedImportant, ...keepRegular].sort((a, b) => {
    const aTime = a.timestamp ? new Date(a.timestamp).getTime() : 0
    const bTime = b.timestamp ? new Date(b.timestamp).getTime() : 0
    return aTime - bTime
  })
}

/**
 * Calculate exponential backoff delay for retries.
 *
 * Uses exponential backoff with jitter to avoid thundering herd.
 *
 * @param attemptNumber - Current attempt number (0-indexed)
 * @param baseDelay - Base delay in milliseconds
 * @param maxDelay - Maximum delay in milliseconds
 * @returns Delay in milliseconds
 */
export function calculateBackoffDelay(
  attemptNumber: number,
  baseDelay: number,
  maxDelay: number
): number {
  // Calculate exponential delay: baseDelay * 2^attemptNumber
  const exponentialDelay = baseDelay * Math.pow(2, attemptNumber)

  // Cap at max delay
  const cappedDelay = Math.min(exponentialDelay, maxDelay)

  // Add jitter (±25%) to avoid thundering herd
  const jitter = cappedDelay * (0.75 + Math.random() * 0.5)

  return Math.floor(jitter)
}
