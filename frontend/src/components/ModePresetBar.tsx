import React, { useState, useEffect, useCallback, useRef } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { Settings, Copy, LayoutDashboard, ArrowLeft, Eye, Plus } from 'lucide-react'
import { useAuthStore } from '../stores/useAuthStore'
import { isWorkflowReadOnly } from '../utils/workflowPermissions'
import { useModeStore } from '../stores/useModeStore'
import { useGlobalPresetStore, usePresetApplication, usePresetManagement } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { PlannerFile, PresetLLMConfig, WorkflowManifest } from '../services/api-types'
import PresetModal from './PresetModal'
import { agentApi, workflowManifestApi } from '../services/api'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip'
import ModalPortal from './ui/ModalPortal'
import { useChatStore, useLLMStore } from '../stores'
import { useMCPStore } from '../stores/useMCPStore'
import { useAppStore } from '../stores/useAppStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import { dedupeServerNames } from '../utils/mcpServerAlias'
import { GlobalActivityMonitor } from './GlobalActivityMonitor'
import WorkflowWalkthrough from './workflow/WorkflowWalkthrough'
import { ProductSurfaceSwitcher } from './ProductSurfaceSwitcher'
import WorkspaceTopBarControls from './WorkspaceTopBarControls'
import ProvidersControl from './topbar/ProvidersControl'
import ConfirmationDialog from './ui/ConfirmationDialog'
import {
  LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT,
  LLM_DISCOVERY_ONBOARDING_OPENED_EVENT,
  dismissWorkflowWalkthrough,
  getLLMDiscoveryOnboardingState,
  isWorkflowWalkthroughDismissed,
} from '../utils/onboarding'
import { openWorkflowPresetPage } from '../utils/workflowSessionRestore'
import { currentActiveSession, currentSessionId, headerStatusLabel } from '../utils/globalActivityMonitorStatus'

const workflowManifestToPreset = (manifest: WorkflowManifest, workspacePath: string): CustomPreset => {
  const caps = manifest.capabilities
  return {
    id: manifest.id || workspacePath,
    label: manifest.label || workspacePath.split('/').pop() || workspacePath,
    createdAt: new Date(manifest.created_at || 0).getTime(),
    agentMode: 'workflow',
    selectedFolder: {
      filepath: workspacePath,
      content: '',
      last_modified: manifest.updated_at || '',
      type: 'folder',
      children: [],
    },
    selectedServers: caps?.selected_servers || [],
    selectedTools: caps?.selected_tools || [],
    selectedSkills: caps?.selected_skills || [],
    selectedSecrets: caps?.selected_secrets || [],
    selectedGlobalSecretNames: caps?.selected_global_secret_names ?? null,
    browserMode: (caps?.browser_mode || 'none') as CustomPreset['browserMode'],
    cdpPorts: caps?.cdp_ports || [],
    useCodeExecutionMode: caps?.use_code_execution_mode || false,
    llmConfig: caps?.llm_config ? { ...caps.llm_config } : undefined,
    employee_id: manifest.ownership?.employee_id ?? undefined,
  }
}

/**
 * Global Mode & Preset Bar - always visible at the top level
 * Allows users to select mode (multi-agent/workflow) and presets regardless of active tabs
 */
