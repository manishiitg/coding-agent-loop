import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('bot and notification settings separation', () => {
  it('keeps Gmail and workflow webhooks out of the interactive Bots modal', () => {
    const bots = readFileSync('src/components/settings/BotConnectorModal.tsx', 'utf8')
    expect(bots).not.toContain('Gmail')
    expect(bots).not.toContain('Slack Incoming Webhook')
    expect(bots).toContain('Interactive channels')
  })

  it('exposes Notifications at workflow level and keeps it out of the global header', () => {
    const header = readFileSync('src/components/ModePresetBar.tsx', 'utf8')
    const workflowToolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')
    const notifications = readFileSync('src/components/workflow/WorkflowNotificationPopup.tsx', 'utf8')
    expect(header).not.toContain('notification-settings-button')
    expect(workflowToolbar).toContain('data-testid="workflow-notification-settings-button"')
    expect(workflowToolbar).toContain('<WorkflowNotificationPopup')
    expect(workflowToolbar).toContain('<BellRing')
    expect(notifications).toContain('Agentic notification delivery')
    expect(notifications).toContain('Workflow Slack webhook')
    expect(notifications).toContain('Gmail account channel')
    expect(notifications).toContain('The agent never reads a webhook URL')
    expect(notifications).not.toContain('My routing')
  })
})
