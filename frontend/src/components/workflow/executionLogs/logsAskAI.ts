/** Context-aware Ask AI messages for execution logs. Pure builders: the same
 *  inputs always produce the same message, so the chat agent lands on the
 *  exact run, step, and error the user is looking at. */

export interface LogsFailedStep {
  title: string
  error?: string
}

export interface LogsRunAskInput {
  runFolder: string
  folderLabel: string
  stepCount: number
  failedSteps: LogsFailedStep[]
}

export interface LogsStepAskInput {
  runFolder: string
  folderLabel: string
  stepTitle: string
  status: 'completed' | 'failed' | 'running' | 'pending'
  duration?: string
  tokens?: string
  preview?: string
  error?: string
}

export interface LogsErrorAskInput {
  runFolder: string
  folderLabel: string
  stepTitle: string
  error: string
}

const excerpt = (text: string, maxLength = 500): string => {
  const normalized = text.replace(/\s+/g, ' ').trim()
  return normalized.length > maxLength ? `${normalized.slice(0, maxLength).trimEnd()}…` : normalized
}

const statusWord = (status: LogsStepAskInput['status']): string => {
  switch (status) {
    case 'completed': return 'completed'
    case 'failed': return 'failed'
    case 'running': return 'still running'
    case 'pending': return 'has not run yet'
  }
}

export function buildRunAskMessage({ runFolder, folderLabel, stepCount, failedSteps }: LogsRunAskInput): string {
  const run = `${folderLabel} (${runFolder})`
  if (stepCount === 0) {
    return `Run ${run} has no step logs. Help me figure out why nothing was recorded for this run.`
  }
  if (failedSteps.length === 0) {
    return `Summarize run ${run}: what each of its ${stepCount} steps did, and whether anything looks unusual.`
  }
  const names = failedSteps.map(step => `"${step.title}"`).join(', ')
  const firstError = failedSteps.find(step => step.error?.trim())?.error
  return `Help me investigate run ${run}. ${failedSteps.length} of ${stepCount} steps failed: ${names}.` +
    (firstError ? ` First error: ${excerpt(firstError)}.` : '') +
    ' Explain what went wrong and suggest a fix.'
}

export function buildStepAskMessage({ folderLabel, stepTitle, status, duration, tokens, preview, error }: LogsStepAskInput): string {
  const facts = [
    `step "${stepTitle}" ${statusWord(status)}`,
    duration ? `took ${duration}` : '',
    tokens ? `used ${tokens}` : '',
  ].filter(Boolean).join(', ')
  const evidence = error?.trim() ? ` Error: ${excerpt(error)}.` : preview?.trim() ? ` Latest output: ${excerpt(preview, 300)}.` : ''
  return `In run ${folderLabel}, ${facts}.${evidence} Explain what happened and whether I should worry.`
}

export function buildErrorAskMessage({ folderLabel, stepTitle, error }: LogsErrorAskInput): string {
  return `In run ${folderLabel}, the step "${stepTitle}" hit this error: ${excerpt(error)}. Explain in plain words what caused it and how to fix it.`
}

export function buildEmptyAskMessage(): string {
  return 'I do not see any execution runs. Help me run this workflow so logs appear.'
}
