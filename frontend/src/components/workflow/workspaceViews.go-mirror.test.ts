import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { PRIMARY_WORKSPACE_TOOLBAR_VIEWS, WORKSPACE_VIEWS } from './workspaceViews'
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
      'learnings',
      'knowledgebase',
      'database',
      'files',
    ])
  })
})
