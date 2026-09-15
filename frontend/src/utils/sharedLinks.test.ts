// @vitest-environment happy-dom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { consumeSharedReturnPath, rememberSharedReturnPath, sharedLink, sharedReturnPath, SHARE_RETURN_KEY } from './sharedLinks'

describe('Shared file links', () => {
  beforeEach(() => {
    const storage = (): Storage => {
      const values = new Map<string, string>()
      return {
        get length() { return values.size },
        clear: () => values.clear(),
        getItem: key => values.get(key) ?? null,
        key: index => [...values.keys()][index] ?? null,
        removeItem: key => { values.delete(key) },
        setItem: (key, value) => { values.set(key, String(value)) },
      }
    }
    vi.stubGlobal('sessionStorage', storage())
    vi.stubGlobal('localStorage', storage())
  })

  it('preserves Unicode, spaces and query-sensitive characters', () => {
    const path = 'Workflow/testing/db/assets/日本 report + &.pdf'
    const url = new URL(sharedLink('http://127.0.0.1:18743', 'file', path))
    const decoded = new TextDecoder().decode(Uint8Array.from(atob(url.searchParams.get('path')!), c => c.charCodeAt(0)))
    expect(decoded).toBe(path)
    expect(url.searchParams.has('token')).toBe(false)
    expect(url.origin).toBe('http://127.0.0.1:18743')
  })
  it('only restores internal asset pages after OAuth', () => {
    expect(sharedReturnPath('/file?path=YWJj')).toBe('/file?path=YWJj')
    expect(sharedReturnPath('/file/YWJj')).toBe('/file/YWJj')
    for (const value of ['//evil.com/file?path=x', '/\\evil.com/file?path=x', 'https://evil.com/file?path=x', '/admin?path=x', '/file']) expect(sharedReturnPath(value)).toBeNull()
  })

  it('restores the share URL from the OAuth-state fallback when the session context is replaced', () => {
    const target = '/file?path=V29ya2Zsb3cvZXhhbXBsZS9yZXBvcnQucGRm&uid=user-1'
    rememberSharedReturnPath(target, 'oauth-state-1')
    sessionStorage.removeItem(SHARE_RETURN_KEY)

    expect(consumeSharedReturnPath('oauth-state-1')).toBe(target)
    expect(consumeSharedReturnPath('oauth-state-1')).toBeNull()
  })

  it('does not restore another OAuth flow or an unsafe destination', () => {
    rememberSharedReturnPath('/file?path=YWJj', 'oauth-state-1')
    sessionStorage.removeItem(SHARE_RETURN_KEY)

    expect(consumeSharedReturnPath('oauth-state-2')).toBeNull()
    rememberSharedReturnPath('https://evil.example/file?path=YWJj', 'oauth-state-3')
    expect(consumeSharedReturnPath('oauth-state-3')).toBeNull()
  })
})
