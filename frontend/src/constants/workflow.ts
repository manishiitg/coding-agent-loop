import type { WorkflowConstants, WorkflowPhase as APIWorkflowPhase } from '../services/api-types'

// Dynamic workflow constants - will be loaded from backend
let workflowConstants: WorkflowConstants | null = null

// Function to load workflow constants from backend
export const loadWorkflowConstants = async (): Promise<WorkflowConstants> => {
  if (workflowConstants) {
    return workflowConstants
  }

  try {
    const { agentApi } = await import('../services/api')
    const response = await agentApi.getWorkflowConstants()
    if (response.success) {
      workflowConstants = response.constants
      return workflowConstants
    } else {
      throw new Error(response.message || 'Failed to load workflow constants')
    }
  } catch (error) {
    console.error('[WORKFLOW_CONSTANTS] Failed to load workflow constants:', error)
    // Return empty constants if API fails
    return {
      phases: []
    }
  }
}

// Helper functions to get constants
export const getWorkflowPhases = async (): Promise<APIWorkflowPhase[]> => {
  const constants = await loadWorkflowConstants()
  return constants.phases
}

export const getWorkflowPhaseById = async (id: string): Promise<APIWorkflowPhase | undefined> => {
  const phases = await getWorkflowPhases()
  return phases.find(phase => phase.id === id)
}

// Workflow status messages
export const WORKFLOW_MESSAGES = {
  CHECKING_STATUS: 'Checking workflow status for preset:',
  WORKFLOW_APPROVED: 'Workflow already approved, skipping to execution',
  WORKFLOW_OBJECTIVE: 'Workflow objective:',
  CALLING_SUBMIT: 'Calling onWorkflowSubmit with objective:',
  WORKFLOW_NOT_APPROVED: 'Workflow exists but not approved, showing for re-approval',
  NO_WORKFLOW_EXISTS: 'No workflow exists, proceeding with objective input',
  ERROR_CHECKING_STATUS: 'Error checking workflow status:',
  CLEARED_STATE: 'Cleared all workflow state'
} as const

// Helper to get the first phase ID from backend (used as default)
export const getDefaultWorkflowPhase = async (): Promise<string> => {
  try {
    const phases = await getWorkflowPhases()
    return phases.length > 0 ? phases[0].id : 'variable-extraction'
  } catch (error) {
    console.error('[WORKFLOW] Failed to get default phase:', error)
    return 'variable-extraction' // Fallback
  }
}

// Type definitions - workflow phase is now a string (dynamic from backend)
export type WorkflowPhase = string
export type WorkflowStatus = string
