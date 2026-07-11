import React, { useState, useEffect, useCallback, useRef } from 'react'
import { Workflow, Users, Settings, Copy, Keyboard, Bot, Building2, HelpCircle } from 'lucide-react'
import { useModeStore } from '../stores/useModeStore'
import { useGlobalPresetStore, usePresetApplication, usePresetManagement } from '../stores/useGlobalPresetStore'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { PlannerFile, PresetLLMConfig, ScheduledJob, WorkflowManifest } from '../services/api-types'
import PresetModal from './PresetModal'
import WorkflowScheduleRunsPanel from './scheduler/WorkflowScheduleRunsPanel'
import BotConnectorModal from './settings/BotConnectorModal'
import { WorkflowsOverviewPopup } from './WorkflowsOverviewPage'
import { schedulerApi } from '../api/scheduler'
import { agentApi, workflowManifestApi } from '../services/api'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from './ui/tooltip'
import ModalPortal from './ui/ModalPortal'
import { useChatStore } from '../stores'
import { useMCPStore } from '../stores/useMCPStore'
import { useAppStore } from '../stores/useAppStore'
import { useCommandDialogStore } from '../stores/useCommandDialogStore'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { GlobalActivityMonitor } from './GlobalActivityMonitor'
import WorkflowWalkthrough from './workflow/WorkflowWalkthrough'
import { RunloopLockup } from './branding/RunloopLogo'
import WorkspaceTopBarControls from './WorkspaceTopBarControls'
import { useAppVersion } from './topbar/useAppVersion'
import ConfirmationDialog from './ui/ConfirmationDialog'
import {
  LLM_DISCOVERY_ONBOARDING_CLEARED_EVENT,
  LLM_DISCOVERY_ONBOARDING_OPENED_EVENT,
  dismissWorkflowWalkthrough,
  getLLMDiscoveryOnboardingState,
  isWorkflowWalkthroughDismissed,
} from '../utils/onboarding'
import { openWorkflowPresetPage } from '../utils/workflowSessionRestore'

const getModeIcon = (category: string) => {
  switch (category) {
    case 'workflow':
      return <Workflow className="w-3 h-3" />
    case 'multi-agent':
      return <Users className="w-3 h-3" />
    default:
      return <Users className="w-3 h-3" />
  }
}

const getModeName = (category: string) => {
  switch (category) {
    case 'workflow':
      return 'Automation Mode'
    case 'multi-agent':
      return 'Chief of Staff'
    default:
      return 'Chief of Staff'
  }
}

const MODE_PILLS = [
  {
    key: 'workflow' as const,
    label: 'Automation',
    icon: Workflow,
    activeClasses: 'bg-purple-50 text-purple-700 shadow-sm ring-1 ring-purple-200 dark:bg-purple-500/20 dark:text-purple-100 dark:ring-purple-500/40',
    inactiveClasses: 'text-gray-500 dark:text-gray-400',
  },
  {
    key: 'multi-agent' as const,
    label: 'Chief of Staff',
    icon: Users,
    activeClasses: 'bg-indigo-50 text-indigo-700 shadow-sm ring-1 ring-indigo-200 dark:bg-indigo-500/20 dark:text-indigo-100 dark:ring-indigo-500/40',
    inactiveClasses: 'text-gray-500 dark:text-gray-400',
  },
] as const

type WorkflowScheduleSummary = {
  scheduledWorkflows: number
  runningWorkflows: number
  totalSchedules: number
  runningSchedules: number
}

const EMPTY_WORKFLOW_SCHEDULE_SUMMARY: WorkflowScheduleSummary = {
  scheduledWorkflows: 0,
  runningWorkflows: 0,
  totalSchedules: 0,
  runningSchedules: 0,
}

const WORKFLOW_SCHEDULE_HEADER_LIMIT = 10_000

const getWorkflowScheduleKey = (job: ScheduledJob): string => (
  job.workflow_id ||
  job.preset_query_id ||
  job.workspace_path ||
  job.workflow_label ||
  job.name ||
  job.id
)

