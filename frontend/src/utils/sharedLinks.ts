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
export function sharedReturnPath(value: string | null): string | null {
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.includes('\\')) return null
  const url = new URL(value, 'https://agentworks.invalid')
  if (url.origin !== 'https://agentworks.invalid' || !['/file', '/folder', '/report'].includes(url.pathname) || !url.searchParams.has('path')) return null
  return url.pathname + url.search
}
