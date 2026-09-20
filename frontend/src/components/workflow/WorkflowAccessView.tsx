import { useEffect, useState, type ReactNode } from 'react'
import { ShieldCheck } from 'lucide-react'
import WorkflowSharePopup from './WorkflowSharePopup'
import UsersAdminPanel from '../admin/UsersAdminPanel'
import { useAuthStore } from '../../stores/useAuthStore'
import { useWorkflowManifestStore } from '../../stores/useWorkflowManifestStore'
import { hasWorkflowOwnerAccess } from '../../utils/workflowPermissions'
import { WorkspaceViewHeader } from './WorkspaceViewHeader'
import { WorkspaceViewActions } from './WorkspaceViewActions'
import { usePersistentTab } from '../../hooks/usePersistentTab'
import { getAccessTabAskAIMessage, type AccessTabId } from './workspaceAskAI'
import { normalizeWorkspacePath } from '../../utils/workspacePathUtils'

interface WorkflowAccessViewProps {
  workspacePath: string | null
  headerAction?: ReactNode
}

const ACCESS_TABS: Array<{ value: AccessTabId; label: string }> = [
  { value: 'workflow', label: 'This workflow' },
  { value: 'users', label: 'Users' },
]

/**
 * Access, as a right-side workspace view like Notify, Pulse and Backup --
 * not a modal (user request, 2026-09-03). Two tabs:
 *
 *  - "This workflow": who may see or edit the open workflow (owners and
 *    read-only readers).
 *  - "Users": the deployment's accounts and roles (admins only).
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
  const visibleTabs = ACCESS_TABS.filter(option => (option.value === 'workflow' && workflowTab) || (option.value === 'users' && usersTab))
  const [tab, setTab] = usePersistentTab<AccessTabId>('agentworks.tab.access', 'workflow', ACCESS_TABS.map(option => option.value))
  const activeTab = visibleTabs.some(option => option.value === tab) ? tab : visibleTabs[0]?.value
  useEffect(() => {
    if (activeTab && activeTab !== tab) setTab(activeTab)
  }, [activeTab, tab, setTab])
  // Every tab loads on mount, so Refresh always remounts.
  const [tabNonce, setTabNonce] = useState(0)

  const scopeName = workspacePath?.split('/').filter(Boolean).pop() || 'Workflow'

  return (
    <div className="flex h-full min-h-0 w-full max-w-none flex-col bg-background">
      <WorkspaceViewHeader
        icon={ShieldCheck}
        title="Access"
        subtitle={activeTab === 'users'
          ? 'Accounts and roles for this deployment.'
          : `${scopeName} · owners edit, run, share and delete; read-only people chat, run and watch.`}
        actions={<>
          {headerAction}
          {activeTab && (
            <WorkspaceViewActions
              workspacePath={workspacePath}
              message={getAccessTabAskAIMessage(activeTab)}
              onRefresh={() => setTabNonce(nonce => nonce + 1)}
              refreshLabel={`Refresh ${visibleTabs.find(option => option.value === activeTab)?.label ?? 'view'}`}
            />
          )}
        </>}
        tabs={visibleTabs.length > 1
          ? { value: activeTab ?? 'workflow', onChange: (value: string) => setTab(value as AccessTabId), options: visibleTabs, ariaLabel: 'Access sections' }
          : undefined}
      />

      <div className="flex-1 overflow-y-auto">
        {!activeTab ? (
          <p className="px-5 py-6 text-sm text-muted-foreground">You can't manage access for this workflow.</p>
        ) : (
          <div key={`${activeTab}:${tabNonce}`} className="p-4">
            {activeTab === 'workflow' && workspacePath ? (
              <WorkflowSharePopup workspacePath={workspacePath} readOnly={workflowReadOnly} />
            ) : activeTab === 'users' ? (
              <UsersAdminPanel />
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
}