const summarizeWorkflowSchedules = (jobs: ScheduledJob[]): WorkflowScheduleSummary => {
  const scheduledWorkflowKeys = new Set<string>()
  const runningWorkflowKeys = new Set<string>()
  let runningSchedules = 0

  jobs.forEach((job) => {
    const key = getWorkflowScheduleKey(job)
    scheduledWorkflowKeys.add(key)

    if (job.last_status === 'running') {
      runningSchedules += 1
      runningWorkflowKeys.add(key)
    }
  })

  return {
    scheduledWorkflows: scheduledWorkflowKeys.size,
    runningWorkflows: runningWorkflowKeys.size,
    totalSchedules: jobs.length,
    runningSchedules,
  }
}

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
  const { selectedModeCategory, setModeCategory, getAgentModeFromCategory } = useModeStore()
  const presetModeCategory = selectedModeCategory === 'workflow' || selectedModeCategory === 'multi-agent'
    ? selectedModeCategory
    : null
  const { setWorkspaceMinimized, workspaceMinimized, agentMode } = useAppStore()
  // Use toolList to get all available servers, not just enabled ones
  const toolList = useMCPStore(state => state.toolList)
  const appVersion = useAppVersion()
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const activePresetForSchedule = (getActivePreset as any)(selectedModeCategory) as ReturnType<typeof getActivePreset>

  // Get presets for current mode
  const presetsForMode = presetModeCategory === null
    ? []
    : getPresetsForMode(presetModeCategory)

  const [showPresetDropdown, setShowPresetDropdown] = useState(false)
  const [showPresetModal, setShowPresetModal] = useState(false)
  const [editingPreset, setEditingPreset] = useState<CustomPreset | null>(null)
  const [showShortcuts, setShowShortcuts] = useState(false)
  const [showRunsPanel, setShowRunsPanel] = useState(false)
  const [workflowScheduleSummary, setWorkflowScheduleSummary] = useState<WorkflowScheduleSummary>(EMPTY_WORKFLOW_SCHEDULE_SUMMARY)
  const [showPlansManager, setShowPlansManager] = useState(false)
  const [showBotConnector, setShowBotConnector] = useState(false)
  const [restoreWorkspaceAfterBotConnector, setRestoreWorkspaceAfterBotConnector] = useState(false)
  const [showWorkflowsPopup, setShowWorkflowsPopup] = useState(false)
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
  const isOrganizationView = showWorkflowsOverview
  const shouldShowScheduleHeader = selectedModeCategory === 'workflow' || isOrganizationView
  const isMultiAgentMode = selectedModeCategory === 'multi-agent'
  const shouldShowBotConnector = selectedModeCategory === 'multi-agent' || selectedModeCategory === 'workflow' || isOrganizationView

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

  const handleModePillClick = useCallback((modeKey: 'multi-agent' | 'workflow') => {
    setModeCategory(modeKey)
    setShowWorkflowsOverview(false)
  }, [setModeCategory, setShowWorkflowsOverview])

  // Fetch global workflow schedule metadata for the header so it can show
  // running/total counts before the schedules popup is opened.
  useEffect(() => {
    if (!shouldShowScheduleHeader) {
      setWorkflowScheduleSummary(EMPTY_WORKFLOW_SCHEDULE_SUMMARY)
      return
    }

    let cancelled = false

    const loadScheduleState = async () => {
      try {
        const resp = await schedulerApi.listJobs({
          entity_type: 'workflow',
          limit: WORKFLOW_SCHEDULE_HEADER_LIMIT,
        })
        if (cancelled) return

        const jobs = resp.jobs ?? []
        setWorkflowScheduleSummary(summarizeWorkflowSchedules(jobs))
      } catch {
        if (cancelled) return
        setWorkflowScheduleSummary(EMPTY_WORKFLOW_SCHEDULE_SUMMARY)
      }
    }

    loadScheduleState()
    const interval = window.setInterval(loadScheduleState, 10000)

    return () => {
      cancelled = true
      window.clearInterval(interval)
    }
  }, [shouldShowScheduleHeader, showRunsPanel]) // refresh after runs panel closes or mode changes

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

  const workflowCountLabel =`${workflowScheduleSummary.scheduledWorkflows} automation${workflowScheduleSummary.scheduledWorkflows !== 1 ? 's' : ''}`
  const scheduleCountLabel = `${workflowScheduleSummary.totalSchedules} schedule${workflowScheduleSummary.totalSchedules !== 1 ? 's' : ''}`
  const workflowScheduleTooltip = workflowScheduleSummary.runningWorkflows > 0
    ? `${workflowScheduleSummary.runningWorkflows} of ${workflowScheduleSummary.scheduledWorkflows} scheduled automations running now; ${workflowScheduleSummary.runningSchedules} of ${workflowScheduleSummary.totalSchedules} schedules running`
    : `${workflowCountLabel} scheduled; ${scheduleCountLabel} total`

  const openBotConnector = useCallback(() => {
    const shouldRestore = !workspaceMinimized
    setRestoreWorkspaceAfterBotConnector(shouldRestore)
    if (shouldRestore) setWorkspaceMinimized(true)
    setShowBotConnector(true)
  }, [workspaceMinimized, setWorkspaceMinimized])

  const closeBotConnector = useCallback(() => {
    setShowBotConnector(false)
    if (restoreWorkspaceAfterBotConnector) setWorkspaceMinimized(false)
    setRestoreWorkspaceAfterBotConnector(false)
  }, [restoreWorkspaceAfterBotConnector, setWorkspaceMinimized])

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
    enableContextSummarization?: boolean,
    enableBrowserAccess?: boolean,
    selectedSecrets?: string[],
    selectedGlobalSecretNames?: string[] | null,
    browserMode?: 'none' | 'headless' | 'cdp' | 'playwright'
  ) => {
    try {
      const effectiveMode = editingPreset ? editingPreset.agentMode : agentMode
      const globalSecretNamesForBackend = selectedGlobalSecretNames === undefined
        ? (editingPreset?.selectedGlobalSecretNames === undefined ? [] : editingPreset.selectedGlobalSecretNames)
        : selectedGlobalSecretNames

      // Workflow mode: save to file-backed manifest (not DB)
      if (effectiveMode === 'workflow' && editingPreset?.selectedFolder?.filepath) {
        const workspacePath = editingPreset.selectedFolder.filepath
        const payload = {
          workspace_path: workspacePath,
          label,
          capabilities: {
            selected_servers: selectedServers || [],
            selected_tools: selectedTools || [],
            selected_skills: selectedSkills || [],
            selected_secrets: selectedSecrets || [],
            selected_global_secret_names: globalSecretNamesForBackend,
            browser_mode: browserMode || 'none',
            use_code_execution_mode: useCodeExecutionMode || false,
            llm_config: llmConfig || undefined,
          },
        }
        await agentApi.updateWorkflowManifest(payload)
        // Refresh manifests and rebuild workflow presets in zustand (triggers re-renders)
        await refreshPresets()
        setShowPresetModal(false)
        setEditingPreset(null)
        return
      }

      // Multi-agent mode: save the user's chat capability profile.
      const savedPreset = await savePreset(
        label,
        query,
        selectedServers,
        selectedTools,
        selectedSkills,
        effectiveMode,
        selectedFolder,
        llmConfig,
        useCodeExecutionMode,
        editingPreset?.id,
        enableContextSummarization,
        enableBrowserAccess,
        undefined, // enableContextEditing
        selectedSecrets,
        selectedGlobalSecretNames,
        browserMode
      )

      // Apply the preset immediately if it's a new one
      if (savedPreset && !editingPreset) {
        handlePresetClick(savedPreset)
      }

      setShowPresetModal(false)
      setEditingPreset(null)
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
    }
  }, [editingPreset, savePreset, handlePresetClick])

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
        <div className="flex items-center justify-between gap-3">
          {/* Left: App logo + Mode Indicator */}
          <div className="flex min-w-0 items-center gap-3">
            {/* App logo (relocated from the former left sidebar header) */}
            <RunloopLockup className="mr-1 shrink-0" version={appVersion} />

            {/* Segmented control — single bordered container, active segment elevated */}
            <div
              data-tour="top-mode-switcher"
              data-testid="tour-top-mode-switcher"
              className="flex shrink-0 items-center bg-gray-100 dark:bg-gray-800/80 border border-gray-200 dark:border-gray-700 rounded-lg p-0.5"
              role="tablist"
              aria-label="Select mode"
            >
              {MODE_PILLS.map((mode) => {
                const isActive = selectedModeCategory === mode.key && !showWorkflowsOverview
                const Icon = mode.icon
                return (
                  <button
                    key={mode.key}
                    role="tab"
                    aria-selected={isActive}
                    aria-label={`Switch to ${mode.label} mode`}
                    onClick={() => handleModePillClick(mode.key)}
                    className={`relative flex items-center gap-1.5 whitespace-nowrap px-3 py-1 rounded-md text-xs font-medium transition-all duration-150 cursor-pointer ${
                      isActive ? mode.activeClasses : mode.inactiveClasses
                    }`}
                    type="button"
                  >
                    <Icon className="w-3 h-3" />
                    <span className="whitespace-nowrap">{mode.label}</span>
                  </button>
                )
              })}
              <button
                role="tab"
                aria-selected={showWorkflowsOverview}
                aria-label="Switch to Organization view"
                onClick={() => setShowWorkflowsOverview(!showWorkflowsOverview)}
                className={`relative flex items-center gap-1.5 whitespace-nowrap px-3 py-1 rounded-md text-xs font-medium transition-all duration-150 cursor-pointer ${
                  showWorkflowsOverview
                    ? 'bg-slate-50 text-slate-700 shadow-sm ring-1 ring-slate-200 dark:bg-slate-700/70 dark:text-slate-100 dark:ring-slate-500/50'
                    : 'text-gray-500 dark:text-gray-400'
                }`}
                type="button"
              >
                <Building2 className="w-3 h-3" />
                <span>Org</span>
              </button>
            </div>

            {/* Center: Preset Information */}
            <div className="flex min-w-0 items-center gap-3">
              {/* Preset Information - Show ONLY for workflow mode */}
              {(() => {
                // For workflow mode only, always show preset selector
                // Chat mode no longer supports presets
                if (selectedModeCategory === 'workflow' && !isOrganizationView) {
                  return (
                    <div className="relative flex items-center">
                      <div
                        data-tour="workflow-add-edit"
                        data-testid="tour-workflow-add-edit"
                        className="flex items-center bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-600 rounded-md overflow-hidden"
                      >
                        <button
                          onClick={handlePresetDropdownToggle}
                          className="flex min-w-0 items-center gap-2 px-3 py-1 hover:bg-gray-100 dark:hover:bg-slate-700 transition-colors"
                        >
                          {activePreset ? (
                            <>
                              <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                              <span className="block max-w-[190px] truncate whitespace-nowrap text-sm font-medium text-gray-700 dark:text-gray-300">
                                {activePreset.label}
                              </span>
                            </>
                          ) : (
                            <>
                              <div className="w-2 h-2 bg-gray-400 rounded-full"></div>
                              <span className="block max-w-[190px] truncate whitespace-nowrap text-sm font-medium text-gray-500 dark:text-gray-400">
                                Select Automation
                              </span>
                            </>
                          )}
                        </button>

                        {/* Settings gear icon - separate clickable element */}
                        {activePreset && (
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
                                      <div className="w-2 h-2 bg-green-500 rounded-full"></div>
                                      <div className="flex-1">
                                        <div className="font-medium">{preset.label}</div>
                                      </div>
                                    </div>
                                  </button>

                                  {/* Edit/Duplicate/Delete buttons */}
                                  {(
                                    <div className="flex gap-1">
                                      {presetModeCategory !== null && isPresetActive(preset.id, presetModeCategory) && (
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

              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={openWorkflowWalkthrough}
                    data-testid="open-walkthrough-button"
                    className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                    aria-label="Open walkthrough"
                  >
                    <HelpCircle className="w-4 h-4" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Walkthrough</TooltipContent>
              </Tooltip>

              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    onClick={() => setShowShortcuts(true)}
                    className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                    aria-label="Keyboard shortcuts"
                    title="Keyboard shortcuts"
                  >
                    <Keyboard className="w-4 h-4" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom">Keyboard shortcuts</TooltipContent>
              </Tooltip>

              {shouldShowBotConnector && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      onClick={openBotConnector}
                      data-tour="bot-connector"
                      data-testid="tour-bot-connector"
                      aria-label="Bot Connector"
                      title="Bot Connector"
                      className="p-1 rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200 transition-colors"
                    >
                      <Bot className="w-4 h-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom">Bot connector</TooltipContent>
                </Tooltip>
              )}

              {shouldShowScheduleHeader && (
                <>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => setShowRunsPanel(true)}
                        data-tour="workflow-schedules"
                        data-testid="tour-workflow-schedules"
                        aria-label="Workflow schedules"
                        className={`relative flex items-center gap-2 rounded-md p-1 transition-colors ${
                          workflowScheduleSummary.runningWorkflows > 0
                            ? 'text-green-700 dark:text-green-300 bg-green-50 dark:bg-green-900/20 hover:bg-green-100 dark:hover:bg-green-900/30'
                            : 'text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-gray-700 dark:hover:text-gray-200'
                        }`}
                      >
                        <Workflow className="w-4 h-4 flex-shrink-0" />
                        {workflowScheduleSummary.runningWorkflows > 0 && (
                          <>
                            <span className="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full bg-green-500 border border-white dark:border-gray-800" />
                            <span className="absolute -top-0.5 -right-0.5 w-2.5 h-2.5 rounded-full bg-green-500 animate-ping opacity-50" />
                          </>
                        )}
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="bottom">
                      {workflowScheduleTooltip}
                    </TooltipContent>
                  </Tooltip>
                </>
              )}

              <span className="mx-0.5 h-5 w-px bg-gray-200 dark:bg-gray-700" />

              {/* Config/account controls relocated from the former left sidebar */}
              <WorkspaceTopBarControls />

            </div>
          </TooltipProvider>
        </div>
      </div>

      {/* Keyboard Shortcuts & Tips Modal */}
      {showShortcuts && (
        <ModalPortal>
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-[9999] p-4" onClick={() => setShowShortcuts(false)}>
            <div className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-full max-w-lg max-h-[calc(100vh-2rem)] overflow-hidden text-gray-900 dark:text-gray-100 flex flex-col" onClick={e => e.stopPropagation()}>
              {/* Header */}
              <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700">
                <h3 className="text-base font-semibold">Keyboard Shortcuts</h3>
                <button
                  onClick={() => setShowShortcuts(false)}
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
                      ['Chief of Staff', 'Ctrl+2'],
                      ['Organization', 'Ctrl+3'],
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
                      ['New Chat (Chief of Staff)', 'Ctrl+N'],
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

      {showBotConnector && (
        <BotConnectorModal
          isOpen={showBotConnector}
          onClose={closeBotConnector}
        />
      )}

      {/* Scheduled Workflow Runs Panel */}
      {showRunsPanel && (
        <WorkflowScheduleRunsPanel onClose={() => setShowRunsPanel(false)} />
      )}

      {/* Workflows Overview Popup */}
      <WorkflowsOverviewPopup
        isOpen={showWorkflowsPopup}
        onClose={() => setShowWorkflowsPopup(false)}
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
