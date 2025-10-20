import React, { useState, useEffect, useCallback } from 'react'
import { MessageCircle, Search, Workflow, Settings, ExternalLink } from 'lucide-react'
import { EventModeToggle } from './events'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication, usePresetManagement } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { PlannerFile, PresetLLMConfig } from '../services/api-types'
import PresetModal from './PresetModal'
import { useMCPStore } from '../stores/useMCPStore'
import { APISamplesDialog } from './APISamplesDialog'

interface ChatHeaderProps {
  chatSessionTitle: string
  chatSessionId: string
  sessionState: 'active' | 'completed' | 'loading' | 'error' | 'not-found'
  onModeSelect: (category: 'chat' | 'deep-research' | 'workflow') => void
}

const getModeIcon = (category: string) => {
  switch (category) {
    case 'chat':
      return <MessageCircle className="w-3 h-3" />
    case 'deep-research':
      return <Search className="w-3 h-3" />
    case 'workflow':
      return <Workflow className="w-3 h-3" />
    default:
      return <MessageCircle className="w-3 h-3" />
  }
}

const getModeName = (category: string) => {
  switch (category) {
    case 'chat':
      return 'Chat Mode'
    case 'deep-research':
      return 'Deep Research Mode'
    case 'workflow':
      return 'Workflow Mode'
    default:
      return 'Chat Mode'
  }
}

