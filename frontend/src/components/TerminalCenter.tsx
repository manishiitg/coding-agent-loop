import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Check, Info, Terminal } from 'lucide-react'
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

function formatTerminalTitle(terminal: TerminalSnapshot): string {
  if (terminal.display_title) return terminal.display_title
  if (terminal.workflow_label && (terminal.step_name || terminal.agent_name)) {
    return `${terminal.workflow_label} -> ${terminal.step_name || terminal.agent_name}`
  }
  if (terminal.workflow_name && (terminal.step_name || terminal.agent_name)) {
    return `${terminal.workflow_name} -> ${terminal.step_name || terminal.agent_name}`
  }
  const workflowName = workflowNameFromPath(terminal.workflow_path)
  const kindLabel = formatExecutionKind(terminal.execution_kind)
  const rawLabel = terminal.label || terminal.execution_id || terminal.owner_id || ''
  const label = isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel)

  if (workflowName && kindLabel) return `${workflowName} -> ${kindLabel}`
  if (label && kindLabel && label.toLowerCase() !== kindLabel.toLowerCase()) return `${kindLabel} -> ${label}`
  return label || kindLabel || workflowName || 'Terminal'
}

function formatTerminalMeta(terminal: TerminalSnapshot): string {
  if (terminal.display_meta) return terminal.display_meta
  const parts = [
    formatExecutionKind(terminal.execution_kind),
    terminal.scope ? humanizeIdentifier(terminal.scope) : '',
    terminal.step_id ? humanizeIdentifier(terminal.step_id) : '',
    workflowNameFromPath(terminal.workflow_path),
  ].filter(Boolean)
  return [...new Set(parts)].join(' · ')
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
    terminal.state ? `state=${terminal.state}` : '',
    terminal.closes_at ? `closes_at=${terminal.closes_at}` : '',
    terminal.retention_seconds ? `retention_seconds=${terminal.retention_seconds}` : '',
    `title=${formatTerminalTitle(terminal)}`,
  ].filter(Boolean).join('\n')
}

