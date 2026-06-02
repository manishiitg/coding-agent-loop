import type { TerminalSnapshot } from '../services/api-types'

/**
 * Identifies the persistent "main agent" terminal (the chat session's own
 * tmux pane) as opposed to a workflow sub-agent / step terminal. Mirrors the
 * backend marker `execution_kind: "main_agent"` with a few legacy fallbacks.
 */
export function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').toLowerCase()
  const ownerID = (terminal.owner_id || '').toLowerCase()
  const terminalID = (terminal.terminal_id || '').toLowerCase()
  return kind === 'main_agent' || kind === 'main' || kind === 'chat' ||
    ownerID.startsWith('main:') ||
    terminalID.includes(':main:')
}

/** Best-effort human label for a terminal, for status chips/indicators. */
export function terminalDisplayLabel(terminal: TerminalSnapshot): string {
  return (terminal.display_title || terminal.label || terminal.agent_name || terminal.terminal_id || 'terminal').trim()
}

/**
 * Maps a browser KeyboardEvent to the key identifier understood by the
 * backend `POST /api/terminals/{id}/key` endpoint, or to a literal string of
 * text to paste via `POST /api/terminals/{id}/input`. Returns null when the
 * event should be ignored (bare modifiers, unsupported combos).
 *
 * Used by the chat input's "keyboard passthrough" mode to drive the main
 * agent terminal as if typing directly into it.
 */
export type TerminalKeyAction =
  | { kind: 'key'; key: string }
  | { kind: 'text'; text: string }
  | null

export function keyEventToTerminalAction(e: {
  key: string
  ctrlKey: boolean
  metaKey: boolean
  altKey: boolean
  shiftKey: boolean
}): TerminalKeyAction {
  const named: Record<string, string> = {
    Enter: 'enter',
    Escape: 'esc',
    Backspace: 'backspace',
    Delete: 'delete',
    ArrowUp: 'up',
    ArrowDown: 'down',
    ArrowLeft: 'left',
    ArrowRight: 'right',
    Home: 'home',
    End: 'end',
    PageUp: 'pageup',
    PageDown: 'pagedown',
  }

  // Tab (and Shift+Tab) — passed through rather than moving focus.
  if (e.key === 'Tab') {
    return { kind: 'key', key: e.shiftKey ? 'btab' : 'tab' }
  }

  // Ctrl chords (e.g. Ctrl+C, Ctrl+D, Ctrl+L). Single printable char only.
  if (e.ctrlKey && !e.metaKey && !e.altKey) {
    const ch = e.key.length === 1 ? e.key.toLowerCase() : ''
    if (/^[a-z0-9]$/.test(ch)) {
      return { kind: 'key', key: `ctrl-${ch}` }
    }
    // Fall through for things like Ctrl+ArrowLeft → treat named key below.
  }

  if (named[e.key] && !e.metaKey && !e.altKey) {
    return { kind: 'key', key: named[e.key] }
  }

  // Printable single character (letters, digits, symbols, space) with no
  // command/meta/alt modifier — sent as literal text.
  if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
    return { kind: 'text', text: e.key }
  }

  return null
}
