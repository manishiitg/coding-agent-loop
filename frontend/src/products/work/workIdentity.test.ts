import { describe, expect, it } from 'vitest'
import { isWorkIdentityComplete } from './workIdentity'

describe('isWorkIdentityComplete', () => {
  it('requires both role and purpose', () => {
    expect(isWorkIdentityComplete(undefined, undefined)).toBe(false)
    expect(isWorkIdentityComplete(null, null)).toBe(false)
    expect(isWorkIdentityComplete({}, '')).toBe(false)
    expect(isWorkIdentityComplete({ icon: '🤖', name: 'Helper' }, 'Help out.')).toBe(false)
    expect(isWorkIdentityComplete({ role: 'Helper' }, '')).toBe(false)
    expect(isWorkIdentityComplete({}, 'Help out.')).toBe(false)
    expect(isWorkIdentityComplete({ role: '  ' }, 'Help out.')).toBe(false)
    expect(isWorkIdentityComplete({ role: 'Helper' }, 'Help out.')).toBe(true)
  })
})
