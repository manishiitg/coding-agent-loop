import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Terminal, X } from 'lucide-react'
import { agentApi } from '../services/api'
import type { TerminalSnapshot } from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'

interface TerminalCenterProps {
  currentSessionId?: string
  compact?: boolean
}

function isOpaqueID(value?: string): boolean {
  if (!value) return false
  return /^[a-z]+:[0-9a-f-]{16,}$/i.test(value) || /^[0-9a-f-]{24,}$/i.test(value)
}

function humanizeIdentifier(value?: string): string {
  if (!value) return ''
  const cleaned = value
    .replace(/^exec[-_:]/i, '')
    .replace(/^step[-_:]\d+[-_:]/i, '')
    .replace(/^main[-_:]/i, '')
    .replace(/[-_]+/g, ' ')
    .trim()
  if (!cleaned) return ''
  return cleaned.charAt(0).toUpperCase() + cleaned.slice(1)
}

function workflowNameFromPath(path?: string): string {
  if (!path) return ''
  const parts = path.split('/').filter(Boolean)
  const workflowIndex = parts.findIndex(part => part === 'Workflow')
  if (workflowIndex >= 0 && parts[workflowIndex + 1]) {
    return humanizeIdentifier(parts[workflowIndex + 1])
  }
  return humanizeIdentifier(parts[parts.length - 1])
}

function formatExecutionKind(kind?: string): string {
  switch (kind) {
    case 'main_agent':
      return 'Main agent'
    case 'workflow_step':
    case 'execution_only':
    case 'step':
      return 'Workflow step'
    case 'background_agent':
      return 'Background agent'
    case 'todo_task':
    case 'sub_agent':
    case 'delegation':
      return 'Sub-agent'
    default:
      return humanizeIdentifier(kind)
  }
}

function formatTerminalKindLabel(terminal: TerminalSnapshot): string {
  const kind = terminal.execution_kind || terminal.scope
  if ((kind === 'workflow_step' || kind === 'execution_only' || kind === 'step') && terminal.step_type) {
    return `${humanizeIdentifier(terminal.step_type)} step`
  }
  return formatExecutionKind(terminal.execution_kind)
}

function terminalWorkflowLabel(terminal: TerminalSnapshot): string {
  return terminal.workflow_label || terminal.workflow_name || workflowNameFromPath(terminal.workflow_path)
}

function terminalTaskLabel(terminal: TerminalSnapshot): string {
  const rawLabel = terminal.label || terminal.execution_id || terminal.owner_id || ''
  const kind = terminal.execution_kind || terminal.scope
  if (kind === 'workflow_step' || kind === 'step' || kind === 'execution_only') {
    return terminal.step_id || terminal.step_name || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
  }
  return terminal.step_name || terminal.agent_name || terminal.step_id || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
}

function formatTerminalTitle(terminal: TerminalSnapshot): string {
  // Title is just the step_id (the most useful identifier). Everything
  // else — parent, chip, workflow name, kind — moves to the meta row
  // so the title stays minimal and scannable in dense lists.
  return terminal.step_id || terminal.step_name || formatTerminalKindLabel(terminal) || terminal.display_title || 'Terminal'
}

// formatTransportChip returns "transport·provider" (e.g. "api·anthropic",
// "structured·claudecode", "tmux·codex") for the title prefix. Falls
// back to inference: tmux_session implies tmux; absence implies the
// caller-supplied step_transport or "api".
function formatTransportChip(terminal: TerminalSnapshot): string {
  let transport = terminal.step_transport || ''
  if (!transport) {
    transport = terminal.tmux_session ? 'tmux' : 'api'
  }
  // Normalize backend strings to the short chip form.
  if (transport === 'structured_cli' || transport === 'structured') transport = 'structured'
  if (transport === 'non_tmux') transport = 'api'
  const provider = terminal.status?.provider_label?.toLowerCase() || ''
  return provider ? `${transport}·${provider}` : transport
}

