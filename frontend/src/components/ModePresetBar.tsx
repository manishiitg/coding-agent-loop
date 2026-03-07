import React, { useState, useEffect, useCallback, useMemo } from 'react'
import { MessageCircle, Workflow, Users, Settings, Trash2, Copy, DollarSign, Keyboard, Clock, CalendarDays, GitBranch } from 'lucide-react'
import { useModeStore } from '../stores/useModeStore'
import { usePresetApplication, usePresetManagement } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { PlannerFile, PresetLLMConfig } from '../services/api-types'
import PresetModal from './PresetModal'
import ChatCostsPopup from './ChatCostsPopup'
import DelegationLogsPopup from './DelegationLogsPopup'
import SchedulePresetPopup from './SchedulePresetPopup'
import WorkflowScheduleRunsPanel from './scheduler/WorkflowScheduleRunsPanel'
import PlansManagerModal from './PlansManagerModal'
import { schedulerApi } from '../api/scheduler'
import { agentApi } from '../services/api'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip'
import { useMCPStore } from '../stores/useMCPStore'
import { useAppStore } from '../stores/useAppStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { useRunningWorkflows } from '../stores/useRunningWorkflowsStore'
import { useChatStore } from '../stores/useChatStore'

const getModeIcon = (category: string) => {
  switch (category) {
    case 'chat':
      return <MessageCircle className="w-3 h-3" />
    case 'workflow':
      return <Workflow className="w-3 h-3" />
    case 'multi-agent':
      return <Users className="w-3 h-3" />
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
    case 'multi-agent':
      return 'Multi Agent Chat'
    default:
      return 'Chat Mode'
  }
}

const MODE_PILLS = [
  {
    key: 'multi-agent' as const,
    label: 'Multi-Agent',
    icon: Users,
    activeClasses: 'bg-indigo-50 text-indigo-700 shadow-sm ring-1 ring-indigo-200 dark:bg-indigo-500/20 dark:text-indigo-100 dark:ring-indigo-500/40',
    inactiveClasses: 'text-gray-500 dark:text-gray-400',
  },
  {
    key: 'workflow' as const,
    label: 'Workflow',
    icon: Workflow,
    activeClasses: 'bg-purple-50 text-purple-700 shadow-sm ring-1 ring-purple-200 dark:bg-purple-500/20 dark:text-purple-100 dark:ring-purple-500/40',
    inactiveClasses: 'text-gray-500 dark:text-gray-400',
  },
  {
    key: 'chat' as const,
    label: 'Chat',
    icon: MessageCircle,
    activeClasses: 'bg-blue-50 text-blue-700 shadow-sm ring-1 ring-blue-200 dark:bg-blue-500/20 dark:text-blue-100 dark:ring-blue-500/40',
    inactiveClasses: 'text-gray-500 dark:text-gray-400',
  },
] as const

/**
 * Global Mode & Preset Bar - always visible at the top level
 * Allows users to select mode (chat/workflow) and presets regardless of chat tabs
 */
