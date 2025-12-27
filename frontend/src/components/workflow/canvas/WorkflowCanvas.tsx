import React, { useCallback, useRef, useImperativeHandle, forwardRef, useEffect } from 'react'
import {
  ReactFlow,
  Background,
  useNodesState,
  useEdgesState,
  useReactFlow,
  BackgroundVariant,
  ReactFlowProvider
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'

import { nodeTypes } from '../nodes'
import { WorkflowToolbar } from './WorkflowToolbar'
import { StepSidebar } from './StepSidebar'
import { VariablesSidebar } from './VariablesSidebar'
import { StepLegend } from './StepLegend'
import { usePlanData, type PlanChanges } from '../hooks/usePlanData'
import { usePlanToFlow, type WorkflowNode, type WorkflowEdge, type StepNodeData, type ConditionalNodeData, type LoopNodeData, type DecisionNodeData, type OrchestratorNodeData } from '../hooks/usePlanToFlow'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useWorkflowExecution } from '../hooks/useWorkflowExecution'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useAppStore } from '../../../stores/useAppStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { agentApi } from '../../../services/api'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep } from '../../../utils/stepConfigMatching'
import type { VariablesManifest } from '../../../services/api-types'

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
  className?: string
}

// Adapter for StepSidebar which uses stepId or ExecutionOptions
type StepSidebarStartPhase = (phaseId: string, stepIdOrOptions?: string | ExecutionOptions) => void

// Ref interface for external control of the canvas
export interface WorkflowCanvasRef {
  refresh: (changedStepIDs?: string[], deletedStepIDs?: string[]) => Promise<PlanChanges | null>
  getStepCount: () => number
}

