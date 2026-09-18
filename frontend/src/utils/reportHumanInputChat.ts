import type { ReportHumanInput } from '../services/api-types'
import type { WorkspacePaneChatResult } from './workspacePaneChat'
import { sendWorkspacePaneMessageToChat } from './workspacePaneChat'

function sourceName(source: string): string {
  if (['technical_review', 'engineering_review', 'ops_review'].includes(source)) return 'Technical Review'
  if (['strategic_review', 'strategy_auditor', 'goal_advisor'].includes(source)) return 'Strategic Review'
  if (source === 'architecture_review') return 'Architecture Review'
  if (source === 'plan_drift_review') return 'Plan Drift Review'
  return 'Pulse'
}

export function buildReportHumanInputChatMessage(
  input: ReportHumanInput,
  workspacePath: string,
  userQuestion: string,
): string {
  const lines = [
    `I want to discuss a pending ${sourceName(input.source)} decision. Do not submit, dismiss, or mark the decision handled yet; answer my question first.`,
    'If I later explicitly choose an option or give a final answer, call answer_human_input_request with the exact IDs below. Record it as answered only; do not mark it consumed.',
    'This is a Needs your decision card in the pulse workspace view. Read its current status with get_human_input_request. After my final answer, save it in that turn without waiting for a separate request to mark it; the card refreshes automatically after a successful save.',
    '',
    `Automation: ${workspacePath}`,
    `Decision ID: ${input.id}`,
    '',
    'Decision:',
    input.question.trim(),
  ]

  if (input.context?.trim()) {
    lines.push('', 'Context:', input.context.trim())
  }
  if (input.options.length > 0) {
    lines.push('', 'Available options:')
    input.options.forEach((option, index) => {
      lines.push(`${index + 1}. ${option.title} [option_id=${option.id}]${option.description ? ` — ${option.description}` : ''}`)
    })
  }
  if (input.evidence?.trim()) {
    lines.push('', `Evidence: ${input.evidence.trim()}`)
  }

  lines.push('', 'My question:', userQuestion.trim())
  return lines.join('\n')
}

/**
 * The user can explicitly delegate one pending decision to the workflow chat.
 * This is intentionally a distinct instruction from "Ask in chat": the latter
 * is advisory and must never answer the decision, while this one authorizes the
 * agent to choose a listed option and carry out the resulting safe workflow
 * work after it has considered the durable evidence.
 */
export function buildReportHumanInputDelegatedActionMessage(
  input: ReportHumanInput,
  workspacePath: string,
): string {
  const lines = [
    `I delegate this pending ${sourceName(input.source)} decision to you. Analyze the current evidence, workflow goal, constraints, and the available options; choose the best supported option and take the resulting safe workflow action.`,
    'Do not ask me to choose between the listed options. Use current evidence and tools to resolve uncertainty where practical. If no option is defensible, do not invent one or take an unsafe action: explain the blocker and leave the decision pending.',
    'After choosing, call answer_human_input_request with the exact decision and option IDs below. Then implement only the authorized workflow action, verify it proportionately, and report the decision, evidence, action, and remaining risk concisely. Do not mark the decision consumed yourself.',
    '',
    `Automation: ${workspacePath}`,
    `Decision ID: ${input.id}`,
    '',
    'Decision:',
    input.question.trim(),
  ]

  if (input.context?.trim()) {
    lines.push('', 'Context:', input.context.trim())
  }
  if (input.options.length > 0) {
    lines.push('', 'Available options:')
    input.options.forEach((option, index) => {
      lines.push(`${index + 1}. ${option.title} [option_id=${option.id}]${option.description ? ` — ${option.description}` : ''}`)
    })
  }
  if (input.evidence?.trim()) {
    lines.push('', `Evidence: ${input.evidence.trim()}`)
  }

  return lines.join('\n')
}

export async function sendReportHumanInputQuestionToChat({
  input,
  workspacePath,
  userQuestion,
}: {
  input: ReportHumanInput
  workspacePath: string
  userQuestion: string
}): Promise<WorkspacePaneChatResult> {
  const question = userQuestion.trim()
  if (!question) throw new Error('Write a question before opening chat.')
  return sendWorkspacePaneMessageToChat({
    workspacePath,
    message: buildReportHumanInputChatMessage(input, workspacePath, question),
  })
}

export async function delegateReportHumanInputActionToChat({
  input,
  workspacePath,
}: {
  input: ReportHumanInput
  workspacePath: string
}): Promise<WorkspacePaneChatResult> {
  return sendWorkspacePaneMessageToChat({
    workspacePath,
    message: buildReportHumanInputDelegatedActionMessage(input, workspacePath),
  })
}
