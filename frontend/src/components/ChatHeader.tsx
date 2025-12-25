import React, { useState, useEffect, useCallback } from 'react'
import { MessageCircle, Workflow, Settings, ExternalLink, Trash2, Copy } from 'lucide-react'
import { EventModeToggle } from './events'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication, usePresetManagement, useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { PlannerFile, PresetLLMConfig } from '../services/api-types'
import PresetModal from './PresetModal'
import { useMCPStore } from '../stores/useMCPStore'
import { APISamplesDialog } from './APISamplesDialog'

interface ChatHeaderProps {
  chatSessionTitle: string
  chatSessionId: string
  sessionState: 'active' | 'completed' | 'loading' | 'error' | 'not-found'
  onModeSelect: (category: 'chat' | 'workflow') => void
}

const getModeIcon = (category: string) => {
  switch (category) {
    case 'chat':
      return <MessageCircle className="w-3 h-3" />
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
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore()
  const enabledServers = useMCPStore(state => state.enabledServers)
  
  // Use the new global preset store
  const { 
    customPresets, 
    savePreset,
    deletePreset,
    duplicatePreset
  } = usePresetManagement()
  
  const { 
    applyPreset, 
    getActivePreset, 
    isPresetActive,
    getPresetsForMode
  } = usePresetApplication()

  // Get active preset for current mode
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')

  
  const [showModeSwitch, setShowModeSwitch] = useState(false)
  const [showPresetDropdown, setShowPresetDropdown] = useState(false)
  const [showPresetModal, setShowPresetModal] = useState(false)
  const [showAPISamples, setShowAPISamples] = useState(false)
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null)
  const [duplicatingPresetId, setDuplicatingPresetId] = useState<string | null>(null)

  // Preset click handler - now uses the global store
  const handlePresetClick = useCallback((preset: CustomPreset | PredefinedPreset) => {
    const result = applyPreset(preset, selectedModeCategory as 'chat' | 'workflow')
    
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
    selectedTools?: string[],
    agentMode?: 'simple' | 'workflow', 
    selectedFolder?: PlannerFile,
    llmConfig?: PresetLLMConfig,
    useCodeExecutionMode?: boolean,
    enableContextSummarization?: boolean
  ) => {
    try {
      console.log('[code_execution] [ChatHeader] handleSavePreset called with:', {
        label,
        editingPreset: editingPreset?.id,
        useCodeExecutionMode,
        type: typeof useCodeExecutionMode
      })
      
      // Use consolidated savePreset function - pass id if editing, undefined if creating
      const savedPreset = await savePreset(
        label, 
        query, 
        selectedServers, 
        selectedTools,
        editingPreset ? editingPreset.agentMode : agentMode, // Use existing agent mode when editing
        selectedFolder, 
        llmConfig,
        useCodeExecutionMode,
        editingPreset?.id, // Pass id if editing, undefined if creating
        enableContextSummarization
      )
      
      // Apply the preset immediately if it's a new one
      if (savedPreset && !editingPreset) {
        handlePresetClick(savedPreset)
      }
      
      setShowPresetModal(false)
      setEditingPreset(null)
    } catch (error) {
      console.error('[code_execution] [ChatHeader] Failed to save preset:', error)
    }
  }, [editingPreset, savePreset, handlePresetClick])

  const handleDeletePreset = useCallback(async (presetId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (confirm('Are you sure you want to delete this workflow preset? This action cannot be undone.')) {
      try {
        await deletePreset(presetId)
        setShowPresetDropdown(false)
      } catch (error) {
        console.error('Failed to delete preset:', error)
        alert('Failed to delete workflow preset. Please try again.')
      }
    }
  }, [deletePreset])

  const handleDuplicatePreset = useCallback(async (presetId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    e.preventDefault()
    
    if (duplicatingPresetId) {
      return
    }
    
    setDuplicatingPresetId(presetId)
    try {
      const duplicatedPreset = await duplicatePreset(presetId)
      if (duplicatedPreset) {
        setShowPresetDropdown(false)
        // Optionally apply the duplicated preset
        handlePresetClick(duplicatedPreset)
      } else {
        alert('Failed to duplicate preset: No preset was created.')
      }
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error occurred'
      alert(`Failed to duplicate preset: ${errorMessage}`)
    } finally {
      setDuplicatingPresetId(null)
    }
  }, [duplicatePreset, handlePresetClick, duplicatingPresetId])

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
                const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')
                
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
                              {/* Show folder path and agent mode only for chat mode, not workflow mode */}
                              {selectedModeCategory === 'chat' && activePreset.selectedFolder && (
                                <span className="text-xs text-gray-500 dark:text-gray-400">
                                  ({activePreset.selectedFolder.filepath})
                                </span>
                              )}
                              {selectedModeCategory === 'chat' && activePreset.agentMode && (
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
                            {/* No Preset Option - only show for chat mode */}
                            {selectedModeCategory === 'chat' && (
                              <button
                                onClick={() => {
                                  useGlobalPresetStore.getState().clearActivePreset('chat')
                                  setShowPresetDropdown(false)
                                }}
                                className={`w-full text-left p-2 rounded-md text-sm transition-colors ${
                                  !activePreset
                                    ? 'bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100'
                                    : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                                }`}
                              >
                                <div className="flex items-center gap-2">
                                  <div className="w-2 h-2 bg-gray-400 rounded-full"></div>
                                  <span className="font-medium">No Preset</span>
                                </div>
                              </button>
                            )}
                            
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
                            {getPresetsForMode(selectedModeCategory as 'chat' | 'workflow')
                              .map((preset: CustomPreset | PredefinedPreset) => (
                                <div key={preset.id} className="flex items-center gap-1">
                                  <button
                                    onClick={() => {
                                      handlePresetClick(preset)
                                      setShowPresetDropdown(false)
                                    }}
                                    className={`flex-1 text-left p-2 rounded-md text-sm transition-colors ${
                                      isPresetActive(preset.id, selectedModeCategory as 'chat' | 'workflow')
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
                                  
                                  {/* Edit/Duplicate/Delete buttons - only show for custom presets */}
                                  {customPresets.some(cp => cp.id === preset.id) && (
                                    <div className="flex gap-1">
                                      {isPresetActive(preset.id, selectedModeCategory as 'chat' | 'workflow') && (
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
                                      {/* Duplicate button - show for all custom presets */}
                                      <button
                                        onClick={(e) => {
                                          e.stopPropagation()
                                          e.preventDefault()
                                          handleDuplicatePreset(preset.id, e)
                                        }}
                                        disabled={duplicatingPresetId === preset.id || !!duplicatingPresetId}
                                        className="p-1 rounded hover:bg-blue-100 dark:hover:bg-blue-900/20 text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 disabled:opacity-50 disabled:cursor-not-allowed"
                                        title={duplicatingPresetId === preset.id ? "Duplicating..." : "Duplicate preset"}
                                      >
                                        {duplicatingPresetId === preset.id ? (
                                          <div className="w-3 h-3 border-2 border-blue-600 border-t-transparent rounded-full animate-spin" />
                                        ) : (
                                          <Copy className="w-3 h-3" />
                                        )}
                                      </button>
                                      {/* Delete button - show for all custom presets, especially workflow ones */}
                                      {(selectedModeCategory === 'workflow' || preset.agentMode === 'workflow') && (
                                        <button
                                          onClick={(e) => handleDeletePreset(preset.id, e)}
                                          className="p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/20 text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300"
                                          title="Delete workflow preset"
                                        >
                                          <Trash2 className="w-3 h-3" />
                                        </button>
                                      )}
                                    </div>
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
              
              {/* Session Title - Hide in workflow mode when preset is active to avoid duplication */}
              {chatSessionTitle && !(selectedModeCategory === 'workflow' && activePreset) && (
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
        fixedAgentMode={editingPreset?.agentMode || (selectedModeCategory ? (getAgentModeFromCategory(selectedModeCategory) as 'simple' | 'workflow') : undefined)}
      />
      
      {/* API Samples Dialog */}
      <APISamplesDialog
        isOpen={showAPISamples}
        onClose={() => setShowAPISamples(false)}
      />
    </div>
  )
}
