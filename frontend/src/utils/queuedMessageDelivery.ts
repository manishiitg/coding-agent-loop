/**
 * How a queued message should reach an agent whose turn is still running.
 *
 * There are two live-delivery mechanisms and they are not interchangeable:
 *
 * - `live-query` — POST /api/query with preferLiveInput. Single-entry routing
 *   for retained coding CLIs and interactive workflow chats: the backend sends
 *   to the provider's native live-input transport and owns turn-boundary races.
 * - `steer` — agentApi.sendLiveInput, injecting into an in-flight API-provider
 *   turn. This is what the steer button on a queued chip does. Deliberately not
 *   used for coding CLIs, which route through /api/query instead.
 * - `wait` — no live path; the queue drain sends it once the turn ends.
 *
 * Structured workflow-step turns are excluded by the caller because they have
 * no retained process. API providers with an explicit steer capability use the
 * separate steer endpoint.
 *
 * Auto-notifications are never delivered mid-turn. Interrupting a running agent
 * with step-completion noise is not worth it, and they lose nothing by waiting.
 */
export type QueuedMessageRoute = 'live-query' | 'steer' | 'wait'

export function routeForQueuedMessage(params: {
  isStreaming: boolean
  hasSession: boolean
  canUseLiveQuery: boolean
  canSteer: boolean
}): QueuedMessageRoute {
  const { isStreaming, hasSession, canUseLiveQuery, canSteer } = params
  // Only the mid-turn case is decided here. An idle chat is the queue drain's job.
  if (!isStreaming || !hasSession) return 'wait'
  if (canUseLiveQuery) return 'live-query'
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
