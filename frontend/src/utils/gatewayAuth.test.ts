import { describe, expect, it } from 'vitest'
import { gatewayLoginTarget, isGatewayLoginPath } from './gatewayAuth'

describe('gatewayLoginTarget', () => {
  it('accepts the explicit same-origin gateway login signal', () => {
    expect(gatewayLoginTarget(401, '/login?next=%2Fapi%2Fhealth')).toBe('/login?next=%2Fapi%2Fhealth')
  })

  it('ignores unrelated authorization failures', () => {
    expect(gatewayLoginTarget(403, '/login')).toBeNull()
    expect(gatewayLoginTarget(401, undefined)).toBeNull()
  })

  it('rejects external and protocol-relative redirects', () => {
    expect(gatewayLoginTarget(401, 'https://example.com/login')).toBeNull()
    expect(gatewayLoginTarget(401, '//example.com/login')).toBeNull()
  })

  it('does not redirect when an auth-flow page receives a late 401', () => {
    expect(isGatewayLoginPath('/login')).toBe(true)
    expect(isGatewayLoginPath('/login/')).toBe(true)
    expect(isGatewayLoginPath('/auth/callback')).toBe(true)
    expect(isGatewayLoginPath('/auth/callback/')).toBe(true)
    expect(isGatewayLoginPath('/projects/123')).toBe(false)
  })
})