// extractDoneStats parses the synthetic terminal's "[done · 1240ms · 412 in
// · 28 out · $0.000089]" trailer out of the pane content, so we can
// surface duration / tokens / cost in the meta row without needing
// dedicated backend fields. Returns empty when the trailer is absent —
// tmux providers don't emit a Done line, only real pane scrapes.
function extractDoneStats(content: string): { duration?: string; tokensIn?: string; tokensOut?: string; cost?: string } {
  if (!content) return {}
  const match = content.match(/\[done · ([^\]]+)\][^\[]*$/)
  if (!match) return {}
  const parts = match[1].split('·').map(p => p.trim())
  const out: { duration?: string; tokensIn?: string; tokensOut?: string; cost?: string } = {}
  for (const p of parts) {
    // Backend now emits duration in human-readable form already
    // ("234ms", "30.7s", "2m 14s", "1h 5m") — accept any non-token,
    // non-cost segment as the duration.
    if (p.endsWith(' in')) {
      out.tokensIn = p.replace(' in', '')
    } else if (p.endsWith(' out')) {
      out.tokensOut = p.replace(' out', '')
    } else if (p.startsWith('$')) {
      out.cost = p
    } else if (!out.duration && /\d/.test(p)) {
      out.duration = p
    }
  }
  return out
}

// humanizeMs mirrors the Go-side humanizeDuration: "234ms", "30.7s",
// "2m 14s", "1h 5m". Used when surfacing duration_ms from terminal.status.
function humanizeMs(ms: number): string {
  if (!ms || ms < 0) return ''
  if (ms < 1000) return `${ms}ms`
  const sec = ms / 1000
  if (sec < 60) return `${sec.toFixed(1)}s`
  const secs = Math.floor(ms / 1000)
  const mins = Math.floor(secs / 60)
  const rem = secs % 60
  if (mins < 60) return `${mins}m ${rem}s`
  const hours = Math.floor(mins / 60)
  const remMin = mins % 60
  return `${hours}h ${remMin}m`
}

// formatCost matches the Go-side formatUSD scale: cheap calls keep
// six decimals so a $0.000089 haiku call doesn't render as "$0.0000".
function formatCost(cost: number): string {
  if (cost >= 1) return cost.toFixed(2)
  if (cost >= 0.01) return cost.toFixed(4)
  if (cost > 0) return cost.toFixed(6)
  return '0'
}

function formatTerminalMeta(terminal: TerminalSnapshot): string {
  const chip = formatTransportChip(terminal)
  const parts: string[] = [
    chip,
    terminal.step_type ? humanizeIdentifier(terminal.step_type) : '',
    terminal.scope ? humanizeIdentifier(terminal.scope) : '',
  ]
  if (terminal.step_index && terminal.step_total) {
    parts.push(`step ${terminal.step_index}/${terminal.step_total}`)
  }
  if (terminal.step_attempt && terminal.step_attempt > 1) {
    parts.push(`attempt ${terminal.step_attempt}`)
  }
  if (terminal.step_triggered_by) {
    parts.push(`triggered by ${humanizeIdentifier(terminal.step_triggered_by)}`)
  }
  if (terminal.parent_step_id) {
    parts.push(`parent ${terminal.parent_step_id}`)
  }
  if (terminal.step_execution_mode) {
    parts.push(humanizeIdentifier(terminal.step_execution_mode))
  }
  // Cost / duration / tokens — prefer the structured fields on
  // terminal.status (populated from the streaming_end event's
  // completion meta for both tmux and non-tmux transports). Fall
  // back to regex-parsing the synthetic [done · ...] trailer for
  // older snapshots that haven't been re-emitted yet.
  const statusStats = terminal.status
  const durationFromStatus = statusStats?.duration_ms ? humanizeMs(statusStats.duration_ms) : ''
  const tokensInFromStatus = statusStats?.input_tokens ? statusStats.input_tokens.toString() : ''
  const tokensOutFromStatus = statusStats?.output_tokens ? statusStats.output_tokens.toString() : ''
  const costFromStatus = statusStats?.cost_usd ? `$${formatCost(statusStats.cost_usd)}` : ''
  const stats = (durationFromStatus || tokensInFromStatus || costFromStatus)
    ? { duration: durationFromStatus, tokensIn: tokensInFromStatus, tokensOut: tokensOutFromStatus, cost: costFromStatus }
    : extractDoneStats(terminal.content)
  if (stats.duration) parts.push(stats.duration)
  if (stats.tokensIn && stats.tokensOut) parts.push(`${stats.tokensIn}↑ ${stats.tokensOut}↓`)
  if (stats.cost) parts.push(stats.cost)
  // Tool count from the live status (set by the adapter event listener).
  if (terminal.status?.tool_count && terminal.status.tool_count > 0) {
    parts.push(`${terminal.status.tool_count} tools`)
  }
  if (terminal.display_meta) parts.push(terminal.display_meta)
  return [...new Set(parts.filter(Boolean))].join(' · ')
}

function terminalClosesAt(terminal: TerminalSnapshot): Date | null {
  if (!terminal.closes_at) return null
  const date = new Date(terminal.closes_at)
  if (Number.isNaN(date.getTime())) return null
  return date
}

