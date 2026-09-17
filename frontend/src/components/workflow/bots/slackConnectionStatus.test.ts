import { describe, expect, it } from 'vitest'
import { slackConnectionStatus } from './slackConnectionStatus'

describe('Slack connection status', () => {
  it('does not treat enabled configuration as a tested connection', () => {
    expect(slackConnectionStatus(true, true, false, false, null)).toBe('Configured · not tested')
    expect(slackConnectionStatus(true, true, false, false, { success: false })).toBe('Test failed')
    expect(slackConnectionStatus(true, true, false, false, { success: true })).toBe('Connected')
  })
  it('shows current activity and disablement ahead of an old success', () => {
    expect(slackConnectionStatus(true, true, false, true, { success: true })).toBe('Testing…')
    expect(slackConnectionStatus(false, true, false, false, { success: true })).toBe('Not connected')
    expect(slackConnectionStatus(true, false, false, false, { success: true })).toBe('Not connected')
  })
})