export const ModePresetBar: React.FC = () => {
  const { selectedModeCategory, setModeCategory, getAgentModeFromCategory } = useModeStore(useShallow(state => ({
    selectedModeCategory: state.selectedModeCategory,
    setModeCategory: state.setModeCategory,
    getAgentModeFromCategory: state.getAgentModeFromCategory,
  })))
  const presetModeCategory = selectedModeCategory === 'workflow' || selectedModeCategory === 'multi-agent'
    ? selectedModeCategory
    : null
  const { setWorkspaceMinimized, agentMode } = useAppStore(useShallow(state => ({
    setWorkspaceMinimized: state.setWorkspaceMinimized,
    agentMode: state.agentMode,
  })))
  const isReadOnlyUser = useAuthStore(state => isWorkflowReadOnly(state.user, state.isMultiUserMode))
  // Use toolList to get all available servers, not just enabled ones
  const toolList = useMCPStore(state => state.toolList)
  const availableServers = React.useMemo(() =>
    [...new Set(toolList.map(t => t.server).filter(Boolean) as string[])],
    [toolList]
  )

  // Use the new global preset store
  const {
    workflowPresets,
    savePreset,
    duplicatePreset,
    refreshPresets,
    loading: presetsLoading
  } = usePresetManagement()

  const {
    applyPreset,
    getActivePreset,
    isPresetActive,
    getPresetsForMode,
    clearActivePreset,
  } = usePresetApplication()

  // Get active preset for current mode (for schedule popup, supports all modes)
  const activePreset = presetModeCategory === null
    ? null
    : getActivePreset(presetModeCategory)
  const activeWorkspacePath = activePreset?.selectedFolder?.filepath?.replace(/\/+$/, '')
  const activeWorkflowAccess = useWorkflowManifestStore(state =>
    activeWorkspacePath ? state.workflows.find(workflow => workflow.workspace_path === activeWorkspacePath)?.my_access : undefined,
  )
  // A member may create their own workflows while being a reader on this
  // particular workflow. Use the manifest-level answer for this header, not
  // only the account-wide role.
  const isActiveWorkflowReadOnly = selectedModeCategory === 'workflow' && activeWorkflowAccess === 'read'
  const isEffectiveReadOnly = isReadOnlyUser || isActiveWorkflowReadOnly
  // Get presets for current mode
  const presetsForMode = presetModeCategory === null
    ? []
    : getPresetsForMode(presetModeCategory)

  const [showPresetDropdown, setShowPresetDropdown] = useState(false)
  const [showPresetModal, setShowPresetModal] = useState(false)
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showWorkflowWalkthrough, setShowWorkflowWalkthrough] = useState(false)
  const [workflowWalkthroughOpenToken, setWorkflowWalkthroughOpenToken] = useState(0)
  const [pendingDuplicatePreset, setPendingDuplicatePreset] = useState<{ id: string; label: string } | null>(null)
  const [duplicatingPreset, setDuplicatingPreset] = useState(false)
  const pendingAutoWalkthroughAfterLLMDiscoveryRef = useRef(false)
  const evaluatedAutoWalkthroughRef = useRef(false)
  const showWorkflowsOverview = useAppStore(s => s.showWorkflowsOverview)
  const setShowWorkflowsOverview = useAppStore(s => s.setShowWorkflowsOverview)
  const setSelectedFile = useWorkspaceStore(state => state.setSelectedFile)
  const setShowFileContent = useWorkspaceStore(state => state.setShowFileContent)
  const showProviders = useLLMStore(state => state.showLLMModal)
  const isOrganizationView = showWorkflowsOverview
  const isGlobalPage = showWorkflowsOverview || showProviders

  // GlobalActivityMonitor excludes only the current session from its pills.
  // A simultaneous scheduled run for the same workflow remains a separate
  // activity, while this selector gives the current session its own live
  // running/needs-input/idle status.
  // This mirrors GlobalActivityMonitor's own currentSessionId derivation so
  // the two stay in agreement about which session is "current".
  const activeSessionsCache = useChatStore(state => state.activeSessionsCache)
  const activeTabId = useChatStore(state => state.activeTabId)
  const chatTabs = useChatStore(state => state.chatTabs)
  const currentSession = currentActiveSession(
    activeSessionsCache,
    currentSessionId(activeTabId, chatTabs, selectedModeCategory, isGlobalPage),
  )
  const currentSessionStatusLabel = currentSession ? headerStatusLabel(currentSession) : null

  const openWorkflowWalkthrough = useCallback(() => {
    setWorkflowWalkthroughOpenToken(token => token + 1)
    setShowWorkflowWalkthrough(true)
  }, [])

  const closeWorkflowWalkthrough = useCallback(() => {
    setShowWorkflowWalkthrough(false)
    dismissWorkflowWalkthrough()
  }, [])

  useEffect(() => {
    const handleOpenWalkthrough = () => openWorkflowWalkthrough()
    window.addEventListener('open-workflow-walkthrough', handleOpenWalkthrough)
    return () => window.removeEventListener('open-workflow-walkthrough', handleOpenWalkthrough)
  }, [openWorkflowWalkthrough])

  useEffect(() => {
    const handleLLMDiscoveryOpened = () => {
      setShowWorkflowWalkthrough(false)
    }

    const handleLLMDiscoveryCleared = () => {
      if (!pendingAutoWalkthroughAfterLLMDiscoveryRef.current) return
      pendingAutoWalkthroughAfterLLMDiscoveryRef.current = false
      if (!isWorkflowWalkthroughDismissed()) {
        openWorkflowWalkthrough()
      }
    }

    window.addEventListener(LLM_DISCOVERY_ONBOARDING_OPENED_EVENT, handleLLMDiscoveryOpened)
    window.addEventListener(LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT, handleLLMDiscoveryCleared)
    return () => {
      window.removeEventListener(LLM_DISCOVERY_ONBOARDING_OPENED_EVENT, handleLLMDiscoveryOpened)
      window.removeEventListener(LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT, handleLLMDiscoveryCleared)
    }
  }, [openWorkflowWalkthrough])

  useEffect(() => {
    if (evaluatedAutoWalkthroughRef.current) return
    evaluatedAutoWalkthroughRef.current = true
    if (isWorkflowWalkthroughDismissed()) return

    const llmDiscoveryState = getLLMDiscoveryOnboardingState()
    if (llmDiscoveryState === 'cleared') {
      openWorkflowWalkthrough()
      return
    }

    pendingAutoWalkthroughAfterLLMDiscoveryRef.current = true
  }, [openWorkflowWalkthrough])

  const returnToWorkspace = useCallback(() => {
    useLLMStore.getState().setShowLLMModal(false)
    setShowWorkflowsOverview(false)
  }, [setShowWorkflowsOverview])

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

  const handleEditWorkflowPreset = useCallback(async (preset: CustomPreset) => {
    const workspacePath = preset.selectedFolder?.filepath
    if (!workspacePath) {
      setEditingPreset(preset)
      setShowPresetModal(true)
      return
    }

    try {
      const response = await workflowManifestApi.getWorkflowManifest(workspacePath)
      setEditingPreset(workflowManifestToPreset(response.manifest, response.workspace_path || workspacePath))
    } catch (error) {
      console.error('[ModePresetBar] Failed to load latest workflow manifest before edit:', error)
      setEditingPreset(preset)
    }

    setShowPresetModal(true)
  }, [])

  // Keep the create action discoverable beside the automation selector. It is
  // intentionally rendered for read-only accounts too (so the role can see
  // how automations are added), but the account-level read-only policy keeps
  // the action disabled rather than opening a form that can never be saved.
  const handleAddWorkflow = useCallback(() => {
    if (isReadOnlyUser) return
    setEditingPreset(null)
    setShowPresetDropdown(false)
    setShowPresetModal(true)
    setWorkspaceMinimized(true)
  }, [isReadOnlyUser, setWorkspaceMinimized])

  // Listen for external trigger to open preset settings (e.g. from workflow toolbar)
  const showPresetSettings = useCommandDialogStore(s => s.showPresetSettings)
  useEffect(() => {
    if (showPresetSettings) {
      useCommandDialogStore.getState().closeDialog('presetSettings')
      const preset = presetModeCategory === null
        ? null
        : getActivePreset(presetModeCategory)
      if (preset) {
        handleEditWorkflowPreset(preset as CustomPreset)
      }
    }
  }, [showPresetSettings, presetModeCategory, getActivePreset, handleEditWorkflowPreset])

  // Preset click handler - now uses the global store
  const handlePresetClick = useCallback((preset: CustomPreset | PredefinedPreset) => {
    // Determine the mode category based on the preset's agentMode
    const presetModeCategory = preset.agentMode === 'workflow' ? 'workflow' : 'multi-agent'

    if (presetModeCategory === 'workflow') {
      setShowPresetDropdown(false)
      void openWorkflowPresetPage(preset, {
        title: preset.label,
        source: 'workflow-dropdown',
      }).catch(error => {
        console.error('Failed to open automation:', error)
      })
      return
    }

    // If preset is for simple mode, route to multi-agent category
    if (presetModeCategory === 'multi-agent' && selectedModeCategory !== 'multi-agent') {
      setModeCategory('multi-agent')
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
    agentMode?: 'multi-agent' | 'workflow',
    selectedFolder?: PlannerFile,
    llmConfig?: PresetLLMConfig,
    useCodeExecutionMode?: boolean,
    selectedSecrets?: string[],
    selectedGlobalSecretNames?: string[] | null,
    browserMode?: 'none' | 'auto' | 'headless' | 'cdp',
    cdpPorts?: number[]
  ) => {
    try {
      const effectiveMode = editingPreset ? editingPreset.agentMode : agentMode
      const globalSecretNamesForBackend = selectedGlobalSecretNames === undefined
        ? (editingPreset?.selectedGlobalSecretNames === undefined ? [] : editingPreset.selectedGlobalSecretNames)
        : selectedGlobalSecretNames
      // PLAT-169: collapse any hyphen/underscore-alias duplicate before this
      // ever reaches the backend. ToolSelectionSection.tsx no longer creates
      // new ones, but a manifest saved before that fix (or edited outside
      // the UI) can still carry both spellings — this self-heals it on the
      // very next successful save instead of permanently failing the
      // backend's duplicate validation until someone hand-edits the JSON.
      const dedupedSelectedServers = selectedServers ? dedupeServerNames(selectedServers) : selectedServers

      // Workflow mode: save to file-backed manifest (not DB)
      if (effectiveMode === 'workflow' && editingPreset?.selectedFolder?.filepath) {
        const workspacePath = editingPreset.selectedFolder.filepath
        const payload = {
          workspace_path: workspacePath,
          label,
          capabilities: {
            selected_servers: dedupedSelectedServers || [],
            selected_tools: selectedTools || [],
            selected_skills: selectedSkills || [],
            selected_secrets: selectedSecrets || [],
            selected_global_secret_names: globalSecretNamesForBackend,
            browser_mode: browserMode || 'none',
            cdp_ports: browserMode === 'cdp' || browserMode === 'auto' ? (cdpPorts || []) : [],
            use_code_execution_mode: useCodeExecutionMode || false,
            llm_config: llmConfig || undefined,
          },
        }
        await agentApi.updateWorkflowManifest(payload)
        // Refresh manifests and rebuild workflow presets in zustand (triggers re-renders)
        await refreshPresets()
        return true
      }

      // Multi-agent mode: save the user's chat capability profile.
      const savedPreset = await savePreset(
        label,
        query,
        dedupedSelectedServers,
        selectedTools,
        selectedSkills,
        effectiveMode,
        selectedFolder,
        llmConfig,
        useCodeExecutionMode,
        editingPreset?.id,
        selectedSecrets,
        selectedGlobalSecretNames,
        browserMode,
        cdpPorts
      )

      // Apply the preset immediately if it's a new one
      if (savedPreset && !editingPreset) {
        handlePresetClick(savedPreset)
      }

      setShowPresetModal(false)
      setEditingPreset(null)
      return true
    } catch (error) {
      console.error('[ModePresetBar] Failed to save preset:', error)
      // Surface the failure — previously this was swallowed (no toast), so a
      // rejected manifest save looked like a silent no-op. The server returns
      // the validation reason as the response body.
      const serverDetail = (error as { response?: { data?: unknown } })?.response?.data
      const detail =
        typeof serverDetail === 'string' && serverDetail.trim() !== ''
          ? serverDetail.trim()
          : error instanceof Error
            ? error.message
            : 'Unknown error'
      useChatStore.getState().addToast(`Failed to save configuration: ${detail}`, 'error')
      return false
    }
  }, [editingPreset, savePreset, handlePresetClick, refreshPresets])

  const handleDeleteWorkflow = useCallback(async (preset: CustomPreset) => {
    const workspacePath = preset.selectedFolder?.filepath
    if (!workspacePath) {
      throw new Error('Automation folder is missing')
    }

    try {
      const deletingActiveWorkflow = useGlobalPresetStore.getState().activePresetIds.workflow === preset.id
      await agentApi.deleteWorkflowFolder(workspacePath)

      if (deletingActiveWorkflow) {
        clearActivePreset('workflow')
        useGlobalPresetStore.getState().setSelectedPresetFolder(null)
        useGlobalPresetStore.getState().setCurrentPresetServers([])
        useGlobalPresetStore.getState().setCurrentPresetTools([])
        useGlobalPresetStore.getState().setCurrentQuery('')
        setSelectedFile(null)
        setShowFileContent(false)
      }

      await refreshPresets()

      setShowPresetModal(false)
      setEditingPreset(null)
      setShowPresetDropdown(false)
    } catch (error) {
      console.error('[ModePresetBar] Failed to delete workflow:', error)
      alert('Failed to delete automation. Please try again.')
      throw error
    }
  }, [clearActivePreset, refreshPresets, setSelectedFile, setShowFileContent])

  const requestDuplicatePreset = useCallback((preset: CustomPreset | PredefinedPreset, e: React.MouseEvent) => {
    e.stopPropagation()
    setShowPresetDropdown(false)
    setPendingDuplicatePreset({ id: preset.id, label: preset.label })
  }, [])

  const handleDuplicatePresetConfirm = useCallback(async () => {
    if (!pendingDuplicatePreset || duplicatingPreset) return

    setDuplicatingPreset(true)
    try {
      const duplicatedPreset = await duplicatePreset(pendingDuplicatePreset.id)
      if (duplicatedPreset) {
        setPendingDuplicatePreset(null)
        handlePresetClick(duplicatedPreset)
      }
    } catch (error) {
      console.error('Failed to duplicate preset:', error)
      useChatStore.getState().addToast('Failed to duplicate automation. Please try again.', 'error')
    } finally {
      setDuplicatingPreset(false)
    }
  }, [duplicatePreset, duplicatingPreset, handlePresetClick, pendingDuplicatePreset])

  // Refresh presets when switching to workflow mode
  useEffect(() => {
    if (selectedModeCategory === 'workflow' && workflowPresets.length === 0 && !presetsLoading) {
      refreshPresets().catch(error => {
        console.error('[ModePresetBar] Failed to refresh presets:', error)
      })
    }
  }, [selectedModeCategory, workflowPresets.length, presetsLoading, refreshPresets])

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

  useEffect(() => {
    if (isOrganizationView && showPresetDropdown) {
      setShowPresetDropdown(false)
    }
  }, [isOrganizationView, showPresetDropdown])

  return (
    <>
      <div className="px-4 py-2 bg-gray-50 dark:bg-gray-800/50 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
        <div className="flex flex-wrap items-center justify-between gap-3 md:flex-nowrap">
          {/* Product and current automation */}
          <div className="flex min-w-0 items-center gap-3">
            {/* Product-level navigation stays separate from AgentWorks modes. */}
            <ProductSurfaceSwitcher className="mr-1" />

            {isGlobalPage && (
              <button
                type="button"
                onClick={returnToWorkspace}
                className="flex shrink-0 items-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                aria-label="Back to workspace"
              >
                <ArrowLeft className="h-4 w-4" />
                <span className="hidden sm:inline">Back</span>
              </button>
            )}

            {isEffectiveReadOnly && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <div className="flex shrink-0 items-center gap-1 rounded-md border border-amber-200 bg-amber-50 px-2 py-1 text-xs font-medium text-amber-700 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
                    <Eye className="w-3 h-3" />
                    <span>Read-only</span>
                  </div>
                </TooltipTrigger>
                <TooltipContent side="bottom">
                  {isActiveWorkflowReadOnly
                    ? 'This automation is shared with you read-only — you can chat and watch runs, but cannot edit its plans, secrets, schedules, or config.'
                    : "Your account has read-only workflow access — you can chat and watch runs, but can't edit plans, secrets, schedules, or config."}
                </TooltipContent>
              </Tooltip>
            )}

            {/* Center: Preset Information */}
            <div className="flex min-w-0 items-center gap-3">
              {/* Preset Information - Show ONLY for workflow mode */}
              {(() => {
                // For workflow mode only, always show preset selector
                // Chat mode no longer supports presets
                if (selectedModeCategory === 'workflow') {
                  return (
                    <div className="relative flex min-w-0 items-center">
                      <div
                        data-tour="workflow-add-edit"
                        data-testid="tour-workflow-add-edit"
                        className="flex min-w-0 items-center bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-md overflow-hidden"
                      >
                        <button
                          onClick={() => {
                            if (isGlobalPage) returnToWorkspace()
                            handlePresetDropdownToggle()
                          }}
                          className="flex min-w-0 items-center gap-2 px-3 py-1 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                          title={currentSessionStatusLabel ? `${activePreset?.label ?? ''} · ${currentSessionStatusLabel}` : undefined}
                        >
                          {activePreset ? (
                            <>
                              {/* Live run status is Global Monitor's job, not this
                                  selector's — it was the same information in two
                                  places. The status text stays in this button's
                                  tooltip (title) for anyone who wants it here. */}
                              <div className="w-2 h-2 shrink-0 bg-green-500 rounded-full"></div>
                              <span className="block max-w-[190px] truncate whitespace-nowrap text-sm font-medium text-gray-700 dark:text-gray-300">
                                {activePreset.label}
                              </span>
                            </>
                          ) : (
                            <>
                              <div className="w-2 h-2 shrink-0 bg-gray-400 rounded-full"></div>
                              <span className="block max-w-[190px] truncate whitespace-nowrap text-sm font-medium text-gray-500 dark:text-gray-400">
                                Select Automation
                              </span>
                            </>
                          )}
                        </button>

                        {/* Settings gear icon - separate clickable element.
                            Hidden for read-only users (PLAT-262): opens the
                            automation's edit settings, which the backend
                            won't let a read-only session mutate anyway. */}
                        {activePreset && !isEffectiveReadOnly && (
                          <button
                            onClick={(e) => {
                              e.stopPropagation()
                              handleEditWorkflowPreset(activePreset as CustomPreset)
                              setWorkspaceMinimized(true)
                            }}
                            className="px-2 py-1 border-l border-gray-200 dark:border-gray-600 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                            title="Edit automation"
                          >
                            <Settings className="w-3 h-3 text-gray-400" />
                          </button>
                        )}

                        {/* Settings gear icon for when no preset is selected */}
                        {!activePreset && !isReadOnlyUser && (
                          <div className="px-2 py-1 border-l border-gray-200 dark:border-gray-600">
                            <Settings className="w-3 h-3 text-gray-300" />
                          </div>
                        )}

                        {/* Keep creation discoverable next to the selected
                            automation for every role. Account-level readers
                            see the affordance but cannot submit a new one. */}
                        <button
                          type="button"
                          data-testid="add-workflow-button"
                          aria-label="Add automation"
                          onClick={handleAddWorkflow}
                          disabled={isReadOnlyUser}
                          title={isReadOnlyUser ? 'Read-only users cannot create automations' : 'Add automation'}
                          className="border-l border-gray-200 px-2 py-1 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:border-gray-600 dark:text-gray-400 dark:hover:bg-slate-700 dark:hover:text-gray-200"
                        >
                          <Plus className="h-3 w-3" />
                        </button>
                      </div>

                      {/* Preset Dropdown */}
                      {showPresetDropdown && (
                        <div className="preset-dropdown absolute top-full left-0 mt-1 w-64 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg shadow-lg z-50">
                          <div className="p-2 space-y-1 max-h-96 overflow-y-auto">
                            {/* Add New Workflow Option */}
                            <button
                              onClick={handleAddWorkflow}
                              disabled={isReadOnlyUser}
                              title={isReadOnlyUser ? 'Read-only users cannot create automations' : undefined}
                              className="w-full text-left p-2 rounded-md text-sm hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300"
                            >
                              <div className="flex items-center gap-2">
                                <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
                                <span className="font-medium">+ Add Automation</span>
                              </div>
                            </button>

                            {/* Loading state */}
                            {presetsLoading && (
                              <div className="p-2 text-sm text-gray-500 dark:text-gray-400 text-center">
                                Loading automations...
                              </div>
                            )}

                            {/* No workflows message */}
                            {!presetsLoading && presetsForMode.length === 0 && (
                              <div className="p-2 text-sm text-gray-500 dark:text-gray-400 text-center">
                                No automations available. Create one to get started.
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
                                      presetModeCategory !== null && isPresetActive(preset.id, presetModeCategory)
                                        ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                                        : 'hover:bg-gray-100 dark:hover:bg-slate-700 text-gray-700 dark:text-gray-300'
                                    }`}
                                  >
                                    <div className="flex items-center gap-2">
                                      <div className="w-2 h-2 shrink-0 bg-green-500 rounded-full"></div>
                                      <div className="flex-1">
                                        <div className="font-medium">{preset.label}</div>
                                      </div>
                                    </div>
                                  </button>

                                  {/* Edit/Duplicate/Delete buttons */}
                                  {(
                                    <div className="flex gap-1">
                                      {presetModeCategory !== null && isPresetActive(preset.id, presetModeCategory) && !isEffectiveReadOnly && (
                                        <button
                                          onClick={(e) => {
                                            e.stopPropagation()
                                            handleEditWorkflowPreset(preset as CustomPreset)
                                            setShowPresetDropdown(false)
                                            setWorkspaceMinimized(true)
                                          }}
                                          className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-300"
                                          title="Edit automation"
                                        >
                                          <Settings className="w-3 h-3" />
                                        </button>
                                      )}
                                      <button
                                        onClick={(e) => requestDuplicatePreset(preset, e)}
                                        className="p-1 rounded hover:bg-blue-100 dark:hover:bg-blue-900/20 text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300"
                                        title="Duplicate automation"
                                      >
                                        <Copy className="w-3 h-3" />
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
            <div className="flex shrink-0 items-center gap-2">
              <GlobalActivityMonitor />

              <ProvidersControl />

              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => {
                      useLLMStore.getState().setShowLLMModal(false)
                      setShowWorkflowsOverview(true)
                    }}
                    data-tour="global-activity"
                    aria-label="Activity"
                    aria-pressed={showWorkflowsOverview && !showProviders}
                    className={`rounded-md p-1.5 transition-colors ${showWorkflowsOverview && !showProviders
                      ? 'bg-primary/10 text-primary'
                      : 'text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  >
                    <LayoutDashboard className="h-4 w-4" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Activity · Updates and schedules</TooltipContent>
              </Tooltip>

              <span className="mx-0.5 h-5 w-px bg-gray-200 dark:bg-gray-700" />
              <WorkspaceTopBarControls
                onOpenWalkthrough={openWorkflowWalkthrough}
                onOpenShortcuts={() => setShowShortcuts(true)}
              />

            </div>
          </TooltipProvider>
        </div>
      </div>

      {/* Keyboard Shortcuts & Tips Modal */}
      {showShortcuts && (
        <ModalPortal>
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-[9999] p-4" onClick={() => setShowShortcuts(false)}>
            <div role="dialog" aria-modal="true" aria-labelledby="keyboard-shortcuts-title" className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-lg max-h-[calc(100vh-2rem)] overflow-hidden text-gray-900 dark:text-gray-100 flex flex-col" onClick={e => e.stopPropagation()}>
              {/* Header */}
              <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                <h3 id="keyboard-shortcuts-title" className="text-base font-semibold">Keyboard Shortcuts</h3>
                <button
                  onClick={() => setShowShortcuts(false)}
                  aria-label="Close keyboard shortcuts"
                  className="p-1 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                >
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                  </svg>
                </button>
              </div>

              <div className="px-5 py-4 space-y-5 overflow-y-auto min-h-0 flex-1">
                {/* Quick Switcher — featured */}
                <div className="rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 p-3.5">
                  <div className="flex items-center justify-between mb-2">
                    <div className="flex items-center gap-2">
                      <svg className="w-4 h-4 text-blue-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
                      <span className="text-sm font-semibold text-blue-700 dark:text-blue-300">Quick Switcher</span>
                    </div>
                    <kbd className="px-2 py-1 bg-blue-100 dark:bg-blue-800 text-blue-700 dark:text-blue-200 text-xs rounded font-mono font-semibold">Ctrl+K</kbd>
                  </div>
                  <p className="text-xs text-blue-600 dark:text-blue-400 leading-relaxed">
                    Search automations, chats, active work, and retained events. Use @active or @events to narrow the list.
                  </p>
                </div>

                {/* Mode Switching */}
                <div>
                  <p className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2.5">Modes</p>
                  <div className="space-y-1.5">
                    {[
                      ['Automation', 'Ctrl+1'],
                      ['Activity', 'Ctrl+3'],
                    ].map(([label, key]) => (
                      <div key={key} className="flex items-center justify-between py-1">
                        <span className="text-sm text-gray-600 dark:text-gray-300">{label}</span>
                        <kbd className="px-2 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 text-xs rounded font-mono">{key}</kbd>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Layout */}
                <div>
                  <p className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mb-2.5">Layout</p>
                  <div className="space-y-1.5">
                    {[
                      ['Minimize Workspace', 'Ctrl+6'],
                      ['Toggle Auto-scroll', 'Ctrl+7'],
                      ['New Chat', 'Ctrl+N'],
                    ].map(([label, key]) => (
                      <div key={key} className="flex items-center justify-between py-1">
                        <span className="text-sm text-gray-600 dark:text-gray-300">{label}</span>
                        <kbd className="px-2 py-0.5 bg-gray-100 dark:bg-gray-700 text-gray-600 dark:text-gray-300 text-xs rounded font-mono">{key}</kbd>
                      </div>
                    ))}
                  </div>
                </div>

                {/* Multi-Workflow Power Feature */}
                <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 p-3.5">
                  <div className="flex items-center gap-2 mb-2.5">
                    <svg className="w-4 h-4 text-purple-500" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" /></svg>
                    <span className="text-sm font-semibold text-gray-700 dark:text-gray-200">Parallel Automations</span>
                  </div>
                  <div className="space-y-2 text-xs text-gray-500 dark:text-gray-400 leading-relaxed">
                    <div className="flex gap-2">
                      <span className="text-purple-400 mt-0.5">&#9679;</span>
                      <span>Run multiple automations simultaneously &mdash; each has isolated tabs, execution state, and canvas</span>
                    </div>
                    <div className="flex gap-2">
                      <span className="text-purple-400 mt-0.5">&#9679;</span>
                      <span>Use <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-[10px] font-mono">Ctrl+K</kbd> to jump between them instantly &mdash; chat context, streaming, and builder state are all preserved</span>
                    </div>
                    <div className="flex gap-2">
                      <span className="text-purple-400 mt-0.5">&#9679;</span>
                      <span>Start execution on one automation, switch to another to build/edit, and switch back to check progress</span>
                    </div>
                    <div className="flex gap-2">
                      <span className="text-purple-400 mt-0.5">&#9679;</span>
                      <span>Each automation&apos;s stop button only affects its own execution &mdash; other automations keep running</span>
                    </div>
                  </div>
                </div>

                <p className="text-[11px] text-gray-400 dark:text-gray-500 text-center">
                  Use Ctrl on Windows/Linux or Cmd on Mac
                </p>
              </div>
            </div>
          </div>
        </ModalPortal>
      )}

      {/* Preset Modal */}
      <PresetModal
        isOpen={showPresetModal}
        onClose={handleClosePresetModal}
        onSave={handleSavePreset}
        editingPreset={editingPreset}
        availableServers={availableServers}
        hideAgentModeSelection={!!editingPreset}
        fixedAgentMode={editingPreset?.agentMode || (selectedModeCategory ? (getAgentModeFromCategory(selectedModeCategory) as 'multi-agent' | 'workflow') : undefined)}
        agentMode={agentMode}
        onDeleteWorkflow={handleDeleteWorkflow}
      />

      <WorkflowWalkthrough
        isOpen={showWorkflowWalkthrough}
        onClose={closeWorkflowWalkthrough}
        openToken={workflowWalkthroughOpenToken}
      />

      <ConfirmationDialog
        isOpen={pendingDuplicatePreset !== null}
        onClose={() => {
          if (!duplicatingPreset) setPendingDuplicatePreset(null)
        }}
        onConfirm={handleDuplicatePresetConfirm}
        title="Duplicate automation?"
        message={`Create a copy of "${pendingDuplicatePreset?.label || 'this automation'}"? The copy will include its workflow files and configuration.`}
        confirmText="Duplicate"
        type="info"
        isLoading={duplicatingPreset}
        loadingText="Duplicating..."
      />

    </>
  )
}