function terminalSecondsUntilClose(terminal: TerminalSnapshot): number {
  const closesAt = terminalClosesAt(terminal)
  if (!closesAt) return 0
  return Math.max(0, Math.ceil((closesAt.getTime() - Date.now()) / 1000))
}

function formatCloseCountdown(seconds: number): string {
  if (seconds <= 0) return 'closing'
  if (seconds >= 60) return `${Math.ceil(seconds / 60)}m`
  return `${seconds}s`
}

function terminalState(terminal: TerminalSnapshot): string {
  if (!terminal.active && terminalSecondsUntilClose(terminal) > 0) return 'closing'
  if (terminal.state === 'closing' && terminalSecondsUntilClose(terminal) <= 0) return 'completed'
  if (terminal.state) return terminal.state
  return terminal.active ? 'running' : 'completed'
}

function terminalStateLabel(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'active'
    case 'completed':
      return 'completed'
    case 'failed':
      return 'failed'
    case 'closing':
      return `closes in ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}`
    default:
      return terminal.active ? 'active' : 'idle'
  }
}

function terminalStateDescription(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'Active: the coding agent is still running and this terminal is updating.'
    case 'completed':
      return 'Completed: the coding agent finished; this is the retained terminal snapshot.'
    case 'failed':
      return 'Failed: the coding agent or workflow step ended with an error.'
    case 'closing':
      return `Closing: the agent finished and this terminal will be removed in ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}.`
    default:
      return terminal.active ? 'Active terminal' : 'Inactive terminal snapshot'
  }
}

function terminalDotClass(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'bg-emerald-400'
    case 'completed':
      return 'bg-sky-400'
    case 'failed':
      return 'bg-red-400'
    case 'closing':
      return 'bg-amber-400'
    default:
      return 'bg-neutral-500'
  }
}

function terminalStateTextClass(terminal: TerminalSnapshot): string {
  switch (terminalState(terminal)) {
    case 'running':
      return 'text-emerald-300'
    case 'completed':
      return 'text-sky-300'
    case 'failed':
      return 'text-red-300'
    case 'closing':
      return 'text-amber-300'
    default:
      return 'text-neutral-500'
  }
}

function canDismissTerminal(terminal: TerminalSnapshot): boolean {
  const state = terminalState(terminal)
  return state === 'completed' || state === 'closing' || state === 'failed'
}

function shortDebugID(value?: string): string {
  if (!value) return ''
  if (value.length <= 18) return value
  return `${value.slice(0, 10)}...${value.slice(-6)}`
}

function terminalDebugText(terminal: TerminalSnapshot): string {
  return [
    `terminal_id=${terminal.terminal_id}`,
    terminal.tmux_session ? `tmux_session=${terminal.tmux_session}` : '',
    `session_id=${terminal.session_id}`,
    terminal.owner_id ? `owner_id=${terminal.owner_id}` : '',
    terminal.execution_id ? `execution_id=${terminal.execution_id}` : '',
    terminal.execution_kind ? `execution_kind=${terminal.execution_kind}` : '',
    terminal.step_type ? `step_type=${terminal.step_type}` : '',
    terminal.state ? `state=${terminal.state}` : '',
    terminal.closes_at ? `closes_at=${terminal.closes_at}` : '',
    terminal.retention_seconds ? `retention_seconds=${terminal.retention_seconds}` : '',
    `title=${formatTerminalTitle(terminal)}`,
  ].filter(Boolean).join('\n')
}

function isScrolledNearBottom(el: HTMLElement): boolean {
  return el.scrollHeight - el.scrollTop - el.clientHeight < 24
}

function terminalUpdatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.updated_at || terminal.created_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

function terminalCreatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.created_at || terminal.updated_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

// isMainAgentTerminal returns true for the persistent chat-session
// terminal that the user keeps coming back to. We pin it to the top of
// every list so it's the first thing the eye lands on when switching
// to Debug view.
function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').toLowerCase()
  return kind === 'main_agent' || kind === 'main' || kind === 'chat'
}

function sortTerminalsNewestFirst(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    const mainDelta = (isMainAgentTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    return terminalUpdatedTime(b) - terminalUpdatedTime(a)
  })
}

function sortActiveTerminalsStable(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    const mainDelta = (isMainAgentTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    const createdDelta = terminalCreatedTime(a) - terminalCreatedTime(b)
    if (createdDelta !== 0) return createdDelta
    return terminalPaneKey(a).localeCompare(terminalPaneKey(b))
  })
}

function terminalPaneKey(terminal: TerminalSnapshot): string {
  return terminal.tmux_session || terminal.terminal_id
}

