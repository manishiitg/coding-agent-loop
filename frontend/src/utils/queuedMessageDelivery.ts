/**
 * How a queued message should reach an agent whose turn is still running.
 *
 * There are two live-delivery mechanisms and they are not interchangeable:
 *
 * - `live-query` — POST /api/query with preferLiveInput. Kept as an explicit
 *   route type for callers that know a retained CLI is ready, but the automatic
 *   queue drain does not choose it while a turn is running. A CLI may be inside
 *   a tool transaction and unable to acknowledge injected input; treating that
 *   temporary busy state as a failed chat request produced user-visible 409s.
 * - `steer` — agentApi.sendLiveInput, injecting into an in-flight API-provider
 *   turn. This is what the steer button on a queued chip does. Deliberately not
 *   used for coding CLIs, which route through /api/query instead.
 * - `wait` — no live path; the queue drain sends it once the turn ends.
 *
 * Messages for workflow/tmux CLI chats remain queued until the current turn is
 * idle, then the normal queue drain starts the next turn. API providers with an
 * explicit steer capability can still accept a mid-turn steer.
 *
 * Auto-notifications are never delivered mid-turn. Interrupting a running agent
 * with step-completion noise is not worth it, and they lose nothing by waiting.
 */
export type QueuedMessageRoute = 'live-query' | 'steer' | 'wait'

export function routeForQueuedMessage(params: {
  isStreaming: boolean
  hasSession: boolean
  isWorkflowMode: boolean
  isTmuxCLIProvider: boolean
  canSteer: boolean
}): QueuedMessageRoute {
  const { isStreaming, hasSession, isWorkflowMode, isTmuxCLIProvider, canSteer } = params
  // Only the mid-turn case is decided here. An idle chat is the queue drain's job.
  if (!isStreaming || !hasSession) return 'wait'
  if (isTmuxCLIProvider || isWorkflowMode) return 'wait'
  if (canSteer) return 'steer'
  return 'wait'
}

/** Auto-notifications wait for idle; everything else is a person expecting an answer. */
export function splitQueuedMessages(messages: string[], autoNotificationPrefix: string): {
  human: string[]
  auto: string[]
} {
  return {
    human: messages.filter(message => !message.startsWith(autoNotificationPrefix)),
    auto: messages.filter(message => message.startsWith(autoNotificationPrefix)),
  }
}
