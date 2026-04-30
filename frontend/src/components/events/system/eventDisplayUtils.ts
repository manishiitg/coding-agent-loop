type AgentEventLike = {
  agent_type?: string
  component?: string
  correlation_id?: string
  input_data?: {
    [k: string]: string
  }
}

const truthyEvaluationValues = new Set([
  '1',
  'true',
  'yes',
  'eval',
  'evaluation',
  'full-evaluation',
  'evaluation-scoring',
])

function normalized(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function hasEvaluationMarker(value?: string): boolean {
  const v = normalized(value)
  return v.includes('workshop-eval') || v.includes('evaluation')
}

export function isEvaluationAgentEvent(event: AgentEventLike): boolean {
  const inputData = event.input_data || {}
  const agentType = normalized(event.agent_type)

  if (agentType === 'evaluation_scoring' || agentType === 'todo_planner_evaluation_debugger') {
    return true
  }

  const explicitValues = [
    inputData.IsEvaluationMode,
    inputData.isEvaluationMode,
    inputData.is_evaluation_mode,
    inputData.workshop_mode,
    inputData.execution_type,
    inputData.agent_mode,
    inputData.phase,
  ]
  if (explicitValues.some(value => truthyEvaluationValues.has(normalized(value)))) {
    return true
  }

  return [
    event.component,
    event.correlation_id,
    inputData.RunFolder,
    inputData.WorkspacePath,
    inputData.StepExecutionPath,
  ].some(hasEvaluationMarker)
}
