import { useEffect, useState, type ReactNode } from 'react'
import { Share2, ShieldCheck, Users } from 'lucide-react'
import WorkflowSharePopup from './WorkflowSharePopup'
import UsersAdminPanel from '../admin/UsersAdminPanel'
import { useAuthStore } from '../../stores/useAuthStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { hasWorkflowOwnerAccess } from '../../utils/workflowPermissions'
import { normalizeWorkspacePath } from '../../utils/workspacePathUtils'

type AccessTab = 'workflow' | 'users'

interface WorkflowAccessViewProps {
  workspacePath: string | null
  headerAction?: ReactNode
}

/**
 * Access, as a right-side workspace view like Notify, Pulse and Backup --
 * not a modal (user request, 2026-09-03). Two tabs:
 *
 *  - "This workflow": who may see or edit the open workflow (owners and
 *    read-only readers).
 *  - "Users": the deployment's accounts, roles and passwords (admins only).
 *
 * Both bodies are the existing components rendered `embedded`; this is only
 * the pane shell, header and tabs.
 */
export default function WorkflowAccessView({ workspacePath, headerAction }: WorkflowAccessViewProps) {
  const isMultiUser = useAuthStore(state => state.isMultiUserMode)
  const isAdmin = useAuthStore(state => state.user?.is_admin === true)
  const canManageUsers = useAuthStore(state => state.isMultiUserMode && (state.user?.is_admin === true || hasWorkflowOwnerAccess(state.user, state.isMultiUserMode)))
  const myAccess = useWorkflowManifestStore(state =>
    workspacePath ? state.workflows.find(w => w.workspace_path === normalizeWorkspacePath(workspacePath))?.my_access : undefined,
  )
  const canShareWorkflow = isMultiUser && !!workspacePath && (isAdmin || myAccess === 'owner' || myAccess === 'write')

  const workflowTab = isMultiUser && !!workspacePath && (canShareWorkflow || myAccess === 'read')
  const workflowReadOnly = !canShareWorkflow
  const usersTab = canManageUsers
  const firstTab: AccessTab = workflowTab ? 'workflow' : 'users'
  const [tab, setTab] = useState<AccessTab>(firstTab)
  useEffect(() => {
    setTab(current => ((current === 'workflow' && workflowTab) || (current === 'users' && usersTab)) ? current : firstTab)
  }, [firstTab, workflowTab, usersTab])

  const closePane = () => useWorkflowStore.getState().setShowWorkspacePane(false)
  const scopeName = workspacePath?.split('/').filter(Boolean).pop() || 'Workflow'
  const tabClass = (active: boolean) =>
    `inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs transition-colors ${active ? 'bg-background text-foreground shadow-sm border border-border' : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'}`

  return (
    <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      <div className="flex items-start justify-between gap-3 border-b border-border px-4 py-3 sm:px-5 sm:py-3.5">
        <div className="min-w-0">
          <h2 className="flex items-center gap-2 text-base font-semibold text-foreground">
            <ShieldCheck className="h-4 w-4 text-primary" />
            Access
          </h2>
          <p className="mt-0.5 truncate text-xs text-muted-foreground">
            {tab === 'workflow'
              ? `${scopeName} · owners edit, run, share and delete; read-only people chat, run and watch.`
              : 'Accounts, roles and passwords for this deployment.'}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {workflowTab && usersTab && (
            <div className="flex items-center gap-1 rounded-lg bg-muted/60 p-1" role="tablist" aria-label="Access sections">
              <button type="button" role="tab" aria-selected={tab === 'workflow'} className={tabClass(tab === 'workflow')} onClick={() => setTab('workflow')}>
                <Share2 className="h-3.5 w-3.5" /> This workflow
              </button>
              <button type="button" role="tab" aria-selected={tab === 'users'} className={tabClass(tab === 'users')} onClick={() => setTab('users')}>
                <Users className="h-3.5 w-3.5" /> Users
              </button>
            </div>
          )}
          {headerAction}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto">
        {!workflowTab && !usersTab ? (
          <p className="px-5 py-6 text-sm text-muted-foreground">You can't manage access for this workflow.</p>
        ) : tab === 'workflow' && workflowTab && workspacePath ? (
          <WorkflowSharePopup embedded isOpen onClose={closePane} workspacePath={workspacePath} readOnly={workflowReadOnly} />
        ) : usersTab ? (
          <UsersAdminPanel embedded isOpen onClose={closePane} />
        ) : null}
      </div>
    </div>
  )
}
