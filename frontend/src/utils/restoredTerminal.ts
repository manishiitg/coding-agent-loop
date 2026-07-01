import { agentApi } from '../services/api'
import { requestTerminalRefreshBurst } from './terminalRefresh'

function requestRestoredTerminalRefreshes() {
  requestTerminalRefreshBurst()
  if (typeof window === 'undefined') return
  for (const delay of [250, 1000, 2500, 5000]) {
    window.setTimeout(requestTerminalRefreshBurst, delay)
  }
}

// reconnectWorkflowTabs and handleResumePreviousChat can both fire a restore for
// the same session on page load. Track in-flight restores so the second caller
// piggybacks on the first instead of launching a duplicate tmux reattach.
const restoreInFlight = new Set<string>()

export function startRestoredTransportTerminal(
  sessionId: string | null | undefined,
  restoredConversationPath: string | null | undefined,
  restoredConversationSessionId?: string | null,
) {
  const targetSessionId = sessionId?.trim()
  const path = restoredConversationPath?.trim()
  if (!targetSessionId || !path) return
  const sourceSessionId = restoredConversationSessionId?.trim()

  const key = `${targetSessionId}:${path}:${sourceSessionId || ''}`
  if (restoreInFlight.has(key)) return
  restoreInFlight.add(key)

  console.info('[RestoredTerminal] POST /chat-history/restored-terminal', {
    sessionId: targetSessionId,
    path,
    restoredConversationSessionId: sourceSessionId,
  })
  requestRestoredTerminalRefreshes()
  void agentApi.startRestoredTerminal({
    session_id: targetSessionId,
    restored_conversation_path: path,
    restored_conversation_session_id: sourceSessionId || undefined,
  }).then((response) => {
    requestRestoredTerminalRefreshes()
    if (response.started) {
      console.info('[RestoredTerminal] terminal restore started', {
        sessionId: targetSessionId,
        hasTerminalSnapshot: Boolean(response.terminal),
      })
    } else {
      console.warn('[RestoredTerminal] Terminal restore did not start', {
        sessionId: targetSessionId,
        reason: response.reason,
        response,
      })
    }
  }).catch((error) => {
    console.warn('[RestoredTerminal] Failed to start restored terminal', { sessionId: targetSessionId, error })
    // Restore should not block on a stale or already-closed terminal transport.
    // The next submitted turn will still recreate the provider session when needed.
  }).finally(() => {
    restoreInFlight.delete(key)
  })
}
