import { describe, expect, it } from 'vitest'
import { resolveGmailOAuthCallbackUrl } from './gmailOAuthCallback'

describe('resolveGmailOAuthCallbackUrl', () => {
  it('uses the current web origin when the API is same-origin', () => {
    expect(resolveGmailOAuthCallbackUrl('', 'https://agentworks.example.com')).toBe(
      'https://agentworks.example.com/api/human-feedback/gmail/auth/callback',
    )
  })

  it('uses the configured API server for desktop and remote workspaces', () => {
    expect(resolveGmailOAuthCallbackUrl('http://127.0.0.1:8000', 'file://')).toBe(
      'http://127.0.0.1:8000/api/human-feedback/gmail/auth/callback',
    )
    expect(resolveGmailOAuthCallbackUrl('https://remote.example.com/', 'http://localhost:5173')).toBe(
      'https://remote.example.com/api/human-feedback/gmail/auth/callback',
    )
  })

  it('returns the callback path when neither origin is usable', () => {
    expect(resolveGmailOAuthCallbackUrl('', '')).toBe('/api/human-feedback/gmail/auth/callback')
    expect(resolveGmailOAuthCallbackUrl('', 'file://')).toBe('/api/human-feedback/gmail/auth/callback')
  })
})
