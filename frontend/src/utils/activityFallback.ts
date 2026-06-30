import type { ChatTab } from '../stores/useChatStore'

export function isLocalActivityFallbackTab(tab: ChatTab): boolean {
  if (!tab.hasRunningBgAgents) return false
  if (tab.isCompleted) return false
  return tab.isStreaming || tab.isSyntheticTurn || tab.canSteer
}
