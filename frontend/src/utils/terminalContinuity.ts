import type { TerminalSnapshot } from '../services/api-types'

export interface TerminalContinuityOptions {
  sameScope: boolean
  hasPendingActivity: boolean
  emptyPollCount: number
  gracePolls: number
}

export interface TerminalContinuityResult {
  terminals: TerminalSnapshot[]
  emptyPollCount: number
}

function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').toLowerCase()
  const ownerID = (terminal.owner_id || '').toLowerCase()
  const terminalID = (terminal.terminal_id || '').toLowerCase()
  return kind === 'main_agent' || kind === 'main' || kind === 'chat' ||
    ownerID.startsWith('main:') || terminalID.includes(':main:')
}

function isRenderableTerminal(terminal: TerminalSnapshot): boolean {
  return !(terminal.terminal_id.includes(':turn-') && isMainAgentTerminal(terminal))
}

function dedupeByTerminalID(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const byID = new Map<string, TerminalSnapshot>()
  for (const terminal of terminals) byID.set(terminal.terminal_id, terminal)
  return Array.from(byID.values())
}

/**
 * Keep the last real pane mounted while a coding CLI changes foreground turns.
 * During that handoff the terminal registry can briefly return no canonical pane
 * (or only a hidden archived :turn-N snapshot). Replacing the list at that point
 * closes the live xterm and paints a false "Starting terminal" screen even though
 * the retained tmux session is still usable.
 */
export function preserveTerminalContinuity(
  current: TerminalSnapshot[],
  next: TerminalSnapshot[],
  options: TerminalContinuityOptions,
): TerminalContinuityResult {
  const currentRenderable = current.filter(isRenderableTerminal)
  const nextRenderable = next.filter(isRenderableTerminal)

  if (nextRenderable.length > 0) {
    return { terminals: next, emptyPollCount: 0 }
  }

  const emptyPollCount = options.emptyPollCount + 1
  const shouldPreserve = options.sameScope && currentRenderable.length > 0 && (
    options.hasPendingActivity || emptyPollCount <= options.gracePolls
  )
  if (!shouldPreserve) {
    return { terminals: next, emptyPollCount }
  }

  return {
    terminals: dedupeByTerminalID([...next, ...currentRenderable]),
    emptyPollCount,
  }
}
