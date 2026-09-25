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

const botPlatformNames: Record<string, string> = { slack: 'Slack', whatsapp: 'WhatsApp', telegram: 'Telegram', discord: 'Discord', teams: 'Teams' }

/** The bot's platform name ("Slack", "WhatsApp"), from the session's
 * bot_platform or its "bot-<platform>-" session ID; empty when unknown. */
export function botPlatformLabel(botPlatform: string | undefined, sessionId: string): string {
  const raw = (botPlatform || '').trim().toLowerCase() || (/^bot-([a-z]+)-/i.exec(sessionId)?.[1] || '').toLowerCase()
  if (!raw) return ''
  return botPlatformNames[raw] || raw.charAt(0).toUpperCase() + raw.slice(1)
}
