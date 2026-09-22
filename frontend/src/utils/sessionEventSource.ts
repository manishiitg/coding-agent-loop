import type { ChatTab } from '../stores/useChatStore'

// Runs are diagnostics, not interactive conversations. Keep this decision in
// one place so restore, pagination, polling, and SSE use the same source.
export function isExecutionConversationTab(tab: ChatTab | null | undefined): boolean {
  const metadata = tab?.metadata
  return Boolean(
    metadata?.isExecutionRun ||
    metadata?.isScheduledRun ||
    metadata?.isBotRun ||
    // Tabs saved before isExecutionRun was introduced still carry the
    // Work automation run's server-owned trigger session identity.
    (metadata?.isViewOnly && metadata?.agentProfileId === 'work' && tab?.sessionId?.includes(':trigger:')),
  )
}
