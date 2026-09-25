import type { ChatTab } from '../stores/useChatStore'

export type ActivityType = 'Scheduled' | 'Webhook' | 'Manual' | 'Bot' | 'Chat'

/** Tooltip for each activity type's icon, so every row says what started it. */
export const activityTypeLabels: Record<ActivityType, string> = {
  Scheduled: 'Scheduled run',
  Webhook: 'Webhook or function call',
  Manual: 'Manual run',
  Bot: 'Bot conversation (Slack, WhatsApp…)',
  Chat: 'Chat with a person',
}

export function crewActivityTitle(tab: ChatTab | undefined, fallback: string): string {
  return tab?.metadata?.agentProfileIdentityName?.trim() ||
    tab?.metadata?.agentProfileProjectTitle?.trim() ||
    fallback
}
