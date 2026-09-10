import type { ReportHumanInput } from '../services/api-types'
import type { ChatTab, EventViewMode } from '../stores/useChatStore'
import { useChatStore } from '../stores/useChatStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { activateTab } from './activateTab'
import { selectWorkflowPreset } from './workflowNavigation'

function normalizeWorkspacePath(value?: string | null): string {
  return (value || '').trim().replace(/^\/+|\/+$/g, '').toLowerCase()
}

function tabRecency(tab: ChatTab): number {
  return tab.lastAccessedAt ?? tab.createdAt ?? 0
}

function isInteractiveWorkflowTab(tab: ChatTab, presetId: string): boolean {
  return tab.metadata?.mode === 'workflow' &&
    tab.metadata?.presetQueryId === presetId &&
    tab.metadata?.isViewOnly !== true &&
    tab.metadata?.isScheduledRun !== true &&
    tab.metadata?.isBotRun !== true
}

/**
 * Pick the conversational lane for a Pulse question. Read-only schedule/bot
 * tabs are deliberately excluded so asking a question can never take over the
 * schedule observer session.
 */
export function selectReportDiscussionTab(
  tabs: Record<string, ChatTab>,
  target: { mode: 'workflow'; presetId: string },
  activeTabId?: string | null,
): ChatTab | undefined {
  const candidates = Object.values(tabs).filter(tab => isInteractiveWorkflowTab(tab, target.presetId))

  return candidates.sort((left, right) => {
    const leftRunning = left.isStreaming || left.hasRunningBgAgents
    const rightRunning = right.isStreaming || right.hasRunningBgAgents
    if (leftRunning !== rightRunning) return leftRunning ? -1 : 1
    if ((left.tabId === activeTabId) !== (right.tabId === activeTabId)) {
      return left.tabId === activeTabId ? -1 : 1
    }
    const leftBuilder = left.metadata?.phaseId === 'workflow-builder'
    const rightBuilder = right.metadata?.phaseId === 'workflow-builder'
    if (leftBuilder !== rightBuilder) return leftBuilder ? -1 : 1
    return tabRecency(right) - tabRecency(left)
  })[0]
}

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

export type ReportHumanInputChatResult = {
  tabId: string
  reused: boolean
  queuedBehindRunningTurn: boolean
}

async function findWorkflowPreset(workspacePath: string) {
  const find = () => {
    const normalizedTarget = normalizeWorkspacePath(workspacePath)
    return useGlobalPresetStore.getState().workflowPresets.find(preset =>
      normalizeWorkspacePath(preset.selectedFolder?.filepath) === normalizedTarget)
  }

  let preset = find()
  if (!preset) {
    await useGlobalPresetStore.getState().refreshPresets()
    preset = find()
  }
  return preset
}

/**
 * Open the appropriate interactive chat and enqueue a contextual question.
 * Enqueueing uses the same durable per-tab lane as ChatInput: idle/new chats
 * send immediately when ChatArea mounts, while a running chat keeps the message
 * queued for its next turn. Scheduled runs remain independent and untouched.
 */
export async function sendWorkflowMessageToChat({
  workspacePath,
  message,
  newChat = false,
  viewMode = 'terminal',
}: {
  workspacePath: string
  message: string
  /** Explicit user choice; otherwise reuse the usual Ask in chat lane. */
  newChat?: boolean
  /** Pulse's original "Ask in chat" wants the raw terminal visible (the
   *  underlying coding-agent CLI working live is the point) — that's the
   *  default. A caller whose message is a normal conversational ask, not
   *  something to watch execute, should pass 'formatted' instead. */
  viewMode?: EventViewMode
}): Promise<ReportHumanInputChatResult> {
  if (!message.trim()) throw new Error('Write a message before opening chat.')
  let targetTab: ChatTab | undefined
  let tabId: string
  let reused = false

  const preset = await findWorkflowPreset(workspacePath)
  if (!preset) throw new Error(`Could not find the automation for ${workspacePath}.`)

  if (!selectWorkflowPreset(preset)) throw new Error('Failed to open the automation.')

  const latestChatStore = useChatStore.getState()
  targetTab = newChat ? undefined : selectReportDiscussionTab(
    latestChatStore.chatTabs,
    { mode: 'workflow', presetId: preset.id },
    latestChatStore.activeTabId,
  )
  if (targetTab) {
    tabId = targetTab.tabId
    reused = true
  } else {
    tabId = await latestChatStore.createChatTab('Automation Builder', {
      mode: 'workflow',
      phaseId: 'workflow-builder',
      phaseName: 'Automation Builder',
      presetQueryId: preset.id,
    })
    targetTab = useChatStore.getState().getTab(tabId)
  }

  if (!targetTab) throw new Error('Failed to open a chat for this question.')

  // Background agents do not block a new foreground turn; ChatArea only holds
  // the queue while the foreground tab itself is streaming.
  const queuedBehindRunningTurn = targetTab.isStreaming
  const finalChatStore = useChatStore.getState()
  const existingQueue = finalChatStore.getTabConfig(tabId)?.queuedMessages || []
  finalChatStore.setTabConfig(tabId, {
    // An external report request must not erase an unsent draft in this chat.
    queuedMessages: [...existingQueue, message],
  })
  finalChatStore.setTabViewMode(tabId, viewMode)
  finalChatStore.setAutoScroll(true)
  activateTab(tabId)

  if (targetTab.metadata?.mode === 'workflow') {
    const workflowStore = useWorkflowStore.getState()
    workflowStore.setShowChatArea(true)
    workflowStore.setShowWorkspacePane(true)
    workflowStore.setFocusedPane('chat')
  }

  window.setTimeout(() => {
    window.dispatchEvent(new CustomEvent('chat-scroll-to-bottom'))
  }, 50)

  return { tabId, reused, queuedBehindRunningTurn }
}

export async function sendReportHumanInputQuestionToChat({
  input,
  workspacePath,
  userQuestion,
}: {
  input: ReportHumanInput
  workspacePath: string
  userQuestion: string
}): Promise<ReportHumanInputChatResult> {
  const question = userQuestion.trim()
  if (!question) throw new Error('Write a question before opening chat.')
  return sendWorkflowMessageToChat({
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
}): Promise<ReportHumanInputChatResult> {
  return sendWorkflowMessageToChat({
    workspacePath,
    message: buildReportHumanInputDelegatedActionMessage(input, workspacePath),
  })
}
