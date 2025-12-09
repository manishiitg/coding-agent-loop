import React, { useState, useMemo, useEffect, useRef, useCallback } from 'react'
import { X, Trash2, Edit2, Save, ChevronDown, Settings, CheckCircle2, BookOpen, Play } from 'lucide-react'
import ConfirmationDialog from '../../ui/ConfirmationDialog'
import type { ExecutionOptions } from '../../../services/api-types'
import { ExecutionStrategy } from '../../../services/api-types'
import { StepEditPanel } from '../../events/orchestrator/StepEditPanel'
import { useGlobalPresetStore } from '../../../stores/useGlobalPresetStore'
import { useLLMStore } from '../../../stores/useLLMStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { agentApi } from '../../../services/api'
import LLMSelectionDropdown from '../../LLMSelectionDropdown'
import type { LLMOption } from '../../../types/llm'
import type { AgentLLMConfig } from '../../../utils/stepConfigMatching'
import type { 
  WorkflowNode, 
  StepNodeData, 
  ConditionalNodeData, 
  LoopNodeData,
  ValidationNodeData,
  LearningNodeData
} from '../hooks/usePlanToFlow'
import type { PlanStep, PlanningResponse, AgentConfigs } from '../../../utils/stepConfigMatching'
import type { TodoStepWithConfigs } from '../../../utils/stepConfigMatching'

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
  completedStepIndices?: number[]  // 0-based indices of completed steps (for enabling/disabling run button)
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
  completedStepIndices = []
}) => {
  const { availableLLMs } = useLLMStore()
  
  // Get step-specific phases from workflow store (already filtered)
  const { getStepSpecificPhases, loadPhases } = useWorkflowStore()
  const phases = getStepSpecificPhases()
  
  // Get execution options directly from workflow store (single source of truth)
  // This fixes the issue where globalExecutionOptions prop might be null/stale
  const selectedRunFolder = useWorkflowStore(state => state.selectedRunFolder)
  const selectedExecutionMode = useWorkflowStore(state => state.selectedExecutionMode)
  
  const [isPhaseDropdownOpen, setIsPhaseDropdownOpen] = useState(false)
  const phaseDropdownRef = useRef<HTMLDivElement>(null)
  const [isSaving, setIsSaving] = useState(false)
  const [isEditing, setIsEditing] = useState(false)
  const [editedTitle, setEditedTitle] = useState('')
  const [editedDescription, setEditedDescription] = useState('')
  const [editedSuccessCriteria, setEditedSuccessCriteria] = useState('')
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [showDeleteLearningsConfirm, setShowDeleteLearningsConfirm] = useState(false)
  const [isDeletingLearnings, setIsDeletingLearnings] = useState(false)

  // Ensure phases are loaded (store handles deduplication)
  useEffect(() => {
    loadPhases()
  }, [loadPhases])

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (phaseDropdownRef.current && !phaseDropdownRef.current.contains(event.target as Node)) {
        setIsPhaseDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  // Handle phase selection
  const handleSelectPhase = async (phaseId: string) => {
    if (!presetQueryId || !node) return
    
    setIsPhaseDropdownOpen(false)
    
    try {
      // Update workflow status with step ID
      await agentApi.updateWorkflow(presetQueryId, phaseId, null, node.id)
      
      // Start the phase through the parent component
      if (onStartPhase) {
        onStartPhase(phaseId, node.id)
      }
    } catch (error) {
      console.error('[StepSidebar] Failed to start phase:', error)
    }
  }

  // Check if this step can run (all previous steps must be completed)
  const canRunStep = useMemo(() => {
    // First step (index 0) can always run
    if (stepIndex === 0) return true
    // Check all previous steps (0 to stepIndex-1) are completed
    const completedSet = new Set(completedStepIndices)
    for (let i = 0; i < stepIndex; i++) {
      if (!completedSet.has(i)) return false
    }
    return true
  }, [stepIndex, completedStepIndices])

  // Handle run single step
  // Uses workflow store directly for execution options (single source of truth)
  const handleRunStep = useCallback(() => {
    if (!onStartPhase || !node || !canRunStep) return
    
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
  }, [onStartPhase, node, canRunStep, selectedRunFolder, selectedExecutionMode, stepIndex])

  // Handle delete learnings for this step
  const handleDeleteLearnings = useCallback(async () => {
    if (!workspacePath) {
      console.error('[StepSidebar] Cannot delete learnings: workspace path not available')
      return
    }

    setIsDeletingLearnings(true)
    try {
      // stepIndex is 0-based, but step numbers are 1-based
      const stepNumber = stepIndex + 1
      const result = await agentApi.deleteStepLearnings(workspacePath, stepNumber)
      
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
  }, [workspacePath, stepIndex])
  
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
    return {
      id: planStep.id,
      title: planStep.title,
      description: planStep.description,
      success_criteria: planStep.success_criteria,
      why_this_step: planStep.why_this_step,
      context_dependencies: planStep.context_dependencies,
      context_output: Array.isArray(planStep.context_output) 
        ? planStep.context_output.join(', ') 
        : planStep.context_output,
      has_loop: planStep.has_loop,
      loop_condition: planStep.loop_condition,
      max_iterations: planStep.max_iterations,
      loop_description: planStep.loop_description,
      has_condition: planStep.has_condition,
      condition_question: planStep.condition_question,
      condition_context: planStep.condition_context,
      condition_result: planStep.condition_result,
      condition_reason: planStep.condition_reason,
      agent_configs: (planStep as PlanStep & { agent_configs?: AgentConfigs }).agent_configs
    }
  }, [])

  // Convert PlanStep to TodoStepWithConfigs
  const stepWithConfigs: TodoStepWithConfigs | null = useMemo(() => {
    if (!node) return null
    
    // Validation/learning nodes don't have step data
    if (node.type === 'validation' || node.type === 'learning') return null
    
    // Check if step exists (for step/conditional/loop nodes)
    const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
    if (!stepData.step) return null

    const planStep = stepData.step
    
    // Convert PlanStep to TodoStepWithConfigs
    // PlanStep and TodoStepWithConfigs are very similar, but we need to ensure agent_configs is included
    const todoStep: TodoStepWithConfigs = {
      id: planStep.id,
      title: planStep.title,
      description: planStep.description,
      success_criteria: planStep.success_criteria,
      why_this_step: planStep.why_this_step,
      context_dependencies: planStep.context_dependencies,
      context_output: Array.isArray(planStep.context_output) 
        ? planStep.context_output.join(', ') 
        : planStep.context_output,
      has_loop: planStep.has_loop,
      loop_condition: planStep.loop_condition,
      max_iterations: planStep.max_iterations,
      loop_description: planStep.loop_description,
      has_condition: planStep.has_condition,
      condition_question: planStep.condition_question,
      condition_context: planStep.condition_context,
      condition_result: planStep.condition_result,
      condition_reason: planStep.condition_reason,
      // Include agent_configs if it exists in the step
      agent_configs: (planStep as PlanStep & { agent_configs?: AgentConfigs }).agent_configs
    }

    // Convert nested steps if they exist
    if (planStep.if_true_steps) {
      todoStep.if_true_steps = planStep.if_true_steps.map(convertPlanStepToTodoStep)
    }
    if (planStep.if_false_steps) {
      todoStep.if_false_steps = planStep.if_false_steps.map(convertPlanStepToTodoStep)
    }

    return todoStep
  }, [node, convertPlanStepToTodoStep]) // node dependency ensures updates when step data changes

  // Initialize edit fields when node changes or edit mode is enabled
  React.useEffect(() => {
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'loop')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
      }
    }
  }, [node, isEditing])

  // Handle start edit
  const handleStartEdit = () => {
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'loop')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
        setIsEditing(true)
      }
    }
  }

  // Handle save edit (for basic fields)
  const handleSaveEdit = async () => {
    if (!node) return

    setIsSaving(true)
    try {
      await onEditStep(node.id, {
        title: editedTitle,
        description: editedDescription,
        success_criteria: editedSuccessCriteria
      })
      setIsEditing(false)
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
    if (node && (node.type === 'step' || node.type === 'conditional' || node.type === 'loop')) {
      const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
      if (stepData.step) {
        const step = stepData.step
        setEditedTitle(step.title || '')
        setEditedDescription(step.description || '')
        setEditedSuccessCriteria(step.success_criteria || '')
      }
    }
  }

  // Handle save from StepEditPanel
  const handleSave = async (updatedStep: TodoStepWithConfigs) => {
    if (!node) return

    setIsSaving(true)
    try {
      // Get the actual step ID from step data (not node.id which is React Flow node ID)
      const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
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
        stepHasCondition: stepDataForLogging?.has_condition,
        stepHasLoop: stepDataForLogging?.has_loop,
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
        why_this_step: updatedStep.why_this_step,
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

      await onEditStep(stepId, updates)
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
        if (step.if_true_steps) {
          const found = findStepById(step.if_true_steps, id)
          if (found) return found
        }
        if (step.if_false_steps) {
          const found = findStepById(step.if_false_steps, id)
          if (found) return found
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
    const isDisabled = isAlwaysEnabled 
      ? false 
      : (isValidation
          ? agentConfigs?.disable_validation === true
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
    
    return (
      <div className="absolute right-0 top-0 bottom-0 w-[600px] bg-white dark:bg-gray-900 border-l border-gray-200 dark:border-gray-700 shadow-xl z-50 flex flex-col">
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

  // At this point, node must be step/conditional/loop (validation/learning handled above)
  const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
  const step = stepData.step

  return (
    <div className="absolute right-0 top-0 bottom-0 w-[600px] bg-white dark:bg-gray-900 border-l border-gray-200 dark:border-gray-700 shadow-xl z-50 flex flex-col">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
        <div className="flex items-center gap-2">
          <span className="text-base font-semibold text-gray-900 dark:text-gray-100">
            {isEditing ? 'Edit Step' : `Step ${stepIndex + 1}`}
          </span>
        </div>
        <div className="flex items-center gap-1.5">
          {!isEditing && (
            <>
              {/* Phase Dropdown */}
              {phases.length > 0 && (
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
              
              {/* Run Step Button */}
              {onStartPhase && (
                <button
                  onClick={handleRunStep}
                  disabled={isRunning || !canRunStep}
                  className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium bg-green-600 hover:bg-green-700 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  title={
                    isRunning 
                      ? 'Execution in progress...' 
                      : !canRunStep 
                        ? 'Complete previous steps first' 
                        : 'Run this step only'
                  }
                >
                  <Play className="w-3.5 h-3.5" />
                  Run
                </button>
              )}
              {/* Delete Learnings Button */}
              {workspacePath && (
                <button
                  onClick={() => setShowDeleteLearningsConfirm(true)}
                  disabled={isRunning || isDeletingLearnings}
                  className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs font-medium bg-red-600 hover:bg-red-700 text-white rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                  title={
                    isRunning 
                      ? 'Execution in progress...' 
                      : isDeletingLearnings
                        ? 'Deleting learnings...'
                        : 'Delete learnings for this step'
                  }
                >
                  <Trash2 className="w-3.5 h-3.5" />
                  Delete Learnings
                </button>
              )}
              <button
                onClick={handleStartEdit}
                className="p-1.5 rounded-md hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
                title="Edit step"
              >
                <Edit2 className="w-4 h-4" />
              </button>
              <button
                onClick={() => setShowDeleteConfirm(true)}
                className="p-1.5 rounded-md hover:bg-red-100 dark:hover:bg-red-900/30 text-red-500 dark:text-red-400 transition-colors"
                title="Delete step"
              >
                <Trash2 className="w-4 h-4" />
              </button>
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
          {step.has_condition && node.type === 'conditional' ? (
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
                    <p className="text-sm text-gray-700 dark:text-gray-300 mt-2 whitespace-pre-wrap">
                      {step.condition_question}
                    </p>
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
                      <p className="text-sm text-gray-600 dark:text-gray-400 leading-relaxed whitespace-pre-wrap">
                        {step.description}
                      </p>
                    )}
                  </div>

                  {step.success_criteria && (
                    <div className="p-3 bg-green-50 dark:bg-green-900/20 rounded-lg border border-green-200 dark:border-green-800/50">
                      <span className="text-xs font-semibold text-green-700 dark:text-green-300 uppercase tracking-wide">
                        Success Criteria:
                      </span>
                      <p className="text-sm text-gray-700 dark:text-gray-300 mt-2 whitespace-pre-wrap">
                        {step.success_criteria}
                      </p>
                    </div>
                  )}
                </>
              )}

              {step.has_condition && step.condition_question && (
                <div className="p-2 bg-purple-50 dark:bg-purple-900/20 rounded">
                  <span className="text-xs font-medium text-purple-600 dark:text-purple-400">
                    Condition:
                  </span>
                  <p className="text-sm text-gray-700 dark:text-gray-300 mt-1">
                    {step.condition_question}
                  </p>
                </div>
              )}

              {step.has_loop && (
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
                  {step.max_iterations && (
                    <p className="text-xs font-medium text-gray-600 dark:text-gray-400 mt-2">
                      Max iterations: <span className="text-gray-700 dark:text-gray-300">{step.max_iterations}</span>
                    </p>
                  )}
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
            </div>
          )}

          {/* Step Configuration Panel */}
          {step.has_condition && node.type === 'conditional' ? (
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
          ) : (
            <div className="border-t border-gray-200 dark:border-gray-700 pt-4">
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
          node && (node.type === 'step' || node.type === 'conditional' || node.type === 'loop')
            ? (() => {
                const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
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
          node && (node.type === 'step' || node.type === 'conditional' || node.type === 'loop')
            ? (() => {
                const stepData = node.data as StepNodeData | ConditionalNodeData | LoopNodeData
                const stepTitle = stepData.step?.title || `Step ${stepIndex + 1}`
                return `Are you sure you want to delete all learnings for "${stepTitle}" (Step ${stepIndex + 1})? This will permanently delete the learnings folder at \`learnings/step-${stepIndex + 1}/\` and all its contents. This action cannot be undone.`
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

