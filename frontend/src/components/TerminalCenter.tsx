import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, Bug, Check, Copy, CornerDownLeft, CornerUpLeft, GitBranch, History, Info, Power, RefreshCw, Send, Terminal, X } from 'lucide-react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { agentApi } from '../services/api'
import type { PollingEvent, TerminalSnapshot } from '../services/api-types'
import { useChatStore } from '../stores/useChatStore'

interface TerminalCenterProps {
  currentSessionId?: string
  compact?: boolean
  hasConversationActivity?: boolean
}

const TERMINAL_REFRESH_HISTORY_LINES = 10000
const TERMINAL_DETAIL_CACHE_LIMIT = 40
const MAX_PRIOR_ARCHIVED_TURNS_TO_INLINE = 3
const PROMPT_COMPLETION_FALLBACK_SECONDS = 60
const INACTIVE_FALLBACK_SECONDS = 120
const EMPTY_TERMINAL_RESPONSE_GRACE_POLLS = 10
const TERMINAL_POLL_INTERVAL_MS = 1500

interface RoutingRouteSummary {
  route_id?: string
  route_name?: string
  condition?: string
  next_step_id?: string
}

interface RoutingDecision {
  id: string
  stepId?: string
  stepTitle?: string
  selectedRouteId: string
  selectedRouteName?: string
  nextStepId?: string
  routeCount: number
  reasoning?: string
  timestamp?: string
}

type TerminalRailItem =
  | { kind: 'terminal'; terminal: TerminalSnapshot; depth: number }
  | { kind: 'route'; decision: RoutingDecision; depth: number }

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

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined
  return value as Record<string, unknown>
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function routingPayload(event: PollingEvent): Record<string, unknown> | undefined {
  const data = asRecord(event.data)
  const nested = asRecord(data?.data)
  return nested || data
}

function routingRoutes(value: unknown): RoutingRouteSummary[] {
  if (!Array.isArray(value)) return []
  const routes: RoutingRouteSummary[] = []
  for (const item of value) {
    const route = asRecord(item)
    if (!route) continue
    routes.push({
      route_id: stringField(route.route_id) || undefined,
      route_name: stringField(route.route_name) || undefined,
      condition: stringField(route.condition) || undefined,
      next_step_id: stringField(route.next_step_id) || undefined,
    })
  }
  return routes
}

function routingDecisionFromEvent(event: PollingEvent): RoutingDecision | null {
  if (event.type !== 'routing_evaluated') return null
  const payload = routingPayload(event)
  if (!payload) return null
  const response = asRecord(payload.routing_response)
  const selectedRouteId = stringField(response?.selected_route_id)
  if (!selectedRouteId) return null
  const routes = routingRoutes(payload.routes)
  const selectedRoute = routes.find(route => route.route_id === selectedRouteId)
  const stepId = stringField(payload.step_id)
  return {
    id: event.id || `${stepId || 'routing'}:${selectedRouteId}:${event.timestamp || ''}`,
    stepId: stepId || undefined,
    stepTitle: stringField(payload.step_title) || undefined,
    selectedRouteId,
    selectedRouteName: selectedRoute?.route_name,
    nextStepId: selectedRoute?.next_step_id,
    routeCount: routes.length,
    reasoning: stringField(response?.reasoning) || undefined,
    timestamp: event.timestamp,
  }
}

function routeDecisionLabel(decision: RoutingDecision): string {
  return decision.selectedRouteName || humanizeIdentifier(decision.selectedRouteId) || decision.selectedRouteId
}

function routeDecisionTitle(decision: RoutingDecision): string {
  const label = routeDecisionLabel(decision)
  return `Routing: ${label}${decision.nextStepId ? ` -> ${decision.nextStepId}` : ''}`
}

function routeDecisionDedupeKey(decision: RoutingDecision): string {
  return [
    decision.stepId || '',
    decision.selectedRouteId || '',
    decision.nextStepId || '',
  ].join('|')
}

function routingDecisionTime(decision: RoutingDecision): number {
  return decision.timestamp ? new Date(decision.timestamp).getTime() || 0 : 0
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
  return terminal.step_name || terminal.agent_name || visibleStepID(terminal) || (isOpaqueID(rawLabel) ? '' : humanizeIdentifier(rawLabel))
}

function formatTerminalTitle(terminal: TerminalSnapshot): string {
  // Title is just the step_id (the most useful identifier). Everything
  // else — parent, chip, workflow name, kind — moves to the meta row
  // so the title stays minimal and scannable in dense lists.
  const kind = (terminal.execution_kind || terminal.scope || '').toLowerCase()
  if (isMainAgentTerminal(terminal)) {
    return terminal.agent_name || terminal.step_name || 'Main agent'
  }
  if (kind === 'background_agent' || kind === 'background' || kind === 'delegation' || kind === 'todo_task' || kind === 'sub_agent') {
    return terminal.agent_name || terminal.step_name || terminal.display_title || visibleStepID(terminal) || formatTerminalKindLabel(terminal) || 'Terminal'
  }
  return visibleStepID(terminal) || terminal.step_name || formatTerminalKindLabel(terminal) || terminal.display_title || 'Terminal'
}

