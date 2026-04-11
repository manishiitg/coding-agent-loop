import React, { useCallback, useRef, useImperativeHandle, forwardRef, useEffect, useState } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  useReactFlow,
  BackgroundVariant,
  ReactFlowProvider,
  type NodeChange,
  type OnSelectionChangeParams,
  SelectionMode
} from '@xyflow/react'
import { Workflow, List, ArrowRight, ArrowDown, Save, RotateCcw, RefreshCw, Loader2 as Loader2Icon } from 'lucide-react'
import '@xyflow/react/dist/style.css'

import { nodeTypes } from '../nodes'
import { WorkflowToolbar } from './WorkflowToolbar'
import { StepSidebar } from './StepSidebar'
import { VariablesSidebar } from './VariablesSidebar'
import { StepLegend } from './StepLegend'
import { MultiStepSidebar } from './MultiStepSidebar'
import { BatchProgressHeader } from '../BatchProgressHeader'
import { PlanOutlineView } from './PlanOutlineView'
import { usePlanData, type PlanChanges } from '../hooks/usePlanData'
import { useEvaluationPlanData } from '../hooks/useEvaluationPlanData'
import { useOutputPlanData } from '../hooks/useOutputPlanData'
import { usePlanToFlow, type WorkflowNode, type WorkflowEdge, type StepNodeData, type ConditionalNodeData, type DecisionNodeData, type WorkflowArtifactNodeData } from '../hooks/usePlanToFlow'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useWorkflowExecution } from '../hooks/useWorkflowExecution'
import { useWorkspaceState } from '../hooks/useWorkspaceState'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useAppStore } from '../../../stores/useAppStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { agentApi } from '../../../services/api'
import type { PlanStep, PlanningResponse } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import type { VariablesManifest, EvaluationStep } from '../../../services/api-types'
import { buildGroupFolderPath } from '../../../utils/workflowUtils'

// Duration to show highlights before clearing (in ms)
const HIGHLIGHT_DURATION = 4000

import type { ExecutionOptions } from '../../../services/api-types'

interface WorkflowCanvasProps {
  workspacePath: string | null
  presetQueryId: string | null
  currentPhase?: string
  onStartPhase?: (phaseId: string, executionOptions?: ExecutionOptions) => void
  onCreatePlan?: () => void
  showChatArea?: boolean
  onToggleChatArea?: () => void
  toolbarOnly?: boolean  // When true, only render the toolbar (skip React Flow canvas for performance)
  className?: string
}

// Adapter for StepSidebar which uses stepId or ExecutionOptions
type StepSidebarStartPhase = (phaseId: string, stepIdOrOptions?: string | ExecutionOptions) => void

// Ref interface for external control of the canvas
export interface WorkflowCanvasRef {
  refresh: (changedStepIDs?: string[], deletedStepIDs?: string[]) => Promise<PlanChanges | null>
  getStepCount: () => number
  focusStep: (stepId: string) => void  // Alias for highlightStepNode
}

