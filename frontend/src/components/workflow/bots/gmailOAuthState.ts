import type { GmailConnection } from '../../../services/api-types'

// A reconnect is complete only when this OAuth attempt changed the persisted
// connection. `ready` alone is insufficient: an account may already have a
// valid older credential when the user starts Reconnect.
export function gmailOAuthAttemptCompleted(
  before: GmailConnection | undefined,
  after: GmailConnection | undefined,
): boolean {
  return Boolean(
    after?.ready &&
    after.updated_at &&
    after.updated_at !== before?.updated_at,
  )
}
