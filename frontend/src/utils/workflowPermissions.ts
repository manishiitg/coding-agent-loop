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

// Creating an automation is an account-level permission. A contributor can
// edit an automation assigned to them while still being unable to create a
// new one, so workflow write access must not be used as a substitute here.
export function hasWorkflowCreateAccess(user: AuthUser | null | undefined, isMultiUserMode: boolean): boolean {
  if (!isMultiUserMode) {
    return true
  }
  if (user?.is_admin) {
    return true
  }
  if (user?.can_create !== undefined) {
    return user.can_create
  }
  // Compatibility with older servers that did not return can_create.
  return user?.workflow_access === 'owner'
}

// True only once the backend has actually confirmed non-write access (PLAT-262
// read tier) — never a guess from the absence of a signal, which is why this
// is `!hasWorkflowWriteAccess` rather than checking `workflow_access === 'read'`
// directly (that field can be absent even when can_write_workflows is set).
export function isWorkflowReadOnly(user: AuthUser | null | undefined, isMultiUserMode: boolean): boolean {
  return !hasWorkflowWriteAccess(user, isMultiUserMode)
}
