import type { ChatTab } from '../../stores/useChatStore'
import type { PollingEvent } from '../../services/api-types'


const WORKFLOW_CHAT_CONTENT_EVENT_TYPES = new Set(['user_message', 'conversation_end', 'unified_completion'])

export function hasWorkflowChatContent(events?: PollingEvent[]): boolean {
  return (events || []).some(event => WORKFLOW_CHAT_CONTENT_EVENT_TYPES.has(event.type || ''))
}

// createChatTab always mints a fresh crypto.randomUUID() sessionId for a
// brand-new tab, even one with zero conversation behind it -- so a tab's
// `sessionId` alone is truthy even when nothing has ever been loaded into
// it. The real "already has something" signal is whether that session has
// any actual content. Shared by WorkflowLayout.tsx ("+ New chat" reuse) and
// ChatArea.tsx (rename-on-first-message) -- lives here rather than in either
// of those two files since they import each other (ChatArea <- WorkflowLayout)
// and a shared helper needs a leaf module both can safely import from.
export function workflowTabAlreadyHasContent(tab: ChatTab | undefined, tabEvents: Record<string, PollingEvent[]>): boolean {
  return Boolean(tab?.sessionId) && hasWorkflowChatContent(tabEvents[tab!.sessionId!])
}

// "Which blank Chat tab can take a restored conversation" now lives in
// utils/workflowTabResolution.ts (blankWorkflowBuilderTabId), the one
// definition shared by every opener.

/** Detect the legacy corruption produced when Restore reused a read-only
 * runtime tab. A legitimate read-only Schedule restore never carries
 * restoredConversationPath; that marker belongs to interactive restoration. */
export function isMisclassifiedRestoredWorkflowChat(tab: ChatTab): boolean {
  return Boolean(
    tab.metadata?.mode === 'workflow' &&
    tab.metadata?.isViewOnly === true &&
    tab.config?.restoredConversationPath,
  )
}

// Convert an observed scheduled/bot conversation into an editable Builder tab
// without changing the logical conversation identity. The same session ID is
// what lets the backend steer a retained tmux or resume its native CLI handle.
export function convertObservedWorkflowTabToInteractive(tab: ChatTab): ChatTab {
  return {
    ...tab,
    name: 'Automation Builder',
    metadata: {
      ...tab.metadata,
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Automation Builder',
      workflowId: tab.metadata?.workflowId,
      isViewOnly: false,
      isScheduledRun: false,
      scheduledJobName: undefined,
      isBotRun: false,
      botPlatform: undefined,
      readOnlyRestoredAt: undefined,
      userInteractiveContinuation: true,
    },
  }
}
