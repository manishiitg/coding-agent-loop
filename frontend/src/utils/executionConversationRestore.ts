import { conversationToRestoredEvents } from '../../shared/session/restore'
import { agentApi } from '../services/api'
import type { PollingEvent } from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'
import { resolveLiveInputConfirmations } from './liveInputReceipt'

export type ExecutionConversationState = {
  status: string
  hasRunningBackgroundAgents: boolean
  isSyntheticTurn: boolean
  canSteer: boolean
  restoredEvents: PollingEvent[]
}

// Execution histories are diagnostic artifacts, not interactive chats. Keep
// their JSON reader explicit so it can never become a fallback in ChatArea's
// canonical SQLite restore path.
export async function hydrateExecutionConversation(
  sessionId: string,
  workspacePath?: string,
): Promise<ExecutionConversationState> {
  const [conversation, runtime] = await Promise.all([
    agentApi.getChatHistoryResumeConversation(sessionId, workspacePath, 100, 0, true),
    agentApi.getSessionEvents(sessionId, undefined, { limit: 1 }).catch(() => null),
  ])
  const events = resolveLiveInputConfirmations(conversationToRestoredEvents(conversation))
  const chatStore = useChatStore.getState()
  chatStore.setTabEvents(sessionId, events)
  chatStore.setTabHasMoreOlderEvents(sessionId, conversation.history_pagination?.has_more ?? false)
  chatStore.setTabHistoryPagination(sessionId, conversation.history_pagination
    ? { hasMore: conversation.history_pagination.has_more, nextOffset: conversation.history_pagination.next_offset }
    : null)
  return {
    status: runtime?.session_status || 'completed',
    hasRunningBackgroundAgents: runtime?.has_running_background_agents ?? false,
    isSyntheticTurn: runtime?.is_synthetic_turn ?? false,
    canSteer: runtime?.can_steer ?? false,
    restoredEvents: events,
  }
}
