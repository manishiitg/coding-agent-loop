import type { TerminalSnapshot } from '../services/api-types'

export type TerminalRailSection = 'active' | 'attention' | 'workflow' | 'review' | 'other'

export interface TerminalRailLogicalGroup {
  key: string
  title: string
  section: TerminalRailSection
  representative: TerminalSnapshot
  terminals: TerminalSnapshot[]
}

interface OrganizeTerminalRailOptions {
  getState: (terminal: TerminalSnapshot) => string
  isMainAgent: (terminal: TerminalSnapshot) => boolean
}

const REVIEW_LABEL_PATTERN = /\b(review(?:er)?|critic|advisor|audit|pulse|health|harden|maintenance)\b/i
const MESSAGE_SEQUENCE_PATTERN = /^message[-_ ]sequence(?:[-_ ].*)?$/i

function humanize(value?: string): string {
  if (!value) return ''
  const cleaned = value
    .replace(/^todo[-_:]sub[-_:]step[-_:]\d+[-_:]sub[-_:]/i, '')
    .replace(/^step[-_:]\d+[-_:](?:sub|execution)[-_:]/i, '')
    .replace(/^exec[-_:]/i, '')
    .replace(/^main[-_:]/i, '')
    .replace(/[-_]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  if (!cleaned) return ''
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}

export function terminalRailTitle(terminal: TerminalSnapshot): string {
  const preferred = terminal.step_name || terminal.display_title || terminal.agent_name || terminal.step_id || terminal.label
  if (preferred && MESSAGE_SEQUENCE_PATTERN.test(preferred) && terminal.parent_step_id) {
    return `${humanize(terminal.parent_step_id)} sequence`
  }
  return humanize(preferred) || 'Agent'
}

export function terminalRailLogicalKey(terminal: TerminalSnapshot): string {
  const title = terminalRailTitle(terminal).toLowerCase()
  const parent = (terminal.parent_step_id || '').trim().toLowerCase()
  const rawName = (terminal.agent_name || terminal.step_name || '').trim()

  // A message-sequence creates one terminal per turn. Keep those turns under
  // the owning sequence instead of presenting each turn as a separate agent.
  if ((terminal.step_type || '').toLowerCase() === 'message_sequence' || MESSAGE_SEQUENCE_PATTERN.test(rawName)) {
    return `sequence:${parent || terminal.step_id || title}`
  }

  if (terminal.step_id) {
    return `step:${parent}:${terminal.step_id.trim().toLowerCase()}`
  }

  const kind = (terminal.execution_kind || terminal.scope || 'agent').trim().toLowerCase()
  if (title !== 'agent') {
    return `${kind}:${parent}:${title}`
  }
  return `terminal:${terminal.terminal_id}`
}

export function terminalRailGroupSearchText(group: TerminalRailLogicalGroup): string {
  return group.terminals.map(terminal => [
    group.title,
    terminal.step_name,
    terminal.display_title,
    terminal.agent_name,
    terminal.step_id,
    terminal.parent_step_id,
    terminal.execution_kind,
    terminal.status?.provider_label,
    terminal.status?.status_text,
  ].filter(Boolean).join(' ')).join(' ').toLowerCase()
}

function isReviewTerminal(terminal: TerminalSnapshot, title: string): boolean {
  const owner = `${terminal.owner_id || ''} ${terminal.execution_id || ''}`
  const searchableLabel = `${title} ${terminal.agent_name || ''} ${terminal.step_name || ''} ${owner}`
    .replace(/[-_:]+/g, ' ')
  return REVIEW_LABEL_PATTERN.test(searchableLabel)
}

function isWorkflowTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || terminal.scope || '').toLowerCase()
  return Boolean(terminal.step_id || terminal.parent_step_id) || [
    'workflow_step',
    'execution_only',
    'step',
    'todo_task',
    'sub_agent',
    'delegation',
  ].includes(kind)
}

function updatedTime(terminal: TerminalSnapshot): number {
  return new Date(terminal.updated_at || terminal.created_at || '').getTime() || 0
}

function createdTime(terminal: TerminalSnapshot): number {
  return new Date(terminal.created_at || terminal.updated_at || '').getTime() || 0
}

function representativeFor(
  terminals: TerminalSnapshot[],
  getState: OrganizeTerminalRailOptions['getState'],
): TerminalSnapshot {
  const running = terminals.filter(terminal => getState(terminal) === 'running')
  const candidates = running.length > 0 ? running : terminals
  return [...candidates].sort((a, b) => {
    const attemptDelta = (b.step_attempt || 0) - (a.step_attempt || 0)
    if (attemptDelta !== 0) return attemptDelta
    return updatedTime(b) - updatedTime(a)
  })[0]
}

function sectionFor(
  terminal: TerminalSnapshot,
  title: string,
  getState: OrganizeTerminalRailOptions['getState'],
): TerminalRailSection {
  const state = getState(terminal)
  if (state === 'running') return 'active'
  if (state === 'failed' || state === 'stale') return 'attention'
  if (isReviewTerminal(terminal, title)) return 'review'
  if (isWorkflowTerminal(terminal)) return 'workflow'
  return 'other'
}

function compareGroups(a: TerminalRailLogicalGroup, b: TerminalRailLogicalGroup): number {
  const aIndex = a.representative.step_index
  const bIndex = b.representative.step_index
  if (aIndex !== undefined && bIndex !== undefined && aIndex !== bIndex) return aIndex - bIndex
  if (aIndex !== undefined && bIndex === undefined) return -1
  if (aIndex === undefined && bIndex !== undefined) return 1
  const createdDelta = createdTime(a.representative) - createdTime(b.representative)
  if (createdDelta !== 0) return createdDelta
  return a.title.localeCompare(b.title)
}

export function organizeTerminalRail(
  terminals: TerminalSnapshot[],
  options: OrganizeTerminalRailOptions,
): TerminalRailLogicalGroup[] {
  const grouped = new Map<string, TerminalSnapshot[]>()
  for (const terminal of terminals) {
    if (options.isMainAgent(terminal)) continue
    const key = terminalRailLogicalKey(terminal)
    const bucket = grouped.get(key) || []
    bucket.push(terminal)
    grouped.set(key, bucket)
  }

  return Array.from(grouped.entries()).map(([key, groupTerminals]) => {
    const sortedTerminals = [...groupTerminals].sort((a, b) => {
      const attemptDelta = (b.step_attempt || 0) - (a.step_attempt || 0)
      if (attemptDelta !== 0) return attemptDelta
      return updatedTime(b) - updatedTime(a)
    })
    const representative = representativeFor(sortedTerminals, options.getState)
    const title = terminalRailTitle(representative)
    return {
      key,
      title,
      section: sectionFor(representative, title, options.getState),
      representative,
      terminals: sortedTerminals,
    }
  }).sort(compareGroups)
}

// The rail defaults to active-only, but the pane can remain on a child after
// that child completes. Identify that one hidden group so the caller can keep
// it in its normal section until the user selects another terminal.
export function hiddenSelectedTerminalRailGroup(
  groups: TerminalRailLogicalGroup[],
  visibleGroups: TerminalRailLogicalGroup[],
  selectedTerminal?: TerminalSnapshot | null,
): TerminalRailLogicalGroup | null {
  if (!selectedTerminal) return null
  const selectedGroup = groups.find(group => group.terminals.some(terminal => (
    terminal.terminal_id === selectedTerminal.terminal_id &&
    (terminal.tmux_session || '') === (selectedTerminal.tmux_session || '')
  )))
  if (!selectedGroup || visibleGroups.some(group => group.key === selectedGroup.key)) return null
  return selectedGroup
}
