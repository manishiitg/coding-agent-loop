import React, { useState, useMemo, useEffect, useRef, useCallback } from 'react'
import { X, Trash2, Edit2, Save, ChevronDown, Settings, CheckCircle2, BookOpen, Play, Eye, MoreVertical } from 'lucide-react'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import type { ExecutionOptions } from '../../../services/api-types'
import { ExecutionStrategy } from '../../../services/api-types'
import { StepEditPanel } from '../../events/orchestrator/StepEditPanel'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useAppStore } from '../../../stores/useAppStore'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { agentApi } from '../../../services/api'
import LLMSelectionDropdown from '../../LLMSelectionDropdown'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'
import type { LLMOption } from '../../../types/llm'
import type { AgentLLMConfig } from '../../../utils/stepConfigMatching'
import type { 
  WorkflowNode, 
  StepNodeData, 
  ConditionalNodeData, 
  DecisionNodeData,
  LoopNodeData,
  OrchestratorNodeData,
  ValidationNodeData,
  LearningNodeData
} from '../hooks/usePlanToFlow'
import type { PlanStep, PlanningResponse, AgentConfigs, ValidationSchema } from '../../../utils/stepConfigMatching'
import type { TodoStepWithConfigs } from '../../../utils/stepConfigMatching'
import { isRegularStep, isConditionalStep, isDecisionStep, isOrchestrationStep, isHumanInputStep } from '../../../utils/stepConfigMatching'

interface StepSidebarProps {
  node: WorkflowNode | null
  onClose: () => void
  onEditStep: (stepId: string, updates: Partial<PlanStep>) => Promise<void>
  onDeleteStep: (stepId: string) => void
  isRunning: boolean
  stepIndex: number
  workspacePath?: string | null
  presetQueryId?: string | null
  onStartPhase?: (phaseId: string, stepId?: string | ExecutionOptions) => void
  plan?: PlanningResponse | null
  completedStepIndices?: number[]  // Deprecated: no longer used (all steps can run)
  isCompact?: boolean  // When true, use narrower width (400px instead of 600px)
  showChatArea?: boolean  // When true, use lower z-index so ChatArea appears on top
}

