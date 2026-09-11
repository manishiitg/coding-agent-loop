import { describe, expect, it } from 'vitest'
import { gmailConnectionUsesSharedOAuthClient } from './gmailSharedOAuthClient'

describe('gmailConnectionUsesSharedOAuthClient', () => {
  it('requires confirmation for host and legacy connections without a named client', () => {
    expect(gmailConnectionUsesSharedOAuthClient({})).toBe(true)
    expect(gmailConnectionUsesSharedOAuthClient({ client_name: '  ' })).toBe(true)
  })

  it('does not interrupt isolated named-client connections', () => {
    expect(gmailConnectionUsesSharedOAuthClient({ client_name: 'ops-mailbox' })).toBe(false)
  })
})
