/**
 * Workflow Constants
 * 
 * Note: Workflow phases are managed by useWorkflowStore.
 * Use the store for accessing phases data.
 * 
 * Example:
 *   const { phases, loadPhases } = useWorkflowStore()
 *   useEffect(() => { loadPhases() }, [loadPhases])
 * 
 *   // Or for synchronous access after initialization:
 *   useWorkflowStore.getState().getDefaultPhase()
 */

// Workflow status messages (static constants)
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

// Type definitions - workflow phase is now a string (dynamic from backend)
export type WorkflowPhase = string
export type WorkflowStatus = string

// Execution phase ID constant
export const EXECUTION_PHASE_ID = 'execution'
