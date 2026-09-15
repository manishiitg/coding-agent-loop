// Shared links identify assets, never credentials or access grants.
export function sharedLink(base: string, kind: 'file' | 'folder', path: string, uid?: string): string {
  const bytes = new TextEncoder().encode(path)
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  const params = new URLSearchParams({ path: btoa(binary) })
  if (uid) params.set('uid', uid)
  return `${base.replace(/\/$/, '')}/${kind}?${params.toString()}`
}
export const SHARE_RETURN_KEY = 'agentworks:share-return'
const SHARE_RETURN_OAUTH_PREFIX = `${SHARE_RETURN_KEY}:oauth:`

export function sharedReturnPath(value: string | null): string | null {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return null
  const url = new URL(value, 'https://agentworks.invalid')
  if (url.origin !== 'https://agentworks.invalid') return null
  const kinds = ['file', 'folder', 'report']
  const queryRoute = kinds.some(kind => url.pathname === `/${kind}`) && url.searchParams.has('path')
  const legacyRoute = kinds.some(kind => url.pathname.startsWith(`/${kind}/`) && url.pathname.length > kind.length + 2)
  if (!queryRoute && !legacyRoute) return null
  return url.pathname + url.search
}

/**
 * Preserve a share destination across OAuth. sessionStorage is the normal
 * same-tab path; the state-bound localStorage copy covers providers and mail
 * clients that complete sign-in in a replacement browsing context.
 */
export function rememberSharedReturnPath(value: string | null, oauthState: string): void {
  const target = sharedReturnPath(value)
  try {
    if (target) sessionStorage.setItem(SHARE_RETURN_KEY, target)
    else sessionStorage.removeItem(SHARE_RETURN_KEY)
  } catch {
    // Storage can be unavailable in hardened/private browser contexts.
  }
  try {
    const key = SHARE_RETURN_OAUTH_PREFIX + oauthState
    if (target) localStorage.setItem(key, target)
    else localStorage.removeItem(key)
  } catch {
    // The same-tab session copy remains the primary path.
  }
}

/** Read and clear the validated OAuth return destination. */
export function consumeSharedReturnPath(oauthState: string): string | null {
  let sessionValue: string | null = null
  let fallbackValue: string | null = null
  try {
    sessionValue = sessionStorage.getItem(SHARE_RETURN_KEY)
    sessionStorage.removeItem(SHARE_RETURN_KEY)
  } catch {
    // Fall through to the state-bound durable copy.
  }
  try {
    const key = SHARE_RETURN_OAUTH_PREFIX + oauthState
    fallbackValue = localStorage.getItem(key)
    localStorage.removeItem(key)
  } catch {
    // Returning to the app root remains the safe final fallback.
  }
  return sharedReturnPath(sessionValue) || sharedReturnPath(fallbackValue)
}
