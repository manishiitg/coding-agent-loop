import { describe, expect, it } from 'vitest'
import type { PollingEvent } from '../services/api-types'
import { projectSecretsNeedRefresh } from './secretMutationRefresh'

function event(type: 'tool_call_end' | 'tool_execution', toolName: string): PollingEvent {
  return {
    id: 'secret-receipt',
    type,
    data: { type, data: { tool_name: toolName, result: 'saved' } },
  } as PollingEvent
}

describe('projectSecretsNeedRefresh', () => {
  it('refreshes after a project secret is saved or deleted', () => {
    expect(projectSecretsNeedRefresh(event('tool_call_end', 'set_workflow_secret'))).toBe(true)
    expect(projectSecretsNeedRefresh(event('tool_execution', 'delete_workflow_secret'))).toBe(true)
  })

  it('ignores unrelated tools and failed calls', () => {
    expect(projectSecretsNeedRefresh(event('tool_call_end', 'execute_shell_command'))).toBe(false)
    expect(projectSecretsNeedRefresh({ id: 'failed-secret', type: 'tool_call_error', data: { type: 'tool_call_error', data: { tool_name: 'set_workflow_secret' } } } as PollingEvent)).toBe(false)
  })
})