function dedupeTerminalsByPane(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const byPane = new Map<string, TerminalSnapshot>()
  for (const terminal of terminals) {
    const key = terminalPaneKey(terminal)
    const existing = byPane.get(key)
    if (!existing || terminalUpdatedTime(terminal) >= terminalUpdatedTime(existing)) {
      byPane.set(key, terminal)
    }
  }
  return Array.from(byPane.values())
}

// ---------------------------------------------------------------------------
// Structured terminal view — parses the synthetic terminal's plain-text
// buffer into typed rows so we can colorize roles and fold long tool I/O
// behind a one-line summary. Tmux pane scrapes skip this and keep the
// raw <pre> rendering: they're literal screen captures and adding any
// frontend interpretation breaks the "this is exactly what the CLI saw"
// contract.
// ---------------------------------------------------------------------------

type TerminalRow =
  | { kind: 'banner'; text: string }
  | { kind: 'context'; text: string }
  | { kind: 'user'; text: string }
  | { kind: 'asst'; text: string }
  | { kind: 'tool'; name: string; args: string; result?: string; resultPrefix?: '✓' | '✗' }
  | { kind: 'attachment'; text: string }
  | { kind: 'done'; text: string }
  | { kind: 'error'; text: string }
  | { kind: 'plain'; text: string }

function classifyTerminalLine(line: string): TerminalRow {
  if (line.startsWith('$ ')) return { kind: 'banner', text: line.slice(2) }
  if (line.startsWith('↳ ')) return { kind: 'context', text: line.slice(2) }
  if (line.startsWith('> user: ')) return { kind: 'user', text: line.slice(8) }
  if (line.startsWith('< asst: ')) return { kind: 'asst', text: line.slice(8) }
  if (line.startsWith('  ')) return { kind: 'asst', text: line.slice(2) }
  if (line.startsWith('[image ')) return { kind: 'attachment', text: line }
  if (line.startsWith('[document ')) return { kind: 'attachment', text: line }
  if (line.startsWith('[done')) return { kind: 'done', text: line }
  if (line.startsWith('[error]')) return { kind: 'error', text: line.slice(7).trim() }
  // Tool start: "→ tool: name(args)" or "→ name args"
  if (line.startsWith('→ ')) {
    const rest = line.slice(2)
    const toolMatch = rest.match(/^tool:\s*([^(]+)\((.*)\)$/)
    if (toolMatch) {
      return { kind: 'tool', name: toolMatch[1].trim(), args: toolMatch[2] }
    }
    const spaceIdx = rest.indexOf(' ')
    if (spaceIdx > 0) {
      return { kind: 'tool', name: rest.slice(0, spaceIdx), args: rest.slice(spaceIdx + 1) }
    }
    return { kind: 'tool', name: rest, args: '' }
  }
  return { kind: 'plain', text: line }
}

// Pair tool starts with their matching result lines. A line beginning
// "✓ result <name>:" or "✗ result <name>:" or the short "✓ <name> (<dur>) ..."
// form gets merged into the most recent tool row with the same name.
function parseTerminalContent(content: string): TerminalRow[] {
  if (!content) return []
  const lines = content.split('\n')
  const rows: TerminalRow[] = []
  for (const line of lines) {
    // Tool result variants
    const fullResult = line.match(/^([✓✗])\s+result\s+([^:]+):\s*(.*)$/)
    if (fullResult) {
      const [, prefix, name, body] = fullResult
      // Find the most recent tool row with this name that has no result yet
      for (let i = rows.length - 1; i >= 0; i--) {
        const row = rows[i]
        if (row.kind === 'tool' && row.name === name.trim() && !row.result) {
          row.result = body
          row.resultPrefix = prefix as '✓' | '✗'
          break
        }
      }
      continue
    }
    const shortResult = line.match(/^([✓✗])\s+(\S+)\s+\(([^)]+)\)\s*(.*)$/)
    if (shortResult) {
      const [, prefix, name, dur, body] = shortResult
      for (let i = rows.length - 1; i >= 0; i--) {
        const row = rows[i]
        if (row.kind === 'tool' && row.name === name && !row.result) {
          row.result = body ? `${dur} · ${body}` : dur
          row.resultPrefix = prefix as '✓' | '✗'
          break
        }
      }
      continue
    }
    const classified = classifyTerminalLine(line)
    // Coalesce consecutive assistant continuation lines into one row
    if (classified.kind === 'asst' && rows.length > 0 && rows[rows.length - 1].kind === 'asst') {
      const prev = rows[rows.length - 1] as { kind: 'asst'; text: string }
      prev.text = `${prev.text}${prev.text && classified.text ? ' ' : ''}${classified.text}`
      continue
    }
    rows.push(classified)
  }
  return rows
}

