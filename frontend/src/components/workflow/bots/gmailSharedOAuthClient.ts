import type { GmailConnection } from '../../../services/api-types'

export function gmailConnectionUsesSharedOAuthClient(conn: Pick<GmailConnection, 'client_name'>): boolean {
  return !conn.client_name?.trim()
}
