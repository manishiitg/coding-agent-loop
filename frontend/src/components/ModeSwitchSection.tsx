import React, { useState } from 'react'
import { Card } from './ui/Card'
import { MessageCircle, Search, Workflow, Settings, ChevronDown } from 'lucide-react'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'
import { useAppStore } from '../stores/useAppStore'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'

interface ModeSwitchSectionProps {
  minimized?: boolean
}

export const ModeSwitchSection: React.FC<ModeSwitchSectionProps> = ({ minimized = false }) => {
  const { selectedModeCategory, setModeCategory } = useModeStore()
  const { agentMode, setAgentMode } = useAppStore()
  const [showModeSelector, setShowModeSelector] = useState(false)
  const [showSwitchDialog, setShowSwitchDialog] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<ModeCategory | null>(null)

  const getModeIcon = (category: ModeCategory) => {
    switch (category) {
      case 'chat':
        return <MessageCircle className="w-4 h-4 text-blue-600" />
      case 'deep-research':
        return <Search className="w-4 h-4 text-green-600" />
      case 'workflow':
        return <Workflow className="w-4 h-4 text-purple-600" />
      default:
        return <Settings className="w-4 h-4 text-gray-400" />
    }
  }

  const getModeName = (category: ModeCategory) => {
    switch (category) {
      case 'chat':
        return 'Chat Mode'
      case 'deep-research':
        return 'Deep Research Mode'
      case 'workflow':
        return 'Workflow Mode'
      default:
        return 'Unknown Mode'
    }
  }

  const handleModeSelect = (category: ModeCategory) => {
    if (category === selectedModeCategory) {
      setShowModeSelector(false)
      return
    }

    // Check if there's an active chat session
    const hasActiveChat = document.querySelector('[data-chat-active="true"]') !== null
    
    if (hasActiveChat) {
      // Show confirmation dialog
      setPendingModeCategory(category)
      setShowSwitchDialog(true)
      setShowModeSelector(false)
    } else {
      // Switch mode directly
      switchMode(category)
    }
  }

  const switchMode = (category: ModeCategory) => {
    setModeCategory(category)
    
    // Set the corresponding agent mode
    let agentModeToSet: 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
    switch (category) {
      case 'chat':
        agentModeToSet = 'ReAct' // Default to ReAct for chat
        break
      case 'deep-research':
        agentModeToSet = 'orchestrator'
        break
      case 'workflow':
        agentModeToSet = 'workflow'
        break
      default:
        agentModeToSet = 'ReAct'
    }
    
    setAgentMode(agentModeToSet)
    
    // Note: Starting a new chat when switching modes would need to be handled by the parent component
  }

  const handleConfirmSwitch = () => {
    if (pendingModeCategory) {
      switchMode(pendingModeCategory)
      setShowSwitchDialog(false)
      setPendingModeCategory(null)
    }
  }

  const handleCancelSwitch = () => {
    setShowSwitchDialog(false)
    setPendingModeCategory(null)
  }

  if (minimized) {
    return (
      <div className="flex justify-center">
        <div className="w-8 h-8 flex items-center justify-center bg-gray-100 dark:bg-gray-800 rounded-lg">
          {selectedModeCategory ? getModeIcon(selectedModeCategory) : <Settings className="w-4 h-4 text-gray-400" />}
        </div>
      </div>
    )
  }

  const modes: Array<{ category: ModeCategory; name: string; icon: React.ReactNode; description: string }> = [
    {
      category: 'chat',
      name: 'Chat Mode',
      icon: <MessageCircle className="w-4 h-4 text-blue-600" />,
      description: 'Quick conversations and questions'
    },
    {
      category: 'deep-research',
      name: 'Deep Research Mode',
      icon: <Search className="w-4 h-4 text-green-600" />,
      description: 'Multi-step analysis and research'
    },
    {
      category: 'workflow',
      name: 'Workflow Mode',
      icon: <Workflow className="w-4 h-4 text-purple-600" />,
      description: 'Todo-based task execution'
    }
  ]

  return (
    <>
      <Card className="p-3 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-700 shadow-sm">
        <div className="space-y-2">
          {/* Current Mode Display */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <Settings className="w-4 h-4 text-gray-600 dark:text-gray-400" />
              <span className="text-sm font-semibold text-gray-800 dark:text-gray-200">AI Mode</span>
            </div>
            <button
              onClick={() => setShowModeSelector(!showModeSelector)}
              className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
            >
              <ChevronDown className={`w-3 h-3 transition-transform ${showModeSelector ? 'rotate-180' : ''}`} />
            </button>
          </div>

          {/* Current Mode */}
          {selectedModeCategory && (
            <div className="flex items-center gap-2 p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
              {getModeIcon(selectedModeCategory)}
              <div className="flex-1">
                <div className="text-sm font-medium text-blue-900 dark:text-blue-100">
                  {getModeName(selectedModeCategory)}
                </div>
                <div className="text-xs text-blue-700 dark:text-blue-300">
                  {agentMode === 'simple' ? 'Simple Agent' : 
                   agentMode === 'ReAct' ? 'ReAct Agent' :
                   agentMode === 'orchestrator' ? 'Orchestrator Agent' :
                   agentMode === 'workflow' ? 'Workflow Agent' : 'Unknown Agent'}
                </div>
              </div>
            </div>
          )}

          {/* Mode Selector Dropdown */}
          {showModeSelector && (
            <div className="border border-gray-200 dark:border-gray-700 rounded-lg bg-white dark:bg-slate-800 shadow-lg">
              <div className="p-2 space-y-1">
                {modes.map((mode) => (
                  <button
                    key={mode.category}
                    onClick={() => handleModeSelect(mode.category)}
                    className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                      selectedModeCategory === mode.category
                        ? mode.category === 'chat' 
                          ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                          : mode.category === 'deep-research'
                          ? 'bg-green-100 dark:bg-green-900/30 text-green-900 dark:text-green-100'
                          : 'bg-purple-100 dark:bg-purple-900/30 text-purple-900 dark:text-purple-100'
                        : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                    }`}
                  >
                    <div className="flex items-center gap-3">
                      {mode.icon}
                      <div>
                        <div className="font-medium">{mode.name}</div>
                        <div className="text-xs text-gray-500 dark:text-gray-400">
                          {mode.description}
                        </div>
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>

      {/* Mode Switch Confirmation Dialog */}
      <ModeSwitchDialog
        isOpen={showSwitchDialog}
        onConfirm={handleConfirmSwitch}
        onCancel={handleCancelSwitch}
        newModeCategory={pendingModeCategory || 'chat'}
        currentModeCategory={selectedModeCategory || 'chat'}
      />
    </>
  )
}
