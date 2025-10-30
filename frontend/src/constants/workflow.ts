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

// Legacy constants for backward compatibility (will be deprecated)
export const WORKFLOW_PHASES = {
  PRE_VERIFICATION: 'pre-verification',
  POST_VERIFICATION: 'post-verification',
  POST_VERIFICATION_TODO_REFINEMENT: 'post-verification-todo-refinement'
} as const


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

// Type definitions based on constants
export type WorkflowPhase = typeof WORKFLOW_PHASES[keyof typeof WORKFLOW_PHASES]
export type WorkflowStatus = typeof WORKFLOW_PHASES[keyof typeof WORKFLOW_PHASES] // Same as WorkflowPhase
