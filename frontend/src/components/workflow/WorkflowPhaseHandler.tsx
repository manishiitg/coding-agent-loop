import React, { useState, useEffect } from 'react'
import { getWorkflowPhases } from '../../constants/workflow'
import { agentApi } from '../../services/api'
import type { WorkflowSelectedOption, WorkflowSelectedOptions } from '../../services/api-types'

interface WorkflowPhaseHandlerProps {
  phase: string
  presetQueryId?: string
  onStateChange?: (newPhase: string) => void
}

export const WorkflowPhaseHandler: React.FC<WorkflowPhaseHandlerProps> = ({
  phase,
  presetQueryId,
  onStateChange,
}) => {

  const [allPhases, setAllPhases] = useState<Array<{ id: string; title: string; description: string }>>([])
  const [selectedOptions, setSelectedOptions] = useState<Record<string, string>>({}) // group -> optionId

  // Load all phases for state overview
  useEffect(() => {
    const loadPhases = async () => {
      try {
        const allPhasesData = await getWorkflowPhases()
        setAllPhases(allPhasesData)
      } catch (error) {
        console.error('[WORKFLOW_PHASE_HANDLER] Failed to load phases:', error)
      }
    }

    loadPhases()
  }, [])

  // Load current workflow's selected options
  useEffect(() => {
    const loadWorkflowOptions = async () => {
      if (!presetQueryId) return
      
      try {
        const workflowStatus = await agentApi.getWorkflowStatus(presetQueryId)
        if (workflowStatus.workflow?.selected_options) {
          // Convert selected_options to the format expected by the component
          const savedOptions: Record<string, string> = {}
          workflowStatus.workflow.selected_options.selections.forEach(selection => {
            savedOptions[selection.group] = selection.option_id
          })
          setSelectedOptions(savedOptions)
        }
      } catch (error) {
        console.error('[WORKFLOW_PHASE_HANDLER] Failed to load workflow options:', error)
      }
    }

    loadWorkflowOptions()
  }, [presetQueryId])

  // Only render the workflow states overview
  return (
    <WorkflowStatesOverview 
      allPhases={allPhases} 
      currentPhase={phase} 
      presetQueryId={presetQueryId || ''}
      selectedOptions={selectedOptions}
      setSelectedOptions={setSelectedOptions}
      onStateChange={onStateChange}
    />
  )
}

