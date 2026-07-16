import type { TerminalSnapshot } from '../services/api-types'

export function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').trim().toLowerCase()
  const ownerID = (terminal.owner_id || '').trim().toLowerCase()
  const sessionID = (terminal.session_id || '').trim().toLowerCase()
  const terminalID = (terminal.terminal_id || '').trim().toLowerCase()
  const canonicalOwner = ownerID.startsWith('main:') || (!!sessionID && ownerID === sessionID)
  const canonicalTerminalID = terminalID.includes(':main:')

  // An explicit non-main owner is authoritative. Provider callbacks can
  // inherit main_agent from their parent while describing a child pane.
  if (ownerID && !canonicalOwner) return false

  return canonicalOwner || canonicalTerminalID || kind === 'main_agent' || kind === 'main' || kind === 'chat'
}
