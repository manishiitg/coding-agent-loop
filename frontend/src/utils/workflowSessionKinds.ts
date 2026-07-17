export interface WorkflowSessionIdentity {
  sessionId?: string | null
  triggeredBy?: string | null
  botPlatform?: string | null
}

/**
 * Scheduled sessions are an independent read-only lane. This applies to both
 * workflow schedules and Chief of Staff schedules.
 */
export function isScheduledSession(identity: WorkflowSessionIdentity): boolean {
  const trigger = (identity.triggeredBy || '').toLowerCase()
  const sessionId = (identity.sessionId || '').toLowerCase()

  return trigger.includes('schedule') ||
    trigger === 'cron' ||
    sessionId.startsWith('schedule-') ||
    sessionId.includes('-schedule-')
}

/**
 * Schedule and bot runs are independent, read-only workflow lanes. They may run
 * alongside the workflow's single interactive builder chat.
 */
export function isExternalReadOnlyWorkflowSession(identity: WorkflowSessionIdentity): boolean {
  const trigger = (identity.triggeredBy || '').toLowerCase()
  const sessionId = (identity.sessionId || '').toLowerCase()
  const botPlatform = (identity.botPlatform || '').toLowerCase()

  return isScheduledSession(identity) ||
    trigger.includes('bot') ||
    trigger.includes('whatsapp') ||
    trigger.includes('slack') ||
    botPlatform !== '' ||
    sessionId.startsWith('bot-')
}
