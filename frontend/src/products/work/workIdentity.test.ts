import { describe, expect, it } from 'vitest'
import { isWorkIdentityComplete } from './workIdentity'

describe('isWorkIdentityComplete', () => {
  it('requires both role and instructions', () => {
    expect(isWorkIdentityComplete(undefined)).toBe(false)
    expect(isWorkIdentityComplete(null)).toBe(false)
    expect(isWorkIdentityComplete({})).toBe(false)
    expect(isWorkIdentityComplete({ icon: '🤖', name: 'Helper' })).toBe(false)
    expect(isWorkIdentityComplete({ role: 'Helper' })).toBe(false)
    expect(isWorkIdentityComplete({ instructions: 'Be brief.' })).toBe(false)
    expect(isWorkIdentityComplete({ role: '  ', instructions: 'Be brief.' })).toBe(false)
    expect(isWorkIdentityComplete({ role: 'Helper', instructions: 'Be brief.' })).toBe(true)
  })
})
