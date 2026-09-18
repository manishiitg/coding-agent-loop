import type { ChatHistorySession } from '../services/api-types'
import type { PollingEvent } from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { isExternalReadOnlyWorkflowSession } from './workflowSessionKinds'

/** Only structured execution metadata may select a run; chat text is not a run identity. */
export function historyExecutionRunFolder(session: ChatHistorySession, events: ReadonlyArray<PollingEvent>): string | null {
  if (session.run_folder && session.run_folder !== 'new') return session.run_folder
  for (let index = events.length - 1; index >= 0; index--) {
    let data: unknown = events[index].data
    for (let depth = 0; depth < 4 && data && typeof data === 'object'; depth++) {
      const fields = data as Record<string, unknown>
      const folder = fields.run_folder ?? fields.selected_run_folder
      if (typeof folder === 'string' && folder.trim() && folder !== 'new') return folder.trim()
      data = fields.data
    }
  }
  return null
}

export function openHistoryExecutionLogs(session: ChatHistorySession): void {
  if (!isExternalReadOnlyWorkflowSession({ sessionId: session.session_id, botPlatform: session.bot_platform })) return
  const folder = historyExecutionRunFolder(session, useChatStore.getState().getTabEvents(session.session_id))
  const workflow = useWorkflowStore.getState()
  workflow.setSelectedRunFolder(folder)
  workflow.openWorkspaceView('execution-logs', `history:${session.session_id}`)
}