interface StructuredTerminalViewProps {
  content: string
  scrollRef: React.RefObject<HTMLDivElement | null>
  onScroll: (e: React.UIEvent<HTMLDivElement>) => void
}

const StructuredTerminalView: React.FC<StructuredTerminalViewProps> = ({ content, scrollRef, onScroll }) => {
  const rows = useMemo(() => parseTerminalContent(content), [content])
  // Tool rows are collapsed when their result is "long" (>80 chars); the
  // user can click the chevron to flip individual rows. We key by row
  // index — content updates are append-only so indexes stay stable for
  // existing rows.
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const toggle = useCallback((idx: number) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }, [])
  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className="flex-1 overflow-auto overscroll-contain p-2.5 font-mono text-[12px] leading-5"
    >
      {rows.map((row, idx) => {
        switch (row.kind) {
          case 'banner':
            return (
              <div key={idx} className="text-cyan-300">
                <span className="text-neutral-500">$ </span>{row.text}
              </div>
            )
          case 'context':
            return <div key={idx} className="text-neutral-500">↳ {row.text}</div>
          case 'user':
            return (
              <div key={idx} className="text-blue-300 whitespace-pre-wrap break-words">
                <span className="text-blue-500">&gt; user: </span>{row.text}
              </div>
            )
          case 'asst':
            return (
              <div key={idx} className="text-neutral-100 whitespace-pre-wrap break-words">
                <span className="text-neutral-500">&lt; asst: </span>{row.text}
              </div>
            )
          case 'tool': {
            const hasResult = row.result !== undefined
            const isError = row.resultPrefix === '✗'
            const longResult = (row.result?.length ?? 0) > 80
            const isOpen = expanded.has(idx) || (hasResult && !longResult)
            const statusColor = !hasResult
              ? 'text-yellow-300'
              : isError
                ? 'text-red-400'
                : 'text-emerald-400'
            return (
              <div key={idx}>
                <button
                  type="button"
                  onClick={() => toggle(idx)}
                  className="w-full text-left hover:bg-white/5 rounded px-0.5 -mx-0.5"
                >
                  <span className={statusColor}>
                    {hasResult ? (isError ? '✗' : '✓') : '→'}
                  </span>
                  <span className="text-amber-300 ml-1">{row.name}</span>
                  {row.args && (
                    <span className="text-neutral-400 ml-1">
                      ({row.args.length > 60 && !isOpen ? `${row.args.slice(0, 60)}…` : row.args})
                    </span>
                  )}
                  {hasResult && longResult && (
                    <span className="text-neutral-600 ml-1">{isOpen ? '▾' : '▸'}</span>
                  )}
                </button>
                {hasResult && isOpen && (
                  <div className={`ml-4 whitespace-pre-wrap break-words ${isError ? 'text-red-300' : 'text-neutral-300'}`}>
                    {row.result}
                  </div>
                )}
              </div>
            )
          }
          case 'attachment':
            return <div key={idx} className="text-neutral-500">{row.text}</div>
          case 'done':
            return <div key={idx} className="text-emerald-400 mt-1">{row.text}</div>
          case 'error':
            return <div key={idx} className="text-red-400">[error] {row.text}</div>
          case 'plain':
            return <div key={idx} className="text-neutral-300 whitespace-pre-wrap break-words">{row.text}</div>
        }
      })}
    </div>
  )
}

function isSyntheticTerminal(terminal: TerminalSnapshot): boolean {
  const transport = (terminal.step_transport || '').toLowerCase()
  if (transport === 'tmux') return false
  if (transport === 'api' || transport === 'structured' || transport === 'structured_cli' || transport === 'non_tmux') return true
  // Fall back to tmux_session presence — pane scrapes always have one.
  return !terminal.tmux_session
}

