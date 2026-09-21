import type { ChatTab } from '../stores/useChatStore'

/**
 * A workflow surface may only render a tab owned by the selected workflow.
 *
 * Old persisted builder tabs can predate workflowId. Keep that narrow
 * compatibility path only when there is no explicitly-owned tab for the active
 * workflow. A tab explicitly owned by another workflow must never be accepted.
 */
export function workflowTabBelongsToPreset(
  tab: ChatTab | null | undefined,
  activePresetId: string | null,
  tabs: Record<string, ChatTab>,
): boolean {
  if (!tab || tab.metadata?.mode !== 'workflow') return false

  const tabPresetId = tab.metadata?.workflowId
  if (tabPresetId) return tabPresetId === activePresetId

  if (!activePresetId || tab.metadata?.phaseId !== 'workflow-builder') return false

  const hasExplicitTabForPreset = Object.values(tabs).some(candidate =>
    candidate.metadata?.mode === 'workflow' &&
    candidate.metadata?.workflowId === activePresetId &&
    (candidate.sessionId || candidate.isStreaming)
  )
  return !hasExplicitTabForPreset
}

export function activeWorkflowTabIdForPreset(
  activeTabId: string | null,
  activePresetId: string | null,
  tabs: Record<string, ChatTab>,
): string | undefined {
  const tab = activeTabId ? tabs[activeTabId] : undefined
  return workflowTabBelongsToPreset(tab, activePresetId, tabs) ? activeTabId ?? undefined : undefined
}
