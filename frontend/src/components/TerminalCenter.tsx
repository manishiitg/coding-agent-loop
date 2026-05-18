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
  const workflowName = terminalWorkflowLabel(terminal)
  const kindLabel = formatTerminalKindLabel(terminal)
  const taskLabel = terminalTaskLabel(terminal)

  if (workflowName && kindLabel && taskLabel && taskLabel.toLowerCase() !== kindLabel.toLowerCase()) {
    return `${workflowName} -> ${kindLabel} -> ${taskLabel}`
  }
  if (workflowName && taskLabel) return `${workflowName} -> ${taskLabel}`
  if (workflowName && kindLabel) return `${workflowName} -> ${kindLabel}`
  if (taskLabel && kindLabel && taskLabel.toLowerCase() !== kindLabel.toLowerCase()) return `${kindLabel} -> ${taskLabel}`
  return taskLabel || kindLabel || terminal.display_title || workflowName || 'Terminal'
}

function formatTerminalMeta(terminal: TerminalSnapshot): string {
  const parts = [
    terminal.step_id ? `step ${terminal.step_id}` : '',
    terminal.step_type ? humanizeIdentifier(terminal.step_type) : '',
    terminal.scope ? humanizeIdentifier(terminal.scope) : '',
    terminal.display_meta,
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

function sortTerminalsNewestFirst(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => terminalUpdatedTime(b) - terminalUpdatedTime(a))
}

function sortActiveTerminalsStable(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
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

export const TerminalCenter: React.FC<TerminalCenterProps> = ({ currentSessionId, compact }) => {
  const terminalCenterOpen = useChatStore(state => state.terminalCenterOpen)
  const [viewAll, setViewAll] = useState(false)
  const [terminals, setTerminals] = useState<TerminalSnapshot[]>([])
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [userSelectedID, setUserSelectedID] = useState<string | null>(null)
  const [copiedTerminalID, setCopiedTerminalID] = useState<string | null>(null)
  const [dismissedTerminalIDs, setDismissedTerminalIDs] = useState<Set<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)
  const terminalOutputRef = useRef<HTMLPreElement | null>(null)
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

  const groupedTerminals = useMemo(() => {
    const uniqueTerminals = dedupeTerminalsByPane(terminals)
    const activeTerminals = sortActiveTerminalsStable(
      uniqueTerminals.filter(terminal => terminalState(terminal) === 'running'),
    )
    const finishedTerminals = sortTerminalsNewestFirst(
      uniqueTerminals.filter(terminal => terminalState(terminal) !== 'running'),
    )
    return {
      activeTerminals,
      finishedTerminals,
      orderedTerminals: [...activeTerminals, ...finishedTerminals],
    }
  }, [terminals])

  useEffect(() => {
    if (!terminalCenterOpen) return
    void fetchTerminals()
    const interval = window.setInterval(fetchTerminals, 600)
    return () => window.clearInterval(interval)
  }, [terminalCenterOpen, fetchTerminals])

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

  if (!terminalCenterOpen) {
    return null
  }

  const renderTerminalCard = (terminal: TerminalSnapshot) => (
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
      className={`min-w-[180px] max-w-[280px] rounded border px-2.5 py-2 text-left text-xs transition-colors ${
        terminalPaneKey(terminal) === selectedTerminalKey
          ? 'border-neutral-500 bg-neutral-800 text-neutral-100 shadow-sm'
          : 'border-neutral-700 bg-neutral-900/30 text-neutral-400 hover:bg-neutral-800/60 hover:text-neutral-200'
      }`}
    >
      <div className="flex items-center gap-1.5">
        <span
          className={`h-2 w-2 rounded-full ${terminalDotClass(terminal)}`}
          title={terminalStateDescription(terminal)}
          aria-label={terminalStateDescription(terminal)}
        />
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
        {canDismissTerminal(terminal) && (
          <button
            type="button"
            onClick={event => {
              event.stopPropagation()
              void dismissTerminal(terminal)
            }}
            className="shrink-0 rounded p-0.5 text-neutral-500 hover:bg-neutral-700 hover:text-neutral-100"
            title="Remove terminal from UI"
            aria-label="Remove terminal from UI"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        )}
      </div>
      <div className="mt-1 truncate text-[11px] opacity-70">
        {formatTerminalMeta(terminal) || 'Current session'} · {terminalStateLabel(terminal)}
      </div>
    </div>
  )

  return (
    <div className={`${compact ? 'my-2' : 'my-3'} rounded-md border border-neutral-700 bg-[#202020] text-neutral-100 shadow-sm`}>
      <div className="flex items-center justify-between gap-3 border-b border-neutral-800 px-2.5 py-2">
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

      <div className="px-2 pb-2 pt-2">
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
              {groupedTerminals.activeTerminals.map(renderTerminalCard)}
              {groupedTerminals.finishedTerminals.map(renderTerminalCard)}
            </div>

            {selectedTerminal && (
              <div className="rounded-md border border-neutral-800 bg-[#111]">
                <div className="flex items-center justify-between gap-3 border-b border-white/10 px-3 py-2 text-xs text-gray-400">
                  <span className="truncate">{formatTerminalTitle(selectedTerminal)}</span>
                  <div className="flex shrink-0 items-center gap-2">
                    <span
                      className={terminalStateTextClass(selectedTerminal)}
                      title={terminalStateDescription(selectedTerminal)}
                    >
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
                <pre ref={terminalOutputRef} onScroll={handleTerminalScroll} className="max-h-[46vh] overflow-auto overscroll-contain p-2.5 font-mono text-[12px] leading-5 text-gray-100 whitespace-pre">
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