const WorkflowCanvasInner = forwardRef<WorkflowCanvasRef, WorkflowCanvasProps>(({
  workspacePath,
  presetQueryId,
  currentPhase,
  onStartPhase,
  onCreatePlan,
  showChatArea = false,
  onToggleChatArea,
  toolbarOnly = false,
  className = ''
}, ref) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const highlightTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const { setViewport, getNode, updateNode } = useReactFlow()
  const hasInitializedView = React.useRef(false)

  // --- Performance diagnostics for workflow switching ---
  const renderCountRef = useRef(0)
  const lastPresetRef = useRef(presetQueryId)
  renderCountRef.current++
  if (lastPresetRef.current !== presetQueryId) {
    console.log(`%c[WorkflowCanvas] Preset switched: ${lastPresetRef.current?.slice(0,8)} → ${presetQueryId?.slice(0,8)}`, 'color: orange; font-weight: bold')
    lastPresetRef.current = presetQueryId
  }
  if (renderCountRef.current % 50 === 0) {
    console.log(`%c[WorkflowCanvas] render #${renderCountRef.current} (preset: ${presetQueryId?.slice(0,8)})`, 'color: gray')
  }
  // Store step ID to focus on after nodes update (from backend plan changes)
  const pendingFocusStepIdRef = React.useRef<string | null>(null)
  // Store current viewport state (x, y, zoom) to preserve it during refresh
  const viewportStateRef = React.useRef<{ x: number; y: number; zoom: number } | null>(null)
  const viewportSaveTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null)

  // Get workflow mode, layout direction, and canvas view mode
  const layoutDirection = useWorkflowStore(state => state.layoutDirection)
  const setLayoutDirection = useWorkflowStore(state => state.setLayoutDirection)
  const canvasViewMode = useWorkflowStore(state => state.canvasViewMode)
  const setCanvasViewMode = useWorkflowStore(state => state.setCanvasViewMode)
  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const workflowWorkspaceSelectionTouched = useWorkflowStore(state => state.workflowWorkspaceSelectionTouched)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)

  const isExecutionWorkspace =
    workflowWorkspaceView === 'execution' ||
    (workflowWorkspaceSelectionTouched &&
      workflowWorkspaceView === null &&
      (currentPhase === 'execution' || currentPhase === 'evaluation-execution'))
  const isBuilderWorkspace =
    workflowWorkspaceView === 'builder' ||
    (workflowWorkspaceSelectionTouched &&
      workflowWorkspaceView === null &&
      currentPhase === 'workflow-builder')

  // Generate localStorage key for viewport state (workspace-specific)
  const getViewportStorageKey = React.useCallback(() => {
    return workspacePath
      ? `workflow-viewport-${workspacePath}`
      : 'workflow-viewport-default'
  }, [workspacePath])

  // PERF: Debounced viewport change handler — saves to localStorage at most once per 500ms
  // instead of on every pixel of pan/zoom (which was causing excessive localStorage writes)
  const onViewportChange = React.useCallback((viewport: { x: number; y: number; zoom: number }) => {
    viewportStateRef.current = { x: viewport.x, y: viewport.y, zoom: viewport.zoom }
    if (hasInitializedView.current) {
      if (viewportSaveTimerRef.current) clearTimeout(viewportSaveTimerRef.current)
      viewportSaveTimerRef.current = setTimeout(() => {
        try {
          const storageKey = getViewportStorageKey()
          localStorage.setItem(storageKey, JSON.stringify(viewportStateRef.current))
        } catch { /* ignore */ }
      }, 500)
    }
  }, [getViewportStorageKey])

  // Get workflow layout file path
  const getLayoutFilePath = React.useCallback(() => {
    if (!workspacePath) return null
    return `${workspacePath}/planning/workflow_layout.json`
  }, [workspacePath])

  // Load saved node positions and offsets from workspace
  const loadSavedLayout = React.useCallback(async (): Promise<{
    positions: Map<string, { x: number; y: number }>;
    offsets: Map<string, { parentId: string; dx: number; dy: number }>;
    layoutDirection?: 'LR' | 'TB';
  } | null> => {
    const layoutPath = getLayoutFilePath()
    if (!layoutPath) return null

    try {
      const response = await agentApi.getPlannerFileContent(layoutPath)
      if (response.success && response.data?.content) {
        const layout = JSON.parse(response.data.content)
        const positions = new Map<string, { x: number; y: number }>()
        const offsets = new Map<string, { parentId: string; dx: number; dy: number }>()
        let savedDirection: 'LR' | 'TB' | undefined
        
        if (layout.nodePositions && typeof layout.nodePositions === 'object') {
          Object.entries(layout.nodePositions).forEach(([nodeId, pos]: [string, unknown]) => {
            // CRITICAL: Never load saved positions for header nodes
            // They must always use the enforced horizontal layout from usePlanToFlow
            if (nodeId === 'start' || nodeId === 'execution-settings' || nodeId === 'variables') {
              return // Skip header nodes
            }
            if (pos && typeof pos === 'object' && 'x' in pos && 'y' in pos) {
              positions.set(nodeId, { x: (pos as { x: number }).x, y: (pos as { x: number; y: number }).y })
            }
          })
        }
        
        // Load child offsets if available (version 1.1+)
        if (layout.childOffsets && typeof layout.childOffsets === 'object') {
          Object.entries(layout.childOffsets).forEach(([nodeId, offset]: [string, unknown]) => {
            if (offset && typeof offset === 'object' && 'parentId' in offset && 'dx' in offset && 'dy' in offset) {
              offsets.set(nodeId, {
                parentId: (offset as { parentId: string }).parentId,
                dx: (offset as { dx: number }).dx,
                dy: (offset as { dy: number }).dy
              })
            }
          })
        }

        // Load layout direction if available (version 1.2+)
        if (layout.layoutDirection === 'LR' || layout.layoutDirection === 'TB') {
          savedDirection = layout.layoutDirection
        }
        
        console.log('[WorkflowCanvas] 📂 Loaded saved layout:', positions.size, 'positions,', offsets.size, 'offsets, direction:', savedDirection)
        return { positions, offsets, layoutDirection: savedDirection }
      }
    } catch {
      // File doesn't exist yet - that's okay
      // No saved layout found - this is normal for new workspaces
    }
    return null
  }, [getLayoutFilePath])

  // Save node positions to workspace
  const saveLayout = React.useCallback(async (): Promise<void> => {
    const layoutPath = getLayoutFilePath()
    if (!layoutPath || !workspacePath) {
      console.warn('[WorkflowCanvas] Cannot save layout: no workspace path')
      alert('Cannot save layout: no workspace path')
      return
    }

    console.log('[WorkflowCanvas] Saving layout...', { layoutPath, workspacePath })

    // Only save parent node positions (children are calculated from offsets)
    // Save positions for main parent nodes, sub-agents, and validation/learning/evaluation nodes
    // (all of these are now independently draggable)
    const parentPositions: Record<string, { x: number; y: number }> = {}
    nodesRef.current.forEach(node => {
      // CRITICAL: Never save header node positions - they must always use enforced horizontal layout
      if (node.id === 'start' || node.id === 'execution-settings' || node.id === 'variables') {
        return // Skip header nodes
      }
      
      // Allow sub-agents to be saved as parent positions (they're independently draggable)
      const isSubAgent = node.id.includes('-sub-agent-')
      if (isSubAgent) {
        parentPositions[node.id] = { x: node.position.x, y: node.position.y }
        return
      }
      
      // Allow validation, learning, and evaluation nodes to be saved as parent positions
      // (they're independently draggable)
      if (node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact') {
        parentPositions[node.id] = { x: node.position.x, y: node.position.y }
        return
      }
      
      // Skip if this is a child node (has a parent) - these should not be saved
      if (childToParentRef.current.has(node.id)) {
        return
      }
      
      // Only save positions for main parent nodes (step, conditional, decision, loop, orchestrator, human_input, start, end)
      if (node.type === 'step' || 
          node.type === 'conditional' || 
          node.type === 'decision' || 
          node.type === 'human_input' ||
          node.type === 'start' ||
          node.type === 'end') {
        parentPositions[node.id] = { x: node.position.x, y: node.position.y }
      }
    })

    // Also save offsets for child nodes relative to their parents
    // Note: Sub-agents, validation, learning, and evaluation nodes are now saved as parent positions, not as offsets
    // This ensures we can restore the exact layout the user had
    const childOffsets: Record<string, { parentId: string; dx: number; dy: number }> = {}
    nodesRef.current.forEach(node => {
      // Skip sub-agents, validation, learning, and evaluation nodes (they're saved as parent positions now)
      if (node.id.includes('-sub-agent-') || 
          node.type === 'validation' || 
          node.type === 'learning' || 
          node.type === 'evaluation' ||
          node.type === 'workflow-artifact') {
        return
      }
      
      const parentId = childToParentRef.current.get(node.id)
      if (parentId) {
        const parentNode = nodesRef.current.find(n => n.id === parentId)
        if (parentNode) {
          const offset = childOffsetsRef.current.get(node.id)
          if (offset) {
            childOffsets[node.id] = {
              parentId,
              dx: offset.dx,
              dy: offset.dy
            }
          }
        }
      }
    })

    console.log('[WorkflowCanvas] Parent positions to save:', Object.keys(parentPositions).length, parentPositions)
    console.log('[WorkflowCanvas] Child offsets to save:', Object.keys(childOffsets).length, childOffsets)
    console.log('[WorkflowCanvas] Layout direction to save:', layoutDirection)

    const layoutData = {
      nodePositions: parentPositions,
      childOffsets: childOffsets,
      layoutDirection: layoutDirection,
      version: '1.2',
      savedAt: new Date().toISOString()
    }

    setIsSavingLayout(true)
    try {
      console.log('[WorkflowCanvas] Calling updatePlannerFile...', { layoutPath, dataSize: JSON.stringify(layoutData).length })
      const response = await agentApi.updatePlannerFile(layoutPath, JSON.stringify(layoutData, null, 2), 'Save workflow layout')
      console.log('[WorkflowCanvas] Save response:', response)
      setHasUnsavedLayoutChanges(false)
      console.log('[WorkflowCanvas] ✅ Saved layout to workspace:', Object.keys(parentPositions).length, 'node positions')
    } catch (error) {
      console.error('[WorkflowCanvas] ❌ Failed to save layout:', error)
      alert(`Failed to save layout: ${error instanceof Error ? error.message : String(error)}`)
      throw error
    } finally {
      setIsSavingLayout(false)
    }
  }, [getLayoutFilePath, workspacePath, layoutDirection])

  // Delete layout file and reset to default (auto-layout)
  const deleteLayout = React.useCallback(async (): Promise<void> => {
    const layoutPath = getLayoutFilePath()
    if (!layoutPath || !workspacePath) {
      console.warn('[WorkflowCanvas] Cannot delete layout: no workspace path')
      alert('Cannot delete layout: no workspace path')
      return
    }

    console.log('[WorkflowCanvas] Deleting layout file...', { layoutPath, workspacePath })

    setIsDeletingLayout(true)
    try {
      // Delete the layout file
      await agentApi.deletePlannerFile(layoutPath, 'Reset workflow layout to default')
      console.log('[WorkflowCanvas] ✅ Deleted layout file')
      
      // Clear unsaved changes flag
      setHasUnsavedLayoutChanges(false)
      
      // Clear any saved layout state
      // The layout will automatically use auto-layout on next refresh since the file is gone
      
      // Trigger a refresh to re-apply auto-layout
      // This will happen naturally when the component re-renders or when nodes are updated
      // We can force a refresh by clearing current positions and triggering a node update
      currentPositionsRef.current.clear()
      currentOffsetsRef.current.clear()
      
      // Force a re-layout by updating nodes (this will trigger auto-layout since no saved layout exists)
      // The existing layout restoration logic will handle this automatically
      console.log('[WorkflowCanvas] Layout reset - will use auto-layout on next render')
    } catch (error: any) {
      // 404 = file doesn't exist, which is fine (already reset)
      if (error?.response?.status === 404 || error?.status === 404) {
        console.log('[WorkflowCanvas] Layout file not found (already reset)')
        setHasUnsavedLayoutChanges(false)
        currentPositionsRef.current.clear()
        currentOffsetsRef.current.clear()
      } else {
        console.error('[WorkflowCanvas] ❌ Failed to delete layout:', error)
        throw error
      }
    } finally {
      setIsDeletingLayout(false)
    }
  }, [getLayoutFilePath, workspacePath])

  // Variables state
  const [variablesManifest, setVariablesManifest] = React.useState<VariablesManifest | null>(null)
  const [isLoadingVariables, setIsLoadingVariables] = React.useState(false)
  const [showVariablesSidebar, setShowVariablesSidebar] = React.useState(false)
  
  // Workflow store actions
  const setVariablesManifestInStore = useWorkflowStore.getState().setVariablesManifest
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const stepProgress = useWorkflowStore(state => state.stepProgress)
  
  // Get workspace minimized state to determine if StepSidebar should be compact
  const workspaceMinimized = useAppStore(state => state.workspaceMinimized)
  
  // Calculate if StepSidebar should be in compact mode (when both ChatArea and Workspace are open)
  const isStepSidebarCompact = showChatArea && !workspaceMinimized
  

  // Highlight execution folder in workspace when selectedRunFolder changes
  // This ensures workspace shows the correct group folder during multi-group execution
  const { highlightFile } = useWorkspaceStore()
  const prevSelectedRunFolderRef = useRef<string | null>(null)
  useEffect(() => {
    // Reset ref if selectedRunFolder is cleared
    if (!selectedRunFolder || selectedRunFolder === 'new') {
      prevSelectedRunFolderRef.current = null
      return
    }

    // Only highlight if selectedRunFolder actually changed and is valid
    if (selectedRunFolder !== prevSelectedRunFolderRef.current && workspacePath) {
      prevSelectedRunFolderRef.current = selectedRunFolder

      // Construct execution folder path
      const executionPath = `${workspacePath}/runs/${selectedRunFolder}/execution`

      // PERF: Use getState() to avoid fetchFiles reference changes triggering this effect
      useWorkspaceStore.getState().fetchFiles(workspacePath || undefined).then(() => {
        // Small delay to ensure files are loaded before highlighting
        setTimeout(() => {
          highlightFile(executionPath)
        }, 100)
      }).catch(err => {
        console.error('[WorkflowCanvas] Failed to refresh files before highlighting:', err)
        // Still try to highlight even if refresh fails
        highlightFile(executionPath)
      })
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRunFolder, workspacePath, highlightFile])

  // Load workflow data for the main canvas and append eval/output artifacts to it.
  const planData = usePlanData(workspacePath)
  // Keep last non-null plan so PlanOutlineView doesn't unmount during refresh
  const [stablePlan, setStablePlan] = React.useState<PlanningResponse | null>(null)
  React.useEffect(() => {
    if (planData.plan) setStablePlan(planData.plan)
  }, [planData.plan])
  const evalData = useEvaluationPlanData(workspacePath)
  const outputData = useOutputPlanData(workspacePath)

  const plan = planData.plan
  const evaluationPlan = evalData.evaluationPlan
  const outputPlan = outputData.outputPlan
  const refreshEvaluationPlan = evalData.refresh
  const refreshOutputPlan = outputData.refresh

  const loading = planData.loading || evalData.loading || outputData.loading
  const error = planData.error
  const changes = planData.changes

  const loadPlanRefresh = planData.refresh
  const clearChanges = planData.clearChanges
  const setChanges = planData.setChanges

  // *** NEW CONSOLIDATED API ***
  // Load all workspace state (run folders, variables, phases, progress) in one call
  // This replaces the old individual API calls and eliminates race conditions
  const {
    state: workspaceState,
    loading: isLoadingWorkspaceState,
    error: workspaceStateError,
    isRetrying: isRetryingWorkspaceState,
    retryCountdown: workspaceStateRetryCountdown,
    refresh: refreshWorkspaceState
  } = useWorkspaceState(workspacePath, selectedRunFolder)

  // Sync workspace state to local state for backward compatibility
  // TODO: Eventually migrate all consumers to use workspaceState directly
  React.useEffect(() => {
    if (workspaceState) {
      const manifest = workspaceState.variables_manifest || null
      setVariablesManifest(manifest)
      setIsLoadingVariables(false)

    } else if (!isLoadingWorkspaceState) {
      setVariablesManifest(null)
      setIsLoadingVariables(false)
    } else {
      setIsLoadingVariables(isLoadingWorkspaceState)
    }
  }, [workspaceState, isLoadingWorkspaceState])

  // Transform run folders for WorkflowToolbar (memoized to avoid repeated transformations)
  const runFoldersForToolbar = React.useMemo(() => {
    if (!workspaceState?.run_folders) return []
    return workspaceState.run_folders.map(f => ({ name: f.name, progress: f.progress || undefined }))
  }, [workspaceState?.run_folders])

  useEffect(() => {
    if (!isBuilderWorkspace || !workspaceState?.run_folders?.length) {
      return
    }

    const availableRunFolders = new Set(workspaceState.run_folders.map(folder => folder.name))
    const selectedBuilderIteration = selectedRunFolder && selectedRunFolder !== 'new'
      ? selectedRunFolder.split('/')[0]
      : null

    if (
      selectedRunFolder &&
      selectedRunFolder !== 'new' &&
      availableRunFolders.has(selectedRunFolder) &&
      selectedBuilderIteration === 'iteration-0'
    ) {
      return
    }

    const preferredGroupId = selectedGroupIds[0]
      || variablesManifest?.groups?.find(group => group.enabled !== false)?.name
      || null

    const builderGroupRunFolder = preferredGroupId
      ? buildGroupFolderPath(preferredGroupId, 'iteration-0', variablesManifest)
      : null

    const fallbackBuilderRunFolder =
      (builderGroupRunFolder && availableRunFolders.has(builderGroupRunFolder) && builderGroupRunFolder)
      || (availableRunFolders.has('iteration-0') ? 'iteration-0' : null)
      || workspaceState.run_folders.find(folder => folder.name.startsWith('iteration-0/'))?.name
      || null

    if (fallbackBuilderRunFolder) {
      setSelectedRunFolder(fallbackBuilderRunFolder)
    }
  }, [
    isBuilderWorkspace,
    workspaceState?.run_folders,
    selectedRunFolder,
    selectedGroupIds,
    variablesManifest,
    setSelectedRunFolder
  ])

  // Log workspace state errors
  React.useEffect(() => {
    if (workspaceStateError) {
      console.error('[WorkflowCanvas] Workspace state error:', workspaceStateError)
    }
  }, [workspaceStateError])

  // Callback for opening variables sidebar
  const handleOpenVariablesSidebar = useCallback(() => {
    setShowVariablesSidebar(true)
    setSelectedNode(null) // Close step sidebar if open
  }, [])

  // Callback for when variables are updated
  const handleVariablesUpdate = useCallback((manifest: VariablesManifest) => {
    setVariablesManifest(manifest)
    // Also update in workflow store for buildExecutionOptions to access
    setVariablesManifestInStore(manifest)
  }, [setVariablesManifestInStore])

  // Refresh handler - reloads plan, step config, and workspace state
  const handleRefresh = useCallback(async () => {
    if (!workspacePath) return

    console.log('[WorkflowCanvas] Refreshing plan, step config, and workspace state...')

    // Save current viewport state before refresh
    // Only save if viewport has been initialized (not on first load)
    const currentViewport = hasInitializedView.current ? viewportStateRef.current : null
    console.log('[WorkflowCanvas] Saving viewport state before refresh:', currentViewport, 'hasInitializedView:', hasInitializedView.current)

    await Promise.all([
      loadPlanRefresh(),
      refreshEvaluationPlan(),
      refreshOutputPlan(),
      refreshWorkspaceState()
    ])

    // Restore viewport state after refresh completes
    // Only restore if we had a saved viewport (not on first load)
    // Use a small delay to ensure nodes have been updated
    if (currentViewport && hasInitializedView.current) {
      setTimeout(() => {
        console.log('[WorkflowCanvas] Restoring viewport state after refresh:', currentViewport)
        setViewport(
          { x: currentViewport.x, y: currentViewport.y, zoom: currentViewport.zoom },
          { duration: 300 }
        )
      }, 100)
    }

    console.log('[WorkflowCanvas] Refresh completed')
  }, [workspacePath, loadPlanRefresh, refreshEvaluationPlan, refreshOutputPlan, refreshWorkspaceState, setViewport])

  // Workflow execution
  const {
    status,
    stopWorkflow
  } = useWorkflowExecution()

  // Current step and status from store (set by ChatArea polling when step_progress_updated events arrive)
  const currentStepId = useWorkflowStore(state => state.currentStepId)
  const stepStatusMap = useWorkflowStore(state => state.stepStatusMap)

  const isExecuting = status === 'running'

  // Refs for callbacks that need to be defined early
  const handleRunFromStepRef = React.useRef<((stepIndex: number, stepId: string) => void) | null>(null)
  const handleOpenSidebarRef = React.useRef<((nodeId: string) => void) | null>(null)
  
  // React Flow state (need to define before usePlanToFlow to use in callbacks)
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<WorkflowEdge>([])
  const [selectedNode, setSelectedNode] = React.useState<WorkflowNode | null>(null)

  // Multi-selection state for configuring multiple steps at once
  const [selectedNodes, setSelectedNodes] = useState<WorkflowNode[]>([])

  // Track Shift key for toggling between pan (default) and selection mode
  const [isShiftPressed, setIsShiftPressed] = useState(false)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => { if (e.key === 'Shift') setIsShiftPressed(true) }
    const handleKeyUp = (e: KeyboardEvent) => { if (e.key === 'Shift') setIsShiftPressed(false) }
    window.addEventListener('keydown', handleKeyDown)
    window.addEventListener('keyup', handleKeyUp)
    return () => {
      window.removeEventListener('keydown', handleKeyDown)
      window.removeEventListener('keyup', handleKeyUp)
    }
  }, [])

  // Store latest nodes in ref to avoid dependency issues
  const nodesRef = React.useRef(nodes)
  React.useEffect(() => {
    nodesRef.current = nodes
  }, [nodes])

  // Track unsaved layout changes
  const [hasUnsavedLayoutChanges, setHasUnsavedLayoutChanges] = React.useState(false)
  const [isSavingLayout, setIsSavingLayout] = React.useState(false)
  const [isDeletingLayout, setIsDeletingLayout] = React.useState(false)
  const saveLayoutTimeoutRef = React.useRef<NodeJS.Timeout | null>(null)
  
  // Map of parent node ID to child node IDs (for grouped movement)
  const nodeGroupsRef = React.useRef<Map<string, string[]>>(new Map())
  
  // Map of child node ID to parent node ID (for quick lookup)
  const childToParentRef = React.useRef<Map<string, string>>(new Map())
  
  // Map of child node ID to relative offset from parent { dx, dy }
  const childOffsetsRef = React.useRef<Map<string, { dx: number; dy: number }>>(new Map())

  // Store current node positions before refresh (to preserve layout when saving from sidebar)
  const currentPositionsRef = React.useRef<Map<string, { x: number; y: number }>>(new Map())
  const currentOffsetsRef = React.useRef<Map<string, { parentId: string; dx: number; dy: number }>>(new Map())

  // Build node groups: map parent nodes to their child nodes (validation, learning, evaluation, sub-agents)
  const buildNodeGroups = useCallback((currentNodes: WorkflowNode[]) => {
    const groups = new Map<string, string[]>()
    const childToParent = new Map<string, string>()
    const offsets = new Map<string, { dx: number; dy: number }>()

    // Helper to check if a node is a parent node type
    const isParentNode = (node: WorkflowNode): boolean => {
      return node.type === 'step' || 
             node.type === 'conditional' || 
             node.type === 'decision' || 
                node.type === 'human_input'
    }

    // Also treat sub-agents as parent nodes (they have learning/validation children)
    const isSubAgentNode = (node: WorkflowNode): boolean => {
      return node.id.includes('-sub-agent-')
    }

    // First pass: Build groups for regular parent nodes (step, conditional, decision, loop, orchestrator, human_input)
    currentNodes.forEach(parentNode => {
      if (!isParentNode(parentNode)) return

      const children: string[] = []
      
      // Find validation, learning, and evaluation nodes by parentStepId
      currentNodes.forEach(childNode => {
        if (childNode.type === 'validation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            // Calculate relative offset
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        } else if (childNode.type === 'learning') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        } else if (childNode.type === 'evaluation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === parentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, parentNode.id)
            const dx = childNode.position.x - parentNode.position.x
            const dy = childNode.position.y - parentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        }
      })

      if (children.length > 0) {
        groups.set(parentNode.id, children)
      }
    })

    // Second pass: Build groups for sub-agents (they have learning/validation children)
    currentNodes.forEach(subAgentNode => {
      if (!isSubAgentNode(subAgentNode)) return

      const children: string[] = []
      
      // Find validation, learning, and evaluation nodes that belong to this sub-agent
      currentNodes.forEach(childNode => {
        if (childNode.type === 'validation' || childNode.type === 'learning' || childNode.type === 'evaluation') {
          const data = childNode.data as { parentStepId?: string }
          if (data.parentStepId === subAgentNode.id) {
            children.push(childNode.id)
            childToParent.set(childNode.id, subAgentNode.id)
            const dx = childNode.position.x - subAgentNode.position.x
            const dy = childNode.position.y - subAgentNode.position.y
            offsets.set(childNode.id, { dx, dy })
          }
        }
      })

      if (children.length > 0) {
        groups.set(subAgentNode.id, children)
      }
    })

    nodeGroupsRef.current = groups
    childToParentRef.current = childToParent
    childOffsetsRef.current = offsets
  }, [])

  // Custom onNodesChange handler that groups nodes together
  const onNodesChange = useCallback((changes: NodeChange[]) => {
    // Allow all nodes to be draggable: sub-agents, validation, learning, evaluation, and parent nodes
    // These nodes can be manually positioned independently
    const filteredChanges = changes.filter(change => {
      if (change.type === 'position') {
        const nodeId = change.id
        // Allow sub-agents to be draggable (they're children but should be independently movable)
        if (nodeId.includes('-sub-agent-')) {
          return true // Allow sub-agents to be draggable
        }
        // Allow validation, learning, and evaluation nodes to be draggable
        const node = nodesRef.current.find(n => n.id === nodeId)
        if (node && (node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact')) {
          return true // Allow validation, learning, and evaluation nodes to be draggable
        }
        // Check if this is a child node (has a parent) - these should not be draggable
        // But we've already handled sub-agents and validation/learning/evaluation above
        if (childToParentRef.current.has(nodeId)) {
          return false // Ignore position changes for other child nodes
        }
      }
      return true
    })

    // Apply filtered changes
    onNodesChangeBase(filteredChanges)

    // Check if any parent node position changed (including sub-agents, validation, learning, evaluation)
    const parentPositionChanges = new Map<string, { x: number; y: number }>()
    
    filteredChanges.forEach(change => {
      if (change.type === 'position' && change.position) {
        const nodeId = change.id
        const node = nodesRef.current.find(n => n.id === nodeId)
        // Include sub-agents, validation, learning, and evaluation nodes as independently movable
        const isSubAgent = nodeId.includes('-sub-agent-')
        const isValidationLearningEval = node && (node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact')
        // Check if this is a parent node (not a child) OR a sub-agent OR validation/learning/evaluation
        if (isSubAgent || isValidationLearningEval || (nodeGroupsRef.current.has(nodeId) && !childToParentRef.current.has(nodeId))) {
          parentPositionChanges.set(nodeId, { x: change.position.x, y: change.position.y })
        }
      }
    })

    // If any parent nodes moved, update their children (with cascading updates)
    if (parentPositionChanges.size > 0) {
      setNodes((nds) => {
        // First pass: update direct children
        // Note: Sub-agents SHOULD move with their parent orchestrator
        // Validation, learning, and evaluation nodes remain independent
        let updatedNodes = nds.map(node => {
          const parentId = childToParentRef.current.get(node.id)
          
          // Skip if this is a validation, learning, or evaluation node
          // These are independent and can be manually positioned
          const isValidationLearningEval = node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact'
          if (isValidationLearningEval) {
            return node // These nodes are independent, don't update them here
          }
          
          if (parentId && parentPositionChanges.has(parentId)) {
            const newParentPos = parentPositionChanges.get(parentId)!
            const offset = childOffsetsRef.current.get(node.id)
            if (offset) {
              return {
                ...node,
                position: {
                  x: newParentPos.x + offset.dx,
                  y: newParentPos.y + offset.dy
                }
              }
            }
          }
          return node
        })

        // Second pass: update children of nodes that moved in first pass (cascading)
        // This handles orchestrator -> sub-agents -> learning nodes
        const nodesThatMoved = new Set<string>()
        updatedNodes.forEach(node => {
          const parentId = childToParentRef.current.get(node.id)
          if (parentId && parentPositionChanges.has(parentId)) {
            nodesThatMoved.add(node.id)
          }
        })

        // Update children of nodes that moved
        // Skip validation, learning, and evaluation nodes (they're independent)
        updatedNodes = updatedNodes.map(node => {
          // Skip validation, learning, and evaluation nodes - they're independent
          const isValidationLearningEval = node.type === 'validation' || node.type === 'learning' || node.type === 'evaluation' || node.type === 'workflow-artifact'
          if (isValidationLearningEval) {
            return node
          }
          
          const parentId = childToParentRef.current.get(node.id)
          if (parentId && nodesThatMoved.has(parentId)) {
            // Find the updated parent node
            const updatedParent = updatedNodes.find(n => n.id === parentId)
            if (updatedParent) {
              const offset = childOffsetsRef.current.get(node.id)
              if (offset) {
                return {
                  ...node,
                  position: {
                    x: updatedParent.position.x + offset.dx,
                    y: updatedParent.position.y + offset.dy
                  }
                }
              }
            }
          }
          return node
        })

        return updatedNodes
      })
    }

    // Mark as unsaved if ANY node moved (including standalone nodes)
    // We already filtered out invalid moves in filteredChanges
    const hasPositionChanges = filteredChanges.some(c => c.type === 'position')
    if (hasPositionChanges) {
      setHasUnsavedLayoutChanges(true)
      
      // Debounce save (will be saved manually via button, but track changes)
      if (saveLayoutTimeoutRef.current) {
        clearTimeout(saveLayoutTimeoutRef.current)
      }
    }
  }, [onNodesChangeBase, setNodes])

  // Single reusable function to focus/position a node at the top-left of the screen
  const focusNode = useCallback((
    nodeId: string,
    options?: {
      topPadding?: number  // Vertical padding from top (default: 50)
      selectNode?: boolean  // Whether to select the node (default: false)
      delay?: number  // Delay before positioning (default: 100ms)
    }
  ) => {
    const {
      topPadding = 50,
      selectNode = false,
      delay = 100
    } = options || {}

    setTimeout(() => {
      const flowNode = getNode(nodeId)
      if (flowNode) {
        const padding = 150 // Padding from left edge
        setViewport(
          {
            x: padding - flowNode.position.x, // Position on left with padding
            y: topPadding - flowNode.position.y, // Position at top with padding
            zoom: 1.0
          },
          { duration: 500 }
        )

        // Optionally select the node (opens sidebar)
        // Use nodesRef.current to get the latest node data (updated when nodes change)
        if (selectNode) {
          // Use a function to get the latest node from current state
          setSelectedNode(prev => {
            const latestNode = nodesRef.current.find(n => n.id === nodeId) as WorkflowNode | undefined
            // Only update if we found a node and it's different from current
            if (latestNode && (!prev || prev.id !== latestNode.id)) {
              return latestNode
            }
            // If node not found but we had a previous selection, keep it (might be a timing issue)
            return prev || latestNode || null
          })
        }
      }
    }, delay)
  }, [getNode, setViewport])

  // Handle opening sidebar for a node
  const handleOpenSidebar = useCallback(async (nodeId: string) => {
    setShowVariablesSidebar(false) // Close variables sidebar if open
    
    // First, try to find and select the node immediately (before refresh)
    const currentNode = nodesRef.current.find(n => n.id === nodeId)
    if (currentNode) {
      setSelectedNode(currentNode)
    }
    
    // For sub-agent nodes, extract the step ID to find the node after refresh
    let stepIdToFind: string | undefined
    if (currentNode && currentNode.type === 'step') {
      const stepData = currentNode.data as StepNodeData
      stepIdToFind = stepData?.step?.id
    }
    
    // Refresh plan.json from API to ensure we have the latest data before opening sidebar
    try {
      await loadPlanRefresh()
    } catch (error) {
      console.error('[WorkflowCanvas] Failed to refresh plan before opening sidebar:', error)
    }
    
    // After refresh, ensure the node is still selected
    setTimeout(() => {
      // Try to find by node ID first
      let updatedNode = nodesRef.current.find(n => n.id === nodeId)
      
      // If not found and we have a step ID, try finding by step ID (for sub-agents)
      if (!updatedNode && stepIdToFind) {
        updatedNode = nodesRef.current.find(n => {
          if (n.type === 'step') {
            const stepData = n.data as StepNodeData
            return stepData?.step?.id === stepIdToFind
          }
          return false
        }) as WorkflowNode | undefined
      }
      
      if (updatedNode) {
        setSelectedNode(updatedNode)
        focusNode(updatedNode.id, { topPadding: 150, selectNode: false, delay: 0 })
      } else {
        // Fallback: try to focus on original nodeId anyway
        focusNode(nodeId, { topPadding: 150, selectNode: false, delay: 0 })
      }
    }, 200)
  }, [focusNode, loadPlanRefresh])

  // Handle navigating to a step from legend (without opening sidebar)
  const handleNavigateToStep = useCallback((nodeId: string) => {
    focusNode(nodeId, { topPadding: 150, selectNode: false, delay: 100 })
    console.log('[WorkflowCanvas] Navigated to step from legend:', nodeId)
  }, [focusNode])

  // Store handleOpenSidebar in ref for early access
  React.useEffect(() => {
    handleOpenSidebarRef.current = handleOpenSidebar
  }, [handleOpenSidebar])

  // Memoize callbacks to prevent usePlanToFlow from recalculating on every render
  const handleRunFromStepCallback = useCallback((stepIndex: number, stepId: string) => {
    if (handleRunFromStepRef.current) {
      handleRunFromStepRef.current(stepIndex, stepId)
    }
  }, [])

  const handleOpenSidebarCallback = useCallback((nodeId: string) => {
    if (handleOpenSidebarRef.current) {
      handleOpenSidebarRef.current(nodeId)
    }
  }, [])

  // Stabilize stepStatusMap by serializing it - Maps are compared by reference, so we need to serialize
  // to detect actual content changes. This prevents unnecessary recalculations in usePlanToFlow.
  const stableStepStatusMap = React.useMemo(() => {
    if (!stepStatusMap || stepStatusMap.size === 0) {
      return null // Return null instead of the Map to ensure stable reference
    }
    // Serialize Map to object for stable comparison
    const serialized = Object.fromEntries(stepStatusMap)
    return serialized
  }, [stepStatusMap])

  // Convert plan to React Flow nodes and edges (with change highlights and run callback)
  const planFlow = usePlanToFlow(plan, {
    changes,  // Pass changes to highlight modified nodes
    onRunFromStep: handleRunFromStepCallback,
    onOpenSidebar: handleOpenSidebarCallback,
    isExecuting,
    stepStatusMap: stableStepStatusMap,  // Pass stabilized step status map
    workspacePath,  // Pass workspace path for file opening
    selectedRunFolder: selectedRunFolder ?? undefined,  // Pass selected run folder for file opening (convert null to undefined)
    variablesManifest,  // Pass variables manifest for Variables node
    onOpenVariablesSidebar: handleOpenVariablesSidebar,  // Callback for opening variables sidebar
    isLoadingVariables,  // Whether variables are loading
    layoutDirection,  // Layout direction: 'LR' for horizontal, 'TB' for vertical
    disabled: toolbarOnly || canvasViewMode === 'plan'
  })

  const augmentedFlow = React.useMemo(() => {
    if (!planFlow.nodes.length) {
      return planFlow
    }

    const nodes = [...planFlow.nodes]
    const edges = [...planFlow.edges]
    const endNode = nodes.find(node => node.id === 'end')

    if (!endNode) {
      return planFlow
    }

    const addonConfigs: WorkflowArtifactNodeData[] = [
      {
        id: 'workflow-evaluation-artifact',
        title: 'Evaluation',
        description: 'Review the workflow run with evaluation steps.',
        kind: 'evaluation',
        configured: !!(evaluationPlan?.steps && evaluationPlan.steps.length > 0),
        detail: evaluationPlan?.steps?.length
          ? `${evaluationPlan.steps.length} step${evaluationPlan.steps.length === 1 ? '' : 's'} configured`
          : 'Configure in workflow builder chat'
      },
      {
        id: 'workflow-output-artifact',
        title: 'Final Report',
        description: 'Generate the markdown summary report for each group run.',
        kind: 'output',
        configured: !!outputPlan?.step,
        detail: outputPlan?.step?.title || 'Configure in workflow builder chat'
      }
    ]

    const artifactBaseX = layoutDirection === 'LR' ? endNode.position.x + 220 : endNode.position.x
    const artifactBaseY = layoutDirection === 'TB' ? endNode.position.y + 170 : endNode.position.y

    addonConfigs.forEach((config, index) => {
      const position = layoutDirection === 'LR'
        ? { x: artifactBaseX, y: artifactBaseY + (index * 150) - 75 }
        : { x: artifactBaseX + (index * 260) - 130, y: artifactBaseY }

      nodes.push({
        id: config.id,
        type: 'workflow-artifact',
        position,
        data: config,
        draggable: true
      })

      edges.push({
        id: `end-to-${config.id}`,
        source: 'end',
        target: config.id,
        type: 'smoothstep',
        style: {
          stroke: config.kind === 'evaluation' ? '#0ea5e9' : '#f59e0b',
          strokeWidth: 2,
          strokeDasharray: '6 4'
        }
      })
    })

    return { nodes, edges }
  }, [planFlow, evaluationPlan, outputPlan, layoutDirection])

  const { nodes: initialNodes, edges: initialEdges } = augmentedFlow

  // Helper function to highlight and position a specific step node
  const highlightStepNode = useCallback((stepId: string) => {
    // Find the node by matching step.id in node data (works for both top-level and branch steps)
    // Branch steps have node IDs like "step-3-true-0" but step.id is the actual step ID
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'decision') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData
        const nodeStepId = nodeData?.step?.id
        // Match by step.id (for branch steps) or by node ID (for top-level steps)
        return nodeStepId === stepId || (nodeStepId === undefined && node.id === stepId)
      }
      return false
    })

    if (nodeToFocus) {
      console.log('[WorkflowCanvas] highlightStepNode found node:', nodeToFocus.id)
      // Focus viewport on the node but don't select it (don't open sidebar)
      // User can manually open sidebar if needed
      focusNode(nodeToFocus.id, { topPadding: 150, selectNode: false, delay: 100 })
    } else {
      console.log('[WorkflowCanvas] highlightStepNode - no node found for stepId:', stepId)
    }
  }, [focusNode])

  // Auto-focus disabled - running step name is now shown in StepLegend instead
  // This prevents the canvas from jumping around during workflow execution

  // Handle "run from step" button click on nodes - runs only the single step
  // Uses workflow store directly for execution options (single source of truth)
  const handleRunFromStep = useCallback((stepIndex: number, stepId: string) => {
    // Find the node that matches this stepId
    // The node ID might be stepId, or it might be step-${stepIndex} if stepId doesn't exist
    // We need to find the node by matching step.id in the node data
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'decision') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData
        const nodeStepId = nodeData?.step?.id || node.id
        // Match by stepId or by stepIndex if stepId matches
        return nodeStepId === stepId || (nodeData?.stepIndex === stepIndex && node.id === stepId)
      }
      return false
    })

    if (nodeToFocus) {
      // Position viewport to show step at top-left (but don't open sidebar)
      focusNode(nodeToFocus.id, { topPadding: 150, selectNode: false, delay: 100 })
    }

    if (onStartPhase) {
      // Create execution options to run only this single step
      // Use buildExecutionOptions so execution starts with the current workflow store settings.
      const buildExecutionOptions = useWorkflowStore.getState().buildExecutionOptions
      const baseOptions = buildExecutionOptions()
      const executionOptions: ExecutionOptions = {
        ...baseOptions,  // Include all flags from buildExecutionOptions
        execution_strategy: 'run_single_step',
        resume_from_step: stepIndex + 1  // 1-based step number (target step)
      }
      onStartPhase('execution', executionOptions)
    }
  }, [onStartPhase, focusNode])

  // Store handleRunFromStep in ref for early access
  React.useEffect(() => {
    handleRunFromStepRef.current = handleRunFromStep
  }, [handleRunFromStep])

  // Expose methods via ref
  useImperativeHandle(ref, () => ({
    refresh: async (changedStepIDs?: string[], deletedStepIDs?: string[]) => {
      // Refresh plan to get latest data
      await loadPlanRefresh()

      // If granular change data is provided, use it directly
      if (changedStepIDs || deletedStepIDs) {
        // The backend combines added and updated into changed_step_ids
        // For now, we'll treat all changedStepIDs as "updated" since the backend combines them
        // The visual highlighting will work correctly (blue ring for updated steps)
        const updated = changedStepIDs?.filter(id => !deletedStepIDs?.includes(id)) || []
        const deleted = deletedStepIDs || []
        const changes: PlanChanges = {
          added: [], // Backend combines added into changed_step_ids, so we can't distinguish here
          updated,
          deleted,
          hasChanges: updated.length > 0 || deleted.length > 0
        }
        // Set changes directly from granular event data
        if (changes.hasChanges) {
          setChanges(changes)
        }
        return changes
      }

      // No granular data - just refresh without setting changes
      return null
    },
    getStepCount: () => {
      // Count steps from plan data
      if (!plan?.steps) return 0
      return plan.steps.length
    },
    focusStep: (stepId: string) => {
      // Use the existing highlightStepNode function
      highlightStepNode(stepId)
    }
  }), [loadPlanRefresh, plan, setChanges, highlightStepNode])

  // Store step ID to focus on when changes are detected (will focus after nodes update)
  React.useEffect(() => {
    if (changes?.hasChanges) {
      // Store the step ID to focus on (will be used after nodes are updated)
      const stepToFocus = changes.added?.[0] || changes.updated?.[0]
      if (stepToFocus) {
        pendingFocusStepIdRef.current = stepToFocus
      }
      
      // Clear any existing timeout
      if (highlightTimeoutRef.current) {
        clearTimeout(highlightTimeoutRef.current)
      }
      
      // Set new timeout to clear highlights
      highlightTimeoutRef.current = setTimeout(() => {
        console.log('[WorkflowCanvas] Clearing change highlights after', HIGHLIGHT_DURATION, 'ms')
        clearChanges()
      }, HIGHLIGHT_DURATION)
    }

    // Cleanup on unmount
    return () => {
      if (highlightTimeoutRef.current) {
        clearTimeout(highlightTimeoutRef.current)
      }
    }
  }, [changes, clearChanges])

  // Track previous nodes/edges to detect actual changes
  const prevNodesRef = React.useRef<typeof initialNodes>([])
  const prevEdgesRef = React.useRef<typeof initialEdges>([])
  // Track previous layout direction to skip position restoration when direction changes
  const prevLayoutDirectionRef = React.useRef(layoutDirection)
  
  // CRITICAL: Force header nodes to correct positions after nodes update
  // Ensure header nodes maintain correct positions (safety net in case something tries to override them)
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    if (nodes.length === 0 || initialNodes.length === 0) return
    
    const execNode = initialNodes.find(n => n.id === 'execution-settings')
    const varsNode = initialNodes.find(n => n.id === 'variables')
    const startNode = initialNodes.find(n => n.id === 'start')
    
    if (!execNode && !varsNode && !startNode) return
    
    // Check if any header node position has been overridden
    const currentExec = nodes.find(n => n.id === 'execution-settings')
    const currentVars = nodes.find(n => n.id === 'variables')
    const currentStart = nodes.find(n => n.id === 'start')
    
    let needsFix = false
    
    if (execNode && currentExec && 
        (currentExec.position.x !== execNode.position.x || currentExec.position.y !== execNode.position.y)) {
      needsFix = true
    }
    if (varsNode && currentVars && 
        (currentVars.position.x !== varsNode.position.x || currentVars.position.y !== varsNode.position.y)) {
      needsFix = true
    }
    if (startNode && currentStart && 
        (currentStart.position.x !== startNode.position.x || currentStart.position.y !== startNode.position.y)) {
      needsFix = true
    }
    
    if (needsFix) {
      // Use updateNode API to restore correct positions
      if (execNode) updateNode('execution-settings', { position: execNode.position })
      if (varsNode) updateNode('variables', { position: varsNode.position })
      if (startNode) updateNode('start', { position: startNode.position })
    }
  }, [nodes, initialNodes, updateNode])

  // Rebuild node groups when nodes change
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    if (nodes.length > 0) {
      buildNodeGroups(nodes)
    }
  }, [nodes, buildNodeGroups, toolbarOnly])

  // Update nodes when plan changes (only if nodes actually changed)
  React.useEffect(() => {
    // Skip node/edge updates when canvas is hidden (toolbarOnly or plan mode) — no React Flow to update
    if (toolbarOnly || canvasViewMode === 'plan') return

    // Compare by reference first (fast path)
    if (prevNodesRef.current === initialNodes && prevEdgesRef.current === initialEdges) {
      return // No change
    }
    
    // Compare by length, IDs, node data (status), and step configs to detect actual changes
    const nodesChanged =
      prevNodesRef.current.length !== initialNodes.length ||
      prevNodesRef.current.some((node, i) => {
        const newNode = initialNodes[i]
        if (!newNode) return true
        // Check if ID changed
        if (node?.id !== newNode.id) return true
        // Check if position changed (important for layout direction changes)
        if (node?.position?.x !== newNode.position?.x || node?.position?.y !== newNode.position?.y) return true
        // Check if status changed (important for completed steps highlighting)
        if (node?.data?.status !== newNode.data?.status) return true
        
        // Check if VariablesNode manifest changed
        if (node?.type === 'variables' || newNode.type === 'variables') {
          const oldData = node?.data as VariablesNodeData | undefined
          const newData = newNode.data as VariablesNodeData | undefined
          const oldManifest = oldData?.manifest
          const newManifest = newData?.manifest
          const oldManifestStr = JSON.stringify(oldManifest)
          const newManifestStr = JSON.stringify(newManifest)
          if (oldManifestStr !== newManifestStr) {
            console.log(`[WorkflowPlanUpdate] Variables node manifest changed`)
            return true
          }
        }
        
        // Check if step data changed (especially agent_configs)
        // This is important when saving config in the side panel
        const oldData = node?.data as StepNodeData | ConditionalNodeData | DecisionNodeData | undefined
        const newData = newNode?.data as StepNodeData | ConditionalNodeData | DecisionNodeData | undefined
        const oldStep = oldData?.step
        const newStep = newData?.step
        if (oldStep && newStep) {
          // Compare agent_configs by JSON stringify (handles nested objects)
          const oldConfigs = JSON.stringify(oldStep.agent_configs || {})
          const newConfigs = JSON.stringify(newStep.agent_configs || {})
          if (oldConfigs !== newConfigs) {
            console.log(`[WorkflowPlanUpdate] Node ${node.id} agent_configs changed`)
            return true
          }
          // Also check if other step fields changed
          const oldStepStr = JSON.stringify(oldStep)
          const newStepStr = JSON.stringify(newStep)
          if (oldStepStr !== newStepStr) {
            console.log(`[WorkflowPlanUpdate] Node ${node.id} step data changed`)
            return true
          }
        } else if (oldStep !== newStep) {
          // One has step data and the other doesn't
          console.log(`[WorkflowPlanUpdate] Node ${node.id} step data presence changed`)
          return true
        }
        return false
      })
    
    const edgesChanged = 
      prevEdgesRef.current.length !== initialEdges.length ||
      prevEdgesRef.current.some((edge, i) => edge?.id !== initialEdges[i]?.id)
    
    if (nodesChanged) {
      // Nodes changed - will apply positions from usePlanToFlow
      console.log(`%c[WorkflowCanvas] setNodes: ${initialNodes.length} nodes (preset: ${presetQueryId?.slice(0,8)})`, 'color: #4CAF50')
      console.time(`[WorkflowCanvas] setNodes-${presetQueryId?.slice(0,8)}`)

      // Check if we have a selected node - if so, preserve focus on it instead of resetting to start
      const currentSelectedId = selectedNodeIdRef.current
      const hasSelectedNode = currentSelectedId !== null &&
        initialNodes.some(n => n.id === currentSelectedId)

      setNodes(initialNodes)

      // Check if layout direction changed - if so, skip position restoration
      // to allow the new auto-layout to take effect
      const layoutDirectionChanged = prevLayoutDirectionRef.current !== layoutDirection
      if (layoutDirectionChanged) {
        prevLayoutDirectionRef.current = layoutDirection
        // Clear saved positions so the new layout is used
        currentPositionsRef.current.clear()
        currentOffsetsRef.current.clear()
        // Mark as having unsaved changes since positions changed
        setHasUnsavedLayoutChanges(true)
      }

      // Always try to restore positions after nodes regenerate (unless layout direction changed)
      // Priority: 1) Saved layout from file, 2) Current positions (captured before refresh), 3) Auto-layout
      if (initialNodes.length > 0 && !layoutDirectionChanged) {
        // Extract header node positions from initialNodes BEFORE any restoration
        // These positions are calculated by usePlanToFlow and MUST be preserved
        const headerNodePositions = new Map<string, { x: number; y: number }>()
        initialNodes.forEach(node => {
          if (node.id === 'start' || node.id === 'execution-settings' || node.id === 'variables') {
            headerNodePositions.set(node.id, { x: node.position.x, y: node.position.y })
          }
        })
        // Checking for saved layout...
        
        // First try to load saved layout from file
        loadSavedLayout().then(savedLayout => {
          // If saved layout has a different direction, update store and bail out to let re-render handle it
          if (savedLayout?.layoutDirection && savedLayout.layoutDirection !== layoutDirection) {
            console.log('[WorkflowCanvas] 🔄 Restoring saved layout direction:', savedLayout.layoutDirection)
            setLayoutDirection(savedLayout.layoutDirection)
            return
          }

          // Use saved layout if available, otherwise use current positions (captured before refresh)
          const positionsToUse = savedLayout?.positions && savedLayout.positions.size > 0
            ? savedLayout.positions
            : currentPositionsRef.current
          const offsetsToUse = savedLayout?.offsets && savedLayout.offsets.size > 0
            ? savedLayout.offsets
            : currentOffsetsRef.current
          
          if (positionsToUse.size > 0) {
            setNodes((nds) => {
              // First, apply saved/current positions to parent nodes
              let updated = nds.map(node => {
                const savedPos = positionsToUse.get(node.id)
                
                // Header nodes are skipped from restoration (will be forced to correct positions later)

                // Apply saved position unless it's a header node (start, execution-settings, variables)
                // Header nodes MUST always use the enforced horizontal layout from usePlanToFlow
                if (savedPos && node.id !== 'start' && node.id !== 'execution-settings' && node.id !== 'variables') {
                  return { ...node, position: savedPos }
                }
                return node
              })
              
            // Build groups from original auto-layout to get parent-child relationships
              buildNodeGroups(nds)
              
              // If we have saved/current offsets, use them (version 1.1+)
              // Otherwise, fall back to calculating from original auto-layout
              // Note: Sub-agents are now saved as parent positions, not offsets
              if (offsetsToUse.size > 0) {
                // Apply offsets in multiple passes to handle cascading parent-child relationships
                // Pass 1: Apply offsets for nodes whose parent is a top-level parent (orchestrator, step, etc.)
                // Pass 2: Apply learning/validation offsets (relative to sub-agents or other parents)
                
                // First pass: Apply offsets for nodes whose parent is a top-level parent (orchestrator, step, etc.)
                // Skip sub-agents, validation, learning, and evaluation nodes (they're loaded from parentPositions, not offsets)
                updated = updated.map(node => {
                  // Skip sub-agents, validation, learning, and evaluation nodes - they're loaded from parentPositions, not offsets
                  if (node.id.includes('-sub-agent-') || 
                      node.type === 'validation' || 
                      node.type === 'learning' || 
                      node.type === 'evaluation' ||
                      node.type === 'workflow-artifact') {
                    return node
                  }
                  
                  const savedOffset = offsetsToUse.get(node.id)
                  if (savedOffset) {
                    const parentNode = updated.find(n => n.id === savedOffset.parentId)
                    // Only apply if parent is a top-level parent (not a sub-agent)
                    if (parentNode && !parentNode.id.includes('-sub-agent-')) {
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + savedOffset.dx,
                          y: parentNode.position.y + savedOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
                
                // Second pass: Apply offsets for nodes whose parent is a sub-agent (learning/validation nodes)
                updated = updated.map(node => {
                  const savedOffset = offsetsToUse.get(node.id)
                  if (savedOffset) {
                    const parentNode = updated.find(n => n.id === savedOffset.parentId)
                    // Only apply if parent is a sub-agent
                    if (parentNode && parentNode.id.includes('-sub-agent-')) {
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + savedOffset.dx,
                          y: parentNode.position.y + savedOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
              } else {
                // Fallback: calculate offsets from original auto-layout (for old saved layouts)
                updated = updated.map(node => {
                  const parentId = childToParentRef.current.get(node.id)
                  if (parentId) {
                    const parentNode = updated.find(n => n.id === parentId)
                    const originalParentNode = nds.find(n => n.id === parentId)
                    const originalNode = nds.find(n => n.id === node.id)
                    
                    if (parentNode && originalParentNode && originalNode) {
                      const originalOffset = {
                        dx: originalNode.position.x - originalParentNode.position.x,
                        dy: originalNode.position.y - originalParentNode.position.y
                      }
                      
                      return {
                        ...node,
                        position: {
                          x: parentNode.position.x + originalOffset.dx,
                          y: parentNode.position.y + originalOffset.dy
                        }
                      }
                    }
                  }
                  return node
                })
              }
              
              // Rebuild groups with final positions to ensure offsets are correct for future moves
              buildNodeGroups(updated)
              
              // CRITICAL: Force header nodes to correct positions
              updated = updated.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
              
              // Clear current positions after use (they've been applied)
              if (positionsToUse === currentPositionsRef.current) {
                currentPositionsRef.current.clear()
                currentOffsetsRef.current.clear()
              }
              
              return updated
            })
          } else {
            // No saved layout - force header nodes immediately
            setNodes((nds) => {
              return nds.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
            })
          }
        }).catch(err => {
          console.error('[WorkflowCanvas] Failed to load saved layout:', err)
          // If saved layout fails, try to use current positions
          if (currentPositionsRef.current.size > 0) {
            setNodes((nds) => {
              let updated = nds.map(node => {
                const savedPos = currentPositionsRef.current.get(node.id)
                
                // Header nodes are skipped from restoration (will be forced to correct positions later)

                // Apply saved position unless it's a header node (start, execution-settings, variables)
                if (savedPos && node.id !== 'start' && node.id !== 'execution-settings' && node.id !== 'variables') {
                  return { ...node, position: savedPos }
                }
                return node
              })
              buildNodeGroups(updated)
              
              // CRITICAL: Force header nodes to correct positions
              updated = updated.map(node => {
                if (headerNodePositions.has(node.id)) {
                  return { ...node, position: headerNodePositions.get(node.id)! }
                }
                return node
              })
              
              // Clear current positions after use
              currentPositionsRef.current.clear()
              currentOffsetsRef.current.clear()
              return updated
            })
          }
        })
      } else {
        // No saved layout or layout direction changed - ensure header nodes have correct positions from usePlanToFlow
        const headerNodePositions = new Map<string, { x: number; y: number }>()
        initialNodes.forEach(node => {
          if (node.id === 'start' || node.id === 'execution-settings' || node.id === 'variables') {
            headerNodePositions.set(node.id, { x: node.position.x, y: node.position.y })
          }
        })
        
        if (headerNodePositions.size > 0) {
          setNodes((nds) => {
            return nds.map(node => {
              if (headerNodePositions.has(node.id)) {
                return { ...node, position: headerNodePositions.get(node.id)! }
              }
              return node
            })
          })
          
          // Also use updateNode API to force positions
          headerNodePositions.forEach((pos, nodeId) => {
            updateNode(nodeId, { position: pos })
          })
        }
      }
      
      // Only reset view initialization flag if we don't have a selected node
      // If we have a selected node, we'll re-focus on it after nodes update
      if (!hasSelectedNode) {
        hasInitializedView.current = false
      }
      
      prevNodesRef.current = initialNodes
      
      // After nodes are updated, check if we need to focus on a changed step (from backend updates)
      // Use setTimeout to ensure nodes are fully rendered in React Flow
      if (pendingFocusStepIdRef.current) {
        const stepIdToFocus = pendingFocusStepIdRef.current
        // Store focusNode in a local variable to avoid dependency issues
        const focusNodeFn = focusNode
        setTimeout(() => {
          // Find the node for this step ID - prioritize step.id over node.id for accurate matching
          // For orchestration steps: nodeData.step.id is the wrapper step ID (e.g., "orchestrate-hdfc-bank-login")
          // For conditional steps: nodeData.step.id is the wrapper step ID
          // For branch steps: nodeData.step.id is the actual step ID from plan.json (not the constructed node ID)
          const nodeToFocus = initialNodes.find(n => {
            if (n.type === 'step' || n.type === 'conditional' || n.type === 'decision') {
              const nodeData = n.data as StepNodeData | ConditionalNodeData | DecisionNodeData
              // Match by step.id first (this is the actual step ID from plan.json - the wrapper step ID for orchestration/conditional)
              // This matches what the backend sends in changed_step_ids
              const stepId = nodeData?.step?.id
              if (stepId === stepIdToFocus) {
                return true
              }
              // Fallback: match by node.id only if step.id doesn't exist (shouldn't happen for valid steps)
              if (!stepId && n.id === stepIdToFocus) {
                return true
              }
              return false
            }
            return false
          })
          
          if (nodeToFocus) {
            // Focus on the changed step (position viewport, but don't open sidebar)
            focusNodeFn(nodeToFocus.id, { topPadding: 150, selectNode: false, delay: 0 })
            const nodeData = nodeToFocus.data as StepNodeData | ConditionalNodeData | DecisionNodeData
            console.log('[WorkflowCanvas] Auto-focused on step that was changed by backend:', {
              stepId: stepIdToFocus,
              nodeId: nodeToFocus.id,
              stepTitle: nodeData?.step?.title,
              matchedBy: nodeData?.step?.id === stepIdToFocus ? 'step.id' : 'node.id'
            })
          } else {
            console.warn('[WorkflowCanvas] Could not find node for changed step ID:', stepIdToFocus, {
              availableNodes: initialNodes
                .filter(n => n.type === 'step' || n.type === 'conditional' || n.type === 'decision')
                .map(n => {
                  const nodeData = n.data as StepNodeData | ConditionalNodeData | DecisionNodeData
                  return {
                    nodeId: n.id,
                    stepId: nodeData?.step?.id,
                    stepTitle: nodeData?.step?.title
                  }
                })
            })
          }
          
          // Clear the pending focus
          pendingFocusStepIdRef.current = null
        }, 200) // Small delay to ensure React Flow has rendered the nodes
      }
    }
    
    if (nodesChanged) {
      console.timeEnd(`[WorkflowCanvas] setNodes-${presetQueryId?.slice(0,8)}`)
    }

    if (edgesChanged) {
      console.log(`%c[WorkflowCanvas] setEdges: ${initialEdges.length} edges (preset: ${presetQueryId?.slice(0,8)})`, 'color: #4CAF50')
      setEdges(initialEdges)
      prevEdgesRef.current = initialEdges
    }

  }, [initialNodes, initialEdges, setNodes, setEdges, focusNode, buildNodeGroups, loadSavedLayout, layoutDirection, setLayoutDirection, updateNode, presetQueryId])

  // Store selected node ID in ref to track which node is selected
  const selectedNodeIdRef = React.useRef<string | null>(null)
  React.useEffect(() => {
    if (selectedNode) {
      selectedNodeIdRef.current = selectedNode.id
    } else {
      selectedNodeIdRef.current = null
    }
  }, [selectedNode])

  // Update selectedNode when nodes change (e.g., when plan is refreshed from backend)
  // This ensures the side panel shows updated step data when plan changes
  // Also re-focuses on the selected node after nodes update (e.g., after saving step config)
  React.useEffect(() => {
    const selectedId = selectedNodeIdRef.current

    if (!selectedId || nodes.length === 0) {
      return
    }

    // Find the corresponding node in the new nodes array by ID
    const updatedNode = nodes.find(n => n.id === selectedId) as WorkflowNode | undefined
    if (!updatedNode) {
      // Selected node no longer exists (was deleted)
      setSelectedNode(null)
      // Reset view initialization since selected node is gone
      hasInitializedView.current = false
      return
    }

    // Check if we need to update selectedNode by comparing with current selection
    // Use a ref to get the current selectedNode without causing dependency issues
    const currentSelected = selectedNode
    if (!currentSelected || currentSelected.id !== selectedId) {
      // Selection changed or was cleared - don't update
      return
    }

    // Compare step data to see if it changed
    const oldData = currentSelected.data as StepNodeData | ConditionalNodeData | DecisionNodeData | undefined
    const newData = updatedNode.data as StepNodeData | ConditionalNodeData | DecisionNodeData | undefined
    const oldStep = oldData?.step
    const newStep = newData?.step

    let shouldUpdate = false
    if (oldStep && newStep) {
      // Compare by JSON stringify to detect any changes
      const oldStepStr = JSON.stringify(oldStep)
      const newStepStr = JSON.stringify(newStep)
      if (oldStepStr !== newStepStr) {
        shouldUpdate = true
      }
    } else if (updatedNode !== currentSelected) {
      // Node structure changed (e.g., type changed)
      shouldUpdate = true
    }

    if (shouldUpdate) {
      setSelectedNode(updatedNode)
      // Re-focus on the selected node after update (e.g., after saving step config)
      // This ensures the view stays focused on the same step when the sidebar is closed
      setTimeout(() => {
        focusNode(selectedId, { topPadding: 150, selectNode: false, delay: 0 })
      }, 100)
    }
  }, [nodes, selectedNode, focusNode]) // Include focusNode in dependencies

  // Load saved viewport from localStorage
  const savedViewportRef = React.useRef<{ x: number; y: number; zoom: number } | null>(null)
  React.useEffect(() => {
    try {
      const storageKey = getViewportStorageKey()
      const saved = localStorage.getItem(storageKey)
      if (saved) {
        const parsed = JSON.parse(saved)
        if (parsed && typeof parsed.x === 'number' && typeof parsed.y === 'number' && typeof parsed.zoom === 'number') {
          savedViewportRef.current = { x: parsed.x, y: parsed.y, zoom: parsed.zoom }
        }
      }
    } catch (error) {
      console.error('[WorkflowCanvas] Failed to load viewport from localStorage:', error)
    }
  }, [getViewportStorageKey])

  // Set initial view to show start node (left side) on first load, or restore saved viewport
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    if (!hasInitializedView.current && nodes.length > 0) {
      // If we have a saved viewport, use it instead of positioning on start node
      if (savedViewportRef.current) {
        setTimeout(() => {
          setViewport({
            x: savedViewportRef.current!.x,
            y: savedViewportRef.current!.y,
            zoom: savedViewportRef.current!.zoom
          })
          viewportStateRef.current = savedViewportRef.current
          hasInitializedView.current = true
        }, 200)
        return
      }

      // Otherwise, position on start node (default behavior)
      const startNode = nodes.find(node => node.id === 'start')
      if (startNode) {
        // Use a small timeout to ensure React Flow has rendered and layout is complete
        setTimeout(() => {
          // Get the actual node position from React Flow (includes layout calculations)
          const flowNode = getNode('start')
          if (flowNode) {
            // Calculate viewport to show start node on the left side
            // In React Flow, viewport x/y are offsets from the origin
            // To show the start node on the left with padding:
            // - x: negative of node's x position + padding (moves viewport left to show node)
            // - y: center vertically on the start node
            const padding = 150 // Padding from left edge
            const canvasHeight = window.innerHeight || 800
            const viewportX = padding - flowNode.position.x
            const viewportY = (canvasHeight / 2) - flowNode.position.y - ((flowNode.height || 36) / 2)
            setViewport({ x: viewportX, y: viewportY, zoom: 0.9 })
            // Update viewport ref to match
            viewportStateRef.current = { x: viewportX, y: viewportY, zoom: 0.9 }
            hasInitializedView.current = true
            console.log('[WorkflowCanvas] Initial viewport set to show start node:', { viewportX, viewportY, nodePosition: flowNode.position })
          } else {
            // Fallback: use node position directly with simple calculation
            const padding = 150
            const canvasHeight = window.innerHeight || 800
            const viewportX = padding - startNode.position.x
            const viewportY = (canvasHeight / 2) - startNode.position.y
            setViewport({ x: viewportX, y: viewportY, zoom: 0.9 })
            // Update viewport ref to match
            viewportStateRef.current = { x: viewportX, y: viewportY, zoom: 0.9 }
            hasInitializedView.current = true
            console.log('[WorkflowCanvas] Initial viewport set (fallback) to show start node:', { viewportX, viewportY, nodePosition: startNode.position })
          }
        }, 200) // Slightly longer timeout to ensure layout is complete
      } else {
        console.warn('[WorkflowCanvas] Start node not found in nodes:', nodes.map(n => n.id))
      }
    }
  }, [nodes, setViewport, getNode])

  // Track previous stepStatusMap to detect actual changes
  const prevStepStatusMapRef = React.useRef<Map<string, 'pending' | 'running' | 'completed' | 'failed'>>(new Map())

  // Update node status based on maps from events (only when stepStatusMap actually changes)
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden

    // Check if stepStatusMap actually changed by comparing entries
    const hasChanged = stepStatusMap.size !== prevStepStatusMapRef.current.size ||
      Array.from(stepStatusMap.entries()).some(([stepId, status]) => 
        prevStepStatusMapRef.current.get(stepId) !== status
      )

    if (!hasChanged) {
      return // No actual changes, skip update
    }

    setNodes(nds => {
      let hasUpdates = false
      const updatedNodes = nds.map(node => {
        // Only update status for step-type nodes (step, conditional, loop, decision)
        // Validation and learning nodes have different status types
        if (node.type === 'step' || node.type === 'conditional' || node.type === 'decision') {
          const nodeData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData
          const stepId = nodeData?.step?.id || node.id
          const stepStatus = stepStatusMap.get(stepId)
          const currentStatus = nodeData?.status
          
          // Only update if status actually changed
          if (stepStatus && stepStatus !== currentStatus) {
            hasUpdates = true
            if (node.type === 'step') {
              return {
                ...node,
                data: { 
                  ...node.data, 
                  status: stepStatus
                } as StepNodeData
              } as WorkflowNode
            } else if (node.type === 'conditional') {
              return {
                ...node,
                data: { 
                  ...node.data, 
                  status: stepStatus
                } as ConditionalNodeData
              } as WorkflowNode
            } else if (node.type === 'decision') {
              return {
                ...node,
                data: { 
                  ...node.data, 
                  status: stepStatus
                } as DecisionNodeData
              } as WorkflowNode
            }
          }
        }
        return node
      })

      // Only return new array if there were actual updates
      return hasUpdates ? updatedNodes : nds
    })
    
    // Update previous status map (for tracking changes)
    prevStepStatusMapRef.current = new Map(stepStatusMap)
  }, [stepStatusMap, setNodes])


  // Handle node selection - disabled: nodes no longer open sidebar on click
  // Sidebar is now opened via settings icon button on nodes
  const onNodeClick = useCallback(() => {
    // Do nothing - clicking nodes no longer opens sidebar
    // Sidebar is opened via settings icon button instead
  }, [])

  // Handle node deselection
  const onPaneClick = useCallback(() => {
    setSelectedNode(null)
    setSelectedNodes([])
  }, [])

  // Handle multi-selection changes from React Flow
  const onSelectionChange = useCallback(({ nodes: selectedFlowNodes }: OnSelectionChangeParams) => {
    // Filter to only include configurable step nodes
    const configurableTypes = ['step', 'conditional', 'decision', 'todo_task', 'human_input']
    const stepNodes = selectedFlowNodes.filter(n =>
      configurableTypes.includes(n.type || '')
    ) as WorkflowNode[]
    setSelectedNodes(stepNodes)

    // If exactly one node is selected, also update selectedNode for sidebar compatibility
    if (stepNodes.length === 1) {
      setSelectedNode(stepNodes[0])
    } else {
      // Non-configurable or multiple selection - clear single selection
      setSelectedNode(null)
    }
  }, [])

  // Extract step IDs from selected nodes for multi-step configuration
  const selectedStepIds = React.useMemo(() => {
    return selectedNodes.map(node => {
      // Get step ID from node data if available, otherwise use node ID
      const data = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | undefined
      return data?.step?.id || node.id
    })
  }, [selectedNodes])

  // Handle start phase with execution options (for toolbar)
  const handleStartPhase = useCallback((phaseId: string, executionOptions?: ExecutionOptions) => {
    if (onStartPhase) {
      onStartPhase(phaseId, executionOptions)
    }
  }, [onStartPhase])

  // Handle start phase with stepId or ExecutionOptions (for StepSidebar) - adapter function
  // Note: stepId is already stored in workflow status by StepSidebar before calling this
  const handleStartPhaseForStep: StepSidebarStartPhase = useCallback((phaseId: string, stepIdOrOptions?: string | ExecutionOptions) => {
    // If ExecutionOptions is provided, pass it directly
    if (stepIdOrOptions && typeof stepIdOrOptions === 'object' && 'execution_strategy' in stepIdOrOptions) {
      const options = stepIdOrOptions as ExecutionOptions
      // If running a single step, try to highlight the node
      if (options.resume_from_step && selectedNode) {
        // Highlight the currently selected node (which is the step being run)
        highlightStepNode(selectedNode.id)
      }
      if (onStartPhase) {
        onStartPhase(phaseId, options)
      }
      return
    }
    
    // Otherwise, handle as stepId (string)
    const stepId = stepIdOrOptions as string | undefined
    // Highlight the step node if stepId is provided
    if (stepId) {
      highlightStepNode(stepId)
    }
    
    // Just trigger the phase start without execution options
    if (onStartPhase) {
      onStartPhase(phaseId, undefined)
    }
  }, [onStartPhase, highlightStepNode, selectedNode])

  // Get total step count
  const totalSteps = plan?.steps?.length || 0


  // Handle edit step
  const handleEditStep = useCallback(async (stepId: string, updates: Partial<PlanStep> | Partial<EvaluationStep>) => {
    if (!plan) return
    
    // Capture current node positions before refresh to preserve layout
    const positions = new Map<string, { x: number; y: number }>()
    const offsets = new Map<string, { parentId: string; dx: number; dy: number }>()
    nodesRef.current.forEach(node => {
      // Save parent node positions
      if (!childToParentRef.current.has(node.id)) {
        positions.set(node.id, { x: node.position.x, y: node.position.y })
      } else {
        // Save child offsets
        const parentId = childToParentRef.current.get(node.id)
        const offset = childOffsetsRef.current.get(node.id)
        if (parentId && offset) {
          offsets.set(node.id, {
            parentId,
            dx: offset.dx,
            dy: offset.dy
          })
        }
      }
    })
    currentPositionsRef.current = positions
    currentOffsetsRef.current = offsets
    console.log('[WorkflowCanvas] Captured current positions before save:', positions.size, 'positions', offsets.size, 'offsets')
    
    // First try to find in top-level steps (for backward compatibility)
    const stepIndex = plan.steps.findIndex(s => s.id === stepId)
    
    console.log('[WorkflowCanvas] handleEditStep:', {
      stepId,
      stepIndex,
      foundStep: stepIndex >= 0,
      stepTitle: stepIndex >= 0 ? plan.steps[stepIndex]?.title : 'N/A',
      stepHasCondition: stepIndex >= 0 ? (plan.steps[stepIndex] ? isConditionalStep(plan.steps[stepIndex]) : false) : false,
      hasAgentConfigs: 'agent_configs' in updates,
      updatesKeys: Object.keys(updates),
      isBranchStep: stepIndex < 0
    })
    
    if (stepIndex >= 0) {
      // Top-level step - use existing updateStep function
      const foundStep = plan.steps[stepIndex]
      if (foundStep.id !== stepId) {
        console.error('[WorkflowCanvas] Step ID mismatch!', {
          requestedStepId: stepId,
          foundStepId: foundStep.id,
          stepIndex,
          foundStepTitle: foundStep.title,
          foundStepHasCondition: isConditionalStep(foundStep)
        })
        throw new Error(`Step ID mismatch: requested ${stepId} but found ${foundStep.id} at index ${stepIndex}`)
      }
      
      await planData.updateStep(stepIndex, updates)
      
      // Highlight the step node after saving config
      highlightStepNode(stepId)
    } else {
      // Branch step - use backend API (handles nested steps recursively)
      if (!workspacePath) {
        throw new Error('Workspace path is required')
      }

      // Separate plan updates and config updates
      const { agent_configs, ...planUpdates } = updates

      // Send update instructions to backend
      const promises: Promise<{ success: boolean; message: string; data?: unknown }>[] = []

      // Update plan if there are plan-related fields
      if (Object.keys(planUpdates).length > 0) {
        promises.push(
          agentApi.updatePlanStep(workspacePath, stepId, planUpdates)
        )
      }

      // Update config if agent_configs is provided
      if (agent_configs !== undefined) {
        promises.push(
          agentApi.updateStepConfig(workspacePath, stepId, agent_configs)
        )
      }

      // Wait for all updates to complete
      await Promise.all(promises)

      // Refresh plan from backend
      await loadPlanRefresh()

      // Highlight the step node after saving
      highlightStepNode(stepId)
    }
  }, [plan, workspacePath, planData, highlightStepNode, loadPlanRefresh])

  // Handle delete step
  const handleDeleteStep = useCallback(async (stepId: string) => {
    if (!plan) return
    
    const stepIndex = plan.steps.findIndex(s => s.id === stepId)
    if (stepIndex >= 0) {
      await planData.deleteStep(stepIndex)
      setSelectedNode(null)
    }
  }, [plan, planData])

  // Handle bulk update steps
  const handleBulkUpdateSteps = useCallback(async (updates: Array<{ stepId: string; updates: Partial<PlanStep> }>) => {
    if (!plan || !workspacePath) {
      throw new Error('No plan loaded or workspace path missing')
    }

    console.log('[WorkflowCanvas] handleBulkUpdateSteps:', {
      updateCount: updates.length,
      stepIds: updates.map(u => u.stepId)
    })

    // Prepare batch update request
    const batchUpdates = updates.map(({ stepId, updates: stepUpdates }) => {
      const { agent_configs, ...planUpdates } = stepUpdates
      return {
        stepId,
        planUpdates: Object.keys(planUpdates).length > 0 ? planUpdates : undefined,
        configUpdates: agent_configs !== undefined ? agent_configs : undefined
      }
    }).filter(u => u.planUpdates !== undefined || u.configUpdates !== undefined)

    if (batchUpdates.length === 0) {
      console.log('[WorkflowCanvas] No updates to apply in bulk update')
      return
    }

    // Call backend batch update API
    const result = await agentApi.batchUpdateSteps(workspacePath, batchUpdates)

    // Log errors if any occurred
    if (result.data?.errors && result.data.errors.length > 0) {
      console.warn('[WorkflowCanvas] Batch update completed with errors:', {
        updatedSteps: result.data.updated_steps,
        updatedConfigs: result.data.updated_configs,
        errors: result.data.errors
      })
      // Optionally show error notification to user
      // You could add a toast notification here if needed
    } else {
      console.log('[WorkflowCanvas] Bulk update completed:', {
        updatedSteps: result.data?.updated_steps || 0,
        updatedConfigs: result.data?.updated_configs || 0
      })
    }

    // Refresh plan from backend
    await loadPlanRefresh()
  }, [plan, workspacePath, loadPlanRefresh])


  // Handle toggle dependency edges

  // Unified loading state - wait for ALL data before showing canvas
  // This ensures consistent state: plan, step_config, run folders, variables, phases, progress
  const isFullyLoaded = !loading && !isLoadingWorkspaceState
  const loadingMessages = []
  if (loading) loadingMessages.push('plan & step config')
  if (isLoadingWorkspaceState) loadingMessages.push('workspace state')

  if (!isFullyLoaded) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-gray-400 dark:border-gray-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-gray-500 dark:text-gray-400">
            Loading {loadingMessages.join(' & ')}...
          </span>
          <span className="text-xs text-gray-400 dark:text-gray-500">
            Please wait while we load everything
          </span>
        </div>
      </div>
    )
  }

  // Error state - show errors from plan loading or workspace state loading
  // Treat "plan.json not found" as "no plan" rather than an error (new workflows don't have plan.json yet)
  const isPlanNotFoundError = error && /not found|does not exist|planning must be run first/i.test(error)
  const effectiveError = isPlanNotFoundError ? null : error
  const hasError = effectiveError || workspaceStateError

  if (hasError) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <div className="flex flex-col items-center gap-3 text-center max-w-md">
          <div className="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <span className="text-2xl">⚠️</span>
          </div>
          <div className="flex flex-col gap-2">
            {effectiveError && (
              <span className="text-sm text-red-600 dark:text-red-400">
                <strong>Plan error:</strong> {effectiveError}
              </span>
            )}
            {workspaceStateError && (
              <div className="flex flex-col gap-2">
                <span className="text-sm text-red-600 dark:text-red-400">
                  <strong>Workspace error:</strong> {workspaceStateError}
                </span>
                {isRetryingWorkspaceState && (
                  <div className="flex items-center gap-2 text-sm text-blue-600 dark:text-blue-400">
                    <div className="w-4 h-4 border-2 border-blue-600 dark:border-blue-400 border-t-transparent rounded-full animate-spin" />
                    <span>
                      Retrying in {workspaceStateRetryCountdown !== null ? `${workspaceStateRetryCountdown} second${workspaceStateRetryCountdown !== 1 ? 's' : ''}...` : '5 seconds...'}
                    </span>
                  </div>
                )}
              </div>
            )}
          </div>
          <button
            onClick={() => {
              loadPlanRefresh()
              refreshWorkspaceState()
            }}
            className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={isRetryingWorkspaceState}
          >
            {isRetryingWorkspaceState ? 'Retrying...' : 'Retry Loading'}
          </button>
        </div>
      </div>
    )
  }

  // No plan state
  const hasPlan = !!(plan && plan.steps && plan.steps.length > 0)
  if (!hasPlan) {
    return (
      <div className={`flex flex-col h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <WorkflowToolbar
          status={status}
          hasPlan={false}
          currentPhase={currentPhase}
          workspacePath={workspacePath}
          totalSteps={0}
          presetQueryId={presetQueryId}
          runFolders={runFoldersForToolbar}
          variablesManifest={variablesManifest}
          stepProgress={stepProgress}
          isLoadingWorkspaceState={isLoadingWorkspaceState}
          onStartPhase={handleStartPhase}
          onStop={stopWorkflow}
          onCreatePlan={onCreatePlan || (() => {})}
          showChatArea={showChatArea}
          onToggleChatArea={onToggleChatArea}
          onRefresh={handleRefresh}
        />
        <div className="flex-1 flex items-center justify-center">
          <div className="flex flex-col items-center gap-4 text-center">
            <div className="w-16 h-16 rounded-full bg-gray-100 dark:bg-gray-800 flex items-center justify-center">
              <span className="text-3xl">📋</span>
            </div>
            <div>
              <h3 className="text-lg font-medium text-gray-900 dark:text-gray-100">
                No Plan Yet
              </h3>
              <p className="text-sm text-gray-500 dark:text-gray-400 mt-1">
                Create a plan to visualize your workflow
              </p>
            </div>
            {onCreatePlan && (
              <button
                onClick={onCreatePlan}
                className="px-6 py-2.5 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600 font-medium"
              >
                Build Plan
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`flex flex-col h-full ${className}`} ref={reactFlowWrapper}>
      {/* Toolbar */}
      <WorkflowToolbar
        status={status}
        hasPlan={true}
        plan={plan || undefined}
        currentPhase={currentPhase}
        workspacePath={workspacePath}
        totalSteps={totalSteps}
        presetQueryId={presetQueryId}
        runFolders={runFoldersForToolbar}
        variablesManifest={variablesManifest}
        stepProgress={stepProgress}
        isLoadingWorkspaceState={isLoadingWorkspaceState}
        onStartPhase={handleStartPhase}
        onStop={stopWorkflow}
        onBulkUpdateSteps={handleBulkUpdateSteps}
        onCreatePlan={onCreatePlan || (() => {})}
        showChatArea={showChatArea}
        onToggleChatArea={onToggleChatArea}
        onRefresh={handleRefresh}
        onSaveLayout={saveLayout}
        onDeleteLayout={deleteLayout}
        hasUnsavedLayoutChanges={hasUnsavedLayoutChanges}
        isSavingLayout={isSavingLayout}
        isDeletingLayout={isDeletingLayout}
        selectedStepIds={selectedStepIds}
      />

      {/* Canvas area — skip when toolbarOnly to avoid rendering 1000+ SVG nodes */}
      {toolbarOnly ? null : canvasViewMode === 'plan' ? (
        <div className="flex-1 min-h-0 relative">
          {stablePlan && <PlanOutlineView
            plan={stablePlan}
            stepProgress={stepProgress}
            stepStatusMap={stepStatusMap}
            onStepClick={(stepId) => { setCanvasViewMode('flow'); handleNavigateToStep(stepId) }}
            onFileClick={(filePath) => {
              useWorkspaceStore.getState().highlightFile(filePath)
            }}
            onRefresh={handleRefresh}
            workspacePath={workspacePath}
            className="h-full"
          />}
          {/* Floating view mode toggle — bottom left */}
          <button
            onClick={() => setCanvasViewMode('flow')}
            className="absolute bottom-3 left-3 z-20 flex items-center gap-1.5 rounded-md border border-border bg-card/95 backdrop-blur shadow-md px-2 py-1 text-[10px] font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
            title="Switch to flow diagram"
          >
            <Workflow className="w-3 h-3" />
            <span>Flow</span>
          </button>
        </div>
      ) : <div className="flex-1 min-h-0 relative flex">
        <div className={`flex-1 min-h-0 h-full transition-all duration-300 ${
          selectedNode
            ? (showChatArea ? 'mr-[50vw]' : (isStepSidebarCompact ? 'mr-[400px]' : 'mr-[600px]'))
            : selectedStepIds.length >= 2
              ? (isStepSidebarCompact ? 'mr-[400px]' : 'mr-[500px]')
              : showVariablesSidebar
                ? 'mr-[450px]'
                : ''
        }`}>
        <ReactFlow
          className="w-full h-full bg-gray-50 dark:bg-gray-900"
          style={{ width: '100%', height: '100%' }}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          onSelectionChange={onSelectionChange}
          multiSelectionKeyCode={["Shift", "Control", "Meta"]}
          selectionOnDrag={isShiftPressed}
          panOnDrag={!isShiftPressed}
          panOnScroll
          selectionKeyCode="Shift"
          selectionMode={SelectionMode.Partial}
          // PERF: Only render nodes visible in the viewport (React Flow virtualization).
          // Reduces SVG node count significantly for large workflows.
          onlyRenderVisibleElements
          onViewportChange={onViewportChange}
          nodeTypes={nodeTypes}
          fitView={false}
          fitViewOptions={{ padding: 0.1, minZoom: 1.0, maxZoom: 1.5 }}
          minZoom={0.1}
          maxZoom={2}
          defaultViewport={{ x: 100, y: 0, zoom: 0.9 }}
          attributionPosition="bottom-right"
        >
          <Background 
            variant={BackgroundVariant.Dots} 
            gap={20} 
            size={1} 
            color="#e5e7eb"
            className="dark:!bg-gray-900"
          />
        </ReactFlow>

        {/* Batch Progress Header - Above Legend */}
        <BatchProgressHeader position="canvas" />

        {/* Step Legend - Bottom Left */}
        {plan && plan.steps && plan.steps.length > 0 && (
          <StepLegend
            plan={plan}
            nodes={nodes}
            selectedNodeId={selectedNode?.id || null}
            onStepClick={handleNavigateToStep}
            workspacePath={workspacePath}
            currentStepId={currentStepId}
          />
        )}

        {/* Floating view mode toggle — top left */}
        <button
          onClick={() => setCanvasViewMode('plan')}
          className="absolute top-3 left-3 z-20 flex items-center gap-1.5 rounded-md border border-border bg-card/95 backdrop-blur shadow-md px-2 py-1 text-[10px] font-medium text-muted-foreground hover:text-foreground hover:bg-accent transition-colors"
          title="Switch to plan outline"
        >
          <List className="w-3 h-3" />
          <span>Plan</span>
        </button>

        {/* Floating canvas controls — top right */}
        <div className="absolute top-3 right-3 z-20 flex items-center gap-0.5 rounded-md border border-border bg-card/95 backdrop-blur shadow-md px-1 py-0.5">
          {/* Refresh */}
          <button
            onClick={() => handleRefresh()}
            className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
            title="Refresh plan & variables"
          >
            <RefreshCw className="w-3 h-3" />
          </button>
          {/* Layout direction */}
          <button
            onClick={() => {
              const next = layoutDirection === 'LR' ? 'TB' : 'LR'
              setLayoutDirection(next)
            }}
            className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
            title={layoutDirection === 'LR' ? 'Horizontal (click for vertical)' : 'Vertical (click for horizontal)'}
          >
            {layoutDirection === 'LR' ? <ArrowRight className="w-3 h-3" /> : <ArrowDown className="w-3 h-3" />}
          </button>
          {/* Save layout */}
          <button
            onClick={() => saveLayout()}
            disabled={isSavingLayout}
            className={`p-1 rounded transition-colors ${
              isSavingLayout ? 'text-muted-foreground cursor-not-allowed'
                : hasUnsavedLayoutChanges ? 'text-blue-500 hover:bg-blue-500/10 animate-pulse'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            }`}
            title={isSavingLayout ? 'Saving...' : hasUnsavedLayoutChanges ? 'Save layout (unsaved)' : 'Save layout'}
          >
            {isSavingLayout ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <Save className="w-3 h-3" />}
          </button>
          {/* Reset layout */}
          <button
            onClick={() => {
              if (window.confirm('Reset layout to default? This cannot be undone.')) {
                deleteLayout()
              }
            }}
            disabled={isDeletingLayout}
            className={`p-1 rounded transition-colors ${
              isDeletingLayout ? 'text-muted-foreground cursor-not-allowed'
                : 'text-muted-foreground hover:bg-muted hover:text-foreground'
            }`}
            title={isDeletingLayout ? 'Resetting...' : 'Reset layout'}
          >
            {isDeletingLayout ? <Loader2Icon className="w-3 h-3 animate-spin" /> : <RotateCcw className="w-3 h-3" />}
          </button>
        </div>
        </div>

        {/* Step Sidebar - Single step selected */}
        {selectedNode && (
          <StepSidebar
            node={selectedNode}
            onClose={() => setSelectedNode(null)}
            onEditStep={handleEditStep}
            onDeleteStep={handleDeleteStep}
            isRunning={status === 'running'}
            stepIndex={'stepIndex' in selectedNode.data && typeof selectedNode.data.stepIndex === 'number' ? selectedNode.data.stepIndex : 0}
            workspacePath={workspacePath}
            presetQueryId={presetQueryId}
            onStartPhase={handleStartPhaseForStep}
            plan={plan}
            isCompact={isStepSidebarCompact}
            showChatArea={showChatArea}
          />
        )}

        {/* Multi-Step Sidebar - Multiple steps selected */}
        {!selectedNode && selectedStepIds.length >= 2 && handleBulkUpdateSteps && (
          <MultiStepSidebar
            selectedStepIds={selectedStepIds}
            plan={plan || null}
            onClose={() => setSelectedNodes([])}
            onBulkUpdate={handleBulkUpdateSteps}
            isCompact={isStepSidebarCompact}
            showChatArea={showChatArea}
          />
        )}

        {/* Variables Sidebar */}
        {showVariablesSidebar && (
          <VariablesSidebar
            workspacePath={workspacePath}
            onClose={() => setShowVariablesSidebar(false)}
            onUpdate={handleVariablesUpdate}
            showChatArea={showChatArea}
          />
        )}
      </div>}

    </div>
  )
})

// Add display name for debugging
WorkflowCanvasInner.displayName = 'WorkflowCanvasInner'

// Wrap with ReactFlowProvider for hooks to work
export const WorkflowCanvasWithProvider = forwardRef<WorkflowCanvasRef, WorkflowCanvasProps>((props, ref) => {
  return (
    <ReactFlowProvider>
      <WorkflowCanvasInner {...props} ref={ref} />
    </ReactFlowProvider>
  )
})

WorkflowCanvasWithProvider.displayName = 'WorkflowCanvasWithProvider'

export const WorkflowCanvas = WorkflowCanvasWithProvider

export default WorkflowCanvasWithProvider
