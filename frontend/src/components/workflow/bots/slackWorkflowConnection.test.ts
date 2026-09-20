import { describe, expect, it } from 'vitest'
import { resolveWorkflowSlackConnection } from './slackWorkflowConnection'
import type { SlackConnection } from '../../../services/api-types'

const entry = (overrides: Partial<SlackConnection> & { id: string }): SlackConnection => ({
  display_name: overrides.id,
  enabled: true,
  configured: true,
  is_default: false,
  ...overrides,
})

describe('Workflow Slack connection resolution', () => {
  const connections = [
    entry({ id: 'slack_001', display_name: 'Platform', is_default: true }),
    entry({ id: 'slack_aaa', display_name: 'Alpha', workspace_path: 'Workflow/alpha' }),
  ]

  it('inherits the platform default without a selection', () => {
    const resolved = resolveWorkflowSlackConnection(connections, 'Workflow/alpha', undefined, 'slack_001')
    expect(resolved.effective?.id).toBe('slack_001')
    expect(resolved.own?.id).toBe('slack_aaa')
    expect(resolved.selectionId).toBe('')
  })

  it('prefers the manifest selection over the default', () => {
    const resolved = resolveWorkflowSlackConnection(connections, 'Workflow/alpha', 'slack_aaa', 'slack_001')
    expect(resolved.effective?.id).toBe('slack_aaa')
  })

  it('reports a dangling selection instead of silently falling back', () => {
    const resolved = resolveWorkflowSlackConnection(connections, 'Workflow/alpha', 'slack_ghost', 'slack_001')
    expect(resolved.effective).toBeNull()
    expect(resolved.selectionId).toBe('slack_ghost')
  })

  it('finds nothing without a registry', () => {
    const resolved = resolveWorkflowSlackConnection(undefined, 'Workflow/alpha', undefined, undefined)
    expect(resolved.own).toBeNull()
    expect(resolved.effective).toBeNull()
  })
})