const WorkflowCanvasInner = forwardRef<WorkflowCanvasRef, WorkflowCanvasProps>(({
  workspacePath,
  presetQueryId,
  currentPhase,
  onStartPhase,
  onCreatePlan,
  showChatArea = false,
  onToggleChatArea,
  className = ''
}, ref) => {
  const reactFlowWrapper = useRef<HTMLDivElement>(null)
  const highlightTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  const { fitView, zoomIn, zoomOut, setViewport, getNode } = useReactFlow()
  const hasInitializedView = React.useRef(false)
  // Store step ID to focus on after nodes update (from backend plan changes)
  const pendingFocusStepIdRef = React.useRef<string | null>(null)
  // Store current viewport state (x, y, zoom) to preserve it during refresh
  const viewportStateRef = React.useRef<{ x: number; y: number; zoom: number } | null>(null)
  
  // Generate localStorage key for viewport state (workspace-specific)
  const getViewportStorageKey = React.useCallback(() => {
    return workspacePath 
      ? `workflow-viewport-${workspacePath}` 
      : 'workflow-viewport-default'
  }, [workspacePath])

  // Track completed step indices from selected iteration (for enabling/disabling run buttons)
  const [completedStepIndices, setCompletedStepIndices] = React.useState<number[]>([])
  
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
  
  // Note: We no longer need handleProgressChange callback
  // The completedStepIndices are synced directly from stepProgress in the useEffect below

  // Load initial completedStepIndices from store when component mounts or selectedRunFolder/stepProgress changes
  // This ensures we have the correct state even after page refresh
  // Use ref to track previous indices to prevent loops
  const prevIndicesRef = useRef<string>('')
  useEffect(() => {
    const indices = stepProgress?.completed_step_indices || []
    const indicesStr = JSON.stringify(indices.slice().sort((a, b) => a - b))
    const prevIndicesStr = prevIndicesRef.current
    const indicesChanged = prevIndicesStr !== indicesStr
    
    console.log('[EFFECT_DEBUG] WorkflowCanvas - completedStepIndices sync effect:', {
      selectedRunFolder,
      stepProgressIsNull: stepProgress === null,
      stepProgressRef: stepProgress,
      prevIndicesStr,
      newIndicesStr: indicesStr,
      indicesChanged,
      willUpdate: indicesChanged
    })
    
    if (indicesChanged) {
      prevIndicesRef.current = indicesStr
      setCompletedStepIndices(indices)
    }
  }, [selectedRunFolder, stepProgress]) // Don't include completedStepIndices to prevent loop

  // Highlight execution folder in workspace when selectedRunFolder changes
  // This ensures workspace shows the correct group folder during multi-group execution
  const { highlightFile, fetchFiles } = useWorkspaceStore()
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
      
      console.log('[WorkflowCanvas] Highlighting execution folder due to selectedRunFolder change:', {
        selectedRunFolder,
        executionPath
      })
      
      // Refresh files first to ensure the folder exists in the tree
      fetchFiles().then(() => {
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
  }, [selectedRunFolder, workspacePath, highlightFile, fetchFiles])

  // Load plan data with change detection
  const { plan, loading, error, changes, updateStep, deleteStep, refresh: loadPlanRefresh, clearChanges, setChanges } = usePlanData(workspacePath)

  // Load variables when workspace changes
  React.useEffect(() => {
    if (!workspacePath) {
      setVariablesManifest(null)
      setVariablesManifestInStore(null)
      return
    }

    const loadVariables = async () => {
      setIsLoadingVariables(true)
      try {
        const response = await agentApi.getVariableGroups(workspacePath)
        if (response.success && response.manifest) {
          setVariablesManifest(response.manifest)
          // Also store in workflow store for buildExecutionOptions to access
          setVariablesManifestInStore(response.manifest)
        } else {
          setVariablesManifest(null)
          setVariablesManifestInStore(null)
        }
      } catch (err) {
        console.error('[WorkflowCanvas] Failed to load variables:', err)
        setVariablesManifest(null)
        setVariablesManifestInStore(null)
      } finally {
        setIsLoadingVariables(false)
      }
    }

    loadVariables()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspacePath])

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

  // Refresh handler - reloads plan, step config, and variables
  const handleRefresh = useCallback(async () => {
    if (!workspacePath) return
    
    console.log('[WorkflowCanvas] Refreshing plan, step config, and variables...')
    
    // Save current viewport state before refresh
    // Only save if viewport has been initialized (not on first load)
    const currentViewport = hasInitializedView.current ? viewportStateRef.current : null
    console.log('[WorkflowCanvas] Saving viewport state before refresh:', currentViewport, 'hasInitializedView:', hasInitializedView.current)
    
    // Refresh plan data (this also loads step_config.json)
    await loadPlanRefresh()
    
    // Reload variables
    setIsLoadingVariables(true)
    try {
      const response = await agentApi.getVariableGroups(workspacePath)
      if (response.success && response.manifest) {
        setVariablesManifest(response.manifest)
        setVariablesManifestInStore(response.manifest)
      } else {
        setVariablesManifest(null)
        setVariablesManifestInStore(null)
      }
    } catch (err) {
      console.error('[WorkflowCanvas] Failed to reload variables:', err)
      setVariablesManifest(null)
      setVariablesManifestInStore(null)
    } finally {
      setIsLoadingVariables(false)
    }
    
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
  }, [workspacePath, loadPlanRefresh, setVariablesManifestInStore, setViewport])

  // Workflow execution
  const {
    status,
    stepStatusMap,
    stopWorkflow,
    currentStepId
  } = useWorkflowExecution()
  
  const isExecuting = status === 'running'

  // Refs for callbacks that need to be defined early
  const handleRunFromStepRef = React.useRef<((stepIndex: number, stepId: string) => void) | null>(null)
  const handleOpenSidebarRef = React.useRef<((nodeId: string) => void) | null>(null)
  
  // React Flow state (need to define before usePlanToFlow to use in callbacks)
  const [nodes, setNodes, onNodesChange] = useNodesState<WorkflowNode>([])
  const [edges, setEdges, onEdgesChange] = useEdgesState<WorkflowEdge>([])
  const [selectedNode, setSelectedNode] = React.useState<WorkflowNode | null>(null)

  // Store latest nodes in ref to avoid dependency issues
  const nodesRef = React.useRef(nodes)
  React.useEffect(() => {
    nodesRef.current = nodes
  }, [nodes])

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

  // Convert plan to React Flow nodes and edges (with change highlights and run callback)
  const { nodes: initialNodes, edges: initialEdges } = usePlanToFlow(plan, { 
    // Prerequisite edges are always shown (default: true in usePlanToFlow)
    changes,  // Pass changes to highlight modified nodes
    onRunFromStep: handleRunFromStepCallback,
    onOpenSidebar: handleOpenSidebarCallback,
    isExecuting,
    completedStepIndices,  // Pass completed steps for enabling/disabling run buttons
    stepStatusMap: stepStatusMap,  // Pass step status map from events
    workspacePath,  // Pass workspace path for file opening
    selectedRunFolder,  // Pass selected run folder for file opening
    variablesManifest,  // Pass variables manifest for Variables node
    onOpenVariablesSidebar: handleOpenVariablesSidebar,  // Callback for opening variables sidebar
    isLoadingVariables  // Whether variables are loading
  })

  // Helper function to highlight and position a specific step node
  const highlightStepNode = useCallback((stepId: string) => {
    // Find the node by matching step.id in node data (works for both top-level and branch steps)
    // Branch steps have node IDs like "step-3-true-0" but step.id is the actual step ID
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData
        const nodeStepId = nodeData?.step?.id
        // Match by step.id (for branch steps) or by node ID (for top-level steps)
        return nodeStepId === stepId || (nodeStepId === undefined && node.id === stepId)
      }
      return false
    })
    
    if (nodeToFocus) {
      // Focus viewport on the node without changing sidebar selection.
      // This keeps the sidebar closed for execution events (currentStepId)
      // and simply repositions the view when the sidebar is already open.
      focusNode(nodeToFocus.id, { topPadding: 50, selectNode: false, delay: 100 })
      console.log('[WorkflowCanvas] Highlighted step node:', stepId, '-> node ID:', nodeToFocus.id)
    } else {
      console.warn('[WorkflowCanvas] Could not find node for stepId:', stepId)
    }
  }, [focusNode])

  // Auto-focus on the current running step when it changes
  React.useEffect(() => {
    if (currentStepId) {
      highlightStepNode(currentStepId)
    }
  }, [currentStepId, highlightStepNode])

  // Handle "run from step" button click on nodes - runs only the single step
  // Uses workflow store directly for execution options (single source of truth)
  const handleRunFromStep = useCallback((stepIndex: number, stepId: string) => {
    console.log('[WorkflowCanvas] Run single step clicked:', stepIndex, stepId)
    console.log('[WorkflowCanvas] onStartPhase available:', !!onStartPhase)
    console.log('[WorkflowCanvas] selectedRunFolder:', selectedRunFolder)
    
    // Find the node that matches this stepId
    // The node ID might be stepId, or it might be step-${stepIndex} if stepId doesn't exist
    // We need to find the node by matching step.id in the node data
    const nodeToFocus = nodesRef.current.find(node => {
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData
        const nodeStepId = nodeData?.step?.id || node.id
        // Match by stepId or by stepIndex if stepId matches
        return nodeStepId === stepId || (nodeData?.stepIndex === stepIndex && node.id === stepId)
      }
      return false
    })
    
    if (nodeToFocus) {
      // Position viewport to show step at top-left (but don't open sidebar)
      focusNode(nodeToFocus.id, { topPadding: 150, selectNode: false, delay: 100 })
      console.log('[WorkflowCanvas] Positioned viewport to show step at top-left:', nodeToFocus.id, 'for stepId:', stepId)
    } else {
      console.warn('[WorkflowCanvas] Could not find node for stepId:', stepId, 'stepIndex:', stepIndex)
    }
    
    if (onStartPhase) {
      // Create execution options to run only this single step
      // Use buildExecutionOptions to include all flags (including fallback_to_original_llm_on_failure)
      const buildExecutionOptions = useWorkflowStore.getState().buildExecutionOptions
      const baseOptions = buildExecutionOptions()
      const executionOptions: ExecutionOptions = {
        ...baseOptions,  // Include all flags from buildExecutionOptions
        execution_strategy: 'run_single_step',
        resume_from_step: stepIndex + 1  // 1-based step number (target step)
      }
      console.log('[RESUME_DEBUG] 🚀 Starting single step execution:', {
        stepIndex: stepIndex + 1,
        execution_strategy: executionOptions.execution_strategy,
        resume_from_step: executionOptions.resume_from_step
      })
      onStartPhase('execution', executionOptions)
    } else {
      console.error('[RESUME_DEBUG] ❌ onStartPhase is not available!')
    }
  }, [onStartPhase, focusNode, selectedRunFolder])

  // Store handleRunFromStep in ref for early access
  React.useEffect(() => {
    handleRunFromStepRef.current = handleRunFromStep
  }, [handleRunFromStep])

  // Expose methods via ref
  useImperativeHandle(ref, () => ({
    refresh: async (changedStepIDs?: string[], deletedStepIDs?: string[]) => {
      console.log('[WorkflowPlanUpdate] refresh() called via ref', { changedStepIDs, deletedStepIDs })
      console.log('[WorkflowPlanUpdate] Current plan state:', { 
        hasPlan: !!plan, 
        stepCount: plan?.steps?.length || 0,
        planSteps: plan?.steps?.map(s => ({ id: s.id, title: s.title })) || []
      })
      
      // Refresh plan to get latest data
      await loadPlanRefresh()
      
      // If granular change data is provided, use it directly
      if (changedStepIDs || deletedStepIDs) {
        console.log('[WorkflowPlanUpdate] Using granular change data from events')
        // The backend combines added and updated into changed_step_ids
        // For now, we'll treat all changedStepIDs as "updated" since the backend combines them
        // The visual highlighting will work correctly (blue ring for updated steps)
        // TODO: Update backend to send separate added_step_ids and updated_step_ids for more accurate highlighting
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
        console.log('[WorkflowPlanUpdate] refresh() completed with granular changes:', changes)
        return changes
      }
      
      // No granular data - just refresh without setting changes
      console.log('[WorkflowPlanUpdate] refresh() completed (no granular changes provided)')
      return null
    },
    getStepCount: () => {
      // Count steps from plan data
      if (!plan?.steps) return 0
      return plan.steps.length
    }
  }), [loadPlanRefresh, plan, setChanges])

  // Store step ID to focus on when changes are detected (will focus after nodes update)
  React.useEffect(() => {
    if (changes?.hasChanges) {
      // Store the step ID to focus on (will be used after nodes are updated)
      const stepToFocus = changes.added?.[0] || changes.updated?.[0]
      if (stepToFocus) {
        pendingFocusStepIdRef.current = stepToFocus
        console.log('[WorkflowCanvas] Stored step ID to focus after nodes update:', stepToFocus)
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

  // Update nodes when plan changes (only if nodes actually changed)
  React.useEffect(() => {
    // Compare by reference first (fast path)
    if (prevNodesRef.current === initialNodes && prevEdgesRef.current === initialEdges) {
      return // No change
    }
    
    // Compare by length, IDs, node data (status), and step configs to detect actual changes
    // This ensures nodes update when completedStepIndices changes (which updates status)
    // and when agent_configs are updated (e.g., when saving config in side panel)
    const nodesChanged = 
      prevNodesRef.current.length !== initialNodes.length ||
      prevNodesRef.current.some((node, i) => {
        const newNode = initialNodes[i]
        if (!newNode) return true
        // Check if ID changed
        if (node?.id !== newNode.id) return true
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
        const oldData = node?.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | undefined
        const newData = newNode?.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | undefined
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
      console.log('[WorkflowPlanUpdate] Nodes changed, updating state', {
        prevCount: prevNodesRef.current.length,
        newCount: initialNodes.length
      })
      
      // Check if we have a selected node - if so, preserve focus on it instead of resetting to start
      const currentSelectedId = selectedNodeIdRef.current
      const hasSelectedNode = currentSelectedId !== null && 
        initialNodes.some(n => n.id === currentSelectedId)
      
      setNodes(initialNodes)
      
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
            if (n.type === 'step' || n.type === 'conditional' || n.type === 'loop' || n.type === 'decision' || n.type === 'orchestrator') {
              const nodeData = n.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
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
            const nodeData = nodeToFocus.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
            console.log('[WorkflowCanvas] Auto-focused on step that was changed by backend:', {
              stepId: stepIdToFocus,
              nodeId: nodeToFocus.id,
              stepTitle: nodeData?.step?.title,
              matchedBy: nodeData?.step?.id === stepIdToFocus ? 'step.id' : 'node.id'
            })
          } else {
            console.warn('[WorkflowCanvas] Could not find node for changed step ID:', stepIdToFocus, {
              availableNodes: initialNodes
                .filter(n => n.type === 'step' || n.type === 'conditional' || n.type === 'loop' || n.type === 'decision' || n.type === 'orchestrator')
                .map(n => {
                  const nodeData = n.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
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
    
    if (edgesChanged) {
      console.log('[WorkflowPlanUpdate] Edges changed, updating state', {
        prevCount: prevEdgesRef.current.length,
        newCount: initialEdges.length
      })
      setEdges(initialEdges)
      prevEdgesRef.current = initialEdges
    }
  }, [initialNodes, initialEdges, setNodes, setEdges, focusNode])

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
    console.log('[WorkflowPlanUpdate] Checking selectedNode update', {
      selectedId,
      nodesLength: nodes.length,
      hasSelectedNode: !!selectedNode
    })
    
    if (!selectedId || nodes.length === 0) {
      console.log('[WorkflowPlanUpdate] No selected node or no nodes, skipping update')
      return
    }

    // Find the corresponding node in the new nodes array by ID
    const updatedNode = nodes.find(n => n.id === selectedId) as WorkflowNode | undefined
    if (!updatedNode) {
      // Selected node no longer exists (was deleted)
      console.log('[WorkflowPlanUpdate] Selected node no longer exists, clearing selection')
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
      console.log('[WorkflowPlanUpdate] Selection changed or cleared, skipping update')
      return
    }

    // Compare step data to see if it changed
    const oldData = currentSelected.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | undefined
    const newData = updatedNode.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | undefined
    const oldStep = oldData?.step
    const newStep = newData?.step
    
    let shouldUpdate = false
    if (oldStep && newStep) {
      // Compare by JSON stringify to detect any changes
      const oldStepStr = JSON.stringify(oldStep)
      const newStepStr = JSON.stringify(newStep)
      if (oldStepStr !== newStepStr) {
        console.log('[WorkflowPlanUpdate] Updating selectedNode with new step data from plan refresh', {
          nodeId: selectedId,
          oldStepKeys: Object.keys(oldStep),
          newStepKeys: Object.keys(newStep),
          agentConfigsChanged: JSON.stringify(oldStep.agent_configs || {}) !== JSON.stringify(newStep.agent_configs || {})
        })
        shouldUpdate = true
      } else {
        console.log('[WorkflowPlanUpdate] Selected node step data unchanged')
      }
    } else if (updatedNode !== currentSelected) {
      // Node structure changed (e.g., type changed)
      console.log('[WorkflowPlanUpdate] Node structure changed, updating selectedNode')
      shouldUpdate = true
    } else {
      console.log('[WorkflowPlanUpdate] Selected node unchanged')
    }
    
    if (shouldUpdate) {
      setSelectedNode(updatedNode)
      // Re-focus on the selected node after update (e.g., after saving step config)
      // This ensures the view stays focused on the same step when the sidebar is closed
      setTimeout(() => {
        focusNode(selectedId, { topPadding: 150, selectNode: false, delay: 0 })
        console.log('[WorkflowPlanUpdate] Re-focused on selected node after update:', selectedId)
      }, 100)
    }
  }, [nodes, selectedNode, focusNode]) // Include focusNode in dependencies

  // Load saved viewport state from localStorage on mount
  const savedViewportRef = React.useRef<{ x: number; y: number; zoom: number } | null>(null)
  React.useEffect(() => {
    try {
      const storageKey = getViewportStorageKey()
      const saved = localStorage.getItem(storageKey)
      if (saved) {
        const parsed = JSON.parse(saved)
        if (parsed && typeof parsed.x === 'number' && typeof parsed.y === 'number' && typeof parsed.zoom === 'number') {
          savedViewportRef.current = { x: parsed.x, y: parsed.y, zoom: parsed.zoom }
          console.log('[WorkflowCanvas] Loaded saved viewport from localStorage:', savedViewportRef.current)
        }
      }
    } catch (error) {
      console.error('[WorkflowCanvas] Failed to load viewport from localStorage:', error)
    }
  }, [getViewportStorageKey])

  // Set initial view to show start node (left side) on first load, or restore saved viewport
  React.useEffect(() => {
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
          console.log('[WorkflowCanvas] Restored saved viewport from localStorage:', savedViewportRef.current)
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
        if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop' || node.type === 'decision') {
          const nodeData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData
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
            } else if (node.type === 'loop') {
              return {
                ...node,
                data: { 
                  ...node.data, 
                  status: stepStatus
                } as LoopNodeData
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
  }, [])

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
  const handleEditStep = useCallback(async (stepId: string, updates: Partial<PlanStep>) => {
    if (!plan) return
    
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
      
      await updateStep(stepIndex, updates)
      
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
  }, [plan, workspacePath, updateStep, highlightStepNode, loadPlanRefresh])

  // Handle delete step
  const handleDeleteStep = useCallback(async (stepId: string) => {
    if (!plan) return
    
    const stepIndex = plan.steps.findIndex(s => s.id === stepId)
    if (stepIndex >= 0) {
      await deleteStep(stepIndex)
      setSelectedNode(null)
    }
  }, [plan, deleteStep])

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


  // Handle fit view
  const handleFitView = useCallback(() => {
    fitView({ padding: 0.2 })
  }, [fitView])

  // Handle toggle dependency edges

  // Loading state
  if (loading) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 border-2 border-gray-400 dark:border-gray-500 border-t-transparent rounded-full animate-spin" />
          <span className="text-sm text-gray-500 dark:text-gray-400">Loading plan...</span>
        </div>
      </div>
    )
  }

  // Error state
  if (error) {
    return (
      <div className={`flex items-center justify-center h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <div className="flex flex-col items-center gap-3 text-center">
          <div className="w-12 h-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <span className="text-2xl">⚠️</span>
          </div>
          <span className="text-sm text-red-600 dark:text-red-400">{error}</span>
          <button
            onClick={loadPlanRefresh}
            className="px-4 py-2 text-sm bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded-lg hover:bg-gray-200 dark:hover:bg-gray-600"
          >
            Retry
          </button>
        </div>
      </div>
    )
  }

  // No plan state
  if (!plan || !plan.steps || plan.steps.length === 0) {
    return (
      <div className={`flex flex-col h-full bg-gray-50 dark:bg-gray-900 ${className}`}>
        <WorkflowToolbar
          status={status}
          hasPlan={false}
          currentPhase={currentPhase}
          workspacePath={workspacePath}
          totalSteps={0}
          presetQueryId={presetQueryId}
          onStartPhase={handleStartPhase}
          onStop={stopWorkflow}
          onCreatePlan={onCreatePlan || (() => {})}
          onZoomIn={zoomIn}
          onZoomOut={zoomOut}
          onFitView={handleFitView}
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
                Create Plan
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
        plan={plan}
        currentPhase={currentPhase}
        workspacePath={workspacePath}
        totalSteps={totalSteps}
        presetQueryId={presetQueryId}
        onStartPhase={handleStartPhase}
        onStop={stopWorkflow}
        onBulkUpdateSteps={handleBulkUpdateSteps}
        onCreatePlan={onCreatePlan || (() => {})}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onFitView={handleFitView}
        showChatArea={showChatArea}
        onToggleChatArea={onToggleChatArea}
        onRefresh={handleRefresh}
      />

      {/* React Flow Canvas with Sidebar */}
      <div className="flex-1 relative flex">
        <div className={`flex-1 transition-all duration-300 ${
          selectedNode 
            ? (showChatArea ? 'mr-[50vw]' : (isStepSidebarCompact ? 'mr-[400px]' : 'mr-[600px]'))
            : showVariablesSidebar 
              ? 'mr-[450px]' 
              : ''
        }`}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          onViewportChange={(viewport) => {
            // Track viewport state to preserve it during refresh
            const viewportState = {
              x: viewport.x,
              y: viewport.y,
              zoom: viewport.zoom
            }
            viewportStateRef.current = viewportState
            
            // Save to localStorage (only after initial view has been set)
            if (hasInitializedView.current) {
              try {
                const storageKey = getViewportStorageKey()
                localStorage.setItem(storageKey, JSON.stringify(viewportState))
              } catch (error) {
                console.error('[WorkflowCanvas] Failed to save viewport to localStorage:', error)
              }
            }
          }}
          nodeTypes={nodeTypes}
          fitView={false}
          fitViewOptions={{ padding: 0.1, minZoom: 1.0, maxZoom: 1.5 }}
          minZoom={0.1}
          maxZoom={2}
          defaultViewport={{ x: 100, y: 0, zoom: 0.9 }}
          attributionPosition="bottom-right"
          className="bg-gray-50 dark:bg-gray-900"
        >
          <Background 
            variant={BackgroundVariant.Dots} 
            gap={20} 
            size={1} 
            color="#e5e7eb"
            className="dark:!bg-gray-900"
          />
        </ReactFlow>

        {/* Step Legend - Bottom Left */}
        {plan && plan.steps && plan.steps.length > 0 && (
          <StepLegend
            plan={plan}
            nodes={nodes}
            selectedNodeId={selectedNode?.id || null}
            onStepClick={handleNavigateToStep}
            workspacePath={workspacePath}
          />
        )}
        </div>

        {/* Step Sidebar */}
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
            completedStepIndices={completedStepIndices}
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
