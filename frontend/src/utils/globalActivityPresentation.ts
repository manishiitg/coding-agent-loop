import type { ChatTab } from '../stores/useChatStore'

export type ActivityType = 'Scheduled' | 'Webhook' | 'Manual' | 'Bot' | 'Chat'

export function showsActivityTypeIcon(type: ActivityType): boolean {
  return type === 'Scheduled' || type === 'Webhook'
}

export function crewActivityTitle(tab: ChatTab | undefined, fallback: string): string {
  return tab?.metadata?.agentProfileIdentityName?.trim() ||
    tab?.metadata?.agentProfileProjectTitle?.trim() ||
    fallback
}