function visibleStepID(terminal: TerminalSnapshot): string {
  const value = terminal.step_id || ''
  if (!value) return ''
  if (isMainAgentTerminal(terminal) && value.startsWith('main_agent:')) return ''
  return value
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
  const provider = terminal.status?.provider_label || ''
  const transportLabel = humanizeIdentifier(transport)
  return provider ? `${provider} · ${transportLabel}` : transportLabel
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

function formatTerminalModelLabel(terminal: TerminalSnapshot): string {
  const lines = (terminal.content || '')
    .replace(/\x1B(?:[@-Z\\-_]|\[[0-?]*[ -/]*[@-~])/g, '')
    .split(/\r?\n/)
    .slice(0, 30)
    .map(line => line.replace(/\s+/g, ' ').trim())
    .filter(Boolean)

  for (const line of lines) {
    const explicit = line.match(/\bmodel:\s*([^·|]+?)(?:\s+\/model\b|\s*[·|]|$)/i)
    if (explicit?.[1]) return explicit[1].trim()

    const assignment = line.match(/\bmodel=([^\s·|]+)/i)
    if (assignment?.[1]) return assignment[1].trim()

    const claude = line.match(/\bClaude Code\s+v[\w.-]+.*?\s([A-Za-z]+(?:\s+\d+(?:\.\d+)?){1,2}(?:\s+with\s+[^·|]+)?)(?:\s+·\s+Claude Max|\s*[·|]|$)/i)
    if (claude?.[1]) return claude[1].trim()
  }

  return ''
}

function formatTerminalMeta(terminal: TerminalSnapshot): string {
  const chip = formatTransportChip(terminal)
  const modelLabel = formatTerminalModelLabel(terminal)
  const parts: string[] = [
    isArchivedTurnTerminal(terminal) ? 'Previous turn' : '',
    chip,
    modelLabel,
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

function formatSelectedTerminalMeta(terminal: TerminalSnapshot): string {
  return [formatTerminalMeta(terminal), formatUpdatedAge(terminal)].filter(Boolean).join(' · ')
}

function terminalPreValidationSummary(terminal: TerminalSnapshot): string {
  return terminal.status?.pre_validation_summary || ''
}

function terminalPreValidationClass(terminal: TerminalSnapshot): string {
  switch ((terminal.status?.pre_validation_status || '').toLowerCase()) {
    case 'passed':
      return 'text-emerald-300'
    case 'failed':
      return 'text-red-300'
    default:
      return 'text-neutral-400'
  }
}

function terminalPreValidationChip(terminal: TerminalSnapshot): { label: string; className: string; title: string } | null {
  const summary = terminalPreValidationSummary(terminal)
  if (!summary) return null

  const passed = terminal.status?.pre_validation_passed_checks || 0
  const failed = terminal.status?.pre_validation_failed_checks || 0
  const total = terminal.status?.pre_validation_total_checks || 0
  const countLabel = total > 0 ? `${passed}/${total}` : ''
  switch ((terminal.status?.pre_validation_status || '').toLowerCase()) {
    case 'passed':
      return {
        label: countLabel ? `✓ ${countLabel}` : '✓',
        className: 'border-emerald-700/60 bg-emerald-950/30 text-emerald-300',
        title: summary,
      }
    case 'failed':
      return {
        label: failed > 0 ? `✕ ${failed}` : '✕',
        className: 'border-red-700/60 bg-red-950/30 text-red-300',
        title: summary,
      }
    default:
      return {
        label: countLabel ? `• ${countLabel}` : '•',
        className: 'border-neutral-700 bg-neutral-900 text-neutral-400',
        title: summary,
      }
  }
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
    case 'stale':
      return 'stale'
    case 'closing':
      // The tmux process is already gone (killed 30s after task end);
      // what this countdown measures is when the read-only snapshot
      // expires from the rail. "closes" reads like a live process is
      // shutting down — say "kept" so the user knows the work is done.
      return `kept ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}`
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
    case 'stale':
      return 'Stale: no terminal updates were received for a long time; this pane may have lost its lifecycle event.'
    case 'closing':
      return `Snapshot: the agent finished and this read-only view will be removed in ${formatCloseCountdown(terminalSecondsUntilClose(terminal))}.`
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
    case 'stale':
      return 'bg-zinc-400'
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
    case 'stale':
      return 'text-zinc-300'
    case 'closing':
      return 'text-amber-300'
    default:
      return 'text-neutral-500'
  }
}

function canDismissTerminal(terminal: TerminalSnapshot): boolean {
  const state = terminalState(terminal)
  return state === 'completed' || state === 'closing' || state === 'failed' || state === 'stale'
}

function canForceCompleteTerminal(terminal: TerminalSnapshot): boolean {
  const state = terminalState(terminal)
  return state === 'running' || state === 'stale'
}

function canSendTerminalDebugInput(terminal: TerminalSnapshot): boolean {
  return Boolean(terminal.tmux_session)
}

function hasTerminalDebugActions(terminal: TerminalSnapshot): boolean {
  return Boolean(terminal.terminal_id)
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`
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

function trimTerminalDisplayContent(content: string): string {
  // Tmux screen captures often include the pane's empty rows after the last
  // prompt. Keep the raw snapshot in state, but do not render those trailing
  // blank rows or first-open auto-scroll lands on empty space.
  return content.replace(/(?:[ \t\r]*\n)+[ \t\r]*$/g, '')
}

function terminalUpdatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.updated_at || terminal.created_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

function terminalCreatedTime(terminal: TerminalSnapshot): number {
  const value = new Date(terminal.created_at || terminal.updated_at).getTime()
  return Number.isNaN(value) ? 0 : value
}

function formatUpdatedAge(terminal: TerminalSnapshot): string {
  const updatedAt = terminalUpdatedTime(terminal)
  if (!updatedAt) return ''
  const seconds = Math.max(0, Math.floor((Date.now() - updatedAt) / 1000))
  if (seconds < 5) return 'updated now'
  if (seconds < 60) return `updated ${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `updated ${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  return `updated ${hours}h ago`
}

function formatFallbackSeconds(seconds: number): string {
  if (seconds <= 0) return 'now'
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const rem = seconds % 60
  return rem ? `${minutes}m ${rem}s` : `${minutes}m`
}

function hasPromptCompletionFallbackText(content?: string): boolean {
  if (!content) return false
  return /status:\s*complete(d)?/i.test(content)
}

function terminalFallbackInfo(terminal: TerminalSnapshot): { label: string; title: string } | null {
  if (!terminal.active || terminalState(terminal) !== 'running') return null
  const updatedAt = terminalUpdatedTime(terminal)
  if (!updatedAt) return null
  const ageSeconds = Math.max(0, Math.floor((Date.now() - updatedAt) / 1000))
  const hasPromptFallback = hasPromptCompletionFallbackText(terminal.content)
  const threshold = hasPromptFallback ? PROMPT_COMPLETION_FALLBACK_SECONDS : INACTIVE_FALLBACK_SECONDS
  const remaining = Math.max(0, threshold - ageSeconds)
  if (remaining > 30) return null
  const rule = hasPromptFallback ? 'completion fallback' : 'idle fallback'
  const label = remaining > 0
    ? `${rule} in ${formatFallbackSeconds(remaining)}`
    : `${rule} due`
  const title = hasPromptFallback
    ? `Backend will mark this terminal completed if the STATUS: COMPLETED fallback remains unchanged for ${formatFallbackSeconds(PROMPT_COMPLETION_FALLBACK_SECONDS)}.`
    : `Backend will mark this terminal completed if the screen has no changes for ${formatFallbackSeconds(INACTIVE_FALLBACK_SECONDS)}.`
  return { label, title }
}

// isMainAgentTerminal returns true for the persistent chat-session
// terminal that the user keeps coming back to. We pin it to the top of
// every list so it's the first thing the eye lands on when switching
// to Debug view.
function isMainAgentTerminal(terminal: TerminalSnapshot): boolean {
  const kind = (terminal.execution_kind || '').toLowerCase()
  return kind === 'main_agent' || kind === 'main' || kind === 'chat'
}

function isArchivedTurnTerminal(terminal: TerminalSnapshot): boolean {
  return terminal.terminal_id.includes(':turn-')
}

// turnIndexFromTerminalID parses ":turn-N" out of an archived-turn terminal_id.
// Returns 0 for terminals that don't carry a turn marker so the caller can
// safely sort mixed lists.
function turnIndexFromTerminalID(terminalID: string): number {
  const m = terminalID.match(/:turn-(\d+)/)
  return m ? parseInt(m[1], 10) : 0
}

// findPriorArchivedTurns returns the `:turn-N` archived terminals for the
// same session as `current`, sorted in chronological order. Used both to
// drive the lazy content fetch and to stitch the aggregated scrollback.
function findPriorArchivedTurns(current: TerminalSnapshot, allTerminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const sessionID = (current.session_id || '').trim()
  if (!sessionID || isArchivedTurnTerminal(current) || !isSyntheticTerminal(current)) {
    return []
  }
  const matchingTurns = allTerminals
    .filter(t =>
      t.terminal_id !== current.terminal_id &&
      (t.session_id || '').trim() === sessionID &&
      isArchivedTurnTerminal(t) &&
      isSyntheticTerminal(t),
    )
    .sort((a, b) => turnIndexFromTerminalID(a.terminal_id) - turnIndexFromTerminalID(b.terminal_id))
  return matchingTurns.slice(-MAX_PRIOR_ARCHIVED_TURNS_TO_INLINE)
}

// aggregatePriorTurnContent stitches archived :turn- snapshot bodies (read
// from `contentByID` — the rail poll fetches metadata only, so per-archived
// content has to be loaded on demand and cached) in front of the current
// live terminal's content. Result reads like a tmux pane scrollback.
function aggregatePriorTurnContent(
  current: TerminalSnapshot,
  priorTurns: TerminalSnapshot[],
  contentByID: Record<string, string>,
): string {
  const currentContent = current.content || ''
  if (priorTurns.length === 0) return currentContent
  const parts: string[] = []
  for (const t of priorTurns) {
    const cached = contentByID[t.terminal_id]
    const c = (cached ?? t.content ?? '').trim()
    if (c) parts.push(c)
  }
  if (currentContent.trim()) parts.push(currentContent)
  return parts.join('\n\n')
}

function sortTerminalsNewestFirst(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    const mainDelta = (isMainAgentTerminal(b) && !isArchivedTurnTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) && !isArchivedTurnTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    const archivedDelta = (isArchivedTurnTerminal(a) ? 1 : 0) - (isArchivedTurnTerminal(b) ? 1 : 0)
    if (archivedDelta !== 0) return archivedDelta
    return terminalUpdatedTime(b) - terminalUpdatedTime(a)
  })
}

function sortTerminalsForRail(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  return [...terminals].sort((a, b) => {
    // Rail order must not depend on state or updated_at. A pane moving
    // from running -> completed, or receiving a fresh tmux scrape,
    // should only change its dot/content, not jump around the list.
    const currentMainDelta = (isMainAgentTerminal(b) && !isArchivedTurnTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) && !isArchivedTurnTerminal(a) ? 1 : 0)
    if (currentMainDelta !== 0) return currentMainDelta
    const archivedDelta = (isArchivedTurnTerminal(a) ? 1 : 0) - (isArchivedTurnTerminal(b) ? 1 : 0)
    if (archivedDelta !== 0) return archivedDelta
    const mainDelta = (isMainAgentTerminal(b) ? 1 : 0) - (isMainAgentTerminal(a) ? 1 : 0)
    if (mainDelta !== 0) return mainDelta
    const createdDelta = terminalCreatedTime(a) - terminalCreatedTime(b)
    if (createdDelta !== 0) return createdDelta
    return terminalPaneKey(a).localeCompare(terminalPaneKey(b))
  })
}

function terminalPaneKey(terminal: TerminalSnapshot): string {
  return terminal.terminal_id
}

function terminalDetailCacheKey(terminal: TerminalSnapshot): string {
  return `${terminal.terminal_id}:${terminal.chunk_index}:${terminal.updated_at || terminal.created_at || ''}`
}

function dedupeTerminalsByID(terminals: TerminalSnapshot[]): TerminalSnapshot[] {
  const byID = new Map<string, TerminalSnapshot>()
  for (const terminal of terminals) {
    const existing = byID.get(terminal.terminal_id)
    const terminalIsRunning = terminalState(terminal) === 'running'
    const existingIsRunning = existing ? terminalState(existing) === 'running' : false
    if (
      !existing ||
      (terminalIsRunning && !existingIsRunning) ||
      (
        terminalIsRunning === existingIsRunning &&
        terminalUpdatedTime(terminal) >= terminalUpdatedTime(existing)
      )
    ) {
      byID.set(terminal.terminal_id, terminal)
    }
  }
  return Array.from(byID.values())
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

const TERMINAL_USER_PREVIEW_CHARS = 180
const TERMINAL_TOOL_ARGS_PREVIEW_CHARS = 240

function compactTerminalPreview(value: string, maxChars: number = TERMINAL_USER_PREVIEW_CHARS): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  const chars = Array.from(compact)
  if (chars.length <= maxChars) return compact
  return `${chars.slice(0, maxChars).join('')}...`
}

function shouldCollapseUserMessage(value: string): boolean {
  return value.includes('\n') || Array.from(value.trim()).length > TERMINAL_USER_PREVIEW_CHARS
}

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
    // Coalesce consecutive assistant continuation lines into one row. Join
    // with a newline (not a space) so the markdown structure that the model
    // emitted — lists, paragraph breaks, fenced code, headings — survives
    // and ReactMarkdown can render it. An empty continuation line becomes a
    // blank line, which markdown reads as a paragraph break.
    if (classified.kind === 'asst' && rows.length > 0 && rows[rows.length - 1].kind === 'asst') {
      const prev = rows[rows.length - 1] as { kind: 'asst'; text: string }
      prev.text = prev.text ? `${prev.text}\n${classified.text}` : classified.text
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
  // Optional snapshot is used to render a Claude-Code-style bottom status
  // footer (model · turns · tokens · cost · elapsed) and to drive the
  // streaming spinner when the pane is still active.
  terminal?: TerminalSnapshot | null
}

// SPINNER_FRAMES animates while a synthetic terminal is still active. Same
// vocabulary Claude Code's TUI uses for its working-state indicator.
const SPINNER_FRAMES = ['◐', '◓', '◑', '◒']

function useSpinnerFrame(active: boolean): string {
  const [frame, setFrame] = useState(0)
  useEffect(() => {
    if (!active) return
    const id = window.setInterval(() => {
      setFrame(f => (f + 1) % SPINNER_FRAMES.length)
    }, 110)
    return () => window.clearInterval(id)
  }, [active])
  return SPINNER_FRAMES[frame]
}

// terminalMarkdownComponents tunes ReactMarkdown's element rendering so the
// assistant's markdown reads cleanly inside a terminal pane: monospace
// throughout, no oversized headings, subtle bullets, inline-code chips with
// a faint background. Keeps fenced code blocks readable without dominating
// the scrollback.
const terminalMarkdownComponents = {
  h1: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-2 mb-1 text-cyan-300 font-semibold uppercase tracking-wide text-[12px]">{children}</div>
  ),
  h2: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-2 mb-0.5 text-cyan-300 font-semibold text-[12px]">{children}</div>
  ),
  h3: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-1.5 mb-0.5 text-cyan-200 font-semibold text-[12px]">{children}</div>
  ),
  h4: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-1 mb-0.5 text-neutral-200 font-semibold text-[12px]">{children}</div>
  ),
  h5: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-1 mb-0.5 text-neutral-200 font-semibold text-[12px]">{children}</div>
  ),
  h6: ({ children }: { children?: React.ReactNode }) => (
    <div className="mt-1 mb-0.5 text-neutral-300 font-semibold text-[12px]">{children}</div>
  ),
  p: ({ children }: { children?: React.ReactNode }) => (
    <div className="text-neutral-100 break-words my-1.5 leading-6">{children}</div>
  ),
  ul: ({ children }: { children?: React.ReactNode }) => (
    <ul className="my-1.5 ml-3 list-none text-neutral-100 space-y-0.5">{children}</ul>
  ),
  ol: ({ children }: { children?: React.ReactNode }) => (
    <ol className="my-1.5 ml-3 list-none text-neutral-100 space-y-0.5">{children}</ol>
  ),
  li: ({ children }: { children?: React.ReactNode }) => (
    <li className="relative pl-4 leading-6 before:absolute before:left-0 before:top-0 before:text-neutral-500 before:content-['•']">{children}</li>
  ),
  strong: ({ children }: { children?: React.ReactNode }) => (
    <span className="text-amber-200 font-semibold">{children}</span>
  ),
  em: ({ children }: { children?: React.ReactNode }) => (
    <span className="text-neutral-200 italic">{children}</span>
  ),
  code: ({ inline, children }: { inline?: boolean; children?: React.ReactNode }) => inline
    ? <code className="rounded bg-neutral-800 px-1 text-amber-200">{children}</code>
    : <code className="text-neutral-200">{children}</code>,
  pre: ({ children }: { children?: React.ReactNode }) => (
    <pre className="my-1 rounded bg-neutral-900/80 border border-neutral-800 p-2 overflow-x-auto whitespace-pre text-[11.5px] text-neutral-100">{children}</pre>
  ),
  a: ({ children, href }: { children?: React.ReactNode; href?: string }) => (
    <a href={href} target="_blank" rel="noreferrer" className="text-cyan-300 underline decoration-dotted">{children}</a>
  ),
  blockquote: ({ children }: { children?: React.ReactNode }) => (
    <div className="border-l-2 border-neutral-700 pl-2 text-neutral-300 my-0.5">{children}</div>
  ),
  hr: () => <div className="my-1.5 border-t border-dashed border-neutral-800" />,
  table: ({ children }: { children?: React.ReactNode }) => (
    <div className="my-1 overflow-x-auto"><table className="border-collapse text-[11.5px]">{children}</table></div>
  ),
  th: ({ children }: { children?: React.ReactNode }) => (
    <th className="border border-neutral-700 px-2 py-0.5 text-left text-cyan-200">{children}</th>
  ),
  td: ({ children }: { children?: React.ReactNode }) => (
    <td className="border border-neutral-800 px-2 py-0.5 text-neutral-200">{children}</td>
  ),
}

const TerminalAssistantMarkdown: React.FC<{ text: string }> = ({ text }) => (
  <ReactMarkdown remarkPlugins={[remarkGfm]} components={terminalMarkdownComponents}>
    {text}
  </ReactMarkdown>
)

function formatTokens(n?: number): string {
  if (n === undefined || n === null) return '–'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function formatStatusFooterCost(usd?: number): string {
  if (usd === undefined || usd === null || usd === 0) return ''
  return `$${formatCost(usd)}`
}

const StructuredTerminalView: React.FC<StructuredTerminalViewProps> = ({ content, scrollRef, onScroll, terminal }) => {
  const rows = useMemo(() => parseTerminalContent(content), [content])
  // Long user prompts and tool rows collapse behind one-line summaries.
  // We key by row index; content updates are append-only so indexes stay
  // stable for existing rows.
  const [expanded, setExpanded] = useState<Set<number>>(new Set())
  const toggle = useCallback((idx: number) => {
    setExpanded(prev => {
      const next = new Set(prev)
      if (next.has(idx)) next.delete(idx)
      else next.add(idx)
      return next
    })
  }, [])
  const isStreaming = !!terminal?.active && (terminal.state === 'running' || terminal.state === 'idle' || terminal.state === undefined)
  const spinner = useSpinnerFrame(isStreaming)
  return (
    <div className="min-h-0 min-w-0 flex-1 flex flex-col overflow-hidden bg-[#0b0d0c]">
      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain px-3 py-2.5 font-mono text-[12px] leading-5 selection:bg-cyan-500/25"
      >
        {rows.map((row, idx) => {
          // Subtle divider between turns: a new banner (other than the first
          // row) marks a fresh turn boundary in the aggregated scrollback.
          const showDivider = row.kind === 'banner' && idx > 0
          switch (row.kind) {
            case 'banner':
              return (
                <div key={idx}>
                  {showDivider && <div className="my-2 border-t border-dashed border-neutral-700/60" />}
                  <div className="text-cyan-300/90">
                    <span className="text-neutral-500">$ </span>{row.text}
                  </div>
                </div>
              )
            case 'context':
              return <div key={idx} className="text-neutral-500">↳ {row.text}</div>
            case 'user':
              {
                const longUserMessage = shouldCollapseUserMessage(row.text)
                const isOpen = expanded.has(idx) || !longUserMessage
                const preview = compactTerminalPreview(row.text)
                return (
                  <div key={idx} className="my-1 rounded border border-cyan-900/55 bg-cyan-950/15 px-2 py-1">
                    {longUserMessage ? (
                      <>
                        <button
                          type="button"
                          onClick={() => toggle(idx)}
                          className="w-full rounded px-0.5 -mx-0.5 text-left text-cyan-100 hover:bg-cyan-950/30"
                        >
                          <span className="text-cyan-300">&gt;</span>
                          <span className="ml-1">{preview}</span>
                          <span className="ml-1 text-cyan-500/70">{isOpen ? '▾' : '▸'}</span>
                        </button>
                        {isOpen && (
                          <pre className="mt-1 ml-3 whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-cyan-50">
                            {row.text}
                          </pre>
                        )}
                      </>
                    ) : (
                      <div className="text-cyan-50 whitespace-pre-wrap break-words">
                        <span className="text-cyan-300">&gt;</span> {row.text}
                      </div>
                    )}
                  </div>
                )
              }
            case 'asst':
              return (
                <div key={idx} className="my-1.5 border-l border-neutral-700/70 pl-3 text-[12.5px] leading-6">
                  <TerminalAssistantMarkdown text={row.text} />
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
                <div key={idx} className="my-0.5">
                  <button
                    type="button"
                    onClick={() => toggle(idx)}
                    className="w-full rounded px-0.5 -mx-0.5 text-left hover:bg-white/5"
                  >
                    <span className={statusColor}>
                      {hasResult ? (isError ? '✗' : '⏺') : '→'}
                    </span>
                    <span className="text-amber-200 ml-1">{row.name}</span>
                    {row.args && (
                      <span className="text-neutral-400 ml-1" title={row.args}>
                        ({!isOpen ? compactTerminalPreview(row.args, TERMINAL_TOOL_ARGS_PREVIEW_CHARS) : row.args})
                      </span>
                    )}
                    {hasResult && longResult && (
                      <span className="text-neutral-600 ml-1">{isOpen ? '▾' : '▸'}</span>
                    )}
                  </button>
                  {hasResult && isOpen && (
                    <div className={`ml-1 flex whitespace-pre-wrap break-words ${isError ? 'text-red-300' : 'text-neutral-300'}`}>
                      <span className="text-neutral-600 select-none mr-1">└─</span>
                      <span className="flex-1">{row.result}</span>
                    </div>
                  )}
                </div>
              )
            }
            case 'attachment':
              return <div key={idx} className="text-neutral-500">{row.text}</div>
            case 'done':
              return <div key={idx} className="text-emerald-500/70 mt-1 text-[10.5px] font-mono">{row.text}</div>
            case 'error':
              return <div key={idx} className="text-red-400">[error] {row.text}</div>
            case 'plain':
              return <div key={idx} className="text-neutral-300 whitespace-pre-wrap break-words">{row.text}</div>
          }
        })}
        {isStreaming && (
          <div className="mt-1 text-cyan-300/80">{spinner} <span className="text-neutral-500">working…</span></div>
        )}
      </div>
      {terminal && (() => {
        const st = terminal.status || {}
        const tokensIn = formatTokens(st.input_tokens)
        const tokensOut = formatTokens(st.output_tokens)
        const cost = formatStatusFooterCost(st.cost_usd)
        const dur = typeof st.duration_ms === 'number' && st.duration_ms > 0
          ? `${(st.duration_ms / 1000).toFixed(st.duration_ms < 10_000 ? 1 : 0)}s`
          : ''
        const tools = typeof st.tool_count === 'number' && st.tool_count > 0 ? `${st.tool_count} tools` : ''
        const segments = [
          st.provider_label || terminal.label || terminal.execution_kind || 'pane',
          tools,
          tokensIn !== '–' || tokensOut !== '–' ? `${tokensIn} in · ${tokensOut} out` : '',
          cost,
          dur,
        ].filter(Boolean)
        return (
          <div className="flex items-center gap-2 border-t border-neutral-700/70 bg-[#101211] px-3 py-1 font-mono text-[11px] text-neutral-500">
            <span className={isStreaming ? 'text-cyan-300/80' : 'text-neutral-600'}>{isStreaming ? spinner : '·'}</span>
            <span className="truncate">{segments.join('  ·  ')}</span>
          </div>
        )
      })()}
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

// Error event types that should surface as a banner above the terminal
// pane. Tree view renders these as their own cards; in Terminal mode
// they would otherwise be invisible because the rail only shows pane
// content + synthetic terminal chunks.
const TERMINAL_ERROR_EVENT_TYPES = new Set<string>([
  'orchestrator_agent_error',
  'background_agent_failed',
  'conversation_error',
  'workflow_error',
  'agent_error',
  'tool_call_error',
  'llm_generation_error',
  'context_cancelled',
  'batch_execution_canceled',
])

interface TerminalErrorBannerEntry {
  id: string
  type: string
  message: string
  timestamp?: string
}

const TERMINAL_ERROR_MESSAGE_LIMIT = 220

function compactTerminalErrorMessage(message: string): string {
  const singleLine = message.replace(/\s+/g, ' ').trim()
  if (singleLine.length <= TERMINAL_ERROR_MESSAGE_LIMIT) return singleLine
  return `${singleLine.slice(0, TERMINAL_ERROR_MESSAGE_LIMIT)}...`
}

function extractErrorMessage(event: unknown): string {
  const e = event as { type?: string; data?: unknown }
  const data = e?.data as { data?: Record<string, unknown>; message?: string; error?: string } | undefined
  const payload = (data?.data && typeof data.data === 'object') ? data.data : (data as Record<string, unknown> | undefined)
  if (!payload) return ''
  for (const key of ['error', 'message', 'detail', 'reason']) {
    const v = payload[key]
    if (typeof v === 'string' && v.trim()) return v
  }
  return ''
}

const DISMISSED_TERMINAL_ERRORS_KEY_PREFIX = 'terminal-dismissed-errors:'

function dismissedTerminalErrorsKey(sessionId?: string): string | null {
  return sessionId ? `${DISMISSED_TERMINAL_ERRORS_KEY_PREFIX}${sessionId}` : null
}

function readDismissedTerminalErrorIDs(sessionId?: string): Set<string> {
  const key = dismissedTerminalErrorsKey(sessionId)
  if (!key) return new Set()
  try {
    const parsed = JSON.parse(window.localStorage.getItem(key) || '[]')
    return new Set(Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === 'string') : [])
  } catch {
    return new Set()
  }
}

function writeDismissedTerminalErrorIDs(sessionId: string | undefined, ids: Set<string>) {
  const key = dismissedTerminalErrorsKey(sessionId)
  if (!key) return
  try {
    window.localStorage.setItem(key, JSON.stringify(Array.from(ids).slice(-100)))
  } catch {
    // Best-effort UI preference only.
  }
}

export const TerminalCenter: React.FC<TerminalCenterProps> = ({ currentSessionId, compact, hasConversationActivity = false }) => {
  // terminalCenterOpen was the legacy toggle gate (separate sidekick
  // panel); kept here for any callers that still pass the flag but no
  // longer affects rendering — Debug-mode mount is the only gate.
  // Scope terminals to the current chat session. The workflowEventBridge
  // adds every workflow-step event under the chat tab's sessionID, so
  // filtering by currentSessionId surfaces this chat's workflow steps
  // without leaking terminals from other chat tabs / unrelated workflows.
  const [viewAll, setViewAll] = useState(false)
  const [terminals, setTerminals] = useState<TerminalSnapshot[]>([])
  // archivedTurnContents caches `:turn-N` snapshot bodies so we can stitch
  // prior turns into the live synthetic terminal's scrollback without
  // refetching on every render. Archived turns are immutable once written,
  // so the cache is correct without invalidation.
  const [archivedTurnContents, setArchivedTurnContents] = useState<Record<string, string>>({})
  const [terminalDetailCache, setTerminalDetailCache] = useState<Record<string, TerminalSnapshot>>({})
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [userSelectedID, setUserSelectedID] = useState<string | null>(null)
  const [copiedTerminalID, setCopiedTerminalID] = useState<string | null>(null)
  const [dismissedTerminalIDs, setDismissedTerminalIDs] = useState<Set<string>>(() => new Set())
  const [dismissedRouteIDs, setDismissedRouteIDs] = useState<Set<string>>(() => new Set())
  const [dismissedErrorIDs, setDismissedErrorIDs] = useState<Set<string>>(() => readDismissedTerminalErrorIDs(currentSessionId))
  const [expandedErrorIDs, setExpandedErrorIDs] = useState<Set<string>>(() => new Set())
  const [error, setError] = useState<string | null>(null)
  const [debugInput, setDebugInput] = useState('')
  const [terminalActionBusy, setTerminalActionBusy] = useState<string | null>(null)
  const [debugPanelOpenForID, setDebugPanelOpenForID] = useState<string | null>(null)

  const sessionEvents = useChatStore(state => (
    currentSessionId ? state.tabEvents[currentSessionId] : undefined
  ))
  useEffect(() => {
    setDismissedErrorIDs(readDismissedTerminalErrorIDs(currentSessionId))
  }, [currentSessionId])

  const dismissTerminalError = useCallback((errorID: string) => {
    setDismissedErrorIDs(prev => {
      const next = new Set(prev)
      next.add(errorID)
      writeDismissedTerminalErrorIDs(currentSessionId, next)
      return next
    })
  }, [currentSessionId])

  const toggleTerminalError = useCallback((errorID: string) => {
    setExpandedErrorIDs(prev => {
      const next = new Set(prev)
      if (next.has(errorID)) {
        next.delete(errorID)
      } else {
        next.add(errorID)
      }
      return next
    })
  }, [])

  const routingDecisions = useMemo(() => {
    const byKey = new Map<string, RoutingDecision>()
    for (const event of sessionEvents || []) {
      const decision = routingDecisionFromEvent(event)
      if (decision && dismissedRouteIDs.has(decision.id)) continue
      if (!decision) continue
      const key = routeDecisionDedupeKey(decision)
      const existing = byKey.get(key)
      if (!existing || routingDecisionTime(decision) >= routingDecisionTime(existing)) {
        byKey.set(key, decision)
      }
    }
    return Array.from(byKey.values()).sort((a, b) => routingDecisionTime(a) - routingDecisionTime(b))
  }, [sessionEvents, dismissedRouteIDs])
  const routingDecisionByNextStepID = useMemo(() => {
    const byStep = new Map<string, RoutingDecision>()
    for (const decision of routingDecisions) {
      if (!decision.nextStepId || decision.nextStepId === 'end') continue
      byStep.set(decision.nextStepId, decision)
    }
    return byStep
  }, [routingDecisions])
  // Surface error events (llm_generation_error, context_cancelled, etc.)
  // as a banner above the pane. Tree view renders these as their own
  // cards; Terminal mode would otherwise hide them entirely since the
  // rail only carries pane content. Tracks the last few errors for the
  // current session and stays dismissible.
  //
  // CAUTION: the zustand selector returns a value compared by reference.
  // Build the list with useMemo over a narrowly-selected events array
  // so a re-derived list with the same content doesn't trigger an
  // infinite render loop (a previous version returned a fresh [] every
  // call, which Zustand saw as "changed" → re-render → repeat).
  const sessionErrorBanner = useMemo<TerminalErrorBannerEntry[]>(() => {
    if (!sessionEvents || sessionEvents.length === 0) return []
    const out: TerminalErrorBannerEntry[] = []
    for (let i = sessionEvents.length - 1; i >= 0 && out.length < 3; i--) {
      const evt = sessionEvents[i] as unknown as { id?: string; type?: string; timestamp?: string }
      if (!evt?.type || !TERMINAL_ERROR_EVENT_TYPES.has(evt.type)) continue
      const id = evt.id || `${evt.type}-${i}`
      if (dismissedErrorIDs.has(id)) continue
      const message = extractErrorMessage(evt) || evt.type.replace(/_/g, ' ')
      out.push({ id, type: evt.type, message, timestamp: evt.timestamp })
    }
    return out
  }, [sessionEvents, dismissedErrorIDs])
  const terminalOutputRef = useRef<HTMLElement | null>(null)
  const terminalAutoScrollRef = useRef(true)
  const selectedTerminalIDRef = useRef<string | null>(null)
  const fetchInFlightRef = useRef(false)
  const detailRequestSeqRef = useRef(0)
  const terminalsRef = useRef<TerminalSnapshot[]>([])
  const emptyResponseCountRef = useRef(0)
  const lastFetchScopeRef = useRef<string | null>(null)

  useEffect(() => {
    terminalsRef.current = terminals
  }, [terminals])

  const copyTerminalDebug = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminalDebugText(terminal))
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const applyTerminalSnapshotUpdate = useCallback((updated: TerminalSnapshot) => {
    setTerminals(current => current.map(item => (
      item.terminal_id === updated.terminal_id ? { ...item, ...updated } : item
    )))
    setTerminalDetailCache(current => {
      const key = terminalDetailCacheKey(updated)
      const next = Object.fromEntries(
        Object.entries(current).filter(([, detail]) => detail.terminal_id !== updated.terminal_id),
      ) as Record<string, TerminalSnapshot>
      next[key] = updated
      return next
    })
  }, [])

  const forceCompleteTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canForceCompleteTerminal(terminal)) return
    const optimistic: TerminalSnapshot = {
      ...terminal,
      active: false,
      state: 'completed',
      closes_at: undefined,
      retention_seconds: 0,
      updated_at: new Date().toISOString(),
    }
    applyTerminalSnapshotUpdate(optimistic)
    setTerminalActionBusy('complete')
    try {
      const updated = await agentApi.completeTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to mark terminal complete')
    } finally {
      setTerminalActionBusy(current => current === 'complete' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const forceFailTerminal = useCallback(async (terminal: TerminalSnapshot) => {
    const optimistic: TerminalSnapshot = {
      ...terminal,
      active: false,
      state: 'failed',
      closes_at: undefined,
      retention_seconds: 0,
      updated_at: new Date().toISOString(),
    }
    applyTerminalSnapshotUpdate(optimistic)
    setTerminalActionBusy('fail')
    try {
      const updated = await agentApi.failTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to mark terminal failed')
    } finally {
      setTerminalActionBusy(current => current === 'fail' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const refreshTerminalSnapshot = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canSendTerminalDebugInput(terminal)) return
    setTerminalActionBusy('refresh')
    try {
      const updated = await agentApi.refreshTerminal(terminal.terminal_id, { lines: TERMINAL_REFRESH_HISTORY_LINES })
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to refresh terminal')
    } finally {
      setTerminalActionBusy(current => current === 'refresh' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const killTerminalSession = useCallback(async (terminal: TerminalSnapshot) => {
    if (!canSendTerminalDebugInput(terminal)) return
    const confirmed = window.confirm(`Kill tmux session ${terminal.tmux_session}? This stops the underlying coding agent process.`)
    if (!confirmed) return
    setTerminalActionBusy('kill')
    try {
      const updated = await agentApi.killTerminal(terminal.terminal_id)
      applyTerminalSnapshotUpdate(updated)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to kill terminal tmux session')
    } finally {
      setTerminalActionBusy(current => current === 'kill' ? null : current)
    }
  }, [applyTerminalSnapshotUpdate])

  const copyTerminalPaneText = useCallback(async (terminal: TerminalSnapshot) => {
    await navigator.clipboard.writeText(terminal.content || '')
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const copyTmuxAttachCommand = useCallback(async (terminal: TerminalSnapshot) => {
    if (!terminal.tmux_session) return
    await navigator.clipboard.writeText(`tmux attach -t ${shellQuote(terminal.tmux_session)}`)
    setCopiedTerminalID(terminal.terminal_id)
    window.setTimeout(() => setCopiedTerminalID(current => current === terminal.terminal_id ? null : current), 1500)
  }, [])

  const sendTerminalDebugInput = useCallback(async (terminal: TerminalSnapshot, submit: boolean) => {
    const text = debugInput
    if (!canSendTerminalDebugInput(terminal) || text.length === 0) return
    setTerminalActionBusy(submit ? 'paste-enter' : 'paste')
    try {
      await agentApi.sendTerminalInput(terminal.terminal_id, text, submit)
      setDebugInput('')
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to send terminal input')
    } finally {
      setTerminalActionBusy(current => current === (submit ? 'paste-enter' : 'paste') ? null : current)
    }
  }, [debugInput])

  const sendTerminalDebugKey = useCallback(async (terminal: TerminalSnapshot, key: 'enter' | 'esc') => {
    if (!canSendTerminalDebugInput(terminal)) return
    setTerminalActionBusy(key)
    try {
      await agentApi.sendTerminalKey(terminal.terminal_id, key)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to send ${key}`)
    } finally {
      setTerminalActionBusy(current => current === key ? null : current)
    }
  }, [])

  const toggleDebugPanel = useCallback((terminal: TerminalSnapshot) => {
    const key = terminalPaneKey(terminal)
    setSelectedID(key)
    setUserSelectedID(key)
    setDebugPanelOpenForID(current => current === key ? null : key)
  }, [])

  const fetchTerminals = useCallback(async () => {
    if (fetchInFlightRef.current) return
    fetchInFlightRef.current = true
    const fetchScope = viewAll ? 'all' : (currentSessionId || '')
    if (lastFetchScopeRef.current !== fetchScope) {
      lastFetchScopeRef.current = fetchScope
      emptyResponseCountRef.current = 0
    }

    try {
      const response = await agentApi.listTerminals(viewAll ? undefined : currentSessionId, 'none')
      let visibleTerminals = (response.terminals || []).filter(terminal => !dismissedTerminalIDs.has(terminal.terminal_id))

      if (!viewAll && currentSessionId && visibleTerminals.length === 0 && terminalsRef.current.length > 0) {
        const visibleTerminalIDs = new Set(terminalsRef.current.map(terminal => terminal.terminal_id))
        const fallbackResponse = await agentApi.listTerminals(undefined, 'none')
        const recoveredTerminals = (fallbackResponse.terminals || []).filter(terminal =>
          visibleTerminalIDs.has(terminal.terminal_id) &&
          !dismissedTerminalIDs.has(terminal.terminal_id)
        )
        if (recoveredTerminals.length > 0) {
          visibleTerminals = recoveredTerminals
        }
      }

      const nextTerminals = dedupeTerminalsByID(visibleTerminals)
      setTerminals(current => {
        const currentMatchesScope = viewAll || !currentSessionId || current.every(terminal => terminal.session_id === currentSessionId)
        if (!viewAll && currentSessionId && nextTerminals.length === 0 && current.length > 0 && currentMatchesScope) {
          emptyResponseCountRef.current += 1
          if (emptyResponseCountRef.current <= EMPTY_TERMINAL_RESPONSE_GRACE_POLLS) {
            return current
          }
        }
        emptyResponseCountRef.current = nextTerminals.length === 0 ? emptyResponseCountRef.current : 0
        return nextTerminals
      })
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load terminals')
    } finally {
      fetchInFlightRef.current = false
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
    setTerminalDetailCache(current => (
      Object.fromEntries(
        Object.entries(current).filter(([, detail]) => detail.terminal_id !== terminal.terminal_id),
      ) as Record<string, TerminalSnapshot>
    ))
    if (selectedID === terminalPaneKey(terminal)) {
      setSelectedID(null)
    }
    if (userSelectedID === terminalPaneKey(terminal)) {
      setUserSelectedID(null)
    }
    if (debugPanelOpenForID === terminalPaneKey(terminal)) {
      setDebugPanelOpenForID(null)
    }
    try {
      await agentApi.dismissTerminal(terminal.terminal_id)
    } catch (err) {
      // Keep the terminal hidden locally even if the running backend has not picked up
      // the DELETE route yet. A backend restart will make dismissal persistent there too.
      console.warn('Failed to dismiss terminal on backend', err)
    }
  }, [debugPanelOpenForID, selectedID, userSelectedID])

  // buildTree turns a flat list of terminals into a parent → children
  // tree using parent_step_id. Roots are terminals with no parent_step_id
  // (or whose parent isn't in the list — those are surfaced at top level
  // so we never hide a terminal). Each node carries its depth so the
  // rail can indent with paddingLeft = base + depth * step.
  const buildTree = (
    list: TerminalSnapshot[],
    routeByNextStepID: Map<string, RoutingDecision>,
    routeDecisions: RoutingDecision[],
  ): TerminalRailItem[] => {
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
    const out: TerminalRailItem[] = []
    const visited = new Set<string>()
    const renderedRoutes = new Set<string>()
    const walk = (parent: string, depth: number) => {
      // Defense in depth against cycles + degenerate-deep trees.
      if (depth > 32) return
      for (const t of byParent.get(parent) || []) {
        if (t.step_id && visited.has(t.step_id)) continue
        if (t.step_id) visited.add(t.step_id)
        const routeDecision = t.step_id ? routeByNextStepID.get(t.step_id) : undefined
        const terminalDepth = routeDecision ? depth + 1 : depth
        if (routeDecision && !renderedRoutes.has(routeDecision.id)) {
          out.push({ kind: 'route', decision: routeDecision, depth })
          renderedRoutes.add(routeDecision.id)
        }
        out.push({ kind: 'terminal', terminal: t, depth: terminalDepth })
        if (t.step_id) walk(t.step_id, terminalDepth + 1)
      }
    }
    walk('', 0)
    for (const decision of routeDecisions) {
      if (decision.nextStepId && known.has(decision.nextStepId)) continue
      if (!renderedRoutes.has(decision.id)) {
        out.push({ kind: 'route', decision, depth: 0 })
      }
    }
    return out
  }

  const groupedTerminals = useMemo(() => {
    const uniqueTerminals = dedupeTerminalsByID(terminals)
    // Build a single tree from ALL terminals (active + finished).
    // Splitting them was breaking the parent→child relationship when
    // a child step finished while its parent was still running — the
    // child got displaced into the "Finished" group, losing its
    // visual nesting under the parent. One tree keeps lineage intact;
    // the colored dot on each rail row already conveys per-row state.
    const allTerminals = sortTerminalsForRail(uniqueTerminals)
    const activeTerminals = uniqueTerminals.filter(terminal => terminalState(terminal) === 'running')
    const finishedTerminals = uniqueTerminals.filter(terminal => terminalState(terminal) !== 'running')
    const currentTerminals = sortTerminalsNewestFirst(uniqueTerminals.filter(terminal => !isArchivedTurnTerminal(terminal)))
    return {
      activeTerminals,
      finishedTerminals,
      currentTerminals,
      orderedTerminals: allTerminals,
      tree: buildTree(allTerminals, routingDecisionByNextStepID, routingDecisions),
    }
  }, [terminals, routingDecisionByNextStepID, routingDecisions])

  useEffect(() => {
    // Component is now only mounted when Debug view is active (it's
    // not a sidekick panel anymore), so polling should always run
    // whenever this component is on screen. The previous
    // terminalCenterOpen flag gated a standalone toggle that no
    // longer exists.
    void fetchTerminals()
    const interval = window.setInterval(fetchTerminals, TERMINAL_POLL_INTERVAL_MS)
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
    const preferredTerminal = latestActive || groupedTerminals.currentTerminals[0] || groupedTerminals.orderedTerminals[0]

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

    if (
      selected &&
      preferredTerminal &&
      isArchivedTurnTerminal(selected) &&
      !isArchivedTurnTerminal(preferredTerminal) &&
      terminalPaneKey(selected) !== terminalPaneKey(preferredTerminal)
    ) {
      setSelectedID(terminalPaneKey(preferredTerminal))
      return
    }

    if (!selectedID || !selected) {
      setSelectedID(terminalPaneKey(preferredTerminal))
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
  const selectedTerminalDetailCacheKey = selectedTerminal ? terminalDetailCacheKey(selectedTerminal) : null
  const selectedTerminalView = useMemo(() => {
    if (!selectedTerminal) return null
    const cachedDetail = selectedTerminalDetailCacheKey ? terminalDetailCache[selectedTerminalDetailCacheKey] : undefined
    if (cachedDetail && terminalPaneKey(cachedDetail) === selectedTerminalKey) {
      return { ...selectedTerminal, ...cachedDetail }
    }
    return selectedTerminal
  }, [selectedTerminal, selectedTerminalDetailCacheKey, selectedTerminalKey, terminalDetailCache])
  const priorArchivedTurns = useMemo(
    () => (selectedTerminalView ? findPriorArchivedTurns(selectedTerminalView, terminals) : []),
    [selectedTerminalView, terminals],
  )

  // Lazily fetch full content for each prior :turn- snapshot so the
  // aggregated scrollback isn't blank. The rail's listTerminals poll uses
  // content='none' for payload size, so archived turn bodies aren't already
  // in state. Archived snapshots are immutable, so a one-shot fetch per id
  // is enough — keep a cache to skip refetches when terminals state churns.
  useEffect(() => {
    if (priorArchivedTurns.length === 0) return
    let cancelled = false
    const missing = priorArchivedTurns.filter(t => archivedTurnContents[t.terminal_id] === undefined)
    if (missing.length === 0) return
    void Promise.all(missing.map(async t => {
      try {
        const detail = await agentApi.getTerminal(t.terminal_id)
        return { id: t.terminal_id, content: detail.content || '' }
      } catch (err) {
        console.warn('Failed to load archived turn content', t.terminal_id, err)
        return { id: t.terminal_id, content: '' }
      }
    })).then(results => {
      if (cancelled) return
      setArchivedTurnContents(prev => {
        const next = { ...prev }
        for (const r of results) next[r.id] = r.content
        return next
      })
    })
    return () => { cancelled = true }
  }, [priorArchivedTurns, archivedTurnContents])

  const selectedTerminalDisplayContent = useMemo(
    () => {
      if (!selectedTerminalView) return ''
      const aggregated = aggregatePriorTurnContent(selectedTerminalView, priorArchivedTurns, archivedTurnContents)
      return trimTerminalDisplayContent(aggregated)
    },
    [selectedTerminalView, priorArchivedTurns, archivedTurnContents],
  )
  const selectedRouteDecision = selectedTerminalView?.step_id
    ? routingDecisionByNextStepID.get(selectedTerminalView.step_id)
    : undefined
  const railSpinner = useSpinnerFrame(groupedTerminals.activeTerminals.length > 0)

  useEffect(() => {
    if (!selectedTerminal) {
      return
    }
    const detailCacheKey = terminalDetailCacheKey(selectedTerminal)
    if (terminalDetailCache[detailCacheKey]) return
    const requestSeq = detailRequestSeqRef.current + 1
    detailRequestSeqRef.current = requestSeq
    let cancelled = false
    agentApi.getTerminal(selectedTerminal.terminal_id)
      .then(detail => {
        if (!cancelled && detailRequestSeqRef.current === requestSeq && terminalPaneKey(detail) === selectedTerminalKey) {
          setTerminalDetailCache(current => {
            const next: Record<string, TerminalSnapshot> = {
              ...current,
              [terminalDetailCacheKey(detail)]: detail,
            }
            const entries = Object.entries(next)
            if (entries.length <= TERMINAL_DETAIL_CACHE_LIMIT) return next
            return Object.fromEntries(entries.slice(entries.length - TERMINAL_DETAIL_CACHE_LIMIT)) as Record<string, TerminalSnapshot>
          })
        }
      })
      .catch(err => {
        if (!cancelled) {
          console.warn('Failed to load terminal detail', err)
        }
      })
    return () => {
      cancelled = true
    }
  }, [selectedTerminal?.terminal_id, selectedTerminal?.chunk_index, selectedTerminal?.updated_at, selectedTerminalKey, terminalDetailCache])

  const handleTerminalScroll = useCallback(() => {
    const el = terminalOutputRef.current
    if (!el) return
    terminalAutoScrollRef.current = isScrolledNearBottom(el)
  }, [])

  useEffect(() => {
    const el = terminalOutputRef.current
    if (!el || !selectedTerminalDisplayContent) return

    const terminalChanged = selectedTerminalIDRef.current !== selectedTerminalKey
    if (terminalChanged) {
      selectedTerminalIDRef.current = selectedTerminalKey
      terminalAutoScrollRef.current = true
    }

    if (!terminalAutoScrollRef.current) return

    const frame = window.requestAnimationFrame(() => {
      const maxScrollTop = Math.max(0, el.scrollHeight - el.clientHeight)
      el.scrollTop = maxScrollTop
    })
    return () => window.cancelAnimationFrame(frame)
  }, [selectedTerminalKey, selectedTerminalDisplayContent])


  // Rail item — one row in the left rail. Compact vertical layout:
  // dot + step title (top line), transport chip + closing countdown
  // (bottom line). Click → select; hover → highlight.
  // depth controls left padding so child terminals nest under their
  // parent. Tree connectors (└) appear at depth >= 1.
  const renderRouteRailItem = (decision: RoutingDecision, depth: number = 0) => (
    <div
      key={`route-${decision.id}`}
      className="group block w-full border-l-2 border-l-cyan-400/70 bg-cyan-950/15 py-1.5 pl-2.5 pr-2.5 text-left text-xs text-cyan-100"
      title={routeDecisionTitle(decision)}
    >
      <div className="flex items-center gap-1.5">
        {depth > 0 && (
          <span className="shrink-0 select-none whitespace-pre font-mono text-[10px] text-neutral-500" aria-hidden>
            {Array.from({ length: depth - 1 }, () => '│ ').join('')}└─→
          </span>
        )}
        <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded bg-cyan-400/15 text-cyan-300">
          <GitBranch className="h-3 w-3" />
        </span>
        <span className="min-w-0 flex-1 truncate font-semibold">
          Routing: {routeDecisionLabel(decision)}
        </span>
        <button
          type="button"
          onClick={event => {
            event.stopPropagation()
            setDismissedRouteIDs(current => {
              const next = new Set(current)
              next.add(decision.id)
              return next
            })
          }}
          className="shrink-0 rounded p-0.5 text-cyan-300/50 opacity-0 hover:bg-cyan-900/45 hover:text-cyan-100 group-hover:opacity-100 focus:opacity-100"
          title="Remove routing marker from UI"
          aria-label="Remove routing marker from UI"
        >
          <X className="h-3 w-3" />
        </button>
      </div>
      <div className="mt-0.5 flex items-center gap-1.5 text-[10px] text-cyan-300/75">
        <span className="min-w-0 truncate">
          {decision.nextStepId ? `next ${decision.nextStepId}` : decision.stepTitle || decision.stepId || 'route selected'}
        </span>
        {decision.routeCount > 1 && (
          <span className="shrink-0">· {decision.routeCount} routes</span>
        )}
      </div>
    </div>
  )

  const renderRailItem = (terminal: TerminalSnapshot, depth: number = 0) => (
    (() => {
      const preValidationChip = terminalPreValidationChip(terminal)
      const state = terminalState(terminal)
      const isRunning = state === 'running'
      return (
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
              ? 'border-l-emerald-300 bg-[#222826] text-neutral-50 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)]'
              : 'border-l-transparent text-neutral-400 hover:bg-[#1b1f1d] hover:text-neutral-200'
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
                className="shrink-0 rounded p-0.5 text-neutral-500 opacity-0 hover:bg-neutral-700/80 hover:text-neutral-100 group-hover:opacity-100"
                title="Remove terminal from UI"
                aria-label="Remove terminal from UI"
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </div>
          <div className="mt-0.5 flex items-center gap-1.5 text-[10px] opacity-70">
            {isRunning && (
              <span className="shrink-0 font-mono text-cyan-300/90" title="Terminal is running">
                {railSpinner}
              </span>
            )}
            <span className="min-w-0 truncate">{formatTransportChip(terminal)}</span>
            {preValidationChip && (
              <span
                className={`shrink-0 rounded border px-1 py-0.5 text-[9px] font-semibold leading-none ${preValidationChip.className}`}
                title={preValidationChip.title}
              >
                {preValidationChip.label}
              </span>
            )}
            {isArchivedTurnTerminal(terminal) && (
              <span
                className="inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border border-neutral-700/80 text-neutral-500"
                title="Previous turn"
                aria-label="Previous turn"
              >
                <History className="h-2.5 w-2.5" />
              </span>
            )}
            {state === 'closing' && (
              <span className="shrink-0 text-amber-300">· {terminalStateLabel(terminal)}</span>
            )}
            {terminalFallbackInfo(terminal) && (
              <span className="shrink-0 text-amber-300" title={terminalFallbackInfo(terminal)?.title}>
                · {terminalFallbackInfo(terminal)?.label}
              </span>
            )}
          </div>
        </div>
      )
    })()
  )

  return (
    <div className={`flex min-h-0 min-w-0 flex-col bg-[#191a18] text-neutral-100 ${compact ? '' : 'flex-1 overflow-hidden'}`}>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!hasConversationActivity ? (
          <div className="flex flex-1" />
        ) : (
          <>
        {error && (
          <div className="rounded border border-red-800/50 bg-red-950/20 px-3 py-2 text-xs text-red-200">
            {error}
          </div>
        )}

        {sessionErrorBanner.length > 0 && (
          <div className="flex flex-col gap-1 border-b border-red-900/35 bg-red-950/15 px-3 py-2">
            {sessionErrorBanner.map(entry => {
              const isOpen = expandedErrorIDs.has(entry.id)
              return (
              <div key={entry.id} className="text-xs text-red-300">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="shrink-0 rounded bg-red-900/35 px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-red-200">
                    {entry.type.replace(/_/g, ' ')}
                  </span>
                  <span className="min-w-0 flex-1 truncate leading-5" title={entry.message}>
                    {compactTerminalErrorMessage(entry.message)}
                  </span>
                  <button
                    type="button"
                    onClick={() => toggleTerminalError(entry.id)}
                    className="shrink-0 rounded border border-red-800/60 px-2 py-0.5 text-[10px] font-medium text-red-200 hover:bg-red-900/35"
                    aria-expanded={isOpen}
                  >
                    {isOpen ? 'Close' : 'Open'}
                  </button>
                  <button
                    type="button"
                    onClick={() => dismissTerminalError(entry.id)}
                    className="shrink-0 rounded p-0.5 text-red-400/60 hover:bg-red-900/35 hover:text-red-200"
                    title="Dismiss"
                    aria-label="Dismiss error"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
                {isOpen && (
                  <div className="mt-1 max-h-32 overflow-y-auto rounded border border-red-900/45 bg-red-950/25 p-2 font-mono text-[11px] leading-4 text-red-200">
                    {entry.message}
                  </div>
                )}
              </div>
              )
            })}
          </div>
        )}

        {!error && terminals.length === 0 && routingDecisions.length === 0 && (
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

        {groupedTerminals.orderedTerminals.length > 0 && (
          <div className="flex min-h-0 min-w-0 flex-1 gap-0 overflow-hidden border border-neutral-700/70 bg-[#101211]">
            {/* Left rail — vertical list of all terminals. Scrolls
                independently of the right pane so the user can navigate
                a long list without losing the selected terminal's
                content. Hidden below sm breakpoint to save space. */}
            <div className="hidden w-48 shrink-0 flex-col overflow-y-auto overflow-x-hidden border-r border-neutral-700/70 bg-[#141615] sm:flex">
              {groupedTerminals.tree.map(item => (
                item.kind === 'route'
                  ? renderRouteRailItem(item.decision, item.depth)
                  : renderRailItem(item.terminal, item.depth)
              ))}
            </div>

            {/* Right pane — the selected terminal's content. Header
                bar at top (chip + meta + actions), content below. */}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
              {selectedTerminalView ? (
                <>
                  <div className="flex items-center justify-between gap-3 border-b border-neutral-700/70 bg-[#171a18] px-3 py-2 text-xs text-neutral-400">
                    <div className="flex min-w-0 flex-1 items-center gap-2">
                      {selectedRouteDecision && (
                        <span
                          className="inline-flex max-w-[45%] shrink-0 items-center gap-1 rounded border border-cyan-700/60 bg-cyan-950/25 px-1.5 py-0.5 text-[10px] font-medium text-cyan-200"
                          title={routeDecisionTitle(selectedRouteDecision)}
                        >
                          <GitBranch className="h-3 w-3 shrink-0" />
                          <span className="truncate">{routeDecisionLabel(selectedRouteDecision)}</span>
                        </span>
                      )}
                      <span className="min-w-0 flex-1 truncate opacity-80">
                        {formatSelectedTerminalMeta(selectedTerminalView)}
                      </span>
                    </div>
                    <div className="flex shrink-0 items-center gap-2">
                      {terminalState(selectedTerminalView) === 'closing' && (
                        <span
                          className="text-amber-300"
                          title={terminalStateDescription(selectedTerminalView)}
                        >
                          {terminalStateLabel(selectedTerminalView)}
                        </span>
                      )}
                      {terminalFallbackInfo(selectedTerminalView) && (
                        <span
                          className="rounded border border-amber-800/70 bg-amber-950/30 px-1.5 py-0.5 text-[10px] font-medium text-amber-200"
                          title={terminalFallbackInfo(selectedTerminalView)?.title}
                        >
                          {terminalFallbackInfo(selectedTerminalView)?.label}
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={() => void copyTerminalDebug(selectedTerminalView)}
                        className="inline-flex items-center justify-center rounded p-1 text-neutral-500 hover:bg-neutral-800/80 hover:text-neutral-100"
                        title="Copy terminal debug IDs"
                        aria-label="Copy terminal debug IDs"
                      >
                        {copiedTerminalID === selectedTerminalView.terminal_id ? (
                          <Check className="h-3.5 w-3.5 text-emerald-300" />
                        ) : (
                          <Info className="h-3.5 w-3.5" />
                        )}
                      </button>
                      {hasTerminalDebugActions(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => toggleDebugPanel(selectedTerminalView)}
                          className={`inline-flex items-center justify-center rounded border p-1 hover:bg-neutral-800/80 hover:text-neutral-100 ${
                            debugPanelOpenForID === terminalPaneKey(selectedTerminalView)
                              ? 'border-cyan-700/80 text-cyan-300'
                              : 'border-neutral-700/90 text-neutral-300'
                          }`}
                          title="Debug terminal actions"
                          aria-label="Debug terminal actions"
                        >
                          <Bug className="h-3.5 w-3.5" />
                        </button>
                      )}
                      {canDismissTerminal(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => void dismissTerminal(selectedTerminalView)}
                          className="inline-flex items-center justify-center rounded border border-neutral-700/90 p-1 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100"
                          title="Remove terminal from UI"
                          aria-label="Remove terminal from UI"
                        >
                          <X className="h-3.5 w-3.5" />
                        </button>
                      )}
                    </div>
                  </div>
                  {terminalPreValidationSummary(selectedTerminalView) && (
                    <div className={`border-b border-neutral-700/70 bg-[#151716] px-3 py-1.5 text-xs ${terminalPreValidationClass(selectedTerminalView)}`}>
                      {terminalPreValidationSummary(selectedTerminalView)}
                    </div>
                  )}
                  {debugPanelOpenForID === terminalPaneKey(selectedTerminalView) && hasTerminalDebugActions(selectedTerminalView) && (
                    <div className="flex flex-wrap items-center gap-1.5 border-b border-neutral-700/70 bg-[#151716] px-3 py-2 text-xs">
                      <button
                        type="button"
                        onClick={() => void copyTerminalPaneText(selectedTerminalView)}
                        disabled={!selectedTerminalView.content}
                        className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-not-allowed disabled:opacity-40"
                        title="Copy current pane text"
                        aria-label="Copy current pane text"
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </button>
                      {selectedTerminalView.tmux_session && (
                        <button
                          type="button"
                          onClick={() => void copyTmuxAttachCommand(selectedTerminalView)}
                          className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100"
                          title="Copy tmux attach command"
                          aria-label="Copy tmux attach command"
                        >
                          <Terminal className="h-3.5 w-3.5" />
                        </button>
                      )}
                      {canSendTerminalDebugInput(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => void refreshTerminalSnapshot(selectedTerminalView)}
                          disabled={terminalActionBusy === 'refresh'}
                          className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-50"
                          title="Capture deeper tmux history now"
                          aria-label="Capture deeper tmux history now"
                        >
                          <RefreshCw className="h-3.5 w-3.5" />
                        </button>
                      )}
                      {canForceCompleteTerminal(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => void forceCompleteTerminal(selectedTerminalView)}
                          disabled={terminalActionBusy === 'complete'}
                          className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-50"
                          title="Mark terminal complete in UI"
                          aria-label="Mark terminal complete in UI"
                        >
                          <Check className="h-3.5 w-3.5" />
                        </button>
                      )}
                      <button
                        type="button"
                        onClick={() => void forceFailTerminal(selectedTerminalView)}
                        disabled={terminalActionBusy === 'fail'}
                        className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-50"
                        title="Mark terminal failed in UI"
                        aria-label="Mark terminal failed in UI"
                      >
                        <AlertTriangle className="h-3.5 w-3.5" />
                      </button>
                      {canSendTerminalDebugInput(selectedTerminalView) && (
                        <button
                          type="button"
                          onClick={() => void killTerminalSession(selectedTerminalView)}
                          disabled={terminalActionBusy === 'kill'}
                          className="inline-flex h-7 w-7 items-center justify-center rounded border border-red-900/65 text-red-300 hover:bg-red-950/35 hover:text-red-100 disabled:cursor-wait disabled:opacity-50"
                          title="Kill backing tmux session"
                          aria-label="Kill backing tmux session"
                        >
                          <Power className="h-3.5 w-3.5" />
                        </button>
                      )}
                      {canSendTerminalDebugInput(selectedTerminalView) && (
                        <>
                          <input
                            value={debugInput}
                            onChange={event => setDebugInput(event.target.value)}
                            onKeyDown={event => {
                              if (event.key === 'Enter' && !event.shiftKey) {
                                event.preventDefault()
                                void sendTerminalDebugInput(selectedTerminalView, true)
                              }
                            }}
                            placeholder="Debug paste to this tmux terminal"
                            className="min-w-[220px] flex-1 rounded border border-neutral-700/90 bg-[#101211] px-2 py-1.5 text-neutral-100 placeholder:text-neutral-600 focus:border-cyan-500/80 focus:outline-none"
                          />
                          <button
                            type="button"
                            onClick={() => void sendTerminalDebugInput(selectedTerminalView, false)}
                            disabled={debugInput.length === 0 || terminalActionBusy === 'paste'}
                            className="inline-flex items-center justify-center rounded border border-neutral-700/90 p-1.5 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-not-allowed disabled:opacity-40"
                            title="Paste text into tmux"
                            aria-label="Paste text into tmux"
                          >
                            <Send className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => {
                              if (debugInput.length > 0) {
                                void sendTerminalDebugInput(selectedTerminalView, true)
                              } else {
                                void sendTerminalDebugKey(selectedTerminalView, 'enter')
                              }
                            }}
                            disabled={debugInput.length > 0 ? terminalActionBusy === 'paste-enter' : terminalActionBusy === 'enter'}
                            className="inline-flex items-center justify-center rounded border border-neutral-700/90 p-1.5 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-not-allowed disabled:opacity-40"
                            title={debugInput.length > 0 ? 'Paste text and press Enter' : 'Press Enter in tmux'}
                            aria-label={debugInput.length > 0 ? 'Paste text and press Enter' : 'Press Enter in tmux'}
                          >
                            <CornerDownLeft className="h-3.5 w-3.5" />
                          </button>
                          <button
                            type="button"
                            onClick={() => void sendTerminalDebugKey(selectedTerminalView, 'esc')}
                            disabled={terminalActionBusy === 'esc'}
                            className="inline-flex h-7 w-7 items-center justify-center rounded border border-neutral-700/90 text-neutral-300 hover:bg-neutral-800/80 hover:text-neutral-100 disabled:cursor-wait disabled:opacity-50"
                          title="Press Esc in tmux"
                          aria-label="Press Esc in tmux"
                        >
                          <CornerUpLeft className="h-3.5 w-3.5" />
                        </button>
                        </>
                      )}
                    </div>
                  )}
                  {isSyntheticTerminal(selectedTerminalView) ? (
                    <StructuredTerminalView
                      content={selectedTerminalDisplayContent}
                      scrollRef={terminalOutputRef as React.RefObject<HTMLDivElement | null>}
                      onScroll={handleTerminalScroll}
                      terminal={selectedTerminalView}
                    />
                  ) : (
                    <pre
                      ref={terminalOutputRef as React.RefObject<HTMLPreElement | null>}
                      onScroll={handleTerminalScroll}
                      className="min-w-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain bg-[#0b0d0c] p-2.5 font-mono text-[12px] leading-5 whitespace-pre-wrap break-words text-neutral-100 selection:bg-cyan-500/25"
                    >
                      {selectedTerminalDisplayContent}
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
          </>
        )}
      </div>
    </div>
  )
}
