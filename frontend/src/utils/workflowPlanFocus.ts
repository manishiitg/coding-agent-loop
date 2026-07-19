export const WORKFLOW_PLAN_STEP_FOCUS_EVENT = 'workflow-plan-step-focus'

export interface WorkflowPlanStepFocusDetail {
  stepId: string
  workspacePath?: string | null
}

export function requestWorkflowPlanStepFocus(detail: WorkflowPlanStepFocusDetail): void {
  if (typeof window === 'undefined') return
  window.dispatchEvent(new CustomEvent<WorkflowPlanStepFocusDetail>(WORKFLOW_PLAN_STEP_FOCUS_EVENT, { detail }))
}