export const ModePresetBar: React.FC = () => {
  const { selectedModeCategory, setModeCategory, getAgentModeFromCategory } = useModeStore()
  const { setWorkspaceMinimized, agentMode } = useAppStore()
  // Use toolList to get all available servers, not just enabled ones
  const toolList = useMCPStore(state => state.toolList)
  const availableServers = React.useMemo(() =>
    [...new Set(toolList.map(t => t.server).filter(Boolean) as string[])],
    [toolList]
  )

  // Use the new global preset store
  const {
    customPresets,
    savePreset,
    deletePreset,
    duplicatePreset,
    refreshPresets,
    loading: presetsLoading
  } = usePresetManagement()

  const {
    applyPreset,
    getActivePreset,
    isPresetActive,
    getPresetsForMode
  } = usePresetApplication()

  // Get active preset for current mode (for schedule popup, supports all modes)
  const activePreset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const activePresetForSchedule = (getActivePreset as any)(selectedModeCategory) as ReturnType<typeof getActivePreset>

  // Get presets for current mode
  const presetsForMode = getPresetsForMode(selectedModeCategory as 'chat' | 'workflow')

  const [showPresetDropdown, setShowPresetDropdown] = useState(false)
  const [showPresetModal, setShowPresetModal] = useState(false)
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null)
  const [showCostsPopup, setShowCostsPopup] = useState(false)
  const [showDelegationLogs, setShowDelegationLogs] = useState(false)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showSchedulePopup, setShowSchedulePopup] = useState(false)
  const [showRunsPanel, setShowRunsPanel] = useState(false)
  const [workflowScheduleCount, setWorkflowScheduleCount] = useState(0)
  const [showPlansManager, setShowPlansManager] = useState(false)

  // Fetch workflow schedule count for badge (filtered to current workflow)
  useEffect(() => {
    if (selectedModeCategory !== 'workflow') return
    const currentPresetId = activePreset?.id
    schedulerApi.listJobs({ entity_type: 'workflow' })
      .then(resp => {
        const jobs = resp.jobs ?? []
        const filtered = currentPresetId ? jobs.filter(j => j.preset_query_id === currentPresetId) : []
        setWorkflowScheduleCount(filtered.length)
      })
      .catch(() => {})
  }, [selectedModeCategory, showSchedulePopup, showRunsPanel, activePreset?.id]) // refresh after schedule/runs panel closes or workflow changes

  // Handle ESC and Enter keys for shortcuts modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (showShortcuts) {
        if (event.key === 'Escape' || event.key === 'Enter') {
          event.preventDefault()
          setShowShortcuts(false)
        }
      }
    }

    if (showShortcuts) {
      window.addEventListener('keydown', handleKeyDown)
      return () => window.removeEventListener('keydown', handleKeyDown)
    }
  }, [showShortcuts])

  const runningWorkflows = useRunningWorkflows()
  const chatTabsForBadge = useChatStore(state => state.chatTabs)
  const tabSessionStatusForBadge = useChatStore(state => state.tabSessionStatus)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)

  const runningWorkflowCount = useMemo(() => {
    const seenSessionIds = new Set<string>()
    let count = 0

    runningWorkflows.forEach(wf => {
      if (wf.status === 'running') count++
      if (wf.sessionId) seenSessionIds.add(wf.sessionId)
    })

    Object.values(chatTabsForBadge).forEach(tab => {
      if (tab.metadata?.mode !== 'workflow') return
      if (tab.sessionId && seenSessionIds.has(tab.sessionId)) return
      if (!tab.metadata?.presetQueryId) return
      if (tab.isCompleted) return
      const isStreaming = getTabStreamingStatus(tab.tabId)
      const sessionStatus = tabSessionStatusForBadge[tab.tabId]?.status
      const isRunningSession = sessionStatus === 'running' || sessionStatus === 'active'
      if (isStreaming || isRunningSession) count++
    })

    return count
  }, [runningWorkflows, chatTabsForBadge, tabSessionStatusForBadge, getTabStreamingStatus])

  // Listen for external trigger to open preset settings (e.g. from workflow toolbar)
  const showPresetSettings = useCommandDialogStore(s => s.showPresetSettings)
  useEffect(() => {
    if (showPresetSettings) {
      useCommandDialogStore.getState().closeDialog('presetSettings')
      const preset = getActivePreset(selectedModeCategory as 'chat' | 'workflow')
      if (preset && customPresets.some(cp => cp.id === preset.id)) {
        setEditingPreset(preset as CustomPreset)
        setShowPresetModal(true)
      }
    }
  }, [showPresetSettings, selectedModeCategory, getActivePreset, customPresets])

  // Preset click handler - now uses the global store
  const handlePresetClick = useCallback((preset: CustomPreset | PredefinedPreset) => {
    // Determine the mode category based on the preset's agentMode
    const presetModeCategory = preset.agentMode === 'workflow' ? 'workflow' : 'chat'

    // If preset is for workflow mode, ensure we're in workflow mode
    if (presetModeCategory === 'workflow' && selectedModeCategory !== 'workflow') {
      setModeCategory('workflow')
    }
    // If preset is for chat mode, ensure we're in chat mode
    else if (presetModeCategory === 'chat' && selectedModeCategory !== 'chat') {
      setModeCategory('chat')
    }

    // Apply the preset with the correct mode category
    const result = applyPreset(preset, presetModeCategory)

    if (result.success) {
      setShowPresetDropdown(false)
    } else {
      console.error('Failed to apply preset:', result.error)
    }
  }, [applyPreset, selectedModeCategory, setModeCategory])

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
    selectedSkills?: string[], // Skill folder names for workflow
    agentMode?: 'simple' | 'workflow',
    selectedFolder?: PlannerFile,
    llmConfig?: PresetLLMConfig,
    useCodeExecutionMode?: boolean,
    enableContextSummarization?: boolean,
    useToolSearchMode?: boolean,
    enableBrowserAccess?: boolean,
    selectedSecrets?: string[],
    selectedGlobalSecretNames?: string[] | null,
    camofoxHeaded?: boolean
  ) => {
    try {
      // Use consolidated savePreset function - pass id if editing, undefined if creating
      const savedPreset = await savePreset(
        label,
        query,
        selectedServers,
        selectedTools,
        selectedSkills, // Skill folder names for workflow
        editingPreset ? editingPreset.agentMode : agentMode,
        selectedFolder,
        llmConfig,
        useCodeExecutionMode,
        editingPreset?.id,
        enableContextSummarization,
        useToolSearchMode,
        enableBrowserAccess,
        undefined, // enableContextEditing
        selectedSecrets,
        selectedGlobalSecretNames,
        camofoxHeaded
      )

      // Apply the preset immediately if it's a new one
      if (savedPreset && !editingPreset) {
        handlePresetClick(savedPreset)
      }

      setShowPresetModal(false)
      setEditingPreset(null)
    } catch (error) {
      console.error('[ModePresetBar] Failed to save preset:', error)
    }
  }, [editingPreset, savePreset, handlePresetClick])

  const handleDeletePreset = useCallback(async (presetId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    if (confirm('Are you sure you want to delete this workflow? This action cannot be undone.')) {
      try {
        await deletePreset(presetId)
        setShowPresetDropdown(false)
      } catch (error) {
        console.error('Failed to delete preset:', error)
        alert('Failed to delete workflow. Please try again.')
      }
    }
  }, [deletePreset])

  const handleDuplicatePreset = useCallback(async (presetId: string, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const duplicatedPreset = await duplicatePreset(presetId)
      if (duplicatedPreset) {
        setShowPresetDropdown(false)
        handlePresetClick(duplicatedPreset)
      }
    } catch (error) {
      console.error('Failed to duplicate preset:', error)
      alert('Failed to duplicate workflow. Please try again.')
    }
  }, [duplicatePreset, handlePresetClick])

  // Refresh presets when switching to workflow mode
  useEffect(() => {
    if (selectedModeCategory === 'workflow') {
      // Refresh presets to ensure workflow presets are loaded
      refreshPresets().catch(error => {
        console.error('[ModePresetBar] Failed to refresh presets:', error)
      })
    }
  }, [selectedModeCategory, refreshPresets])

  // Refresh presets when dropdown is opened for workflow mode
  const handlePresetDropdownToggle = useCallback(() => {
    const newState = !showPresetDropdown
    setShowPresetDropdown(newState)

    // If opening dropdown and in workflow mode, ensure presets are refreshed
    if (newState && selectedModeCategory === 'workflow') {
      const currentPresets = getPresetsForMode('workflow')

      // Always refresh when opening dropdown in workflow mode to ensure latest presets
      if (currentPresets.length === 0 && !presetsLoading) {
        refreshPresets().catch(error => {
          console.error('[ModePresetBar] Failed to refresh presets when opening dropdown:', error)
        })
      }
    }
  }, [showPresetDropdown, selectedModeCategory, presetsLoading, refreshPresets, getPresetsForMode])

  // Close dropdowns when clicking outside
  useEffect(() => {
    const onMouseDown = (event: MouseEvent) => {
      const target = event.target as Element
      if (!target.closest('.preset-dropdown')) {
        setShowPresetDropdown(false)
      }
    }
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
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
    <>
      <div className="px-4 py-2 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
        <div className="flex items-center justify-between">
          {/* Left: Mode Indicator */}
          <div className="flex items-center gap-3">
            {/* Segmented control — single bordered container, active segment elevated */}
            <div className="flex items-center bg-gray-100 dark:bg-gray-800/80 border border-gray-200 dark:border-gray-700 rounded-lg p-0.5" role="tablist" aria-label="Select mode">
              {MODE_PILLS.map((mode) => {
                const isActive = selectedModeCategory === mode.key
                const Icon = mode.icon
                return (
                  <button
                    key={mode.key}
                    role="tab"
                    aria-selected={isActive}
                    aria-label={`Switch to ${mode.label} mode`}
                    onClick={() => setModeCategory(mode.key)}
                    className={`relative flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium transition-all duration-150 cursor-pointer ${
                      isActive ? mode.activeClasses : mode.inactiveClasses
                    }`}
                    type="button"
                  >
                    <Icon className="w-3 h-3" />
                    <span>{mode.label}</span>
                    {mode.key === 'workflow' && runningWorkflowCount > 0 && (
                      <span className="relative flex items-center">
                        <span className="flex items-center justify-center min-w-[16px] h-4 px-1 text-[10px] font-semibold text-white bg-green-500 rounded-full leading-none">
                          {runningWorkflowCount}
                        </span>
                        <span className="absolute inset-0 min-w-[16px] h-4 bg-green-500 rounded-full animate-ping opacity-30" />
                      </span>
                    )}
                  </button>
                )
              })}
            </div>

            {/* Center: Preset Information */}
            <div className="flex items-center gap-3">
              {/* Preset Information - Show ONLY for workflow mode */}
              {(() => {
                // For workflow mode only, always show preset selector
                // Chat mode no longer supports presets
                if (selectedModeCategory === 'workflow') {
                  return (
                    <div className="relative flex items-center">
                      <div className="flex items-center bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-md overflow-hidden">
                        <button
                          onClick={handlePresetDropdownToggle}
                          className="flex items-center gap-2 px-3 py-1 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                        >
                          {activePreset ? (
                            <>
                              <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                              <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
                                {activePreset.label}
                              </span>
                            </>
                          ) : (
                            <>
                              <div className="w-2 h-2 bg-gray-400 rounded-full"></div>
                              <span className="text-sm font-medium text-gray-500 dark:text-gray-400">
                                Select Workflow
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
                              setWorkspaceMinimized(true)
                            }}
                            className="px-2 py-1 border-l border-gray-200 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                            title="Edit workflow"
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
                          <div className="p-2 space-y-1 max-h-96 overflow-y-auto">
                            {/* Add New Workflow Option */}
                            <button
                              onClick={() => {
                                setEditingPreset(null)
                                setShowPresetModal(true)
                                setShowPresetDropdown(false)
                                setWorkspaceMinimized(true)
                              }}
                              className="w-full text-left p-2 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300"
                            >
                              <div className="flex items-center gap-2">
                                <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                                <span className="font-medium">+ Add Workflow</span>
                              </div>
                            </button>

                            {/* Loading state */}
                            {presetsLoading && (
                              <div className="p-2 text-sm text-gray-500 dark:text-gray-400 text-center">
                                Loading workflows...
                              </div>
                            )}

                            {/* No workflows message */}
                            {!presetsLoading && presetsForMode.length === 0 && (
                              <div className="p-2 text-sm text-gray-500 dark:text-gray-400 text-center">
                                No workflows available. Create one to get started.
                              </div>
                            )}

                            {/* Available Workflows */}
                            {!presetsLoading && presetsForMode.length > 0 && presetsForMode
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
                                            setWorkspaceMinimized(true)
                                          }}
                                          className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                                          title="Edit workflow"
                                        >
                                          <Settings className="w-3 h-3" />
                                        </button>
                                      )}
                                      <button
                                        onClick={(e) => handleDuplicatePreset(preset.id, e)}
                                        className="p-1 rounded hover:bg-blue-100 dark:hover:bg-blue-900/20 text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300"
                                        title="Duplicate workflow"
                                      >
                                        <Copy className="w-3 h-3" />
                                      </button>
                                      <button
                                        onClick={(e) => handleDeletePreset(preset.id, e)}
                                        className="p-1 rounded hover:bg-red-100 dark:hover:bg-red-900/20 text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300"
                                        title="Delete workflow"
                                      >
                                        <Trash2 className="w-3 h-3" />
                                      </button>
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
            </div>
          </div>

          {/* Right: icons */}
          <TooltipProvider delayDuration={400}>
            <div className="flex items-center gap-3">

              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => setShowShortcuts(true)}
                    className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                  >
                    <Keyboard className="w-4 h-4" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Keyboard shortcuts</TooltipContent>
              </Tooltip>

              {selectedModeCategory === 'multi-agent' && (
                <>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setShowPlansManager(true)}
                        className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                      >
                        <GitBranch className="w-4 h-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">Manage Plans</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => { setWorkspaceMinimized(true); setShowDelegationLogs(true) }}
                        className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                      >
                        <DollarSign className="w-4 h-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">Execution logs & costs</TooltipContent>
                  </Tooltip>
                </>
              )}

              {selectedModeCategory === 'workflow' && (
                <>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setShowSchedulePopup(true)}
                        className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                      >
                        <Clock className="w-4 h-4" />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">Schedule this workflow</TooltipContent>
                  </Tooltip>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setShowRunsPanel(true)}
                        className="relative p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                      >
                        <CalendarDays className="w-4 h-4" />
                        {workflowScheduleCount > 0 && (
                          <span className="absolute -top-1 -right-1 min-w-[14px] h-[14px] flex items-center justify-center rounded-full bg-amber-500 text-white text-[9px] font-bold leading-none px-0.5">
                            {workflowScheduleCount}
                          </span>
                        )}
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">Scheduled workflow runs</TooltipContent>
                  </Tooltip>
                </>
              )}

              {selectedModeCategory === 'chat' && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={() => setShowCostsPopup(true)}
                      className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                    >
                      <DollarSign className="w-4 h-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">Cost analysis</TooltipContent>
                </Tooltip>
              )}

            </div>
          </TooltipProvider>
        </div>
      </div>

      {/* Keyboard Shortcuts Modal */}
      {showShortcuts && (
        <div className="fixed inset-0 bg-black bg-opacity-50 flex items-center justify-center z-50">
          <div className="bg-white dark:bg-gray-800 rounded-lg p-6 max-w-md w-full mx-4">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
                Keyboard Shortcuts
              </h3>
              <button
                onClick={() => setShowShortcuts(false)}
                className="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
              >
                <svg className="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
            
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Multi-Agent Mode</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+1
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Workflow Mode</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+2
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Prototype Mode</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+3
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Switch to Chat Mode</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+4
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Minimize Sidebar</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+5
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Minimize Workspace</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+6
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Toggle Auto-scroll</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Ctrl+7
                </kbd>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-gray-700 dark:text-gray-300">Close Shortcuts</span>
                <kbd className="px-2 py-1 bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-200 text-xs rounded font-mono">
                  Esc
                </kbd>
              </div>
            </div>
            
            <div className="mt-4 pt-4 border-t border-gray-200 dark:border-gray-700">
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Use Ctrl on Windows/Linux or Cmd on Mac
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Preset Modal */}
      <PresetModal
        isOpen={showPresetModal}
        onClose={handleClosePresetModal}
        onSave={handleSavePreset}
        editingPreset={editingPreset}
        availableServers={availableServers}
        hideAgentModeSelection={!!editingPreset}
        fixedAgentMode={editingPreset?.agentMode || (selectedModeCategory ? (getAgentModeFromCategory(selectedModeCategory) as 'simple' | 'workflow') : undefined)}
        agentMode={agentMode}
      />

      {/* Cost Analysis Popup (Chat mode) */}
      <ChatCostsPopup
        isOpen={showCostsPopup}
        onClose={() => setShowCostsPopup(false)}
        isMultiAgent={false}
      />

      {/* Delegation Logs Popup (Multi-agent mode - combined logs + costs) */}
      <DelegationLogsPopup
        isOpen={showDelegationLogs}
        onClose={() => setShowDelegationLogs(false)}
      />

      {/* Scheduled Workflow Runs Panel */}
      {showRunsPanel && (
        <WorkflowScheduleRunsPanel onClose={() => setShowRunsPanel(false)} />
      )}

      {/* Schedule Preset Popup (workflow / multi-agent mode) */}
      {showSchedulePopup && (
        <SchedulePresetPopup
          presetQueryId={activePresetForSchedule?.id ?? null}
          presetLabel={activePresetForSchedule?.label ?? ''}
          entityType={selectedModeCategory === 'workflow' ? 'workflow' : 'chat'}
          workspacePath={activePresetForSchedule?.selectedFolder?.filepath}
          onClose={() => setShowSchedulePopup(false)}
        />
      )}

      {/* Plans Manager Modal (Multi-agent mode) */}
      <PlansManagerModal
        isOpen={showPlansManager}
        onClose={() => setShowPlansManager(false)}
        onSelectPlan={(folder) => {
          const chatStore = useChatStore.getState()
          const activeTabId = chatStore.activeTabId
          if (activeTabId) {
            const currentConfig = chatStore.getTabConfig(activeTabId)
            const existingFileContext = currentConfig?.fileContext ?? []
            const planName = folder.split('/').pop() || folder
            const alreadyInContext = existingFileContext.some(f => f.path === folder)
            if (!alreadyInContext) {
              chatStore.setTabConfig(activeTabId, {
                fileContext: [...existingFileContext, { name: planName, path: folder, type: 'folder' as const }]
              })
            }
          }
          agentApi.updatePlannerFile(`${folder}/.last_used`, new Date().toISOString()).catch(() => {})
        }}
      />
    </>
  )
}
