import type { GmailConnection } from '../../../services/api-types'

export function gmailConnectionUsesSharedOAuthClient(
  conn: Pick<GmailConnection, 'client_name'> & Partial<Pick<GmailConnection, 'shares_host_oauth_client'>>,
): boolean {
  // The server compares actual OAuth client IDs, which also catches a legacy
  // host credential imported under a registry name. Fall back to the old shape
  // during a rolling frontend/backend update.
  return conn.shares_host_oauth_client ?? !conn.client_name?.trim()
}
