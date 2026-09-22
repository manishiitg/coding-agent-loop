import { describe, expect, it } from 'vitest'
import type { AuthUser } from '../services/api'
import { hasWorkflowCreateAccess, workflowAccessFor } from './workflowPermissions'

const user = (overrides: Partial<AuthUser>): AuthUser => ({
  id: 'user-1',
  username: 'user',
  ...overrides,
})

describe('hasWorkflowCreateAccess', () => {
  it('allows local single-user mode', () => {
    expect(hasWorkflowCreateAccess(null, false)).toBe(true)
  })

  it('allows administrators and members with create access', () => {
    expect(hasWorkflowCreateAccess(user({ is_admin: true, can_create: false }), true)).toBe(true)
    expect(hasWorkflowCreateAccess(user({ can_create: true }), true)).toBe(true)
  })

  it('does not treat contributor write access as create access', () => {
    expect(hasWorkflowCreateAccess(user({
      can_create: false,
      can_write_workflows: true,
      workflow_access: 'write',
    }), true)).toBe(false)
  })

  it('falls back safely for responses from older servers', () => {
    expect(hasWorkflowCreateAccess(user({ workflow_access: 'owner' }), true)).toBe(true)
    expect(hasWorkflowCreateAccess(user({ workflow_access: 'write' }), true)).toBe(false)
  })

  it('resolves effective per-workflow access from the map', () => {
    const editor = user({ workflow_access: 'write', workflows: { wf_1: 'read', wf_2: 'owner' } })
    expect(workflowAccessFor(editor, 'wf_1')).toBe('read')
    expect(workflowAccessFor(editor, 'wf_2')).toBe('owner')
    expect(workflowAccessFor(editor, 'wf_missing')).toBe(undefined)
    expect(workflowAccessFor(editor, null)).toBe(undefined)
    expect(workflowAccessFor(user({}), 'wf_1')).toBe(undefined)
  })
})