// Workflow States Overview Component
const WorkflowStatesOverview: React.FC<{
  allPhases: Array<{ id: string; title: string; description: string; options?: Array<{ id: string; label: string; description: string; group: string; default: boolean }> }>
  currentPhase: string
  presetQueryId: string
  selectedOptions: Record<string, string>
  setSelectedOptions: (options: Record<string, string>) => void
  onStateChange?: (newPhase: string) => void
}> = ({ allPhases, currentPhase, presetQueryId, selectedOptions, setSelectedOptions, onStateChange }) => {
  const [isUpdating, setIsUpdating] = useState(false)
  const [isSavingOptions, setIsSavingOptions] = useState(false)

  const handleStateChange = async (newPhaseId: string) => {
    if (newPhaseId === currentPhase || isUpdating) return

    // Find the phase to check if it has options
    const phase = allPhases.find(p => p.id === newPhaseId)
    
    if (phase?.options && phase.options.length > 0) {
      // Set default selections for each group if not already set
      const currentSelections = { ...selectedOptions }
      let hasNewSelections = false
      
      phase.options.forEach(option => {
        if (option.default && !currentSelections[option.group]) {
          currentSelections[option.group] = option.id
          hasNewSelections = true
        }
      })
      
      // If no defaults, set first option from each group
      if (!hasNewSelections) {
        const grouped = getGroupedOptions(newPhaseId)
        Object.entries(grouped).forEach(([group, options]) => {
          if (options.length > 0 && !currentSelections[group]) {
            currentSelections[group] = options[0].id
            hasNewSelections = true
          }
        })
      }
      
      if (hasNewSelections) {
        setSelectedOptions(currentSelections)
      }
      
      // Proceed with current selections
      await executeStateChange(newPhaseId, currentSelections)
      return
    }

    // No options, proceed directly
    await executeStateChange(newPhaseId, null)
  }

  const executeStateChange = async (phaseId: string, selectedOptionsMap: Record<string, string> | null) => {
    try {
      setIsUpdating(true)
      // Changing state
      
      // Create selected options object if selections are provided
      let selectedOptions: WorkflowSelectedOptions | null = null
      if (selectedOptionsMap && Object.keys(selectedOptionsMap).length > 0) {
        const phase = allPhases.find(p => p.id === phaseId)
        const selections: WorkflowSelectedOption[] = []
        
        for (const [group, optionId] of Object.entries(selectedOptionsMap)) {
          const option = phase?.options?.find(o => o.id === optionId && o.group === group)
          if (option) {
            selections.push({
              option_id: option.id,
              option_label: option.label,
              option_value: option.id,
              group: group,
              phase_id: phaseId
            })
          }
        }
        
        if (selections.length > 0) {
          selectedOptions = {
            phase_id: phaseId,
            selections: selections
          }
        }
      }
      
      // Call the update API to change workflow status with selected options
      await agentApi.updateWorkflow(presetQueryId, phaseId, selectedOptions)
      // State updated successfully
      
      // Notify parent component of state change
      onStateChange?.(phaseId)
    } catch (error) {
      console.error('[WORKFLOW_STATES] Failed to update state:', error)
    } finally {
      setIsUpdating(false)
    }
  }

  const handleOptionChange = async (group: string, optionId: string) => {
    const newSelections = {
      ...selectedOptions,
      [group]: optionId
    }
    setSelectedOptions(newSelections)
    
    // Auto-save when options change for current phase
    if (currentPhase) {
      try {
        setIsSavingOptions(true)
        await executeStateChange(currentPhase, newSelections)
      } finally {
        setIsSavingOptions(false)
      }
    }
  }

  const getGroupedOptions = (phaseId: string) => {
    const phase = allPhases.find(p => p.id === phaseId)
    if (!phase?.options) return {}
    
    const grouped: Record<string, Array<{ id: string; label: string; description: string; group: string; default: boolean }>> = {}
    phase.options.forEach(option => {
      if (!grouped[option.group]) {
        grouped[option.group] = []
      }
      grouped[option.group].push({
        id: option.id,
        label: option.label,
        description: option.description,
        group: option.group,
        default: option.default
      })
    })
    return grouped
  }


  if (allPhases.length === 0) {
    return (
      <div className="mt-4 p-4 bg-muted/30 border border-border rounded-lg">
        <div className="flex items-center gap-2 mb-3">
          <div className="w-1.5 h-1.5 bg-muted-foreground rounded-full"></div>
          <span className="text-xs font-medium text-foreground">Workflow States</span>
        </div>
        <div className="text-xs text-muted-foreground">
          Loading workflow states...
        </div>
      </div>
    )
  }

  return (
    <div className="mt-4 p-4 bg-muted/30 border border-border rounded-lg">
      <div className="flex items-center gap-2 mb-3">
        <div className="w-1.5 h-1.5 bg-muted-foreground rounded-full"></div>
        <span className="text-xs font-medium text-foreground">Available Workflow States</span>
        {isUpdating && (
          <div className="w-3 h-3 border-2 border-muted-foreground border-t-primary rounded-full animate-spin"></div>
        )}
      </div>
      
      <div className="space-y-3">
        {allPhases.map((phase) => (
          <div
            key={phase.id}
            className={`p-3 rounded border transition-colors ${
              phase.id === currentPhase
                ? 'bg-primary/10 border-primary/20'
                : 'bg-card border-border/50'
            }`}
          >
            <div className="flex items-start gap-2 mb-3">
              <div className={`w-1.5 h-1.5 rounded-full mt-1.5 ${
                phase.id === currentPhase ? 'bg-primary' : 'bg-muted-foreground/50'
              }`}></div>
              <div className="flex-1 min-w-0">
                <div className={`text-sm font-medium ${
                  phase.id === currentPhase ? 'text-primary' : 'text-foreground'
                }`}>
                  {phase.title}
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  {phase.description}
                </div>
              </div>
              {phase.id === currentPhase && (
                <div className="text-xs text-primary font-medium">Current</div>
              )}
            </div>
            
            
            {/* Options for current phase */}
            {phase.id === currentPhase && phase.options && phase.options.length > 0 && (
              <div className="ml-4 space-y-3">
                {Object.entries(getGroupedOptions(phase.id)).map(([groupName, options]) => (
                  <div key={groupName} className="space-y-2">
                    <label className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                      {groupName.replace(/_/g, ' ')}
                    </label>
                    <select
                      value={selectedOptions[groupName] || ''}
                      onChange={(e) => handleOptionChange(groupName, e.target.value)}
                      disabled={isSavingOptions}
                      className="w-full p-2 text-xs border border-border rounded bg-background focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50 disabled:cursor-not-allowed"
                    >
                      {options.map((option) => (
                        <option key={option.id} value={option.id}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                    <div className="text-xs text-muted-foreground flex items-center gap-2">
                      {selectedOptions[groupName] 
                        ? options.find(opt => opt.id === selectedOptions[groupName])?.description
                        : 'Select an option to see description'
                      }
                      {isSavingOptions && (
                        <div className="w-3 h-3 border-2 border-muted-foreground border-t-primary rounded-full animate-spin"></div>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
            
            {/* Switch button for non-current phases */}
            {phase.id !== currentPhase && (
              <button
                onClick={() => handleStateChange(phase.id)}
                disabled={isUpdating}
                className="mt-2 px-2 py-1 text-xs bg-primary text-primary-foreground rounded hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isUpdating ? 'Switching...' : 'Switch to this phase'}
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}
