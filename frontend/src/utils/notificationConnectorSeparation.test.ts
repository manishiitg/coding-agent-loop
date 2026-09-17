import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('bot and notification settings separation', () => {
  it('keeps email in its own Setup section and webhooks in Notifications', () => {
    const bots = readFileSync('src/components/workflow/WorkflowBotsPanel.tsx', 'utf8')
    expect(bots).not.toContain('Slack Incoming Webhook')
    expect(bots).toContain('This {scopeNoun} answers on')
    expect(bots).not.toContain('Gmail')
    const email = readFileSync('src/components/workflow/WorkflowEmailPanel.tsx', 'utf8')
    expect(email).toContain('<GmailNotifications')
  })

  it('exposes Notifications at workflow level and keeps it out of the global header', () => {
    const header = readFileSync('src/components/ModePresetBar.tsx', 'utf8')
    const workflowToolbar = readFileSync('src/components/workflow/canvas/WorkflowToolbar.tsx', 'utf8')
    // Notify (like Pulse/Backup/Publish) is a registry view now: the toolbar
    // button navigates the pane, and the host's InspectorBody switch is what
    // actually mounts the view component.
    const host = readFileSync('src/components/workflow/canvas/WorkspaceViewHost.tsx', 'utf8')
    const notifications = readFileSync('src/components/workflow/WorkflowNotificationView.tsx', 'utf8')
    expect(header).not.toContain('notification-settings-button')
    expect(header).not.toContain('bot-connector')
    expect(workflowToolbar).toContain('data-testid="workflow-notification-settings-button"')
    // The bots button gets its tour hook from CAPABILITY_BUTTON_ATTRS, so the
    // attribute is no longer a JSX literal; the hook itself must still be in
    // the toolbar and nowhere near the global header.
    expect(workflowToolbar).toContain("'data-tour': 'bot-connector'")
    expect(workflowToolbar).toContain("openFromToolbarMenu('notify')")
    expect(host).toContain('<WorkflowNotificationView')
    expect(workflowToolbar).toContain('Icon={BellRing}')
    expect(notifications).toContain('Agentic notification delivery')
    expect(notifications).toContain('Workflow Slack webhook')
    expect(notifications).toContain('Gmail account channel')
    expect(notifications).toContain('Change with')
    expect(notifications).not.toContain('My routing')
  })
})
