import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('bot and notification settings separation', () => {
  it('keeps Gmail and workflow webhooks out of the interactive Bots modal', () => {
    const bots = readFileSync('src/components/settings/BotConnectorModal.tsx', 'utf8')
    expect(bots).not.toContain('Gmail')
    expect(bots).not.toContain('Slack Incoming Webhook')
    expect(bots).toContain('Interactive channels')
  })

  it('exposes a distinct Notifications header control and one-way settings', () => {
    const header = readFileSync('src/components/ModePresetBar.tsx', 'utf8')
    const notifications = readFileSync('src/components/settings/NotificationPreferencesModal.tsx', 'utf8')
    expect(header).toContain('data-testid="notification-settings-button"')
    expect(header).toContain('<BellRing')
    expect(notifications).toContain('Slack Incoming Webhook')
    expect(notifications).toContain('Gmail notifications')
    expect(notifications).toContain('It is never used for OTPs, approvals, or human_feedback')
  })
})
