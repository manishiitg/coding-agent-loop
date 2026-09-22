import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { normalizeWorkspaceViewId, PRIMARY_WORKSPACE_TOOLBAR_VIEWS, WORKSPACE_VIEWS } from './workspaceViews'
import { UI_CONTROL_CONTRACT } from '../../platform/ui-control/contract.generated'

// The agent's open_workspace_view tool (agent_go/cmd/server/workflow_view_tool.go)
// carries its own copy of the view ids, since Go cannot read this registry.
// Keep the two identical, in order, so a view added here is one the agent can
// open and the agent never offers a view that does not exist.
describe('open_workspace_view mirrors the workspace view registry', () => {
  it('lists exactly the registered view ids', () => {
    const contractFile = resolve(__dirname, '../../../../agent_go/cmd/server/ui_control_contract.json')
    const contract = JSON.parse(readFileSync(contractFile, 'utf8'))
    expect(contract).toEqual(UI_CONTROL_CONTRACT)
    expect(UI_CONTROL_CONTRACT.views.map(v => v.id)).toEqual(WORKSPACE_VIEWS.map(v => v.id))
  })
})

describe('primary workspace toolbar views', () => {
  it('always includes Plan for workflows that do not have steps yet', () => {
    expect(PRIMARY_WORKSPACE_TOOLBAR_VIEWS.map(view => view.id)).toEqual([
      'report',
      'flow',
      'costs',
      'execution-logs',
      'knowledge',
      'webhooks',
      'files',
      'browser',
      'workshop',
    ])
  })

  it('maps schedule and trigger destinations into Automation while retired Setup views fall back', () => {
    expect(WORKSPACE_VIEWS.map(view => view.id)).not.toContain('api-triggers')
    expect(normalizeWorkspaceViewId('api-triggers')).toBe('workshop')
    expect(normalizeWorkspaceViewId('schedules')).toBe('workshop')
    expect(normalizeWorkspaceViewId('webhooks')).toBe('workshop')
    // Bots and Gmail live under the Integrations tabs now: a persisted
    // standalone id remaps to nothing so restore falls back to the default.
    expect(normalizeWorkspaceViewId('bots')).toBeNull()
    expect(normalizeWorkspaceViewId('email')).toBeNull()
    // Secrets and folders live under the Identity tabs now.
    expect(normalizeWorkspaceViewId('secrets')).toBeNull()
    expect(normalizeWorkspaceViewId('folders')).toBeNull()
    expect(normalizeWorkspaceViewId('llm')).toBeNull()
    expect(normalizeWorkspaceViewId('identity')).toBe('identity')
    // Learnings, Knowledgebase, and Database live under the Knowledge tabs.
    expect(normalizeWorkspaceViewId('learnings')).toBe('knowledge')
    expect(normalizeWorkspaceViewId('knowledgebase')).toBe('knowledge')
    expect(normalizeWorkspaceViewId('database')).toBe('knowledge')
  })

  it('orders Setup as Identity, Integrations, then Playbooks', () => {
    const ids = WORKSPACE_VIEWS.map(view => view.id)
    expect(ids.indexOf('identity')).toBeLessThan(ids.indexOf('mcp'))
    expect(ids.indexOf('mcp')).toBeLessThan(ids.indexOf('playbooks'))
  })
})
