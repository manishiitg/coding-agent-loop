import React, { useCallback, useRef, useImperativeHandle, forwardRef } from 'react'
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
import { usePlanToFlow, type WorkflowNode, type WorkflowEdge, type StepNodeData, type ConditionalNodeData, type LoopNodeData } from '../hooks/usePlanToFlow'
import type { VariablesNodeData } from '../nodes/VariablesNode'
import { useWorkflowExecution } from '../hooks/useWorkflowExecution'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { agentApi } from '../../../services/api'
import type { PlanStep } from '../../../utils/stepConfigMatching'
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
  refresh: () => Promise<PlanChanges | null>
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

  // Track completed step indices from selected iteration (for enabling/disabling run buttons)
  const [completedStepIndices, setCompletedStepIndices] = React.useState<number[]>([])
  
  // Variables state
  const [variablesManifest, setVariablesManifest] = React.useState<VariablesManifest | null>(null)
  const [isLoadingVariables, setIsLoadingVariables] = React.useState(false)
  const [showVariablesSidebar, setShowVariablesSidebar] = React.useState(false)
  
  // Workflow store actions
  const setVariablesManifestInStore = useWorkflowStore.getState().setVariablesManifest
  
  // Callback for when progress changes in toolbar
  const handleProgressChange = useCallback((indices: number[]) => {
    setCompletedStepIndices(indices)
  }, [])

  // Load plan data with change detection
  const { plan, loading, error, changes, updateStep, deleteStep, refresh, clearChanges } = usePlanData(workspacePath)

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

  // Workflow execution
  const {
    status,
    stepStatusMap,
    stopWorkflow
  } = useWorkflowExecution()
  
  const isExecuting = status === 'running'

  // Refs for callbacks that need to be defined early
  const handleRunFromStepRef = React.useRef<((stepIndex: number, stepId: string) => void) | null>(null)
  const handleOpenSidebarRef = React.useRef<((nodeId: string) => void) | null>(null)

  // Get selected run folder from workflow store
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  
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
        if (selectNode) {
          const node = nodesRef.current.find(n => n.id === nodeId) as WorkflowNode | undefined
          if (node) {
            setSelectedNode(node)
          }
        }
      }
    }, delay)
  }, [getNode, setViewport])

  // Handle opening sidebar for a node
  const handleOpenSidebar = useCallback((nodeId: string) => {
    setShowVariablesSidebar(false) // Close variables sidebar if open
    focusNode(nodeId, { topPadding: 150, selectNode: true, delay: 100 })
    console.log('[WorkflowCanvas] Opened sidebar and positioned viewport for node:', nodeId)
  }, [focusNode])

  // Handle navigating to a step from legend (without opening sidebar)
  const handleNavigateToStep = useCallback((nodeId: string) => {
    focusNode(nodeId, { topPadding: 150, selectNode: false, delay: 100 })
    console.log('[WorkflowCanvas] Navigated to step from legend:', nodeId)
  }, [focusNode])

  // Store handleOpenSidebar in ref for early access
  React.useEffect(() => {
    handleOpenSidebarRef.current = handleOpenSidebar
  }, [handleOpenSidebar])

  // Convert plan to React Flow nodes and edges (with change highlights and run callback)
  const { nodes: initialNodes, edges: initialEdges } = usePlanToFlow(plan, { 
    // Prerequisite edges are always shown (default: true in usePlanToFlow)
    changes,  // Pass changes to highlight modified nodes
    onRunFromStep: (stepIndex: number, stepId: string) => {
      // Call the ref function if it's available
      if (handleRunFromStepRef.current) {
        handleRunFromStepRef.current(stepIndex, stepId)
      }
    },
    onOpenSidebar: (nodeId: string) => {
      // Call the ref function if it's available
      if (handleOpenSidebarRef.current) {
        handleOpenSidebarRef.current(nodeId)
      }
    },
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
    focusNode(stepId, { topPadding: 50, selectNode: true, delay: 100 })
    console.log('[WorkflowCanvas] Highlighted step node:', stepId)
  }, [focusNode])

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
      if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop') {
        const nodeData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
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
      console.log('[WorkflowCanvas] Calling onStartPhase with execution options:', executionOptions)
      onStartPhase('execution', executionOptions)
    } else {
      console.error('[WorkflowCanvas] onStartPhase is not available!')
    }
  }, [onStartPhase, focusNode, selectedRunFolder])

  // Store handleRunFromStep in ref for early access
  React.useEffect(() => {
    handleRunFromStepRef.current = handleRunFromStep
  }, [handleRunFromStep])

  // Expose methods via ref
  useImperativeHandle(ref, () => ({
    refresh: async () => {
      console.log('[WorkflowPlanUpdate] refresh() called via ref')
      console.log('[WorkflowPlanUpdate] Current plan state:', { 
        hasPlan: !!plan, 
        stepCount: plan?.steps?.length || 0,
        planSteps: plan?.steps?.map(s => ({ id: s.id, title: s.title })) || []
      })
      const detectedChanges = await refresh()
      console.log('[WorkflowPlanUpdate] refresh() completed, changes:', detectedChanges)
      console.log('[WorkflowPlanUpdate] Plan state after refresh:', { 
        hasPlan: !!plan, 
        stepCount: plan?.steps?.length || 0 
      })
      return detectedChanges
    },
    getStepCount: () => {
      // Count steps from plan data
      if (!plan?.steps) return 0
      return plan.steps.length
    }
  }), [refresh, plan])

  // Clear highlights after timeout and auto-focus on changed steps
  React.useEffect(() => {
    if (changes?.hasChanges) {
      // Auto-focus on first added or updated step when plan/step config is updated
      const stepToFocus = changes.added?.[0] || changes.updated?.[0]
      if (stepToFocus) {
        // Find the node for this step
        const node = nodesRef.current.find(n => {
          if (n.type === 'step' || n.type === 'conditional' || n.type === 'loop') {
            const nodeData = n.data as StepNodeData | ConditionalNodeData | LoopNodeData
            const nodeStepId = nodeData?.step?.id || n.id
            return nodeStepId === stepToFocus
          }
          return false
        })
        
        if (node) {
          // Auto-focus on the changed step (position viewport, but don't open sidebar)
          focusNode(node.id, { topPadding: 150, selectNode: false, delay: 300 })
          console.log('[WorkflowCanvas] Auto-focused on step that was added/updated:', stepToFocus, node.id)
        }
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
  }, [changes, clearChanges, focusNode])

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
        const oldData = node?.data as StepNodeData | ConditionalNodeData | LoopNodeData | undefined
        const newData = newNode?.data as StepNodeData | ConditionalNodeData | LoopNodeData | undefined
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
      setNodes(initialNodes)
      // Reset view initialization flag when nodes actually change
      hasInitializedView.current = false
      prevNodesRef.current = initialNodes
    }
    
    if (edgesChanged) {
      console.log('[WorkflowPlanUpdate] Edges changed, updating state', {
        prevCount: prevEdgesRef.current.length,
        newCount: initialEdges.length
      })
      setEdges(initialEdges)
      prevEdgesRef.current = initialEdges
    }
  }, [initialNodes, initialEdges, setNodes, setEdges])

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
    const oldData = currentSelected.data as StepNodeData | ConditionalNodeData | LoopNodeData | undefined
    const newData = updatedNode.data as StepNodeData | ConditionalNodeData | LoopNodeData | undefined
    const oldStep = oldData?.step
    const newStep = newData?.step
    
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
        setSelectedNode(updatedNode)
      } else {
        console.log('[WorkflowPlanUpdate] Selected node step data unchanged')
      }
    } else if (updatedNode !== currentSelected) {
      // Node structure changed (e.g., type changed)
      console.log('[WorkflowPlanUpdate] Node structure changed, updating selectedNode')
      setSelectedNode(updatedNode)
    } else {
      console.log('[WorkflowPlanUpdate] Selected node unchanged')
    }
  }, [nodes, selectedNode]) // Include selectedNode to compare, but logic prevents loops

  // Set initial view to show start node (left side) on first load
  React.useEffect(() => {
    if (!hasInitializedView.current && nodes.length > 0) {
      // Find the start node
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
            const viewportY = (canvasHeight / 2) - flowNode.position.y - ((flowNode.height || 100) / 2)
            setViewport({ x: viewportX, y: viewportY, zoom: 0.9 })
            hasInitializedView.current = true
            // Log removed to reduce console noise
          } else {
            // Fallback: use node position directly with simple calculation
            const padding = 150
            const canvasHeight = window.innerHeight || 800
            const viewportX = padding - startNode.position.x
            const viewportY = (canvasHeight / 2) - startNode.position.y
            setViewport({ x: viewportX, y: viewportY, zoom: 0.9 })
            hasInitializedView.current = true
            // Log removed to reduce console noise
          }
        }, 150) // Slightly longer timeout to ensure layout is complete
      }
    }
  }, [nodes, setViewport, getNode])

  // Track previous status map to detect status changes
  const prevStepStatusMapRef = React.useRef<Map<string, 'pending' | 'running' | 'completed' | 'failed'>>(new Map())

  // Update node status based on step status map from events
  React.useEffect(() => {
    if (stepStatusMap.size > 0) {
      const prevStatusMap = prevStepStatusMapRef.current
      
      setNodes(nds => 
        nds.map(node => {
          // Only update status for step-type nodes (step, conditional, loop)
          // Validation and learning nodes have different status types
          if (node.type === 'step' || node.type === 'conditional' || node.type === 'loop') {
            const nodeData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
            const stepId = nodeData?.step?.id || node.id
            const stepStatus = stepStatusMap.get(stepId)
            
            if (stepStatus) {
              // Detect if step just transitioned to 'running' (was not 'running' before)
              const prevStatus = prevStatusMap.get(stepId)
              if (stepStatus === 'running' && prevStatus !== 'running') {
                // Auto-focus on the node when it starts running (position viewport, but don't open sidebar)
                // This happens when the running label and loader are added to the node
                focusNode(node.id, { topPadding: 150, selectNode: false, delay: 200 })
                console.log('[WorkflowCanvas] Auto-focused on step that started running:', stepId, node.id)
              }
              
              if (node.type === 'step') {
                return {
                  ...node,
                  data: { ...node.data, status: stepStatus } as StepNodeData
                } as WorkflowNode
              } else if (node.type === 'conditional') {
                return {
                  ...node,
                  data: { ...node.data, status: stepStatus } as ConditionalNodeData
                } as WorkflowNode
              } else if (node.type === 'loop') {
                return {
                  ...node,
                  data: { ...node.data, status: stepStatus } as LoopNodeData
                } as WorkflowNode
              }
            }
          }
          return node
        })
      )
      
      // Update previous status map (for tracking changes)
      prevStepStatusMapRef.current = new Map(stepStatusMap)
    }
  }, [stepStatusMap, setNodes, nodes.length, focusNode])

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
    
    const stepIndex = plan.steps.findIndex(s => s.id === stepId)
    if (stepIndex >= 0) {
      await updateStep(stepIndex, updates)
      
      // Highlight the step node after saving config
      highlightStepNode(stepId)
    }
  }, [plan, updateStep, highlightStepNode])

  // Handle delete step
  const handleDeleteStep = useCallback(async (stepId: string) => {
    if (!plan) return
    
    const stepIndex = plan.steps.findIndex(s => s.id === stepId)
    if (stepIndex >= 0) {
      await deleteStep(stepIndex)
      setSelectedNode(null)
    }
  }, [plan, deleteStep])


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
            onClick={refresh}
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
          onProgressChange={handleProgressChange}
          showChatArea={showChatArea}
          onToggleChatArea={onToggleChatArea}
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
        currentPhase={currentPhase}
        workspacePath={workspacePath}
        totalSteps={totalSteps}
        presetQueryId={presetQueryId}
        onStartPhase={handleStartPhase}
        onStop={stopWorkflow}
        onCreatePlan={onCreatePlan || (() => {})}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onFitView={handleFitView}
        onProgressChange={handleProgressChange}
        showChatArea={showChatArea}
        onToggleChatArea={onToggleChatArea}
      />

      {/* React Flow Canvas with Sidebar */}
      <div className="flex-1 relative flex">
        <div className={`flex-1 transition-all duration-300 ${selectedNode ? 'mr-[600px]' : showVariablesSidebar ? 'mr-[450px]' : ''}`}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
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
          />
        )}

        {/* Variables Sidebar */}
        {showVariablesSidebar && (
          <VariablesSidebar
            workspacePath={workspacePath}
            onClose={() => setShowVariablesSidebar(false)}
            onUpdate={handleVariablesUpdate}
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
