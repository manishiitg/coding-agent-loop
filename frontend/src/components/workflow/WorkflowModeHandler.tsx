import React, { useState, useEffect, useCallback, forwardRef, useImperativeHandle } from 'react'
import { WorkflowPresetSelector } from './WorkflowPresetSelector'
import { WorkflowPhaseHandler } from './WorkflowPhaseHandler'
import { agentApi } from '../../services/api'
import { WORKFLOW_PHASES, type WorkflowPhase } from '../../constants/workflow'
import { useAppStore, useChatStore } from '../../stores'
import { usePresetApplication } from '../../stores/useGlobalPresetStore'

interface Preset {
  id: string
  name: string
  description: string
}

interface WorkflowModeHandlerProps {
  // Callbacks and children only
  onPresetSelected: (presetId: string, presetContent: string) => void
  onPresetCleared: () => void
  children: React.ReactNode
  onWorkflowPhaseChange?: (phase: WorkflowPhase) => void  // Callback to update phase in ChatArea
}

export interface WorkflowModeHandlerRef {
  handleWorkflowQuery: (query: string) => Promise<{ objective: string; workflowId: string } | void>
  refreshPresets: () => Promise<void>
}

export const WorkflowModeHandler = forwardRef<WorkflowModeHandlerRef, WorkflowModeHandlerProps>(({
  onPresetSelected,
  onPresetCleared,
  children,
  onWorkflowPhaseChange
}, ref) => {

  // Store subscriptions
  const { agentMode } = useAppStore()
  const { 
    currentWorkflowPhase
  } = useChatStore()
  
  const { getActivePreset } = usePresetApplication()
  
  // Get active preset for workflow mode
  const activeWorkflowPreset = getActivePreset('workflow')
  const selectedWorkflowPreset = activeWorkflowPreset?.id || null
  
  const [availablePresets, setAvailablePresets] = useState<Preset[]>([])
  const [hasAttemptedLoad, setHasAttemptedLoad] = useState<boolean>(false)
  const [isCreatingWorkflow, setIsCreatingWorkflow] = useState<boolean>(false)

  // Use external state from ChatArea
  const currentPhase = currentWorkflowPhase

  // Load presets function - can be called multiple times
  const loadPresets = useCallback(async () => {
    try {
      const response = await agentApi.getPresetQueries(50, 0)
      const presets = response.presets.map((preset: {id: string, label: string, query: string}) => ({
        id: preset.id,
        name: preset.label,
        description: preset.query || 'No description available'
      }))
      setAvailablePresets(presets)
    } catch (error) {
      console.error('[WORKFLOW] Failed to load presets:', error)
    }
  }, [])

  // Refresh presets function - exposed through ref
  const refreshPresets = useCallback(async () => {
    await loadPresets()
  }, [loadPresets])

  // Load presets when workflow mode is selected
  useEffect(() => {
    if (agentMode === 'workflow' && !hasAttemptedLoad) {
      setHasAttemptedLoad(true)
      loadPresets()
    }
  }, [agentMode, hasAttemptedLoad, loadPresets])

  // Reset workflow preset when switching away from workflow mode
  useEffect(() => {
    if (agentMode !== 'workflow') {
      onPresetCleared()
      setHasAttemptedLoad(false)
      setAvailablePresets([])
      onWorkflowPhaseChange?.(WORKFLOW_PHASES.PRE_VERIFICATION)
      setIsCreatingWorkflow(false)
    }
  }, [agentMode, onPresetCleared, onWorkflowPhaseChange])

  // Handle preset restoration when switching to workflow mode
  useEffect(() => {
    if (agentMode === 'workflow' && selectedWorkflowPreset) {
      // Find the preset in available presets
      const selectedPreset = availablePresets.find(p => p.id === selectedWorkflowPreset)
      if (selectedPreset) {
        onPresetSelected(selectedWorkflowPreset, selectedPreset.description)
        onWorkflowPhaseChange?.(WORKFLOW_PHASES.PRE_VERIFICATION)
      } else {
        // If preset not found, load presets first
        if (!hasAttemptedLoad) {
          setHasAttemptedLoad(true)
          
          const loadPresets = async () => {
            try {
              const response = await agentApi.getPresetQueries(50, 0)
              const presets = response.presets.map((preset: {id: string, label: string, query: string}) => ({
                id: preset.id,
                name: preset.label,
                description: preset.query || 'No description available'
              }))
              setAvailablePresets(presets)
              
              // Now try to find the selected preset
              const foundPreset = presets.find(p => p.id === selectedWorkflowPreset)
              if (foundPreset) {
                onPresetSelected(selectedWorkflowPreset, foundPreset.description)
                onWorkflowPhaseChange?.(WORKFLOW_PHASES.PRE_VERIFICATION)
              }
            } catch (error) {
              console.error('[WORKFLOW] Failed to load presets:', error)
            }
          }
          
          loadPresets()
        }
      }
    }
  }, [agentMode, selectedWorkflowPreset, availablePresets, hasAttemptedLoad, onPresetSelected, onWorkflowPhaseChange])

  // Step 1: Create workflow with objective (generates todo list)
  const handleObjectiveSubmit = useCallback(async (objective: string) => {
    if (!selectedWorkflowPreset) return

    try {
      setIsCreatingWorkflow(true)

      // Create workflow - this generates the todo list
      const createResponse = await agentApi.createWorkflow(selectedWorkflowPreset, true)
      // Workflow created

      if (createResponse.workflow?.id) {
        // For workflow mode, we need to use the normal agent execution flow
        // The backend will handle the workflow Deep Search execution
        // We'll return the objective so ChatArea can submit it as a normal query
        // Workflow created, transitioning to planning phase
        onWorkflowPhaseChange?.(WORKFLOW_PHASES.PRE_VERIFICATION)

        // Return the objective so ChatArea can submit it as a normal agent query
        return { objective, workflowId: createResponse.workflow.id }
      }
    } catch (error) {
      console.error('[WORKFLOW] Error creating workflow:', error)
      // Reset to objective input phase on error
      onWorkflowPhaseChange?.(WORKFLOW_PHASES.PRE_VERIFICATION)
    } finally {
      setIsCreatingWorkflow(false)
    }
  }, [selectedWorkflowPreset, onWorkflowPhaseChange])


  // Handle state change from WorkflowStatesOverview
  const handleStateChange = useCallback((newPhase: string) => {
    // State change requested
    onWorkflowPhaseChange?.(newPhase as WorkflowPhase)
  }, [onWorkflowPhaseChange])



  // Handle chat input submission based on current phase
  const handleChatSubmit = useCallback(async (query: string) => {
    // handleChatSubmit called

    if (currentPhase === WORKFLOW_PHASES.PRE_VERIFICATION) {
      // Calling handleObjectiveSubmit
      const result = await handleObjectiveSubmit(query)
      // handleObjectiveSubmit result
      return result // Return the result so ChatArea can access observer info
    } else {
      // No handler for phase
    }
  }, [currentPhase, handleObjectiveSubmit])

  // Expose workflow handler through ref
  useImperativeHandle(ref, () => ({
    handleWorkflowQuery: handleChatSubmit,
    refreshPresets: refreshPresets
  }), [handleChatSubmit, refreshPresets])


  // Show workflow components when in workflow mode
  if (agentMode === 'workflow') {
    return (
      <>
        {selectedWorkflowPreset && (
          <WorkflowPresetSelector
            selectedPresetId={selectedWorkflowPreset}
            availablePresets={availablePresets}
            isCreatingWorkflow={isCreatingWorkflow}
          />
        )}
        {selectedWorkflowPreset && (
          <WorkflowPhaseHandler
            phase={currentPhase || WORKFLOW_PHASES.PRE_VERIFICATION}
            presetQueryId={selectedWorkflowPreset}
            onStateChange={handleStateChange}
          />
        )}
        {children}
      </>
    )
  }

  // For non-workflow modes, just render children
  // Rendering children only
  return <>{children}</>
})

WorkflowModeHandler.displayName = 'WorkflowModeHandler'