import { afterEach, describe, expect, it } from 'vitest'

import { formatDeploymentDateTime, getDisplayTimeZone, setDisplayTimeZone } from './displayTime'

afterEach(() => {
  setDisplayTimeZone(undefined)
})

describe('deployment display timezone', () => {
  it('formats timestamps in the configured deployment timezone', () => {
    setDisplayTimeZone('America/New_York')

    expect(getDisplayTimeZone()).toBe('America/New_York')
    const formatted = formatDeploymentDateTime('2026-09-15T03:45:00Z')
    expect(formatted).toContain('14')
    expect(formatted).toMatch(/EDT|GMT-4/)
  })

  it('ignores an invalid configured timezone', () => {
    setDisplayTimeZone('not/a-timezone')
    expect(getDisplayTimeZone()).toBeUndefined()
    expect(formatDeploymentDateTime('2026-09-15T03:45:00Z')).not.toBe('')
  })
})
