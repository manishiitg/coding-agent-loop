import React, { useEffect, useState } from 'react'
import ChatArea from '../ChatArea'
import { useChatStore } from '../../stores/useChatStore'
import { useCodePrototypeStore } from '../../stores/useCodePrototypeStore'
import { CodePrototypeHeader } from './CodePrototypeHeader'
import { DeployDrawer } from './DeployDrawer'
import { NewProjectWizard } from './NewProjectWizard'
import { PreviewPanel } from './PreviewPanel'
import { LogsPanel } from './LogsPanel'
import { useAuthStore } from '../../stores/useAuthStore'

const CodeChatArea: React.FC<{ onNewChat: () => void }> = ({ onNewChat }) => {
  const activeTabId = useChatStore(s => s.activeTabId)
  return (
    <ChatArea
      onNewChat={onNewChat}
      tabId={activeTabId || undefined}
      hideHeader={true}
    />
  )
}

interface Props {
  onNewChat: () => void
}

export const CodePrototypeLayout: React.FC<Props> = ({ onNewChat }) => {
  const { currentProject, showPreview, showLogs } = useCodePrototypeStore()
  const authUser = useAuthStore(s => s.user)

  console.log('[PROTOTYPE] CodePrototypeLayout mount — project:', currentProject?.name ?? 'none', 'showPreview:', showPreview)

  // Show wizard when there is no current project
  const [showWizard, setShowWizard] = useState(!currentProject)

  // If project was restored from localStorage, don't show wizard
  useEffect(() => {
    if (currentProject) {
      console.log('[PROTOTYPE] project restored from localStorage:', currentProject.name)
      setShowWizard(false)
    }
  }, [currentProject])

  // Apply project config (servers/secrets/skills) to the active chat tab when project changes
  useEffect(() => {
    if (!currentProject || !authUser) return
    const activeTabId = useChatStore.getState().activeTabId
    if (!activeTabId) return
    if (currentProject.config) {
      console.log('[PROTOTYPE] applying project config to tab', activeTabId, '— project:', currentProject.name, currentProject.config)
      useChatStore.getState().setTabConfig(activeTabId, {
        selectedServers: currentProject.config.selected_servers ?? [],
        selectedSecrets: currentProject.config.selected_secrets ?? [],
        selectedSkills: currentProject.config.selected_skills ?? [],
        selectedSubAgents: currentProject.config.selected_subagents ?? [],
      })
    }
  }, [currentProject?.name, authUser])

  const handleNewChat = () => {
    const activeTabId = useChatStore.getState().activeTabId
    if (activeTabId) {
      useChatStore.getState().resetTabChat(activeTabId)
    }
  }

  return (
    <div className="h-full flex flex-col relative">
      <CodePrototypeHeader onNewChat={handleNewChat} />

      <div className="flex-1 min-h-0 overflow-hidden flex">
        <div className={(showPreview || showLogs) ? 'w-1/2 min-w-0' : 'flex-1 min-w-0'}>
          <CodeChatArea onNewChat={onNewChat} />
        </div>
        {(showPreview || showLogs) && (
          <div className="w-1/2 min-w-0 flex flex-col">
            {showPreview && (
              <div className={showLogs ? 'h-1/2 min-h-0 flex-shrink-0' : 'flex-1 min-h-0'}>
                <PreviewPanel />
              </div>
            )}
            {showLogs && (
              <div className={showPreview ? 'h-1/2 min-h-0' : 'flex-1 min-h-0'}>
                <LogsPanel />
              </div>
            )}
          </div>
        )}
      </div>

      <DeployDrawer />

      {showWizard && (
        <NewProjectWizard onClose={() => setShowWizard(false)} />
      )}
    </div>
  )
}
