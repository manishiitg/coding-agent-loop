export const GATEWAY_LOGIN_HEADER = 'X-AgentWorks-Login'

export function gatewayLoginTarget(status: number | undefined, headerValue: unknown): string | null {
  if (status !== 401 || typeof headerValue !== 'string') return null

  const target = headerValue.trim()
  if (!target.startsWith('/') || target.startsWith('//')) return null
  return target
}

let redirectInProgress = false

export function isGatewayLoginPath(pathname: string): boolean {
  // /login suppresses redirects to itself; /auth/callback is likewise an
  // auth-flow page that resolves on its own (success navigates away, failure
  // renders its own error UI). Without this, background requests the shell
  // fires while still logged out 401 at the gateway and bounce the page to
  // /login?next=... mid-exchange, orphaning the OAuth flow after the server
  // already issued the token. (Bites on password-gate-disabled deployments,
  // where no gateway cookie covers those requests; Cognito is affected too.)
  return pathname === '/login' || pathname.startsWith('/login/') ||
    pathname === '/auth/callback' || pathname.startsWith('/auth/callback/')
}

export function redirectToGatewayLogin(target: string | null): boolean {
  // API calls already in flight during logout can return after the login
  // screen has mounted. Do not redirect the login screen to itself; doing so
  // creates an ever-growing /login?next=/login?... URL.
  if (!target || typeof window === 'undefined' || redirectInProgress || isGatewayLoginPath(window.location.pathname)) return false
  redirectInProgress = true
  window.location.assign(target)
  return true
}
