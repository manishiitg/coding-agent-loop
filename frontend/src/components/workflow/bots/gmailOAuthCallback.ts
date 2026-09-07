export const GMAIL_OAUTH_CALLBACK_PATH = '/api/human-feedback/gmail/auth/callback'

// Gmail OAuth is completed by the API server, not by the frontend shell. On
// hosted web installs both normally share an origin. Desktop can render from a
// file/local origin while talking to a different local or remote API server,
// so its configured API base must win.
export function resolveGmailOAuthCallbackUrl(
  apiBaseUrl: string,
  browserOrigin: string,
): string {
  const base = apiBaseUrl.trim() || browserOrigin.trim()
  if (!base) return GMAIL_OAUTH_CALLBACK_PATH

  try {
    const resolved = new URL(GMAIL_OAUTH_CALLBACK_PATH, base)
    if (resolved.protocol !== 'http:' && resolved.protocol !== 'https:') {
      return GMAIL_OAUTH_CALLBACK_PATH
    }
    return resolved.toString()
  } catch {
    return GMAIL_OAUTH_CALLBACK_PATH
  }
}
