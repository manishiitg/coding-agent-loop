import type { ChatHistorySession } from '../services/api-types'

export function isScheduledChatHistorySession(session: ChatHistorySession): boolean {
  return session.session_id.startsWith('schedule-') || session.session_id.startsWith('sched_')
}

function runtimeTransport(session: ChatHistorySession): string {
  const direct = session.runtime?.transport?.trim().toLowerCase()
  if (direct) return direct
  return session.runtime?.agent_session_handle?.provider?.transport?.trim().toLowerCase() || ''
}

function supportsNativeResume(session: ChatHistorySession): boolean {
  const runtime = session.runtime
  if (!runtime || runtime.kind !== 'coding_agent' || runtime.resume_supported === false) return false
  const handle = runtime.agent_session_handle?.provider
  return Boolean(
    runtime.resume_supported || runtime.external_session_id?.trim() || runtime.project_dir_id?.trim() ||
    handle?.native_session_id?.trim() || handle?.project_dir_id?.trim()
  )
}

function usesTerminalRestore(session: ChatHistorySession): boolean {
  return session.runtime?.kind === 'coding_agent' && runtimeTransport(session) === 'tmux'
}

/** Historical schedules are observations, not resumable builder conversations. */
export function chatHistoryOpenDisposition(
  session: ChatHistorySession,
): 'read-only-schedule' | 'read-only-history' | 'interactive-transport' | 'interactive-history' {
  if (isScheduledChatHistorySession(session)) return 'read-only-schedule'
  if (session.can_resume === false) return 'read-only-history'
  if (usesTerminalRestore(session) || supportsNativeResume(session)) return 'interactive-transport'
  return 'interactive-history'
}
