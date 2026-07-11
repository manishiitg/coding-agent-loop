import type { PollingEvent } from '../services/api-types'

const AUTO_NOTIFICATION_PREFIX = '[AUTO-NOTIFICATION]'

export function isInternalAutoNotificationEvent(event: PollingEvent): boolean {
  if (event.type !== 'user_message') return false

  const outer = event.data as Record<string, unknown> | undefined
  const inner = outer?.data as Record<string, unknown> | undefined
  const content = inner?.content ?? outer?.content

  return typeof content === 'string' && content.trim().startsWith(AUTO_NOTIFICATION_PREFIX)
}