export const TerminalCenter: React.FC<TerminalCenterProps> = ({ currentSessionId, compact }) => {
  // terminalCenterOpen was the legacy toggle gate (separate sidekick
  // panel); kept here for any callers that still pass the flag but no
  // longer affects rendering — Debug-mode mount is the only gate.
  // Scope terminals to the current chat session. The workflowEventBridge
  // adds every workflow-step event under the chat tab's sessionID, so
  // filtering by currentSessionId surfaces this chat's workflow steps
  // without leaking terminals from other chat tabs / unrelated workflows.
  const [viewAll, setViewAll] = useState(false)
  const [terminals, setTerminals] = useState<TerminalSnapshot[]>([])
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [userSelectedID, setUserSelectedID] = useState<string | null>(null)
  const [copiedTerminalID, setCopiedTerminalID] = useState<string | null>(null)
  const [dismissedTerminalIDs, setDismissedTerminalIDs] = useState<Set<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)
  const terminalOutputRef = useRef<HTMLElement | null>(null)
  const terminalAutoScrollRef = useRef(true)
  const selectedTerminalIDRef = useRef<string | null>(null)

  const copyTerminalDebug = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminalDebugText(terminal))
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const fetchTerminals = useCallback(async () => {
    try {
      const response = await agentApi.listTerminals(viewAll ? undefined : currentSessionId)
      const visibleTerminals = (response.terminals || []).filter(terminal => !dismissedTerminalIDs.has(terminal.terminal_id))
      setTerminals(dedupeTerminalsByPane(visibleTerminals))
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load terminals')
    }
  }, [currentSessionId, dismissedTerminalIDs, viewAll])

  const dismissTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canDismissTerminal(terminal)) return
    setDismissedTerminalIDs(current => {
      const next = new Set(current)
      next.add(terminal.terminal_id)
      return next
    })
    setTerminals(current => current.filter(item => item.terminal_id !== terminal.terminal_id))
    if (selectedID === terminalPaneKey(terminal)) {
      setSelectedID(null)
    }
    if (userSelectedID === terminalPaneKey(terminal)) {
      setUserSelectedID(null)
    }
    try {
      await agentApi.dismissTerminal(terminal.terminal_id)
    } catch (err) {
      // Keep the terminal hidden locally even if the running backend has not picked up
      // the DELETE route yet. A backend restart will make dismissal persistent there too.
      console.warn('Failed to dismiss terminal on backend', err)
    }
  }, [selectedID, userSelectedID])

  // buildTree turns a flat list of terminals into a parent → children
  // tree using parent_step_id. Roots are terminals with no parent_step_id
  // (or whose parent isn't in the list — those are surfaced at top level
  // so we never hide a terminal). Each node carries its depth so the
  // rail can indent with paddingLeft = base + depth * step.
  const buildTree = (list: TerminalSnapshot[]): Array<{ terminal: TerminalSnapshot; depth: number }> => {
    const byParent = new Map<string, TerminalSnapshot[]>()
    const known = new Set<string>()
    for (const t of list) {
      if (t.step_id) known.add(t.step_id)
    }
    for (const t of list) {
      // Self-parents (step_id === parent_step_id) and cycles in the
      // parent chain would otherwise blow the stack at render time.
      // Treat them as roots so the terminal still surfaces.
      const isSelfParent = !!t.step_id && t.step_id === t.parent_step_id
      const parent = !isSelfParent && t.parent_step_id && known.has(t.parent_step_id) ? t.parent_step_id : ''
      const bucket = byParent.get(parent) || []
      bucket.push(t)
      byParent.set(parent, bucket)
    }
    const out: Array<{ terminal: TerminalSnapshot; depth: number }> = []
    const visited = new Set<string>()
    const walk = (parent: string, depth: number) => {
      // Defense in depth against cycles + degenerate-deep trees.
      if (depth > 32) return
      for (const t of byParent.get(parent) || []) {
        if (t.step_id && visited.has(t.step_id)) continue
        if (t.step_id) visited.add(t.step_id)
        out.push({ terminal: t, depth })
        if (t.step_id) walk(t.step_id, depth + 1)
      }
    }
    walk('', 0)
    return out
  }

  const groupedTerminals = useMemo(() => {
    const uniqueTerminals = dedupeTerminalsByPane(terminals)
    // Build a single tree from ALL terminals (active + finished).
    // Splitting them was breaking the parent→child relationship when
    // a child step finished while its parent was still running — the
    // child got displaced into the "Finished" group, losing its
    // visual nesting under the parent. One tree keeps lineage intact;
    // the colored dot on each rail row already conveys per-row state.
    const allTerminals = sortActiveTerminalsStable(uniqueTerminals)
    const activeTerminals = uniqueTerminals.filter(terminal => terminalState(terminal) === 'running')
    const finishedTerminals = uniqueTerminals.filter(terminal => terminalState(terminal) !== 'running')
    return {
      activeTerminals,
      finishedTerminals,
      orderedTerminals: allTerminals,
      tree: buildTree(allTerminals),
    }
  }, [terminals])

  useEffect(() => {
    // Component is now only mounted when Debug view is active (it's
    // not a sidekick panel anymore), so polling should always run
    // whenever this component is on screen. The previous
    // terminalCenterOpen flag gated a standalone toggle that no
    // longer exists.
    void fetchTerminals()
    const interval = window.setInterval(fetchTerminals, 600)
    return () => window.clearInterval(interval)
  }, [fetchTerminals])

  useEffect(() => {
    if (groupedTerminals.orderedTerminals.length === 0) {
      setSelectedID(null)
      return
    }
    const selected = groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === selectedID)
    const userSelected = groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === userSelectedID)
    const latestActive = groupedTerminals.activeTerminals[0]

    if (userSelected) {
      const userSelectedKey = terminalPaneKey(userSelected)
      if (selectedID !== userSelectedKey) {
        setSelectedID(userSelectedKey)
      }
      return
    }

    if (userSelectedID && !userSelected) {
      setUserSelectedID(null)
    }

    if (!selectedID || !selected) {
      setSelectedID(terminalPaneKey(latestActive || groupedTerminals.orderedTerminals[0]))
    }
  }, [groupedTerminals, selectedID, userSelectedID])

  const selectedTerminal = useMemo(
    () => {
      if (!selectedID) return null
      return groupedTerminals.orderedTerminals.find(terminal => terminalPaneKey(terminal) === selectedID) || null
    },
    [groupedTerminals, selectedID],
  )
  const selectedTerminalKey = selectedTerminal ? terminalPaneKey(selectedTerminal) : null
  const activeCount = groupedTerminals.activeTerminals.length

  const handleTerminalScroll = useCallback(() => {
    const el = terminalOutputRef.current
    if (!el) return
    terminalAutoScrollRef.current = isScrolledNearBottom(el)
  }, [])

  useEffect(() => {
    const el = terminalOutputRef.current
    if (!el || !selectedTerminal?.content) return

    const terminalChanged = selectedTerminalIDRef.current !== selectedTerminalKey
    if (terminalChanged) {
      selectedTerminalIDRef.current = selectedTerminalKey
      terminalAutoScrollRef.current = true
    }

    if (!terminalAutoScrollRef.current) return

    const frame = window.requestAnimationFrame(() => {
      el.scrollTop = el.scrollHeight
    })
    return () => window.cancelAnimationFrame(frame)
  }, [selectedTerminalKey, selectedTerminal?.content])


  // Rail item — one row in the left rail. Compact vertical layout:
  // dot + step title (top line), transport chip + closing countdown
  // (bottom line). Click → select; hover → highlight.
  // depth controls left padding so child terminals nest under their
  // parent. Tree connectors (└) appear at depth >= 1.
  const renderRailItem = (terminal: TerminalSnapshot, depth: number = 0) => (
    <div
      key={terminalPaneKey(terminal)}
      role="button"
      tabIndex={0}
      onClick={() => {
        setSelectedID(terminalPaneKey(terminal))
        setUserSelectedID(terminalPaneKey(terminal))
      }}
      onKeyDown={event => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          setSelectedID(terminalPaneKey(terminal))
          setUserSelectedID(terminalPaneKey(terminal))
        }
      }}
      className={`group block w-full cursor-pointer border-l-2 py-1.5 pl-2.5 pr-2.5 text-left text-xs transition-colors ${
        terminalPaneKey(terminal) === selectedTerminalKey
          ? 'border-l-blue-400 bg-neutral-800 text-neutral-100'
          : 'border-l-transparent text-neutral-400 hover:bg-neutral-800/60 hover:text-neutral-200'
      }`}
    >
      <div className="flex items-center gap-1.5">
        {depth > 0 && (
          <span className="shrink-0 select-none whitespace-pre font-mono text-[10px] text-neutral-500" aria-hidden>
            {Array.from({ length: depth - 1 }, () => '│ ').join('')}└─→
          </span>
        )}
        <span
          className={`h-2 w-2 shrink-0 rounded-full ${terminalDotClass(terminal)}`}
          title={terminalStateDescription(terminal)}
          aria-label={terminalStateDescription(terminal)}
        />
        <span className="min-w-0 flex-1 truncate font-medium">{formatTerminalTitle(terminal)}</span>
        {canDismissTerminal(terminal) && (
          <button
            type="button"
            onClick={event => {
              event.stopPropagation()
              void dismissTerminal(terminal)
            }}
            className="shrink-0 rounded p-0.5 text-neutral-500 opacity-0 hover:bg-neutral-700 hover:text-neutral-100 group-hover:opacity-100"
            title="Remove terminal from UI"
            aria-label="Remove terminal from UI"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>
      <div className="mt-0.5 flex items-center gap-1.5 text-[10px] opacity-70">
        <span className="min-w-0 truncate">{formatTransportChip(terminal)}</span>
        {terminalState(terminal) === 'closing' && (
          <span className="shrink-0 text-amber-300">· {terminalStateLabel(terminal)}</span>
        )}
      </div>
    </div>
  )

  return (
    <div className={`flex min-h-0 min-w-0 flex-col bg-[#202020] text-neutral-100 ${compact ? '' : 'flex-1 overflow-hidden'}`}>
      <div className="flex min-h-0 flex-1 flex-col">
        {error && (
          <div className="rounded border border-red-900/60 bg-red-950/30 px-3 py-2 text-xs text-red-300">
            {error}
          </div>
        )}

        {!error && terminals.length === 0 && (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-12 text-center">
            <Terminal className="h-10 w-10 text-neutral-700" strokeWidth={1.25} />
            <div className="text-sm font-medium text-neutral-300">No terminals yet</div>
            <div className="max-w-md text-xs leading-relaxed text-neutral-500">
              Run a workflow step, send a message to the main agent, or kick off
              a coding-agent task to see its activity stream here. Each call
              becomes its own pane — the rail on the left lists them all, the
              right pane shows live output, tool calls, and cost.
            </div>
            <div className="mt-1 flex items-center gap-1.5 text-[11px] text-neutral-600">
              <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-blue-400" />
              <span>Watching for activity…</span>
            </div>
          </div>
        )}

        {terminals.length > 0 && (
          <div className="flex min-h-0 flex-1 gap-0 overflow-hidden border border-neutral-800 bg-[#111]">
            {/* Left rail — vertical list of all terminals. Scrolls
                independently of the right pane so the user can navigate
                a long list without losing the selected terminal's
                content. Hidden below sm breakpoint to save space. */}
            <div className="hidden w-60 shrink-0 flex-col overflow-y-auto border-r border-neutral-800 bg-[#0d0d0d] sm:flex">
              {groupedTerminals.tree.map(({ terminal, depth }) => renderRailItem(terminal, depth))}
            </div>

            {/* Right pane — the selected terminal's content. Header
                bar at top (chip + meta + actions), content below. */}
            <div className="flex min-w-0 flex-1 flex-col">
              {selectedTerminal ? (
                <>
                  <div className="flex items-center justify-between gap-3 border-b border-white/10 px-3 py-2 text-xs text-gray-400">
                    <span className="min-w-0 flex-1 truncate opacity-80">
                      {formatTerminalMeta(selectedTerminal)}
                    </span>
                    <div className="flex shrink-0 items-center gap-2">
                      {terminalState(selectedTerminal) === 'closing' && (
                        <span
                          className="text-amber-300"
                          title={terminalStateDescription(selectedTerminal)}
                        >
                          {terminalStateLabel(selectedTerminal)}
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={() => void copyTerminalDebug(selectedTerminal)}
                        className="inline-flex items-center justify-center rounded p-1 text-neutral-500 hover:bg-neutral-800 hover:text-neutral-100"
                        title="Copy terminal debug IDs"
                        aria-label="Copy terminal debug IDs"
                      >
                        {copiedTerminalID === selectedTerminal.terminal_id ? (
                          <Check className="h-3.5 w-3.5 text-emerald-300" />
                        ) : (
                          <Info className="h-3.5 w-3.5" />
                        )}
                      </button>
                      {canDismissTerminal(selectedTerminal) && (
                        <button
                          type="button"
                          onClick={() => void dismissTerminal(selectedTerminal)}
                          className="inline-flex items-center justify-center rounded border border-neutral-700 p-1 text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
                          title="Remove terminal from UI"
                          aria-label="Remove terminal from UI"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </div>
                  {isSyntheticTerminal(selectedTerminal) ? (
                    <StructuredTerminalView
                      content={selectedTerminal.content}
                      scrollRef={terminalOutputRef as React.RefObject<HTMLDivElement | null>}
                      onScroll={handleTerminalScroll}
                    />
                  ) : (
                    <pre
                      ref={terminalOutputRef as React.RefObject<HTMLPreElement | null>}
                      onScroll={handleTerminalScroll}
                      className="flex-1 overflow-auto overscroll-contain p-2.5 font-mono text-[12px] leading-5 text-gray-100 whitespace-pre"
                    >
                      {selectedTerminal.content}
                    </pre>
                  )}
                </>
              ) : (
                <div className="flex flex-1 items-center justify-center text-xs text-neutral-500">
                  Select a terminal from the rail to view its content.
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
