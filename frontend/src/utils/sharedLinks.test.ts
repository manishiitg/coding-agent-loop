import { describe, expect, it } from 'vitest'
import { sharedLink, sharedReturnPath } from './sharedLinks'

describe('Shared file links', () => {
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
    for (const value of ['//evil.com/file?path=x', '/\\evil.com/file?path=x', 'https://evil.com/file?path=x', '/admin?path=x', '/file']) expect(sharedReturnPath(value)).toBeNull()
  })
})
