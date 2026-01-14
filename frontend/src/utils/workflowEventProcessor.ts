/**
 * Workflow Event Processor Utility
 *
 * This module provides utilities for extracting workflow information from events.
 * It consolidates event processing logic that was previously duplicated across components.
 */

import type { PollingEvent } from '../services/api-types'
import { getTypedEventData } from '../generated/event-types'
import { EVENT_TYPES } from '../constants/runningWorkflows'

/**
 * Workflow information extracted from events
 */
export interface WorkflowEventInfo {
  /** Step progress information */
  progress?: {
    completed_step_indices?: number[]
    total_steps: number
  }
  /** Title of the current or last completed step */
  stepTitle?: string
  /** Step ID of the currently executing step */
  currentStepId?: string
  /** Step index of the currently executing step */
  currentStepIndex?: number
  /** Name of the currently executing agent */
  agentName?: string
  /** Current orchestrator phase */
  orchestratorPhase?: string
  /** Number of agent turns (from agent_end events) */
  agentTurns?: number
  /** Total context tokens used (from agent_end events) */
  contextTokens?: number
  /** Last tool that was called */
  lastToolName?: string
  /** Server name of the last tool */
  lastToolServerName?: string
  /** Turn number when tool was called */
  lastToolTurn?: number
  /** Context usage percentage */
  contextUsagePercent?: number
  /** Input tokens used (from tool_call_end events) */
  inputTokens?: number
  /** Total context window size (from tool_call_end events) */
  totalTokens?: number
  /** Model ID/name (from tool_call_end events) */
  modelId?: string
  /** Final result text from the last unified_completion event */
  finalResult?: string
  /** Current batch group ID (from batch_group_start events) */
  currentGroupId?: string
  /** Current batch group index (from batch_group_start events) */
  currentGroupIndex?: number
  /** Total batch groups (from batch_group_start events) */
  totalGroups?: number
  /** Current batch run folder (from batch_group_start events) */
  currentRunFolder?: string
}

/**
 * Extract workflow information from events for a session.
 *
 * Processes events in order and returns the latest state. This function
 * consolidates logic for extracting progress, step titles, agent info,
 * and tool call information from event streams.
 *
 * @param events - Array of polling events to process
 * @returns Extracted workflow information
 */
export function extractWorkflowInfo(events: PollingEvent[]): WorkflowEventInfo {
  const info: WorkflowEventInfo = {}

  for (const event of events) {
    const pollingEvent = event as PollingEvent

    // Extract orchestrator metadata from event (added by context_aware_bridge)
    const eventData = pollingEvent.data as { metadata?: Record<string, string> } | undefined
    const metadata = eventData?.metadata
    if (metadata) {
      if (metadata.orchestrator_agent_name) {
        info.agentName = metadata.orchestrator_agent_name
      }
      if (metadata.orchestrator_phase) {
        info.orchestratorPhase = metadata.orchestrator_phase
      }
    }

    // Extract step progress data
    const progressData = getTypedEventData(pollingEvent, 'step_progress_updated')
    if (progressData) {
      // Note: step_progress_updated event no longer includes progress details
      // Progress should be loaded from the API separately if needed
      // We can still track the current step ID if available
      if (progressData.current_step_id) {
        // Current step ID is available but we don't store it in info.progress
        // as progress details are not in the event anymore
      }
    }

    // Note: step_execution_start event handling removed
    // Step information should come from step_progress_updated or other events

    // Extract agent name from agent_start events
    const agentStartData = getTypedEventData(pollingEvent, 'agent_start')
    if (agentStartData?.agent_type) {
      info.agentName = agentStartData.agent_type
    }

    // Extract turns and context from agent_end events
    const agentEndData = getTypedEventData(pollingEvent, 'agent_end')
    if (agentEndData) {
      if (agentEndData.total_tokens !== undefined) {
        info.contextTokens = agentEndData.total_tokens
      }
    }

    // Extract tool call info from tool_call_end events
    const toolCallEndData = getTypedEventData(pollingEvent, 'tool_call_end')
    if (toolCallEndData) {
      if (toolCallEndData.tool_name) info.lastToolName = toolCallEndData.tool_name
      if (toolCallEndData.server_name) info.lastToolServerName = toolCallEndData.server_name
      if (toolCallEndData.turn !== undefined) info.lastToolTurn = toolCallEndData.turn
      if (toolCallEndData.context_usage_percent !== undefined) {
        info.contextUsagePercent = toolCallEndData.context_usage_percent
      }
      if (toolCallEndData.context_window_usage !== undefined) {
        info.inputTokens = toolCallEndData.context_window_usage
      }
      if (toolCallEndData.model_context_window !== undefined) {
        info.totalTokens = toolCallEndData.model_context_window
      }
      if (toolCallEndData.model_id) {
        info.modelId = toolCallEndData.model_id
      }
    }

    // Extract final result from unified_completion events (keep the last one)
    const unifiedCompletionData = getTypedEventData(pollingEvent, 'unified_completion')
    if (unifiedCompletionData?.final_result) {
      info.finalResult = unifiedCompletionData.final_result
    }

    // Extract batch group info from batch_group_start events
    const batchGroupStartData = getTypedEventData(pollingEvent, 'batch_group_start')
    if (batchGroupStartData) {
      if (batchGroupStartData.group_id) {
        info.currentGroupId = batchGroupStartData.group_id
      }
      if (batchGroupStartData.group_index !== undefined) {
        info.currentGroupIndex = batchGroupStartData.group_index
      }
      if (batchGroupStartData.total_groups !== undefined) {
        info.totalGroups = batchGroupStartData.total_groups
      }
      if (batchGroupStartData.run_folder) {
        info.currentRunFolder = batchGroupStartData.run_folder
      }
    }

    // Clear batch group info when batch_group_end is received
    const batchGroupEndData = getTypedEventData(pollingEvent, 'batch_group_end')
    if (batchGroupEndData?.group_id === info.currentGroupId) {
      // Only clear if this is the currently tracked group
      info.currentGroupId = undefined
      info.currentGroupIndex = undefined
      info.totalGroups = undefined
      info.currentRunFolder = undefined
    }
  }

  return info
}

/**
 * Check if events contain workflow completion events.
 *
 * Note: Only workflow_end and unified_completion indicate true workflow completion.
 * agent_end and conversation_end are NOT completion events as workflows have multiple
 * agent calls and each agent has its own conversation.
 *
 * @param events - Array of events to check
 * @returns True if events contain workflow completion
 */
export function hasWorkflowCompletion(events: PollingEvent[]): boolean {
  return events.some(e => e.type && EVENT_TYPES.COMPLETION.includes(e.type as any))
}

/**
 * Check if events contain workflow error events.
 *
 * Note: Only orchestrator_error and workflow_error indicate workflow failure.
 * agent_error and conversation_error are NOT treated as workflow failures as the
 * orchestrator handles these and may retry or continue execution.
 *
 * @param events - Array of events to check
 * @returns True if events contain workflow errors
 */
export function hasWorkflowError(events: PollingEvent[]): boolean {
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