export const StepSidebar: React.FC<StepSidebarProps> = ({
  node,
  onClose,
  onEditStep,
  onDeleteStep,
  isRunning,
  stepIndex,
  workspacePath,
  presetQueryId,
  onStartPhase,
  plan,
  isCompact = false,
  showChatArea = false
}) => {
  const { availableLLMs } = useLLMStore()
  
  // Get step-specific phases from workflow store (already filtered)
  const { getStepSpecificPhases, loadPhases } = useWorkflowStore()
  const phases = getStepSpecificPhases()
  
  // Get execution options directly from workflow store (single source of truth)
  // This fixes the issue where globalExecutionOptions prop might be null/stale
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const selectedExecutionMode = useWorkflowStore(state => state.selectedExecutionMode)
  
  // Workspace store for viewing learnings
  const { setWorkspaceMinimized } = useAppStore()
  const { setSearchQuery, fetchFiles, setExpandedFolders, highlightFile, expandedFolders } = useWorkspaceStore()
  
  const [isPhaseDropdownOpen, setIsPhaseDropdownOpen] = useState(false)
  const phaseDropdownRef = useRef<HTMLDivElement>(null)
  const [isActionsDropdownOpen, setIsActionsDropdownOpen] = useState(false)
  const actionsDropdownRef = useRef<HTMLDivElement>(null)
  const [isSaving, setIsSaving] = useState(false)
  const [isEditing, setIsEditing] = useState(false)
  const [editedTitle, setEditedTitle] = useState('')
  const [editedDescription, setEditedDescription] = useState('')
  const [editedSuccessCriteria, setEditedSuccessCriteria] = useState('')
  const [editedMaxIterations, setEditedMaxIterations] = useState<string>('')
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showDeleteLearningsConfirm, setShowDeleteLearningsConfirm] = useState(false)
  const [isDeletingLearnings, setIsDeletingLearnings] = useState(false)

  // Ensure phases are loaded (store handles deduplication)
  useEffect(() => {
    loadPhases()
  }, [loadPhases])


  // Close dropdowns when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (phaseDropdownRef.current && !phaseDropdownRef.current.contains(event.target as Node)) {
        setIsPhaseDropdownOpen(false)
      }
      if (actionsDropdownRef.current && !actionsDropdownRef.current.contains(event.target as Node)) {
        setIsActionsDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])


  // Check if evaluation step
  const isEvaluationStep = useMemo(() => {
    return (node?.data as any)?.isEvaluationStep === true
  }, [node])

  // Handle phase selection
  const handleSelectPhase = async (phaseId: string) => {
    if (!presetQueryId || !node) return
    
    setIsPhaseDropdownOpen(false)
    
    try {
      // Update workflow status with step ID
      await agentApi.updateWorkflow(presetQueryId, phaseId, null, node.id)
      
      // Start the phase through the parent component
      if (onStartPhase) {
        // For plan-improvement phase, pass execution options with selected_run_folder
        // Other step-specific phases (plan-tool-optimization) can also benefit from this
        if (phaseId === 'plan-improvement' || phaseId === 'plan-tool-optimization') {
          // Build execution options to include selected_run_folder
          const buildExecutionOptions = useWorkflowStore.getState().buildExecutionOptions
          const executionOptions = buildExecutionOptions()
          console.log('[StepSidebar] Starting', phaseId, 'with execution options:', executionOptions)
          onStartPhase(phaseId, executionOptions)
        } else {
          // For other phases, pass stepId as before
          onStartPhase(phaseId, node.id)
        }
      }
    } catch (error) {
      console.error('[StepSidebar] Failed to start phase:', error)
    }
  }

  // Handle run single step
  // Uses workflow store directly for execution options (single source of truth)
  const handleRunStep = useCallback(() => {
    if (!onStartPhase || !node) return
    
    // Sub-agents cannot be run independently (they are part of routing steps)
    if (node.id.includes('-sub-agent-')) {
      console.warn('[StepSidebar] Cannot run sub-agent independently:', node.id)
      return
    }
    
    // Create execution options to run only this single step
    // Read from workflow store directly (not from props which might be null/stale)
    // Use buildExecutionOptions to include all flags (including fallback_to_original_llm_on_failure)
    const buildExecutionOptions = useWorkflowStore.getState().buildExecutionOptions
    const baseOptions = buildExecutionOptions()
    const executionOptions: ExecutionOptions = {
      ...baseOptions,  // Include all flags from buildExecutionOptions
      execution_strategy: ExecutionStrategy.RUN_SINGLE_STEP,
      resume_from_step: stepIndex + 1  // 1-based step number (target step)
    }
    
    console.log('[StepSidebar] Running single step with options:', {
      selectedRunFolder,
      selectedExecutionMode,
      executionOptions
    })
    
    // Start execution phase with single step options
    // The adapter in WorkflowCanvas will handle highlighting the node
    onStartPhase('execution', executionOptions)
  }, [onStartPhase, node, selectedRunFolder, selectedExecutionMode, stepIndex])

  // Handle delete learnings for this step
  const handleDeleteLearnings = useCallback(async () => {
    if (!workspacePath) {
      console.error('[StepSidebar] Cannot delete learnings: workspace path not available')
      return
    }

    // Get step ID
    let stepId: string | undefined
    if (node?.type === 'orchestrator') {
      const orchestratorData = node.data as OrchestratorNodeData
      stepId = orchestratorData.orchestration_step?.id ?? orchestratorData.step?.id
    } else if (node?.data) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData
      stepId = stepData.step?.id
    }

    if (!stepId) {
      console.error('[StepSidebar] Cannot delete learnings: step ID not available')
      return
    }

    setIsDeletingLearnings(true)
    try {
      const result = await agentApi.deleteStepLearnings(workspacePath, stepId)
      
      if (result.success) {
        console.log('[StepSidebar] Successfully deleted learnings:', result.message)
        // Optionally show a success toast/notification here
      } else {
        console.error('[StepSidebar] Failed to delete learnings:', result.message)
        // Optionally show an error toast/notification here
      }
    } catch (error) {
      console.error('[StepSidebar] Error deleting learnings:', error)
      // Optionally show an error toast/notification here
    } finally {
      setIsDeletingLearnings(false)
      setShowDeleteLearningsConfirm(false)
    }
  }, [workspacePath, node])

  // Handle view learnings for this step
  const handleViewLearnings = useCallback(async () => {
    if (!workspacePath || !node) {
      console.error('[StepSidebar] Cannot view learnings: workspace path or node not available')
      return
    }

    // Get step ID from node data
    // For orchestration steps, use orchestration_step.ID (backend uses orchestration_step.ID for learnings)
    // For other steps, use step.ID
    const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
    let stepId: string | undefined
    
    if (node.type === 'orchestrator') {
      const orchestratorData = stepData as OrchestratorNodeData
      stepId = orchestratorData.orchestration_step?.id ?? orchestratorData.step?.id
    } else {
      stepId = stepData.step?.id
    }
    
    if (!stepId) {
      console.error('[StepSidebar] Cannot view learnings: step ID not available')
      return
    }

    // Construct learnings folder path: learnings/{step_id}
    const learningsPath = `learnings/${stepId}`
    const learningsRootPath = 'learnings'
    
    // Open workspace
    setWorkspaceMinimized(false)
    
    // Small delay to ensure workspace is expanded before navigating
    setTimeout(async () => {
      // Refresh files to ensure workspace has latest data
      await fetchFiles()
      
      // Get updated files after refresh
      const storeState = useWorkspaceStore.getState()
      const updatedFiles = storeState.files
      
      // Helper function to find a folder in the file tree
      const findFolderInTree = (fileList: typeof updatedFiles, targetPath: string): typeof updatedFiles[0] | null => {
        for (const file of fileList) {
          // Check if this is the folder we're looking for
          const originalPath = 'originalFilepath' in file ? file.originalFilepath : undefined
          if ((file.filepath === targetPath || originalPath === targetPath) && 
              file.type === 'folder') {
            return file
          }
          // Also check if targetPath ends with this folder's path (for nested paths)
          if (file.filepath && (targetPath.endsWith(file.filepath) || file.filepath.endsWith(targetPath))) {
            if (file.type === 'folder') {
              return file
            }
          }
          // Recurse into children
          if (file.children && file.children.length > 0) {
            const found = findFolderInTree(file.children, targetPath)
            if (found) return found
          }
        }
        return null
      }
      
      // Get current expanded folders
      const currentExpanded = new Set(expandedFolders)
      
      // Remove all folders that are inside learnings/ (but keep learnings/ itself)
      const newExpanded = new Set<string>()
      for (const folderPath of currentExpanded) {
        // Keep learnings/ root folder if it's expanded
        if (folderPath === learningsRootPath || folderPath === `/${learningsRootPath}`) {
          newExpanded.add(folderPath)
        }
        // Don't add any other learnings subfolders - we'll add only the step-specific one
        if (!folderPath.startsWith(learningsRootPath + '/') && 
            !folderPath.startsWith('/' + learningsRootPath + '/')) {
          // Keep folders outside learnings/
          newExpanded.add(folderPath)
        }
      }
      
      // Find the learnings root folder and the step-specific folder
      const learningsRoot = findFolderInTree(updatedFiles, learningsRootPath)
      const folder = findFolderInTree(updatedFiles, learningsPath)
      
      // Add learnings/ root folder if it exists
      if (learningsRoot) {
        const rootOriginalPath = 'originalFilepath' in learningsRoot ? learningsRoot.originalFilepath : undefined
        const rootPath = rootOriginalPath || learningsRoot.filepath
        newExpanded.add(rootPath)
      }
      
      // Add the step-specific learnings folder and its parent paths
      if (folder) {
        const folderOriginalPath = 'originalFilepath' in folder ? folder.originalFilepath : undefined
        const pathToExpand = folderOriginalPath || folder.filepath
        
        // Expand all parent folders needed to show this folder
        const pathParts = pathToExpand.split('/').filter(p => p)
        for (let i = 1; i <= pathParts.length; i++) {
          const parentPath = pathParts.slice(0, i).join('/')
          newExpanded.add(parentPath)
        }
        
        // Set expanded folders (collapsed all learnings subfolders, expanded only step-specific one)
        setExpandedFolders(newExpanded)
        
        // Highlight the folder (this will also trigger auto-scroll)
        const pathToHighlight = folderOriginalPath || folder.filepath
        console.log('[StepSidebar] Highlighting learnings folder:', pathToHighlight)
        highlightFile(pathToHighlight)
      } else {
        // Folder not found, try to expand using path directly
        console.log('[StepSidebar] Folder not found, trying direct path:', learningsPath)
        const pathParts = learningsPath.split('/').filter(p => p)
        for (let i = 1; i <= pathParts.length; i++) {
          const parentPath = pathParts.slice(0, i).join('/')
          newExpanded.add(parentPath)
        }
        setExpandedFolders(newExpanded)
        highlightFile(learningsPath)
      }
      
      // Clear search query to show all files
      setSearchQuery('')
    }, 200)
  }, [workspacePath, node, setWorkspaceMinimized, setSearchQuery, fetchFiles, setExpandedFolders, highlightFile, expandedFolders])
  
  // Get preset information
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const customPresets = useGlobalPresetStore(state => state.customPresets)
  const predefinedPresets = useGlobalPresetStore(state => state.predefinedPresets)

  // Get active preset
  const activePreset = useMemo(() => {
    if (activePresetId) {
      const customPreset = customPresets.find(p => p.id === activePresetId)
      if (customPreset) return customPreset
      
      const predefinedPreset = predefinedPresets.find(p => p.id === activePresetId)
      if (predefinedPreset) return predefinedPreset
    }
    return null
  }, [activePresetId, customPresets, predefinedPresets])

  // Get preset servers
  const presetServers = useMemo(() => {
    if (activePreset?.selectedServers) {
      return activePreset.selectedServers
    }
    return []
  }, [activePreset])

  // Get preset LLM config
  const presetLLMConfig = useMemo(() => {
    if (activePreset?.llmConfig) {
      return activePreset.llmConfig
    }
    return null
  }, [activePreset])

  // Get preset code execution mode
  const presetUseCodeExecutionMode = useMemo(() => {
    return activePreset?.useCodeExecutionMode ?? false
  }, [activePreset])

  // Helper function to convert PlanStep to TodoStepWithConfigs
  // Defined before useMemo to avoid "Cannot access before initialization" error
  const convertPlanStepToTodoStep = useCallback((planStep: PlanStep): TodoStepWithConfigs => {
    const base: TodoStepWithConfigs = {
      id: planStep.id,
      title: planStep.title,
      description: planStep.description,
      success_criteria: planStep.success_criteria,
      context_dependencies: planStep.context_dependencies,
      context_output: Array.isArray(planStep.context_output) 
        ? planStep.context_output.join(', ') 
        : planStep.context_output,
      agent_configs: planStep.agent_configs,
      validation_schema: planStep.validation_schema
    }

    if (isRegularStep(planStep)) {
      return {
        ...base,
        has_loop: planStep.has_loop,
        loop_condition: planStep.loop_condition,
        max_iterations: planStep.max_iterations,
        loop_description: planStep.loop_description,
      }
    }

    if (isConditionalStep(planStep)) {
      return {
        ...base,
        has_condition: true,
        condition_question: planStep.condition_question,
        condition_context: planStep.condition_context,
        condition_result: planStep.condition_result,
        condition_reason: planStep.condition_reason,
        if_true_steps: planStep.if_true_steps?.map(convertPlanStepToTodoStep),
        if_false_steps: planStep.if_false_steps?.map(convertPlanStepToTodoStep),
        if_true_next_step_id: planStep.if_true_next_step_id,
        if_false_next_step_id: planStep.if_false_next_step_id,
      }
    }

    if (isDecisionStep(planStep)) {
      return {
        ...base,
        has_decision_step: true,
        decision_step: planStep.decision_step ? convertPlanStepToTodoStep(planStep.decision_step) : undefined,
        decision_evaluation_question: planStep.decision_evaluation_question,
        if_true_next_step_id: planStep.if_true_next_step_id,
        if_false_next_step_id: planStep.if_false_next_step_id,
      }
    }

    if (isOrchestrationStep(planStep)) {
      return {
        ...base,
        has_orchestration_step: true,
        orchestration_step: planStep.orchestration_step ? convertPlanStepToTodoStep(planStep.orchestration_step) : undefined,
        orchestration_routes: planStep.orchestration_routes?.map(route => ({
          ...route,
          sub_agent_step: convertPlanStepToTodoStep(route.sub_agent_step)
        })),
        next_step_id: planStep.next_step_id,
      } as TodoStepWithConfigs
    }

    if (isHumanInputStep(planStep)) {
      return {
        ...base,
        has_human_input: true,
        question: planStep.question,
        variable_name: planStep.variable_name,
        response_type: planStep.response_type,
        options: planStep.options,
        next_step_id: planStep.next_step_id,
        if_yes_next_step_id: planStep.if_yes_next_step_id,
        if_no_next_step_id: planStep.if_no_next_step_id,
        option_routes: planStep.option_routes,
      } as TodoStepWithConfigs
    }

    return base
  }, [])

  // Convert PlanStep to TodoStepWithConfigs
  const stepWithConfigs: TodoStepWithConfigs | null = useMemo(() => {
    if (!node) return null
    
    // Validation/learning nodes don't have step data
    if (node.type === 'validation' || node.type === 'learning') return null
    
    // Check if step exists (for step/conditional/loop/decision/orchestrator nodes)
    // Sub-agents are type 'step', so they should be handled here
    const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
    if (!stepData || !stepData.step) {
      return null
    }

    const planStep = stepData.step
    
    // Convert PlanStep to TodoStepWithConfigs using the helper function
    return convertPlanStepToTodoStep(planStep)
  }, [node, convertPlanStepToTodoStep]) // node dependency ensures updates when step data changes

  // Initialize edit fields when node changes or edit mode is enabled
  React.useEffect(() => {
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
        // Initialize max_iterations for loop steps (always initialize for loop nodes)
        if ((isRegularStep(step) && step.has_loop) || node.type === 'loop') {
          setEditedMaxIterations((isRegularStep(step) ? step.max_iterations : undefined)?.toString() || '10')
        } else {
          setEditedMaxIterations('')
        }
      }
    }
  }, [node, isEditing])

  // Handle start edit
  const handleStartEdit = () => {
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
        // Initialize max_iterations for loop steps
        if (isRegularStep(step) && step.has_loop) {
          setEditedMaxIterations(step.max_iterations?.toString() || '10')
        } else {
          setEditedMaxIterations('')
        }
        setIsEditing(true)
      }
    }
  }

  // Handle save edit (for basic fields)
  const handleSaveEdit = async () => {
    if (!node) return

    setIsSaving(true)
    try {
      const updates: Partial<PlanStep> = {
        title: editedTitle,
        description: editedDescription,
        success_criteria: editedSuccessCriteria
      }
      
      // Include max_iterations for loop steps
      if (node.type === 'loop') {
        const stepData = node.data as LoopNodeData
        if (stepData.step && isRegularStep(stepData.step) && stepData.step.has_loop) {
          const maxIterations = parseInt(editedMaxIterations, 10)
          if (!isNaN(maxIterations) && maxIterations > 0) {
            (updates as Partial<import('../../../utils/stepConfigMatching').RegularPlanStep>).max_iterations = maxIterations
          } else if (editedMaxIterations.trim() === '') {
            // If empty, use default of 10
            (updates as Partial<import('../../../utils/stepConfigMatching').RegularPlanStep>).max_iterations = 10
          }
        }
      }
      
      await onEditStep(node.id, updates)
      setIsEditing(false)
      onClose()
    } catch (error) {
      console.error('[StepSidebar] Error saving step edit:', error)
    } finally {
      setIsSaving(false)
    }
  }

  // Handle cancel edit
  const handleCancelEdit = () => {
    setIsEditing(false)
    // Reset to original values
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
        // Reset max_iterations for loop steps
        if (isRegularStep(step) && step.has_loop) {
          setEditedMaxIterations(step.max_iterations?.toString() || '10')
        } else {
          setEditedMaxIterations('')
        }
      }
    }
  }

  // Handle save from StepEditPanel
  const handleSave = async (updatedStep: TodoStepWithConfigs) => {
    if (!node) return

    setIsSaving(true)
    try {
      // Get the actual step ID from step data (not node.id which is React Flow node ID)
      const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData
      const stepId = stepData.step?.id
      
      if (!stepId) {
        console.error('[StepSidebar] Cannot save step config: step ID is missing')
        throw new Error('Step ID is required to save configuration')
      }

      // Log detailed information for debugging
      const stepDataForLogging = stepData.step
      console.log('[StepSidebar] Saving step config:', {
        stepId,
        nodeId: node.id,
        nodeType: node.type,
        stepTitle: updatedStep.title,
        stepHasCondition: stepDataForLogging ? isConditionalStep(stepDataForLogging) : false,
        stepHasLoop: stepDataForLogging ? (isRegularStep(stepDataForLogging) && stepDataForLogging.has_loop) : false,
        stepIndex: stepData.stepIndex,
        hasAgentConfigs: !!updatedStep.agent_configs,
        agentConfigsKeys: updatedStep.agent_configs ? Object.keys(updatedStep.agent_configs) : [],
        // Log the actual step object to verify it's the right one
        stepObjectId: stepDataForLogging?.id,
        stepObjectTitle: stepDataForLogging?.title
      })
      
      // Verify step ID matches
      if (stepDataForLogging?.id !== stepId) {
        console.error('[StepSidebar] Step ID mismatch!', {
          stepIdFromData: stepDataForLogging?.id,
          stepIdBeingUsed: stepId,
          nodeId: node.id,
          nodeType: node.type
        })
        throw new Error(`Step ID mismatch: step data has ID "${stepDataForLogging?.id}" but using "${stepId}"`)
      }
      
      // Deep clone agent_configs only to break any potential reference sharing
      // (The StepEditPanel should already create new objects, but this is a safety measure)
      const agentConfigs = updatedStep.agent_configs 
        ? JSON.parse(JSON.stringify(updatedStep.agent_configs))
        : undefined

      // Convert TodoStepWithConfigs back to PlanStep format for updateStep
      const updates: Partial<PlanStep> = {
        title: updatedStep.title,
        description: updatedStep.description,
        success_criteria: updatedStep.success_criteria,
        context_dependencies: updatedStep.context_dependencies,
        context_output: updatedStep.context_output,
        has_loop: updatedStep.has_loop,
        loop_condition: updatedStep.loop_condition,
        max_iterations: updatedStep.max_iterations,
        loop_description: updatedStep.loop_description,
        has_condition: updatedStep.has_condition,
        condition_question: updatedStep.condition_question,
        condition_context: updatedStep.condition_context,
        condition_result: updatedStep.condition_result,
        condition_reason: updatedStep.condition_reason,
        // Include agent_configs in the update (deep cloned to avoid reference sharing)
        agent_configs: agentConfigs
      } as Partial<PlanStep>

      // Save to parent step
      await onEditStep(stepId, updates)

      // For decision steps, also save agent_configs to the inner step
      if (stepDataForLogging && isDecisionStep(stepDataForLogging) && stepDataForLogging.decision_step?.id) {
        const innerStepId = stepDataForLogging.decision_step.id
        console.log('[StepSidebar] Saving agent config to inner step of decision step:', {
          parentStepId: stepId,
          innerStepId: innerStepId,
          hasAgentConfigs: !!agentConfigs
        })
        
        // Save only agent_configs to the inner step (don't update other fields)
        const innerStepUpdates: Partial<PlanStep> = {
          agent_configs: agentConfigs
        }
        
        await onEditStep(innerStepId, innerStepUpdates)
      }
      
      onClose()
    } catch (error) {
      console.error('[StepSidebar] Error saving step:', error)
    } finally {
      setIsSaving(false)
    }
  }

  // Handle validation/learning nodes
  if (node && (node.type === 'validation' || node.type === 'learning')) {
    const nodeData = node.data as ValidationNodeData | LearningNodeData
    const parentStepId = nodeData.parentStepId
    
    // Find parent step in plan
    const findStepById = (steps: PlanStep[], id: string): PlanStep | null => {
      for (const step of steps) {
        if (step.id === id) return step
        if (isConditionalStep(step)) {
          if (step.if_true_steps) {
            const found = findStepById(step.if_true_steps, id)
            if (found) return found
          }
          if (step.if_false_steps) {
            const found = findStepById(step.if_false_steps, id)
            if (found) return found
          }
        }
      }
      return null
    }
    
    const parentStep = plan ? findStepById(plan.steps, parentStepId) : null
    if (!parentStep) return null
    
    const agentConfigs = parentStep.agent_configs as AgentConfigs | undefined
    const isValidation = node.type === 'validation'
    
    // Determine if code execution mode is enabled
    const stepCodeExecSetting = agentConfigs?.use_code_execution_mode
    const useCodeExecutionMode = stepCodeExecSetting !== undefined 
      ? stepCodeExecSetting 
      : presetUseCodeExecutionMode  // Fall back to preset default
    
    // In code exec mode: validation and learning are always enabled
    const isAlwaysEnabled = useCodeExecutionMode
    
    // Helper to convert AgentLLMConfig to LLMOption
    const llmConfigToOption = (config: AgentLLMConfig | undefined): LLMOption | null => {
      if (!config || !config.provider || !config.model_id) {
        return null
      }
      const llm = availableLLMs.find(
        (l) => l.provider === config.provider && l.model === config.model_id
      )
      return llm || null
    }

    // Helper to get preset default LLM for an agent type
    const getPresetDefaultLLM = (agentType: 'execution' | 'validation' | 'learning'): LLMOption | null => {
      if (!presetLLMConfig) {
        return null
      }
      let config: AgentLLMConfig | undefined
      if (agentType === 'execution') {
        config = presetLLMConfig.execution_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
          provider: presetLLMConfig.provider,
          model_id: presetLLMConfig.model_id
        } : undefined)
      } else if (agentType === 'validation') {
        config = presetLLMConfig.validation_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
          provider: presetLLMConfig.provider,
          model_id: presetLLMConfig.model_id
        } : undefined)
      } else if (agentType === 'learning') {
        config = presetLLMConfig.learning_llm || (presetLLMConfig.provider && presetLLMConfig.model_id ? {
          provider: presetLLMConfig.provider,
          model_id: presetLLMConfig.model_id
        } : undefined)
      }
      if (config) {
        return llmConfigToOption(config)
      }
      return null
    }

    // Helper to convert LLMOption to AgentLLMConfig
    const optionToLLMConfig = (option: LLMOption | null): AgentLLMConfig | undefined => {
      if (!option) {
        return undefined
      }
      return {
        provider: option.provider as 'openai' | 'bedrock' | 'openrouter' | 'vertex',
        model_id: option.model,
      }
    }

    // Get current LLM option (step-specific, or preset default, or null for "use preset default")
    const currentLLMOption = isValidation
      ? llmConfigToOption(agentConfigs?.validation_llm) || getPresetDefaultLLM('validation')
      : useCodeExecutionMode
        ? llmConfigToOption(agentConfigs?.execution_llm) || getPresetDefaultLLM('execution')
        : llmConfigToOption(agentConfigs?.learning_llm) || getPresetDefaultLLM('learning')
    
    // Check if disabled (only relevant if not in code exec mode)
    // LLM validation is disabled by default (undefined/null/true = disabled, false = enabled)
    // Learning follows the old pattern (undefined/null = enabled, true = disabled)
    const isDisabled = isAlwaysEnabled
      ? false
      : (isValidation
          ? agentConfigs?.disable_validation !== false  // LLM validation disabled by default
          : agentConfigs?.disable_learning === true)
    
    // Handle LLM change
    const handleLLMSelect = async (llm: LLMOption) => {
      if (!parentStep) return
      
      // Check if the selected LLM is the same as preset default - if so, clear it to use preset
      const presetDefault = isValidation
        ? getPresetDefaultLLM('validation')
        : useCodeExecutionMode
          ? getPresetDefaultLLM('execution')
          : getPresetDefaultLLM('learning')
      
      // If selected LLM matches preset default, clear the step-specific config
      const shouldUsePresetDefault = presetDefault && 
        llm.provider === presetDefault.provider && 
        llm.model === presetDefault.model
      
      const updates: Partial<PlanStep> = {
        agent_configs: {
          ...agentConfigs,
          ...(isValidation 
            ? { validation_llm: shouldUsePresetDefault ? undefined : optionToLLMConfig(llm) }
            : useCodeExecutionMode
              ? { execution_llm: shouldUsePresetDefault ? undefined : optionToLLMConfig(llm) }
              : { learning_llm: shouldUsePresetDefault ? undefined : optionToLLMConfig(llm) }
          )
        } as AgentConfigs
      }
      
      await onEditStep(parentStepId, updates)
    }
    
    // Handle enable/disable toggle (only if not in code exec mode)
    const handleToggleEnabled = async () => {
      if (!parentStep || isAlwaysEnabled) return
      
      const updates: Partial<PlanStep> = {
        agent_configs: {
          ...agentConfigs,
          ...(isValidation 
            ? { disable_validation: !isDisabled }
            : { disable_learning: !isDisabled }
          )
        } as AgentConfigs
      }
      
      await onEditStep(parentStepId, updates)
    }
    
    // When ChatArea is visible, match its width (50% of viewport), otherwise use fixed widths
    const sidebarWidth = showChatArea ? 'w-[50vw]' : (isCompact ? 'w-[400px]' : 'w-[600px]')
    
    return (
      <div className={`absolute right-0 top-0 bottom-0 ${sidebarWidth} bg-white dark:bg-gray-900 border-l border-gray-200 dark:border-gray-700 shadow-xl z-50 flex flex-col transition-all duration-300`}>
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
          <div className="flex items-center gap-2">
            {isValidation ? (
              <CheckCircle2 className="w-5 h-5 text-indigo-500" />
            ) : (
              <BookOpen className="w-5 h-5 text-amber-500" />
            )}
            <span className="font-semibold text-gray-900 dark:text-gray-100">
              {isValidation ? 'Validation' : 'Learning'} Configuration
            </span>
          </div>
          <button
            onClick={onClose}
            className="p-1 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>
        
        {/* Content */}
        <div className="flex-1 overflow-y-auto p-4">
          <div className="mb-4">
            <div className="text-sm text-gray-600 dark:text-gray-400 mb-1">
              Parent Step
            </div>
            <div className="text-base font-medium text-gray-900 dark:text-gray-100">
              {nodeData.parentStepTitle}
            </div>
          </div>
          
          {/* Code Exec Mode Notice */}
          {isAlwaysEnabled && (
            <div className="mb-4 p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
              <div className="text-sm font-medium text-blue-900 dark:text-blue-200 mb-1">
                Code Execution Mode Active
              </div>
              <div className="text-xs text-blue-700 dark:text-blue-300">
                {isValidation ? 'Validation' : 'Learning'} is always enabled in code execution mode and cannot be disabled.
                {!isValidation && ' Learning uses the execution model.'}
              </div>
            </div>
          )}
          
          {/* Enable/Disable Toggle */}
          {!isAlwaysEnabled && (
            <div className="mb-6 p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-1">
                    {isValidation ? 'Validation' : 'Learning'} Status
                  </div>
                  <div className="text-xs text-gray-500 dark:text-gray-400">
                    {isDisabled 
                      ? `${isValidation ? 'Validation' : 'Learning'} is disabled for this step`
                      : `${isValidation ? 'Validation' : 'Learning'} is enabled for this step`
                    }
                  </div>
                </div>
                <label className="relative inline-flex items-center cursor-pointer">
                  <input
                    type="checkbox"
                    checked={!isDisabled}
                    onChange={handleToggleEnabled}
                    className="sr-only peer"
                  />
                  <div className="w-11 h-6 bg-gray-200 peer-focus:outline-none peer-focus:ring-4 peer-focus:ring-blue-300 dark:peer-focus:ring-blue-800 rounded-full peer dark:bg-gray-700 peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all dark:border-gray-600 peer-checked:bg-blue-600"></div>
                </label>
              </div>
            </div>
          )}
          
          {/* LLM Selection */}
          {!isDisabled && (
            <div className="p-4 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-3">
                {isValidation ? 'Validation' : 'Learning'} LLM Model
              </div>
              <div className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                {isValidation 
                  ? 'Select the LLM model to use for validation. If not specified, the preset default will be used.'
                  : useCodeExecutionMode
                    ? 'In code execution mode, learning uses the execution model. Select the execution LLM model.'
                    : 'Select the LLM model to use for learning. If not specified, the preset default will be used.'
                }
              </div>
              
              <div className="flex items-center gap-2">
                <div className="flex-1 min-w-0">
                  <LLMSelectionDropdown
                    availableLLMs={availableLLMs}
                    selectedLLM={currentLLMOption}
                    onLLMSelect={handleLLMSelect}
                    inModal={false}
                    openDirection="down"
                  />
                </div>
              </div>
              
              {currentLLMOption && (
                <div className="mt-2 text-xs text-gray-500 dark:text-gray-400">
                  {llmConfigToOption(isValidation 
                    ? agentConfigs?.validation_llm
                    : useCodeExecutionMode
                      ? agentConfigs?.execution_llm
                      : agentConfigs?.learning_llm
                  ) ? (
                    'Step-specific model configured'
                  ) : (
                    'Using preset default model'
                  )}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    )
  }

  if (!node || node.id === 'end' || !stepWithConfigs) {
    return null
  }

  // At this point, node must be step/conditional/loop/decision/orchestrator (validation/learning handled above)
  const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData | OrchestratorNodeData
  const step = stepData.step

  // When ChatArea is visible, match its width (50% of viewport), otherwise use fixed widths
  const sidebarWidth = showChatArea ? 'w-[50vw]' : (isCompact ? 'w-[400px]' : 'w-[600px]')
  
  // Start node and execution-settings node don't need sidebar (they're simple nodes)
  // Execution mode is now configured directly in the execution-settings node
  if (node && (node.id === 'start' || node.id === 'execution-settings')) {
    return null
  }
  
  return (
    <div className={`absolute right-0 top-0 bottom-0 ${sidebarWidth} bg-white dark:bg-gray-900 border-l border-gray-200 dark:border-gray-700 shadow-xl z-50 flex flex-col transition-all duration-300`}>
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
        <div className="flex flex-col gap-0.5">
          <span className="text-base font-semibold text-gray-900 dark:text-gray-100">
            {isEditing ? 'Edit Step' : `Step ${stepIndex + 1}`}
          </span>
          {!isEditing && step?.id && (
            <span className="text-xs font-mono text-gray-500 dark:text-gray-400">
              ID: {step.id}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1.5">
          {!isEditing && (
            <>
              {/* Phase Dropdown - Hide for evaluation steps */}
              {!isEvaluationStep && phases.length > 0 && (
                <div className="relative" ref={phaseDropdownRef}>
                  <button
                    onClick={() => !isRunning && setIsPhaseDropdownOpen(!isPhaseDropdownOpen)}
                    disabled={isRunning}
                    className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 text-gray-700 dark:text-gray-300 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    title="Run phase for this step"
                  >
                    <Settings className="w-3.5 h-3.5" />
                    Phase
                    <ChevronDown className={`w-3 h-3 transition-transform ${isPhaseDropdownOpen ? 'rotate-180' : ''}`} />
                  </button>
                  
                  {isPhaseDropdownOpen && !isRunning && (
                    <div className="absolute top-full right-0 mt-1 w-64 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50 max-h-[300px] overflow-y-auto">
                      <div className="p-2">
                        <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-2 py-1.5">
                          Run Phase for Step
                        </div>
                        {phases.map((phase) => (
                          <button
                            key={phase.id}
                            onClick={() => handleSelectPhase(phase.id)}
                            className="w-full text-left px-2 py-2 rounded transition-colors hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm"
                          >
                            <div className="font-medium">{phase.title}</div>
                            {phase.description && (
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 line-clamp-2">
                                {phase.description}
                              </div>
                            )}
                          </button>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
              
              {/* Run Step Button - Hide for evaluation steps */}
              {!isEvaluationStep && onStartPhase && (
                <button
                  onClick={handleRunStep}
                  disabled={isRunning || node.id.includes('-sub-agent-')}
                  className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium bg-green-600 hover:bg-green-700 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  title={
                    isRunning 
                      ? 'Execution in progress...' 
                      : node.id.includes('-sub-agent-')
                        ? 'Sub-agents cannot be run independently (run the parent routing step)'
                        : 'Run this step only'
                  }
                >
                  <Play className="w-3.5 h-3.5" />
                  Run
                </button>
              )}
              
              {/* View Learnings Button - Hide for evaluation steps */}
              {!isEvaluationStep && workspacePath && node && (() => {
                const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData | DecisionNodeData
                return stepData.step?.id ? (
                  <button
                    onClick={handleViewLearnings}
                    disabled={isRunning}
                    className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                    title="View learnings for this step"
                  >
                    <Eye className="w-4 h-4" />
                  </button>
                ) : null
              })()}
              
              {/* Edit Step Button */}
              <button
                onClick={handleStartEdit}
                disabled={isRunning}
                className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                title="Edit step"
              >
                <Edit2 className="w-4 h-4" />
              </button>
              
              {/* Actions Dropdown Menu - Hide for evaluation steps */}
              {!isEvaluationStep && (
              <div className="relative" ref={actionsDropdownRef}>
                <button
                  onClick={() => setIsActionsDropdownOpen(!isActionsDropdownOpen)}
                  disabled={isRunning}
                  className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  title="More options"
                >
                  <MoreVertical className="w-4 h-4" />
                </button>
                
                {isActionsDropdownOpen && !isRunning && (
                  <div className="absolute top-full right-0 mt-1 w-48 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50">
                    <div className="py-1">
                      {/* Delete Learnings */}
                      {workspacePath && (
                        <button
                          onClick={() => {
                            setShowDeleteLearningsConfirm(true)
                            setIsActionsDropdownOpen(false)
                          }}
                          disabled={isDeletingLearnings}
                          className="w-full text-left px-3 py-2 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 flex items-center gap-2 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          <Trash2 className="w-4 h-4" />
                          Delete Learnings
                        </button>
                      )}
                      
                      {/* Delete Step */}
                      <button
                        onClick={() => {
                          setShowDeleteConfirm(true)
                          setIsActionsDropdownOpen(false)
                        }}
                        className="w-full text-left px-3 py-2 text-sm text-red-600 dark:text-red-400 hover:bg-red-50 dark:hover:bg-red-900/20 flex items-center gap-2 transition-colors"
                      >
                        <Trash2 className="w-4 h-4" />
                        Delete Step
                      </button>
                    </div>
                  </div>
                )}
              </div>
              )}
            </>
          )}
          {isEditing && (
            <>
              <button
                onClick={handleCancelEdit}
                disabled={isSaving}
                className="px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
              >
                Cancel
              </button>
              <button
                onClick={handleSaveEdit}
                disabled={isSaving}
                className="flex items-center gap-1 px-3 py-1.5 text-xs bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 rounded hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors disabled:opacity-50"
              >
                <Save className="w-3 h-3" />
                {isSaving ? 'Saving...' : 'Save'}
              </button>
            </>
          )}
          <button
            onClick={onClose}
            className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
            title="Close sidebar"
          >
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      {/* Content - Scrollable */}
      <div className="flex-1 overflow-y-auto">
        <div className="p-4 space-y-5">
          {/* For conditional steps, only show condition and edit option */}
          {isConditionalStep(step) && node.type === 'conditional' ? (
            <div className="space-y-4">
              {/* Condition Display */}
              {step.condition_question && (
                <div className="p-4 bg-purple-50 dark:bg-purple-900/20 rounded-lg border border-purple-200 dark:border-purple-800">
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-semibold text-purple-700 dark:text-purple-300 uppercase tracking-wide">
                      Condition:
                    </span>
                    {!isEditing && (
                      <button
                        onClick={handleStartEdit}
                        className="p-1.5 rounded-md hover:bg-purple-100 dark:hover:bg-purple-800 text-purple-600 dark:text-purple-400 transition-colors"
                        title="Edit condition"
                      >
                        <Edit2 className="w-4 h-4" />
                      </button>
                    )}
                  </div>
                  {isEditing ? (
                    <div className="space-y-3">
                      <textarea
                        value={editedDescription || step.condition_question}
                        onChange={(e) => setEditedDescription(e.target.value)}
                        rows={4}
                        className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-purple-500 dark:focus:ring-purple-400 resize-y"
                        placeholder="Enter condition question..."
                      />
                      <div className="flex items-center gap-2">
                        <button
                          onClick={handleCancelEdit}
                          disabled={isSaving}
                          className="px-3 py-1.5 text-xs text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors disabled:opacity-50"
                        >
                          Cancel
                        </button>
                        <button
                          onClick={async () => {
                            if (node) {
                              await onEditStep(node.id, {
                                condition_question: editedDescription || step.condition_question
                              })
                              setIsEditing(false)
                            }
                          }}
                          disabled={isSaving}
                          className="flex items-center gap-1 px-3 py-1.5 text-xs bg-purple-600 hover:bg-purple-700 text-white rounded transition-colors disabled:opacity-50"
                        >
                          <Save className="w-3 h-3" />
                          {isSaving ? 'Saving...' : 'Save'}
                        </button>
                      </div>
                    </div>
                  ) : (
                    <div className="mt-2 text-sm text-gray-700 dark:text-gray-300">
                      <MarkdownRenderer content={step.condition_question} className="text-sm text-gray-700 dark:text-gray-300" />
                    </div>
                  )}
                </div>
              )}
              
              {/* Condition Context (if provided) */}
              {step.condition_context && (
                <div className="p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
                  <span className="text-xs font-medium text-gray-600 dark:text-gray-400">Context: </span>
                  <p className="text-sm text-gray-700 dark:text-gray-300 mt-1 whitespace-pre-wrap">
                    {step.condition_context}
                  </p>
                </div>
              )}
            </div>
          ) : (
            /* Regular step display (non-conditional or conditional but not in conditional node) */
            <div className="space-y-4">
              {isEditing ? (
                // Edit mode
                <div className="space-y-3">
                  <div>
                    <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Title
                    </label>
                    <input
                      type="text"
                      value={editedTitle}
                      onChange={(e) => setEditedTitle(e.target.value)}
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-gray-500 dark:focus:ring-gray-400"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Description
                    </label>
                    <textarea
                      value={editedDescription}
                      onChange={(e) => setEditedDescription(e.target.value)}
                      rows={8}
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-gray-500 dark:focus:ring-gray-400 resize-y min-h-[120px]"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                      Success Criteria
                    </label>
                    <textarea
                      value={editedSuccessCriteria}
                      onChange={(e) => setEditedSuccessCriteria(e.target.value)}
                      rows={5}
                      className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-gray-500 dark:focus:ring-gray-400 resize-y min-h-[100px]"
                    />
                  </div>
                </div>
              ) : (
                // View mode
                <>
                  <div className="space-y-2">
                    <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">
                      {step.title}
                    </h3>
                    {step.description && (
                      <div className="text-sm text-gray-600 dark:text-gray-400">
                        <MarkdownRenderer content={step.description} className="text-sm text-gray-600 dark:text-gray-400" />
                      </div>
                    )}
                  </div>

                  {step.success_criteria && (
                    <div className="p-3 bg-green-50 dark:bg-green-900/20 rounded-lg border border-green-200 dark:border-green-800/50">
                      <span className="text-xs font-semibold text-green-700 dark:text-green-300 uppercase tracking-wide">
                        Success Criteria:
                      </span>
                      <div className="mt-2 text-sm text-gray-700 dark:text-gray-300">
                        <MarkdownRenderer content={step.success_criteria} className="text-sm text-gray-700 dark:text-gray-300" />
                      </div>
                    </div>
                  )}
                </>
              )}

              {isConditionalStep(step) && (
                <div className="space-y-3">
                  {step.condition_question && (
                    <div className="p-2 bg-purple-50 dark:bg-purple-900/20 rounded">
                      <span className="text-xs font-medium text-purple-600 dark:text-purple-400">
                        Condition:
                      </span>
                      <div className="mt-1 text-sm text-gray-700 dark:text-gray-300">
                        <MarkdownRenderer content={step.condition_question} className="text-sm text-gray-700 dark:text-gray-300" />
                      </div>
                    </div>
                  )}
                  
                  {/* Branch routing information */}
                  <div className="p-3 bg-purple-50 dark:bg-purple-900/20 rounded-lg border border-purple-200 dark:border-purple-800/50">
                    <span className="text-xs font-semibold text-purple-700 dark:text-purple-300 uppercase tracking-wide">
                      Branch Routing:
                    </span>
                    <div className="mt-2 space-y-2">
                      {/* True branch (Yes) */}
                      <div>
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-xs font-medium text-green-600 dark:text-green-400">
                            ✓ Yes Branch:
                          </span>
                          {step.if_true_steps && step.if_true_steps.length > 0 ? (
                            <span className="text-xs text-gray-600 dark:text-gray-400">
                              {step.if_true_steps.length} step{step.if_true_steps.length !== 1 ? 's' : ''}
                            </span>
                          ) : (
                            <span className="text-xs text-gray-500 dark:text-gray-500 italic">
                              (no steps)
                            </span>
                          )}
                        </div>
                        {step.if_true_next_step_id && (
                          <p className="text-xs text-gray-700 dark:text-gray-300 ml-4">
                            → {step.if_true_next_step_id === 'end' ? 'End workflow' : step.if_true_next_step_id}
                          </p>
                        )}
                      </div>
                      
                      {/* False branch (No) */}
                      <div>
                        <div className="flex items-center gap-2 mb-1">
                          <span className="text-xs font-medium text-red-600 dark:text-red-400">
                            ✗ No Branch:
                          </span>
                          {step.if_false_steps && step.if_false_steps.length > 0 ? (
                            <span className="text-xs text-gray-600 dark:text-gray-400">
                              {step.if_false_steps.length} step{step.if_false_steps.length !== 1 ? 's' : ''}
                            </span>
                          ) : (
                            <span className="text-xs text-gray-500 dark:text-gray-500 italic">
                              (no steps)
                            </span>
                          )}
                        </div>
                        {step.if_false_next_step_id && (
                          <p className="text-xs text-gray-700 dark:text-gray-300 ml-4">
                            → {step.if_false_next_step_id === 'end' ? 'End workflow' : step.if_false_next_step_id}
                          </p>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              )}

              {((isRegularStep(step) && step.has_loop) || node.type === 'loop') && isRegularStep(step) && (
                <div className="p-3 bg-gray-50 dark:bg-gray-800/50 rounded-lg border border-gray-200 dark:border-gray-700">
                  <span className="text-xs font-semibold text-blue-600 dark:text-blue-400 uppercase tracking-wide">
                    Loop:
                  </span>
                  {step.loop_condition && (
                    <div className="mt-2">
                      <span className="text-xs font-medium text-gray-600 dark:text-gray-400">Until: </span>
                      <p className="text-sm text-gray-700 dark:text-gray-300 mt-1 whitespace-pre-wrap">
                        {step.loop_condition}
                      </p>
                    </div>
                  )}
                  <div className="mt-2">
                    <label className="text-xs font-medium text-gray-600 dark:text-gray-400 block mb-1">
                      Max iterations:
                    </label>
                    <select
                      value={editedMaxIterations || step.max_iterations?.toString() || '10'}
                      onChange={(e) => {
                        setEditedMaxIterations(e.target.value)
                        // Auto-save on change
                        const maxIterations = parseInt(e.target.value, 10)
                        if (!isNaN(maxIterations) && maxIterations > 0 && node) {
                          onEditStep(node.id, { max_iterations: maxIterations }).catch((err) => {
                            console.error('[StepSidebar] Error saving max_iterations:', err)
                          })
                        }
                      }}
                      className="w-full px-2 py-1.5 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                    >
                      {[5, 10, 15, 20, 25, 30, 40, 50, 75, 100].map((value) => (
                        <option key={value} value={value.toString()}>
                          {value}
                        </option>
                      ))}
                    </select>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                      Maximum number of loop iterations allowed (default: 10)
                    </p>
                  </div>
                </div>
              )}

              {/* Dependencies */}
              {(step.context_dependencies?.length || step.context_output) && (
                <div className="space-y-2">
                  <span className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                    Dependencies:
                  </span>
                  <div className="space-y-2 text-sm">
                    {step.context_dependencies && step.context_dependencies.length > 0 && (
                      <div>
                        <span className="font-medium text-blue-600 dark:text-blue-400">Inputs: </span>
                        <span className="text-gray-600 dark:text-gray-400">
                          {step.context_dependencies.join(', ')}
                        </span>
                      </div>
                    )}
                    {step.context_output && (
                      <div>
                        <span className="font-medium text-emerald-600 dark:text-emerald-400">Output: </span>
                        <span className="text-gray-600 dark:text-gray-400">
                          {Array.isArray(step.context_output) ? step.context_output.join(', ') : step.context_output}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
              )}

              {/* Prerequisites */}
              {(() => {
                type PrereqRule = { depends_on_step: string; description: string }
                const agentConfigs = (step as PlanStep & { agent_configs?: AgentConfigs }).agent_configs
                const enabled =
                  agentConfigs?.enable_prerequisite_detection ??
                  (step as unknown as { enable_prerequisite_detection?: boolean }).enable_prerequisite_detection
                const rules =
                  (agentConfigs?.prerequisite_rules ??
                    (step as unknown as { prerequisite_rules?: PrereqRule[] }).prerequisite_rules) as
                    | PrereqRule[]
                    | undefined

                if (!enabled && (!rules || rules.length === 0)) return null

                return (
                  <div className="space-y-2">
                    <span className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                      Prerequisites:
                    </span>
                    <div className="space-y-1 text-sm">
                      <div className="text-xs text-gray-700 dark:text-gray-300">
                        <span className="font-medium">Detection:</span>{' '}
                        <span
                          className={
                            enabled
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-gray-500 dark:text-gray-500'
                          }
                        >
                          {enabled ? 'Enabled' : 'Disabled'}
                        </span>
                      </div>
                      {rules && rules.length > 0 && (
                        <div className="mt-1 space-y-1.5">
                          {rules.map((rule, idx) => (
                            <div
                              key={`${rule.depends_on_step}-${idx}`}
                              className="text-xs text-gray-700 dark:text-gray-300 border border-orange-200 dark:border-orange-800/60 bg-orange-50 dark:bg-orange-900/20 rounded px-2 py-1"
                            >
                              <div>
                                <span className="font-medium text-orange-700 dark:text-orange-300">
                                  Depends on:
                                </span>{' '}
                                <span className="font-mono text-[11px] break-all">
                                  {rule.depends_on_step}
                                </span>
                              </div>
                              {rule.description && (
                                <div className="mt-0.5 text-[11px] text-orange-700 dark:text-orange-300">
                                  {rule.description}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  </div>
                )
              })()}

              {/* Validation Schema */}
              {(() => {
                // For different node types, validation_schema is stored in different places:
                // - Orchestrator: node data or step.validation_schema
                // - Decision: decision_step.validation_schema (nested step)
                // - Conditional: wrapper step.validation_schema (branch steps have their own)
                // - Regular/Loop: step.validation_schema
                let validationSchema: ValidationSchema | undefined
                
                if (node.type === 'orchestrator') {
                  const orchestratorData = node.data as OrchestratorNodeData
                  validationSchema = orchestratorData.validation_schema || step.validation_schema as ValidationSchema | undefined
                } else if (node.type === 'decision' && isDecisionStep(step)) {
                  // For decision steps, validation_schema is on the nested decision_step
                  validationSchema = step.decision_step?.validation_schema as ValidationSchema | undefined
                } else if (node.type === 'conditional' && isConditionalStep(step)) {
                  // For conditional steps, check wrapper step first, then show note about branch steps
                  validationSchema = step.validation_schema as ValidationSchema | undefined
                } else {
                  // Regular, loop, or other step types
                  validationSchema = step.validation_schema as ValidationSchema | undefined
                }
                
                if (!validationSchema || !validationSchema.files || validationSchema.files.length === 0) {
                  // For conditional steps, show a note that branch steps have their own validation schemas
                  if (node.type === 'conditional' && isConditionalStep(step)) {
                    const hasBranchSchemas = 
                      (step.if_true_steps && step.if_true_steps.some(s => s.validation_schema)) ||
                      (step.if_false_steps && step.if_false_steps.some(s => s.validation_schema))
                    
                    if (hasBranchSchemas) {
                      return (
                        <div className="space-y-3">
                          <span className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                            Validation Schema:
                          </span>
                          <div className="p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800/50">
                            <p className="text-xs text-blue-700 dark:text-blue-300">
                              Validation schemas are defined on the branch steps (if_true_steps and if_false_steps), not on the wrapper step.
                            </p>
                          </div>
                        </div>
                      )
                    }
                  }
                  return null
                }

                return (
                  <div className="space-y-3">
                    <span className="text-xs font-semibold text-gray-700 dark:text-gray-300 uppercase tracking-wide">
                      Validation Schema:
                    </span>
                    <div className="space-y-3">
                      {validationSchema.files.map((file, fileIdx) => (
                        <div
                          key={fileIdx}
                          className="p-3 bg-indigo-50 dark:bg-indigo-900/20 rounded-lg border border-indigo-200 dark:border-indigo-800/50"
                        >
                          {/* File Header */}
                          <div className="flex items-center justify-between mb-2">
                            <div className="flex items-center gap-2">
                              <span className="text-xs font-semibold text-indigo-700 dark:text-indigo-300">
                                File: {file.file_name}
                              </span>
                              {file.must_exist && (
                                <span className="px-1.5 py-0.5 text-[10px] font-medium bg-indigo-200 dark:bg-indigo-800 text-indigo-800 dark:text-indigo-200 rounded">
                                  Must Exist
                                </span>
                              )}
                            </div>
                          </div>

                          {/* JSON Checks */}
                          {file.json_checks && file.json_checks.length > 0 && (
                            <div className="mt-2 space-y-2">
                              <div className="text-[10px] font-medium text-indigo-600 dark:text-indigo-400 uppercase tracking-wide">
                                JSON Checks ({file.json_checks.length}):
                              </div>
                              {file.json_checks.map((check, checkIdx) => (
                                <div
                                  key={checkIdx}
                                  className="p-2 bg-white dark:bg-gray-800 rounded border border-indigo-100 dark:border-indigo-900/50"
                                >
                                  {/* JSONPath */}
                                  <div className="mb-1.5">
                                    <span className="text-[10px] font-medium text-gray-600 dark:text-gray-400">Path:</span>
                                    <code className="ml-1 text-[11px] font-mono text-indigo-700 dark:text-indigo-300 bg-indigo-50 dark:bg-indigo-900/30 px-1.5 py-0.5 rounded">
                                      {check.path}
                                    </code>
                                    {check.must_exist && (
                                      <span className="ml-2 px-1 py-0.5 text-[10px] font-medium bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 rounded">
                                        Required
                                      </span>
                                    )}
                                  </div>

                                  {/* Value Type */}
                                  {check.value_type && (
                                    <div className="mb-1 text-[10px] text-gray-600 dark:text-gray-400">
                                      <span className="font-medium">Type:</span>{' '}
                                      <span className="font-mono text-indigo-600 dark:text-indigo-400">{check.value_type}</span>
                                    </div>
                                  )}

                                  {/* Constraints */}
                                  <div className="flex flex-wrap gap-1.5 mt-1.5">
                                    {check.min_length !== undefined && (
                                      <span className="px-1.5 py-0.5 text-[10px] bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded">
                                        Min Length: {check.min_length}
                                      </span>
                                    )}
                                    {check.max_length !== undefined && (
                                      <span className="px-1.5 py-0.5 text-[10px] bg-blue-100 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300 rounded">
                                        Max Length: {check.max_length}
                                      </span>
                                    )}
                                    {check.min_value !== undefined && (
                                      <span className="px-1.5 py-0.5 text-[10px] bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded">
                                        Min Value: {check.min_value}
                                      </span>
                                    )}
                                    {check.max_value !== undefined && (
                                      <span className="px-1.5 py-0.5 text-[10px] bg-green-100 dark:bg-green-900/30 text-green-700 dark:text-green-300 rounded">
                                        Max Value: {check.max_value}
                                      </span>
                                    )}
                                    {check.pattern && (
                                      <span className="px-1.5 py-0.5 text-[10px] bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300 rounded">
                                        Pattern: {check.pattern}
                                      </span>
                                    )}
                                  </div>

                                  {/* Consistency Check */}
                                  {check.consistency_check && (
                                    <div className="mt-1.5 pt-1.5 border-t border-indigo-100 dark:border-indigo-900/50">
                                      <div className="text-[10px] font-medium text-orange-600 dark:text-orange-400 mb-0.5">
                                        Consistency Check:
                                      </div>
                                      <div className="text-[10px] text-gray-600 dark:text-gray-400">
                                        <span className="font-medium">Type:</span>{' '}
                                        <span className="font-mono text-orange-600 dark:text-orange-400">
                                          {check.consistency_check.type}
                                        </span>
                                      </div>
                                      <div className="text-[10px] text-gray-600 dark:text-gray-400 mt-0.5">
                                        <span className="font-medium">Compare with:</span>{' '}
                                        <code className="font-mono text-orange-600 dark:text-orange-400 bg-orange-50 dark:bg-orange-900/30 px-1 py-0.5 rounded">
                                          {check.consistency_check.compare_with_path}
                                        </code>
                                      </div>
                                    </div>
                                  )}
                                </div>
                              ))}
                            </div>
                          )}

                          {/* No JSON Checks Message */}
                          {(!file.json_checks || file.json_checks.length === 0) && (
                            <div className="mt-2 text-[10px] text-gray-500 dark:text-gray-500 italic">
                              No JSON checks defined (only file existence check)
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )
              })()}
            </div>
          )}

          {/* Step Configuration Panel */}
          {isConditionalStep(step) && node.type === 'conditional' ? (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
              <div className="mb-3 p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
                <p className="text-xs text-blue-700 dark:text-blue-300">
                  <strong>Note:</strong> This configuration applies to the conditional LLM and branch step agents. The conditional step itself is not executed.
                </p>
              </div>
              <StepEditPanel
                step={stepWithConfigs}
                stepIndex={stepIndex}
                onSave={handleSave}
                onCancel={() => {}}
                isSaving={isSaving}
                presetServers={presetServers}
                presetLLMConfig={presetLLMConfig}
                presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                isExpanded={true}
                onToggleExpanded={() => {}}
                planSteps={plan?.steps || []}
              />
            </div>
          ) : isDecisionStep(step) && node.type === 'decision' ? (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
              {/* Decision Step Info */}
              <div className="mb-3 p-3 bg-indigo-50 dark:bg-indigo-900/20 rounded-lg border border-indigo-200 dark:border-indigo-800">
                <p className="text-xs text-indigo-700 dark:text-indigo-300">
                  <strong>Decision Step:</strong> Executes the inner step first, then evaluates the output to determine routing.
                </p>
                {step.decision_step && (
                  <div className="mt-2 p-2 bg-white dark:bg-gray-800 rounded border border-indigo-100 dark:border-indigo-900">
                    <p className="text-xs font-medium text-gray-900 dark:text-white">
                      Inner Step: {step.decision_step.title || 'Untitled'}
                    </p>
                    {step.decision_evaluation_question && (
                      <p className="text-xs text-gray-600 dark:text-gray-400 mt-1">
                        Evaluates: {step.decision_evaluation_question}
                      </p>
                    )}
                  </div>
                )}
                {step.if_true_next_step_id && (
                  <p className="text-xs text-green-600 dark:text-green-400 mt-2">
                    ✓ True → {step.if_true_next_step_id === 'end' ? 'End workflow' : step.if_true_next_step_id}
                  </p>
                )}
                {step.if_false_next_step_id && (
                  <p className="text-xs text-red-600 dark:text-red-400 mt-1">
                    ✗ False → {step.if_false_next_step_id === 'end' ? 'End workflow' : step.if_false_next_step_id}
                  </p>
                )}
              </div>
              <StepEditPanel
                step={stepWithConfigs}
                stepIndex={stepIndex}
                onSave={handleSave}
                onCancel={() => {}}
                isSaving={isSaving}
                presetServers={presetServers}
                presetLLMConfig={presetLLMConfig}
                presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                isExpanded={true}
                onToggleExpanded={() => {}}
                planSteps={plan?.steps || []}
              />
            </div>
          ) : isOrchestrationStep(step) && node.type === 'orchestrator' ? (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
              {/* Orchestrator Step Info */}
              <div className="mb-3 p-3 bg-blue-50 dark:bg-blue-900/20 rounded-lg border border-blue-200 dark:border-blue-800">
                <p className="text-xs text-blue-700 dark:text-blue-300">
                  <strong>Orchestrator Step:</strong> Evaluates the input and routes to a specific sub-agent or ends the workflow.
                </p>
                {/* Routes Display */}
                {step.orchestration_routes && step.orchestration_routes.length > 0 && (
                  <div className="mt-3">
                    <p className="text-xs font-semibold text-blue-700 dark:text-blue-300 uppercase tracking-wide mb-2">
                      Available Routes:
                    </p>
                    <div className="space-y-2">
                      {step.orchestration_routes.map((route) => {
                        const isEndRoute = route.route_id?.toLowerCase() === "end"
                        return (
                          <div key={route.route_id || 'unknown'} className="p-2 bg-white dark:bg-gray-800 rounded border border-blue-100 dark:border-blue-900">
                            <div className="flex items-center gap-2 mb-1">
                              <div className={`w-2 h-2 rounded-full flex-shrink-0 ${
                                isEndRoute 
                                  ? "bg-red-500 dark:bg-red-400" 
                                  : "bg-blue-500 dark:bg-blue-400"
                              }`} />
                              <span className={`text-xs font-medium ${
                                isEndRoute
                                  ? "text-red-700 dark:text-red-300"
                                  : "text-blue-700 dark:text-blue-300"
                              }`}>
                                {route.route_name || route.route_id}
                              </span>
                            </div>
                            {route.condition && (
                              <p className="text-xs text-gray-600 dark:text-gray-400 ml-4">
                                Condition: {route.condition}
                              </p>
                            )}
                            {route.context_to_pass && (
                              <p className="text-xs text-gray-500 dark:text-gray-500 ml-4 mt-1 italic">
                                Context: {route.context_to_pass}
                              </p>
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )}
              </div>
              <StepEditPanel
                step={stepWithConfigs}
                stepIndex={stepIndex}
                onSave={handleSave}
                onCancel={() => {}}
                isSaving={isSaving}
                presetServers={presetServers}
                presetLLMConfig={presetLLMConfig}
                presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                isExpanded={true}
                onToggleExpanded={() => {}}
                planSteps={plan?.steps || []}
              />
            </div>
          ) : (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
              {/* Show note for sub-agents (check for both patterns: -sub-agent- and nodes with undefined in ID that might be sub-agents) */}
              {(node.id.includes('-sub-agent-') || (node.id.includes('undefined') && node.type === 'step')) && (
                <div className="mb-3 p-3 bg-cyan-50 dark:bg-cyan-900/20 rounded-lg border border-cyan-200 dark:border-cyan-800">
                  <p className="text-xs text-cyan-700 dark:text-cyan-300">
                    <strong>Sub-Agent:</strong> This configuration applies to the sub-agent step within an orchestration route.
                  </p>
                </div>
              )}
              {isEvaluationStep && (
                <div className="mb-3 p-3 bg-indigo-50 dark:bg-indigo-900/20 rounded-lg border border-indigo-200 dark:border-indigo-800">
                  <p className="text-xs text-indigo-700 dark:text-indigo-300">
                    <strong>Evaluation Step:</strong> This step evaluates the performance of the workflow against specific criteria.
                  </p>
                </div>
              )}
              <StepEditPanel
                step={stepWithConfigs}
                stepIndex={stepIndex}
                onSave={handleSave}
                onCancel={() => {}}
                isSaving={isSaving}
                presetServers={presetServers}
                presetLLMConfig={presetLLMConfig}
                presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                isExpanded={true}
                onToggleExpanded={() => {}}
                planSteps={plan?.steps || []}
              />
            </div>
          )}
        </div>
      </div>

      {/* Delete Step Confirmation Dialog */}
      {/* ESC key closes the dialog and cancels the delete (treats as false/not confirmed) */}
      <ConfirmationDialog
        isOpen={showDeleteConfirm}
        onClose={() => setShowDeleteConfirm(false)}
        onConfirm={() => {
          if (node) {
            onDeleteStep(node.id)
            setShowDeleteConfirm(false)
          }
        }}
        title="Delete Step"
        message={
          node && (node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator')
            ? (() => {
                const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData
                const stepTitle = stepData.step?.title || `Step ${stepIndex + 1}`
                return `Are you sure you want to delete "${stepTitle}"? This action cannot be undone. Any context dependencies referencing this step's output will be automatically removed.`
              })()
            : 'Are you sure you want to delete this step? This action cannot be undone.'
        }
        confirmText="Delete"
        cancelText="Cancel"
        type="danger"
      />

      {/* Delete Learnings Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={showDeleteLearningsConfirm}
        onClose={() => setShowDeleteLearningsConfirm(false)}
        onConfirm={handleDeleteLearnings}
        title="Delete Learnings"
        message={
          node && (node.type === 'step' || node.type === 'conditional' || node.type === 'decision' || node.type === 'loop' || node.type === 'orchestrator')
            ? (() => {
                const stepData = node.data as StepNodeData | ConditionalNodeData | DecisionNodeData | LoopNodeData | OrchestratorNodeData
                const stepTitle = stepData.step?.title || `Step ${stepIndex + 1}`
                // For orchestration steps, use orchestration_step.ID (backend uses orchestration_step.ID for learnings)
                // For other steps, use step.ID
                let stepId: string
                if (node.type === 'orchestrator') {
                  const orchestratorData = stepData as OrchestratorNodeData
                  stepId = orchestratorData.orchestration_step?.id ?? orchestratorData.step?.id ?? `step-${stepIndex + 1}`
                } else {
                  stepId = stepData.step?.id || `step-${stepIndex + 1}`
                }
                return `Are you sure you want to delete all learnings for "${stepTitle}" (Step ${stepIndex + 1})? This will permanently delete the learnings folder at \`learnings/${stepId}/\` and all its contents. This action cannot be undone.`
              })()
            : `Are you sure you want to delete all learnings for Step ${stepIndex + 1}? This will permanently delete the learnings folder and all its contents. This action cannot be undone.`
        }
        confirmText="Delete Learnings"
        cancelText="Cancel"
        type="danger"
      />
    </div>
  )
}

export default StepSidebar