export const TerminalCenter: React.FC<TerminalCenterProps> = ({ currentSessionId, compact }) => {
  const terminalCenterOpen = useChatStore(state => state.terminalCenterOpen)
  const [viewAll, setViewAll] = useState(false)
  const [terminals, setTerminals] = useState<TerminalSnapshot[]>([])
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [userSelectedID, setUserSelectedID] = useState<string | null>(null)
  const [copiedTerminalID, setCopiedTerminalID] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const terminalOutputRef = useRef<HTMLPreElement | null>(null)

  const copyTerminalDebug = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminalDebugText(terminal))
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const fetchTerminals = useCallback(async () => {
    try {
      const response = await agentApi.listTerminals(viewAll ? undefined : currentSessionId)
      setTerminals(response.terminals || [])
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load terminals')
    }
  }, [currentSessionId, viewAll])

  useEffect(() => {
    if (!terminalCenterOpen) return
    void fetchTerminals()
    const interval = window.setInterval(fetchTerminals, 600)
    return () => window.clearInterval(interval)
  }, [terminalCenterOpen, fetchTerminals])

  useEffect(() => {
    if (terminals.length === 0) {
      setSelectedID(null)
      return
    }
    const selected = terminals.find(terminal => terminal.terminal_id === selectedID)
    const userSelected = terminals.find(terminal => terminal.terminal_id === userSelectedID)
    const latestActive = terminals.find(terminal => terminal.active)
    if (!selectedID || !selected || (!selected.active && latestActive && !userSelected?.active)) {
      setSelectedID((latestActive || terminals[0]).terminal_id)
    }
  }, [selectedID, terminals, userSelectedID])

  const selectedTerminal = useMemo(
    () => terminals.find(terminal => terminal.terminal_id === selectedID) || terminals[0],
    [selectedID, terminals],
  )
  const activeCount = terminals.filter(terminal => terminal.active).length

  useEffect(() => {
    const el = terminalOutputRef.current
    if (!el || !selectedTerminal?.content) return
    el.scrollTop = el.scrollHeight
  }, [selectedTerminal?.terminal_id, selectedTerminal?.content])

  if (!terminalCenterOpen) {
    return null
  }

  return (
    <div className={`mx-3 ${compact ? 'my-2' : 'my-3'} rounded-md border border-neutral-700 bg-[#202020] text-neutral-100 shadow-sm`}>
      <div className="flex items-center justify-between gap-3 border-b border-neutral-800 px-3 py-2">
        <div className="flex min-w-0 items-center gap-2 text-sm font-medium text-neutral-300">
          <Terminal className="h-4 w-4 shrink-0" />
          <span>Terminals</span>
          <span className="rounded bg-neutral-800 px-1.5 py-0.5 text-xs text-neutral-400">
            {activeCount > 0 ? `${activeCount} active` : `${terminals.length} recent`}
          </span>
        </div>
        <div className="flex items-center gap-1 text-xs">
          <button
            type="button"
            onClick={() => setViewAll(false)}
            className={`rounded px-2 py-1 ${!viewAll ? 'bg-neutral-700 text-neutral-100' : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200'}`}
          >
            Current
          </button>
          <button
            type="button"
            onClick={() => setViewAll(true)}
            className={`rounded px-2 py-1 ${viewAll ? 'bg-neutral-700 text-neutral-100' : 'text-neutral-400 hover:bg-neutral-800 hover:text-neutral-200'}`}
          >
            All
          </button>
        </div>
      </div>

      <div className="px-3 pb-3 pt-2">
        {error && (
          <div className="rounded border border-red-900/60 bg-red-950/30 px-3 py-2 text-xs text-red-300">
            {error}
          </div>
        )}

        {!error && terminals.length === 0 && (
          <div className="px-1 py-2 text-xs text-neutral-400">No terminal snapshots yet.</div>
        )}

        {terminals.length > 0 && (
          <>
            <div className="mb-2 flex gap-2 overflow-x-auto pb-1">
              {terminals.map(terminal => (
                <div
                  key={terminal.terminal_id}
                  role="button"
                  tabIndex={0}
                  onClick={() => {
                    setSelectedID(terminal.terminal_id)
                    setUserSelectedID(terminal.terminal_id)
                  }}
                  onKeyDown={event => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault()
                      setSelectedID(terminal.terminal_id)
                      setUserSelectedID(terminal.terminal_id)
                    }
                  }}
                  className={`min-w-[180px] max-w-[280px] rounded border px-2.5 py-2 text-left text-xs transition-colors ${
                    terminal.terminal_id === selectedTerminal?.terminal_id
                      ? 'border-neutral-500 bg-neutral-800 text-neutral-100 shadow-sm'
                      : 'border-neutral-700 bg-neutral-900/30 text-neutral-400 hover:bg-neutral-800/60 hover:text-neutral-200'
                  }`}
                >
                  <div className="flex items-center gap-1.5">
                    <span className={`h-2 w-2 rounded-full ${terminalDotClass(terminal)}`} />
                    <span className="min-w-0 flex-1 truncate font-medium">{formatTerminalTitle(terminal)}</span>
                    <button
                      type="button"
                      onClick={event => {
                        event.stopPropagation()
                        void copyTerminalDebug(terminal)
                      }}
                      className="shrink-0 rounded p-0.5 text-neutral-500 hover:bg-neutral-700 hover:text-neutral-100"
                      title="Copy terminal debug IDs"
                      aria-label="Copy terminal debug IDs"
                    >
                      {copiedTerminalID === terminal.terminal_id ? (
                        <Check className="h-3.5 w-3.5 text-emerald-300" />
                      ) : (
                        <Info className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                  <div className="mt-1 truncate text-[11px] opacity-70">
                    {formatTerminalMeta(terminal) || 'Current session'} · {terminalStateLabel(terminal)}
                  </div>
                </div>
              ))}
            </div>

            {selectedTerminal && (
              <div className="rounded-md border border-neutral-800 bg-[#111]">
                <div className="flex items-center justify-between gap-3 border-b border-white/10 px-3 py-2 text-xs text-gray-400">
                  <span className="truncate">{formatTerminalTitle(selectedTerminal)}</span>
                  <div className="flex shrink-0 items-center gap-2">
                    <span className={terminalStateTextClass(selectedTerminal)}>
                      {terminalStateLabel(selectedTerminal)}
                    </span>
                    <button
                      type="button"
                      onClick={() => void copyTerminalDebug(selectedTerminal)}
                      className="inline-flex items-center justify-center rounded border border-neutral-700 p-1 text-neutral-300 hover:bg-neutral-800 hover:text-neutral-100"
                      title="Copy terminal debug IDs"
                      aria-label="Copy terminal debug IDs"
                    >
                      {copiedTerminalID === selectedTerminal.terminal_id ? (
                        <Check className="h-3.5 w-3.5 text-emerald-300" />
                      ) : (
                        <Info className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                </div>
                <div className="flex flex-wrap gap-1.5 border-b border-white/10 px-3 py-2 text-[10px] text-neutral-500">
                  {selectedTerminal.tmux_session && (
                    <span className="rounded bg-neutral-900 px-1.5 py-0.5" title={selectedTerminal.tmux_session}>
                      tmux {shortDebugID(selectedTerminal.tmux_session)}
                    </span>
                  )}
                  <span className="rounded bg-neutral-900 px-1.5 py-0.5" title={selectedTerminal.terminal_id}>
                    terminal {shortDebugID(selectedTerminal.terminal_id)}
                  </span>
                  {selectedTerminal.owner_id && (
                    <span className="rounded bg-neutral-900 px-1.5 py-0.5" title={selectedTerminal.owner_id}>
                      owner {shortDebugID(selectedTerminal.owner_id)}
                    </span>
                  )}
                  {selectedTerminal.execution_id && selectedTerminal.execution_id !== selectedTerminal.owner_id && (
                    <span className="rounded bg-neutral-900 px-1.5 py-0.5" title={selectedTerminal.execution_id}>
                      exec {shortDebugID(selectedTerminal.execution_id)}
                    </span>
                  )}
                </div>
                <pre ref={terminalOutputRef} className="max-h-[46vh] overflow-auto overscroll-contain p-3 font-mono text-[12px] leading-5 text-gray-100 whitespace-pre">
                  {selectedTerminal.content}
                </pre>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