export const ChatHeader: React.FC<ChatHeaderProps> = ({
  chatSessionTitle,
  chatSessionId,
  sessionState,
  onModeSelect
}) => {
  const { selectedModeCategory } = useModeStore()
  const { enabledServers } = useMCPStore()
  
  // Use the new global preset store
  const { 
    customPresets, 
    addPreset, 
    updatePreset
  } = usePresetManagement()
  
  const { 
    applyPreset, 
    getActivePreset, 
    isPresetActive,
    getPresetsForMode
  } = usePresetApplication()

  // Get active preset for current mode
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'deep-research' | 'workflow')

  
  const [showModeSwitch, setShowModeSwitch] = useState(false)
  const [showPresetDropdown, setShowPresetDropdown] = useState(false)
  const [showPresetModal, setShowPresetModal] = useState(false)
  const [showAPISamples, setShowAPISamples] = useState(false)
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null)

  // Preset click handler - now uses the global store
  const handlePresetClick = useCallback((preset: CustomPreset | PredefinedPreset) => {
    const result = applyPreset(preset, selectedModeCategory as 'chat' | 'deep-research' | 'workflow')
    
    if (result.success) {
      setShowPresetDropdown(false)
    } else {
      console.error('Failed to apply preset:', result.error)
    }
  }, [applyPreset, selectedModeCategory]);

  // Memoized callbacks for PresetModal
  const handleClosePresetModal = useCallback(() => {
    setShowPresetModal(false)
    setEditingPreset(null)
  }, [])

  const handleSavePreset = useCallback(async (
    label: string, 
    query: string, 
    selectedServers?: string[], 
    agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow', 
    selectedFolder?: PlannerFile,
    llmConfig?: PresetLLMConfig
  ) => {
    try {
      if (editingPreset) {
        // Editing existing preset - use the existing agent mode
        await updatePreset(editingPreset.id, label, query, selectedServers, editingPreset.agentMode, selectedFolder, llmConfig)
      } else {
        // Creating new preset - allow agent mode selection
        const newPreset = await addPreset(label, query, selectedServers, agentMode, selectedFolder, llmConfig)
        // Apply the new preset immediately
        if (newPreset) {
          handlePresetClick(newPreset)
        }
      }
      setShowPresetModal(false)
      setEditingPreset(null)
    } catch (error) {
      console.error('Failed to save preset:', error)
    }
  }, [editingPreset, updatePreset, addPreset, handlePresetClick])

  // Close dropdowns when clicking outside
  useEffect(() => {
    const onMouseDown = (event: MouseEvent) => {
      const target = event.target as Element
      if (!target.closest('.mode-switch-dropdown') && !target.closest('.preset-dropdown')) {
        setShowModeSwitch(false)
        setShowPresetDropdown(false)
      }
    }
    
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setShowModeSwitch(false)
        setShowPresetDropdown(false)
      }
    }
    
    document.addEventListener('mousedown', onMouseDown)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onMouseDown)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])

  return (
    <div className="border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
      {/* Tier 1: Mode & Preset Bar */}
      <div className="px-4 py-2 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-center justify-between">
          {/* Left: Mode Indicator */}
          <div className="flex items-center gap-3">
            {selectedModeCategory && (
              <div className="relative">
                <button
                  onClick={() => setShowModeSwitch(!showModeSwitch)}
                  className={`flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors cursor-pointer ${
                    selectedModeCategory === 'chat'
                      ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-800'
                      : selectedModeCategory === 'deep-research'
                      ? 'bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 border border-green-200 dark:border-green-800'
                      : 'bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 border border-purple-200 dark:border-purple-800'
                  }`}
                  title="Click to change mode"
                  type="button"
                  aria-haspopup="menu"
                  aria-expanded={showModeSwitch}
                  aria-controls="mode-switch-menu"
                >
                  {getModeIcon(selectedModeCategory)}
                  <span>{getModeName(selectedModeCategory)}</span>
                  <Settings className="w-3 h-3" />
                </button>
                
                {/* Direct Mode Selection Dropdown */}
                {showModeSwitch && (
                  <div
                    id="mode-switch-menu"
                    role="menu"
                    aria-label="Select mode"
                    className="mode-switch-dropdown absolute top-full left-0 mt-1 w-64 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg shadow-lg z-50"
                  >
                    <div className="p-2 space-y-1">
                      {/* Chat Mode */}
                      <button
                        onClick={() => {
                          onModeSelect('chat')
                          setShowModeSwitch(false)
                        }}
                        className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                          selectedModeCategory === 'chat'
                            ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                            : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                        }`}
                      >
                        <div className="flex items-center gap-3">
                          <MessageCircle className="w-4 h-4 text-blue-600" />
                          <div>
                            <div className="font-medium">Chat Mode</div>
                            <div className="text-xs text-gray-500 dark:text-gray-400">
                              Quick conversations and questions
                            </div>
                          </div>
                        </div>
                      </button>
                      
                      {/* Deep Research Mode */}
                      <button
                        onClick={() => {
                          onModeSelect('deep-research')
                          setShowModeSwitch(false)
                        }}
                        className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                          selectedModeCategory === 'deep-research'
                            ? 'bg-green-100 dark:bg-green-900/30 text-green-900 dark:text-green-100'
                            : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                        }`}
                      >
                        <div className="flex items-center gap-3">
                          <Search className="w-4 h-4 text-green-600" />
                          <div>
                            <div className="font-medium">Deep Research Mode</div>
                            <div className="text-xs text-gray-500 dark:text-gray-400">
                              Multi-step analysis and research
                            </div>
                          </div>
                        </div>
                      </button>
                      
                      {/* Workflow Mode */}
                      <button
                        onClick={() => {
                          onModeSelect('workflow')
                          setShowModeSwitch(false)
                        }}
                        className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                          selectedModeCategory === 'workflow'
                            ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-900 dark:text-purple-100'
                            : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                        }`}
                      >
                        <div className="flex items-center gap-3">
                          <Workflow className="w-4 h-4 text-purple-600" />
                          <div>
                            <div className="font-medium">Workflow Mode</div>
                            <div className="text-xs text-gray-500 dark:text-gray-400">
                              Todo-based task execution
                            </div>
                          </div>
                        </div>
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}
            
            {/* Center: Preset Information & Session Title */}
            <div className="flex items-center gap-3">
              {/* Preset Information - Show for chat mode even when no preset is selected */}
              {(() => {
                const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'deep-research' | 'workflow')
                
                // For chat mode, always show preset selector
                if (selectedModeCategory === 'chat' || activePreset) {
                  return (
                    <div className="relative flex items-center">
                      <div className="flex items-center bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-md overflow-hidden">
                        <button
                          onClick={() => setShowPresetDropdown(!showPresetDropdown)}
                          className="flex items-center gap-2 px-3 py-1 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                        >
                          {activePreset ? (
                            <>
                              <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                                {activePreset.label}
                              </span>
                              {activePreset.selectedFolder && (
                                <span className="text-xs text-gray-500 dark:text-gray-400">
                                  ({activePreset.selectedFolder.filepath})
                                </span>
                              )}
                              {activePreset.agentMode && (
                                <span className="text-xs bg-gray-100 dark:bg-gray-600 text-gray-600 dark:text-gray-300 px-1.5 py-0.5 rounded">
                                  {activePreset.agentMode}
                                </span>
                              )}
                            </>
                          ) : (
                            <>
                              <div className="w-2 h-2 bg-gray-400 rounded-full"></div>
                              <span className="text-sm font-medium text-gray-500 dark:text-gray-400">
                                Select Preset
                              </span>
                            </>
                          )}
                        </button>
                        
                        {/* Settings gear icon - separate clickable element */}
                        {activePreset && customPresets.some(cp => cp.id === activePreset.id) && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              setEditingPreset(activePreset as CustomPreset)
                              setShowPresetModal(true)
                            }}
                            className="px-2 py-1 border-l border-gray-200 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                            title="Edit preset"
                          >
                            <Settings className="w-3 h-3 text-gray-400" />
                          </button>
                        )}
                        
                        {/* Settings gear icon for when no preset is selected */}
                        {!activePreset && (
                          <div className="px-2 py-1 border-l border-gray-200 dark:border-gray-600">
                            <Settings className="w-3 h-3 text-gray-300" />
                          </div>
                        )}
                      </div>
                      
                      {/* Preset Dropdown */}
                      {showPresetDropdown && (
                        <div className="preset-dropdown absolute top-full left-0 mt-1 w-64 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg shadow-lg z-50">
                          <div className="p-2 space-y-1">
                            {/* Add New Preset Option */}
                            <button
                              onClick={() => {
                                setEditingPreset(null) // null means creating new preset
                                setShowPresetModal(true)
                                setShowPresetDropdown(false)
                              }}
                              className="w-full text-left p-2 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300 border-t border-gray-200 dark:border-gray-600 mt-2 pt-2"
                            >
                              <div className="flex items-center gap-2">
                                <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                                <span className="font-medium">+ Add New Preset</span>
                              </div>
                            </button>
                            
                            {/* Available Presets */}
                            {getPresetsForMode(selectedModeCategory as 'chat' | 'deep-research' | 'workflow')
                              .map((preset: CustomPreset | PredefinedPreset) => (
                                <div key={preset.id} className="flex items-center gap-1">
                                  <button
                                    onClick={() => {
                                      handlePresetClick(preset)
                                      setShowPresetDropdown(false)
                                    }}
                                    className={`flex-1 text-left p-2 rounded-md text-sm transition-colors ${
                                      isPresetActive(preset.id, selectedModeCategory as 'chat' | 'deep-research' | 'workflow')
                                        ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                                        : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                                    }`}
                                  >
                                    <div className="flex items-center gap-2">
                                      <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                                      <div className="flex-1">
                                        <div className="font-medium">{preset.label}</div>
                                        {preset.agentMode && (
                                          <div className="text-xs text-gray-500 dark:text-gray-400">
                                            {preset.agentMode}
                                          </div>
                                        )}
                                      </div>
                                    </div>
                                  </button>
                                  
                                  {/* Edit button - only show for custom presets that are currently selected */}
                                  {customPresets.some(cp => cp.id === preset.id) && 
                                   isPresetActive(preset.id, selectedModeCategory as 'chat' | 'deep-research' | 'workflow') && (
                                    <button
                                      onClick={(e) => {
                                        e.stopPropagation()
                                        setEditingPreset(preset as CustomPreset)
                                        setShowPresetModal(true)
                                        setShowPresetDropdown(false)
                                      }}
                                      className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                                      title="Edit preset"
                                    >
                                      <Settings className="w-3 h-3" />
                                    </button>
                                  )}
                                </div>
                              ))}
                          </div>
                        </div>
                      )}
                    </div>
                  )
                }
                return null
              })()}
              
              {/* Session Title */}
              {chatSessionTitle && (
                <h2 className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">
                  {chatSessionTitle}
                </h2>
              )}
              
              {/* Session Status */}
              {chatSessionId && (
                <span className="text-xs text-gray-500 dark:text-gray-400">
                  {sessionState === 'active' ? 'Live' : 
                   sessionState === 'completed' ? 'Historical' :
                   sessionState === 'loading' ? 'Loading...' :
                   sessionState === 'error' ? 'Error' :
                   'Not Found'}
                </span>
              )}
            </div>
          </div>
          
          {/* Right: Event Controls */}
          <div className="flex items-center gap-3">
            {/* External Connection Button - Show when there's an active preset */}
            {activePreset && (
              <button
                onClick={() => setShowAPISamples(true)}
                className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-colors bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 border border-gray-200 dark:border-gray-600 hover:bg-gray-200 dark:hover:bg-gray-600"
                title="View External Connection Examples"
              >
                <ExternalLink className="w-3 h-3" />
                <span>External Connection</span>
              </button>
            )}
            
            {/* Event Mode Toggle */}
            <EventModeToggle />
          </div>
        </div>
      </div>
      
      {/* Preset Modal */}
      <PresetModal
        isOpen={showPresetModal}
        onClose={handleClosePresetModal}
        onSave={handleSavePreset}
        editingPreset={editingPreset}
        availableServers={enabledServers}
        hideAgentModeSelection={!!editingPreset}
        fixedAgentMode={editingPreset?.agentMode}
      />
      
      {/* API Samples Dialog */}
      <APISamplesDialog
        isOpen={showAPISamples}
        onClose={() => setShowAPISamples(false)}
      />
    </div>
  )
}
