import React from 'react'
import { X, Plus } from 'lucide-react'
import { useWorkflowStore, type WorkflowChatTab } from '../../stores/useWorkflowStore'
import type { WorkflowPhase, ExecutionOptions } from '../../services/api-types'

interface WorkflowChatTabsProps {
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
}

export const WorkflowChatTabs: React.FC<WorkflowChatTabsProps> = ({ onStartPhase }) => {
  const {
    workflowChatTabs,
    activeWorkflowTabId,
    switchWorkflowTab,
    closeWorkflowTab,
    phases,
    getPhaseById
  } = useWorkflowStore()

  const tabs = Object.values(workflowChatTabs).sort((a, b) => a.createdAt - b.createdAt)
  const activeTab = activeWorkflowTabId ? workflowChatTabs[activeWorkflowTabId] : null

  const handleTabClick = (tabId: string) => {
    switchWorkflowTab(tabId)
  }

  const handleTabClose = async (e: React.MouseEvent, tabId: string) => {
    e.stopPropagation()
    await closeWorkflowTab(tabId)
  }

  const handleNewPhase = () => {
    // Find first available phase (or use default)
    const defaultPhase = phases.length > 0 ? phases[0] : null
    if (defaultPhase) {
      onStartPhase(defaultPhase.id)
    }
  }

  // Get phase icon/color if available
  const getPhaseColor = (phaseId: string) => {
    const phase = getPhaseById(phaseId)
    // You can extend this with phase-specific colors
    return 'bg-blue-500'
  }

  if (tabs.length === 0) {
    return null // Don't show tabs if none exist
  }

  return (
    <div className="flex items-center gap-1 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800 px-2 py-1 overflow-x-auto">
      {/* Existing Tabs */}
      {tabs.map((tab) => {
        const isActive = tab.tabId === activeWorkflowTabId
        const phaseColor = getPhaseColor(tab.phaseId)
        
        return (
          <button
            key={tab.tabId}
            onClick={() => handleTabClick(tab.tabId)}
            className={`
              flex items-center gap-2 px-3 py-1.5 rounded-t-md text-sm font-medium transition-colors
              ${isActive
                ? 'bg-white dark:bg-gray-900 text-gray-900 dark:text-gray-100 border-b-2 border-blue-500'
                : 'text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-900 dark:hover:text-gray-100'
              }
            `}
          >
            {/* Status Indicator */}
            <div className={`w-2 h-2 rounded-full ${tab.isActive ? phaseColor : 'bg-gray-400'}`} />
            
            {/* Tab Name */}
            <span className="whitespace-nowrap">{tab.phaseName}</span>
            
            {/* Close Button */}
            <button
              onClick={(e) => handleTabClose(e, tab.tabId)}
              className={`
                ml-1 p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-600
                ${isActive ? 'opacity-70 hover:opacity-100' : 'opacity-0 hover:opacity-70'}
                transition-opacity
              `}
              title="Close tab"
            >
              <X className="w-3 h-3" />
            </button>
          </button>
        )
      })}

      {/* New Phase Button */}
      <button
        onClick={handleNewPhase}
        className="flex items-center gap-1 px-2 py-1.5 text-sm text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 rounded transition-colors"
        title="Start new phase"
      >
        <Plus className="w-4 h-4" />
        <span className="text-xs">New Phase</span>
      </button>
    </div>
  )
}

