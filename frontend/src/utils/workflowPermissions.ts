import type { AuthUser } from '../services/api'

export function hasWorkflowWriteAccess(user: AuthUser | null | undefined, isMultiUserMode: boolean): boolean {
  if (user?.can_write_workflows !== undefined) {
    return user.can_write_workflows
  }
  if (!isMultiUserMode) {
    return true
  }
  return user?.workflow_access === 'write' || user?.workflow_access === 'owner'
}

export function hasWorkflowOwnerAccess(user: AuthUser | null | undefined, isMultiUserMode: boolean): boolean {
  if (user?.can_manage_workflow_access !== undefined) {
    return user.can_manage_workflow_access
  }
  if (!isMultiUserMode) {
    return true
  }
  return user?.workflow_access === 'owner'
}
