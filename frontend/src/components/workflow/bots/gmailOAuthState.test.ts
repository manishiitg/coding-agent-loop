import { describe, expect, it } from 'vitest'
import type { GmailConnection } from '../../../services/api-types'
import { gmailOAuthAttemptCompleted } from './gmailOAuthState'

function connection(overrides: Partial<GmailConnection> = {}): GmailConnection {
  return {
    id: 'gmail_001',
    display_name: 'sender@example.com',
    enabled: true,
    is_default: true,
    ready: true,
    auth: { gws_installed: true, authenticated: true, has_gmail_scope: true },
    updated_at: '2026-09-07T10:00:00Z',
    ...overrides,
  }
}

describe('gmailOAuthAttemptCompleted', () => {
  it('does not treat an already-ready credential as a completed reconnect', () => {
    const before = connection()
    expect(gmailOAuthAttemptCompleted(before, connection())).toBe(false)
  })

  it('requires both a newer persisted connection and a ready credential', () => {
    const before = connection()
    expect(gmailOAuthAttemptCompleted(before, connection({ updated_at: '2026-09-07T10:01:00Z' }))).toBe(true)
    expect(gmailOAuthAttemptCompleted(before, connection({ ready: false, updated_at: '2026-09-07T10:01:00Z' }))).toBe(false)
  })

  it('accepts the first persisted timestamp for a legacy undated connection', () => {
    const before = connection({ updated_at: undefined })
    expect(gmailOAuthAttemptCompleted(before, connection({ updated_at: '2026-09-07T10:01:00Z' }))).toBe(true)
  })
})
