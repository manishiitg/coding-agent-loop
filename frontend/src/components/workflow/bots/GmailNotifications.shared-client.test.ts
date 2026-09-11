import { describe, expect, it } from 'vitest'
import { gmailConnectionUsesSharedOAuthClient } from './gmailSharedOAuthClient'

describe('gmailConnectionUsesSharedOAuthClient', () => {
  it('requires confirmation for host and legacy connections without a named client', () => {
    expect(gmailConnectionUsesSharedOAuthClient({})).toBe(true)
    expect(gmailConnectionUsesSharedOAuthClient({ client_name: '  ' })).toBe(true)
  })

  it('uses the server verdict for named imports and isolated clients', () => {
    expect(gmailConnectionUsesSharedOAuthClient({ client_name: 'legacy-import', shares_host_oauth_client: true })).toBe(true)
    expect(gmailConnectionUsesSharedOAuthClient({ client_name: 'ops-mailbox', shares_host_oauth_client: false })).toBe(false)
  })
})
