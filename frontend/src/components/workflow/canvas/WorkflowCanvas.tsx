import React, { useCallback, useRef, useImperativeHandle, forwardRef, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  useReactFlow,
  BackgroundVariant,
  ReactFlowProvider,
  type NodeChange
} from '@xyflow/react'
import { RefreshCw } from 'lucide-react'
import '@xyflow/react/dist/style.css'

import { useModeStore } from '../../../stores/useModeStore'
import { nodeTypes } from '../nodes'
import { WorkflowToolbar } from './WorkflowToolbar'
import { VariablesSidebar } from './VariablesSidebar'
import { StepLegend } from './StepLegend'
import { BatchProgressHeader } from '../BatchProgressHeader'
import { PlanOutlineView } from './PlanOutlineView'
import { ReportView } from '../ReportViewer'
import { usePlanData, type PlanChanges } from '../hooks/usePlanData'
import { useEvaluationPlanData } from '../hooks/useEvaluationPlanData'
import { useOutputPlanData } from '../hooks/useOutputPlanData'
import { usePlanToFlow, type WorkflowNode, type WorkflowEdge, type StepNodeData, type ConditionalNodeData, type WorkflowArtifactNodeData } from '../hooks/usePlanToFlow'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useWorkflowExecution } from '../hooks/useWorkflowExecution'
import { useWorkspaceState } from '../hooks/useWorkspaceState'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { agentApi } from '../../../services/api'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { VariablesManifest } from '../../../services/api-types'
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
  sharedToolbar?: boolean
  paneClassName?: string
  className?: string
}

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
  sharedToolbar = false,
  paneClassName = '',
  className = ''
}, ref) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const highlightTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const { setViewport, getNode, updateNode, fitView, getViewport } = useReactFlow()
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
  // Flow view is vertical-only.
  const layoutDirection = 'TB' as const
  const canvasViewMode = useWorkflowStore(state => state.canvasViewMode)
  const setCanvasViewMode = useWorkflowStore(state => state.setCanvasViewMode)
  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const selectedGroupIds = useWorkflowStore(state => state.selectedGroupIds)
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)

  const isBuilderWorkspace = workflowWorkspaceView === null || workflowWorkspaceView === 'builder'

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
            if (nodeId === 'start' || nodeId === 'variables') {
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

  // Variables state
  const [variablesManifest, setVariablesManifest] = React.useState<VariablesManifest | null>(null)
  const [isLoadingVariables, setIsLoadingVariables] = React.useState(false)
  const [showVariablesSidebar, setShowVariablesSidebar] = React.useState(false)
  
  // Workflow store actions
  const setVariablesManifestInStore = useWorkflowStore.getState().setVariablesManifest
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
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

    // Only highlight if selectedRunFolder actually changed and is valid.
    // Guard: skip when the workflow canvas isn't the active mode — this effect
    // can fire while the canvas stays mounted in other modes (e.g. multi-agent
    // chat), and the fetchFiles(workspacePath) below would overwrite the
    // workspace state with workflow-scoped files, leaving the multi-agent file
    // panel empty after the filter pass.
    const activeMode = useModeStore.getState().selectedModeCategory
    if (activeMode !== 'workflow') {
      return
    }

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
    return workspaceState.run_folders.map(f => ({ name: f.name }))
  }, [workspaceState?.run_folders])

  useEffect(() => {
    if (!isBuilderWorkspace || !workspaceState?.run_folders?.length) {
      return
    }

    const availableRunFolders = new Set(workspaceState.run_folders.map(folder => folder.name))
    const activeRunFolder = workspaceState.active_executions?.find(execution => execution.run_folder)?.run_folder
    if (activeRunFolder && availableRunFolders.has(activeRunFolder) && selectedRunFolder !== activeRunFolder) {
      setSelectedRunFolder(activeRunFolder)
      return
    }

    if (
      selectedRunFolder &&
      selectedRunFolder !== 'new' &&
      availableRunFolders.has(selectedRunFolder)
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
    workspaceState?.active_executions,
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
    status
  } = useWorkflowExecution()

  // Current step and status from store (set by ChatArea polling when step_progress_updated events arrive)
  const currentStepId = useWorkflowStore(state => state.currentStepId)
  const stepStatusMap = useWorkflowStore(state => state.stepStatusMap)

  // React Flow state (need to define before usePlanToFlow to use in callbacks)
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<WorkflowEdge>([])
  // Store latest nodes in ref to avoid dependency issues
  const nodesRef = React.useRef(nodes)
  React.useEffect(() => {
    nodesRef.current = nodes
  }, [nodes])

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
    onNodesChangeBase(filteredChanges as NodeChange<WorkflowNode>[])

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

  }, [onNodesChangeBase, setNodes])

  // Single reusable function to focus/position a node at the top-left of the screen
  const focusNode = useCallback((
    nodeId: string,
    options?: {
      topPadding?: number  // Vertical padding from top (default: 50)
      delay?: number  // Delay before positioning (default: 100ms)
    }
  ) => {
    const {
      topPadding = 50,
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

      }
    }, delay)
  }, [getNode, setViewport])

  // Handle navigating to a step from legend (without opening sidebar)
  const handleNavigateToStep = useCallback((nodeId: string) => {
    focusNode(nodeId, { topPadding: 150, delay: 100 })
    console.log('[WorkflowCanvas] Navigated to step from legend:', nodeId)
  }, [focusNode])

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

    const artifactBaseX = endNode.position.x
    const artifactBaseY = endNode.position.y + 170

    addonConfigs.forEach((config, index) => {
      const position = { x: artifactBaseX + (index * 260) - 130, y: artifactBaseY }

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
  }, [planFlow, evaluationPlan, outputPlan])

  const { nodes: initialNodes, edges: initialEdges } = augmentedFlow

  // Helper function to highlight and position a specific step node
  const highlightStepNode = useCallback((stepId: string) => {
    // Find the node by matching step.id in node data (works for both top-level and branch steps)
    // Branch steps have node IDs like "step-3-true-0" but step.id is the actual step ID
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData
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
      focusNode(nodeToFocus.id, { topPadding: 150, delay: 100 })
    } else {
      console.log('[WorkflowCanvas] highlightStepNode - no node found for stepId:', stepId)
    }
  }, [focusNode])

  // Auto-focus disabled - running step name is now shown in StepLegend instead
  // This prevents the canvas from jumping around during workflow execution

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
  // CRITICAL: Force header nodes to correct positions after nodes update
  // Ensure header nodes maintain correct positions (safety net in case something tries to override them)
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    if (nodes.length === 0 || initialNodes.length === 0) return
    
    const varsNode = initialNodes.find(n => n.id === 'variables')
    const startNode = initialNodes.find(n => n.id === 'start')
    
    if (!varsNode && !startNode) return
    
    // Check if any header node position has been overridden
    const currentVars = nodes.find(n => n.id === 'variables')
    const currentStart = nodes.find(n => n.id === 'start')
    
    let needsFix = false
    
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
      if (varsNode) updateNode('variables', { position: varsNode.position })
      if (startNode) updateNode('start', { position: startNode.position })
    }
  }, [nodes, initialNodes, updateNode, toolbarOnly, canvasViewMode])

  // Rebuild node groups when nodes change
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    hasInitializedView.current = false
  }, [canvasViewMode, toolbarOnly])

  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return // Skip when canvas is hidden
    if (nodes.length > 0) {
      buildNodeGroups(nodes)
    }
  }, [nodes, buildNodeGroups, toolbarOnly, canvasViewMode])

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
        const oldData = node?.data as StepNodeData | ConditionalNodeData | undefined
        const newData = newNode?.data as StepNodeData | ConditionalNodeData | undefined
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
      setNodes(initialNodes)

      // Always try to restore positions after nodes regenerate (unless layout direction changed)
      // Priority: 1) Saved layout from file, 2) Current positions (captured before refresh), 3) Auto-layout
      if (initialNodes.length > 0) {
        // Extract header node positions from initialNodes BEFORE any restoration
        // These positions are calculated by usePlanToFlow and MUST be preserved
        const headerNodePositions = new Map<string, { x: number; y: number }>()
        initialNodes.forEach(node => {
          if (node.id === 'start' || node.id === 'variables') {
            headerNodePositions.set(node.id, { x: node.position.x, y: node.position.y })
          }
        })
        // Checking for saved layout...
        
        // First try to load saved layout from file
        loadSavedLayout().then(savedLayout => {
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

                // Apply saved position unless it's a header node (start, variables)
                // Header nodes MUST always use the enforced horizontal layout from usePlanToFlow
                if (savedPos && node.id !== 'start' && node.id !== 'variables') {
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

                // Apply saved position unless it's a header node (start, variables)
                if (savedPos && node.id !== 'start' && node.id !== 'variables') {
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
          if (node.id === 'start' || node.id === 'variables') {
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
      
      hasInitializedView.current = false
      
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
            if (n.type === 'step' || n.type === 'conditional') {
              const nodeData = n.data as StepNodeData | ConditionalNodeData
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
            focusNodeFn(nodeToFocus.id, { topPadding: 150, delay: 0 })
            const nodeData = nodeToFocus.data as StepNodeData | ConditionalNodeData
            console.log('[WorkflowCanvas] Auto-focused on step that was changed by backend:', {
              stepId: stepIdToFocus,
              nodeId: nodeToFocus.id,
              stepTitle: nodeData?.step?.title,
              matchedBy: nodeData?.step?.id === stepIdToFocus ? 'step.id' : 'node.id'
            })
          } else {
            console.warn('[WorkflowCanvas] Could not find node for changed step ID:', stepIdToFocus, {
              availableNodes: initialNodes
                .filter(n => n.type === 'step' || n.type === 'conditional')
                .map(n => {
                  const nodeData = n.data as StepNodeData | ConditionalNodeData
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

  }, [initialNodes, initialEdges, setNodes, setEdges, focusNode, buildNodeGroups, loadSavedLayout, layoutDirection, updateNode, presetQueryId, toolbarOnly, canvasViewMode])

  // Fit the full plan on first render so the workflow shape is visible by default.
  React.useEffect(() => {
    if (toolbarOnly || canvasViewMode === 'plan') return
    if (!hasInitializedView.current && nodes.length > 0) {
      const fitTimer = window.setTimeout(() => {
        window.requestAnimationFrame(() => {
          Promise.resolve(
            fitView({ padding: 0.18, duration: 350, minZoom: 0.15, maxZoom: 1.1 })
          ).finally(() => {
            viewportStateRef.current = getViewport()
            hasInitializedView.current = true
          })
        })
      }, 220)

      return () => window.clearTimeout(fitTimer)
    }
  }, [nodes, fitView, getViewport, toolbarOnly, canvasViewMode])

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
        // Only update status for step-type nodes (step, conditional, loop)
        // Validation and learning nodes have different status types
        if (node.type === 'step' || node.type === 'conditional') {
          const nodeData = node.data as StepNodeData | ConditionalNodeData
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
  }, [stepStatusMap, setNodes, toolbarOnly, canvasViewMode])


  const onNodeClick = useCallback(() => {}, [])
  const onPaneClick = useCallback(() => {}, [])

  // Handle start phase with execution options (for toolbar)
  const handleStartPhase = useCallback((phaseId: string, executionOptions?: ExecutionOptions) => {
    if (onStartPhase) {
      onStartPhase(phaseId, executionOptions)
    }
  }, [onStartPhase])

  // Unified loading state - wait for ALL data before showing canvas
  // This ensures consistent state: plan, step_config, run folders, variables, phases, progress
  const isFullyLoaded = !loading && !isLoadingWorkspaceState
  const loadingMessages = []
  if (loading) loadingMessages.push('plan & step config')
  if (isLoadingWorkspaceState) loadingMessages.push('workspace state')

  if (!isFullyLoaded) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${paneClassName} ${className}`}>
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
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${paneClassName} ${className}`}>
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
      <div className={`flex flex-col h-full bg-gray-50 dark:bg-gray-900 ${className} ${sharedToolbar && showChatArea ? 'md:contents' : ''}`}>
        <div className={sharedToolbar && showChatArea ? 'md:col-span-2 md:row-start-1' : ''}>
          <WorkflowToolbar
            status={status}
            hasPlan={false}
            currentPhase={currentPhase}
            workspacePath={workspacePath}
            presetQueryId={presetQueryId}
            runFolders={runFoldersForToolbar}
            variablesManifest={variablesManifest}
            isLoadingWorkspaceState={isLoadingWorkspaceState}
            onStartPhase={handleStartPhase}
            onCreatePlan={onCreatePlan || (() => {})}
            showChatArea={showChatArea}
            onToggleChatArea={onToggleChatArea}
            onRefresh={handleRefresh}
          />
        </div>
        <div className={`${sharedToolbar && showChatArea ? 'flex-1 md:col-start-2 md:row-start-2' : 'flex-1'} ${paneClassName} flex min-h-0 items-center justify-center`}>
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
    <div className={`flex flex-col h-full ${className} ${sharedToolbar && showChatArea ? 'md:contents' : ''}`} ref={reactFlowWrapper}>
      <div className={sharedToolbar && showChatArea ? 'md:col-span-2 md:row-start-1' : ''}>
        <WorkflowToolbar
          status={status}
          hasPlan={true}
          plan={plan || undefined}
          currentPhase={currentPhase}
          workspacePath={workspacePath}
          presetQueryId={presetQueryId}
          runFolders={runFoldersForToolbar}
          variablesManifest={variablesManifest}
          isLoadingWorkspaceState={isLoadingWorkspaceState}
          onStartPhase={handleStartPhase}
          onCreatePlan={onCreatePlan || (() => {})}
          showChatArea={showChatArea}
          onToggleChatArea={onToggleChatArea}
          onRefresh={handleRefresh}
        />
      </div>

      <div className={`${sharedToolbar && showChatArea ? 'flex-1 md:col-start-2 md:row-start-2' : 'flex-1'} ${paneClassName} min-h-0`}>
        {/* Canvas area — skip when toolbarOnly to avoid rendering 1000+ SVG nodes */}
        {toolbarOnly ? null : canvasViewMode === 'plan' ? (
          <div className="h-full min-h-0 relative">
            {stablePlan && <PlanOutlineView
              plan={stablePlan}
                stepStatusMap={stepStatusMap}
              onStepClick={(stepId) => { setCanvasViewMode('flow'); handleNavigateToStep(stepId) }}
              onFileClick={(filePath) => {
                useWorkspaceStore.getState().highlightFile(filePath)
              }}
              onRefresh={handleRefresh}
              workspacePath={workspacePath}
              className="h-full"
            />}
          </div>
        ) : canvasViewMode === 'report' ? (
          <div className="h-full min-h-0 relative">
            {workspacePath && <ReportView workspacePath={workspacePath} mobilePreview={showChatArea} />}
          </div>
        ) : <div className="h-full min-h-0 relative flex">
          <div className={`flex-1 min-h-0 h-full transition-all duration-300 ${showVariablesSidebar ? 'mr-[450px]' : ''}`}>
        <ReactFlow
          className="w-full h-full bg-gray-50 dark:bg-gray-900"
          style={{ width: '100%', height: '100%' }}
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          panOnDrag
          panOnScroll
          nodesDraggable={false}
          nodesConnectable={false}
          nodesFocusable={false}
          elementsSelectable={false}
          edgesFocusable={false}
          onlyRenderVisibleElements={false}
          onViewportChange={onViewportChange}
          nodeTypes={nodeTypes}
          fitView={false}
          fitViewOptions={{ padding: 0.18, minZoom: 0.15, maxZoom: 1.1 }}
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
            selectedNodeId={null}
            onStepClick={handleNavigateToStep}
            workspacePath={workspacePath}
            currentStepId={currentStepId}
          />
        )}

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
        </div>
        </div>

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
