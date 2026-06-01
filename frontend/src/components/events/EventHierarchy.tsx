import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import type { PollingEvent, SessionExecutionTreeResponse } from '../../services/api-types';
import { EventDispatcher, getOwnedTerminalOwnerKeys, type DelegationStats, type EventNode } from './EventDispatcher';
import { agentApi } from '../../services/api';
import { useChatStore } from '../../stores/useChatStore';
import { MAX_EVENTS_TO_PROCESS, MAX_CHILD_EVENTS_PER_DELEGATION } from '../../constants/events';
import { NEVER_DISPLAY_EVENTS, HIDDEN_EVENTS } from './eventModeUtils';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import { useRenderLogger, useMemoLogger } from '../../utils/renderLogger';
import { compareEventsChronologically, summarizeEventForDebug } from '../../utils/eventOrdering';
import './EventHierarchy.css';

// Event types that get grouped and collapsed together as "tool calls".
// llm_generation_end naturally occurs between tool call batches — including it
// prevents many tiny "+ 1 tool" groups from forming.
const TOOL_CALL_TYPES = new Set(['tool_call_start', 'tool_call_end', 'tool_call_error', 'token_usage', 'llm_generation_end']);
const OWNER_COLLAPSED_TOOL_EVENT_TYPES = new Set(['tool_call_start', 'tool_call_end', 'tool_call_error']);

// Delegation events appear in the flat list between tool_call_start and tool_call_end in workflow/multi-agent mode.
// Including them in the scan prevents them from breaking consecutive tool call groups.
const DELEGATION_BRIDGE_TYPES = new Set(['delegation_start', 'delegation_end']);

// Event types that should be parented under an in-progress (still running) agent session.
// Only agent-internal events — NOT user_message, delegation_start, etc.
const IN_PROGRESS_CHILD_TYPES = new Set([
  'tool_call_start', 'tool_call_end', 'tool_call_error',
  'token_usage', 'llm_generation_end', 'llm_generation_start',
]);

const EXECUTION_OWNER_EVENT_TYPES = new Set([
  'delegation_start',
  'background_agent_started',
  'orchestrator_agent_start',
]);

const OWNER_PRIMARY_COMPLETION_EVENT_TYPES = new Set([
  'orchestrator_agent_end',
  'background_agent_completed',
  'background_agent_failed',
  'background_agent_terminated',
  'delegation_end',
  'todo_task_step_completed',
  'batch_group_end',
  'batch_execution_end',
  'workflow_end',
  'agent_end',
]);

const EXECUTION_FAILED_EVENT_TYPES = new Set([
  'orchestrator_agent_error',
  'background_agent_failed',
  'conversation_error',
  'workflow_error',
  'agent_error',
]);

const EXECUTION_CANCELED_EVENT_TYPES = new Set([
  'context_cancelled',
  'batch_execution_canceled',
]);

const EXECUTION_NODE_STATUS_MIRROR_EVENT_TYPES = new Set([
  'orchestrator_agent_end',
  'orchestrator_agent_error',
  'background_agent_completed',
  'background_agent_failed',
  'background_agent_terminated',
]);

const TREE_VISIBLE_EXECUTION_DETAIL_TYPES = new Set([
  'workflow_progress',
  'background_agent_completed',
  'background_agent_failed',
  'routing_evaluated',
  'pre_validation_completed',
  'independent_steps_selected',
  'todo_steps_extracted',
  'todo_task_route_selected',
  'todo_task_item_created',
  'todo_task_item_updated',
  'todo_task_item_completed',
  'todo_task_step_completed',
  'todo_task_status_update',
  'variables_extracted',
  'step_token_usage',
]);

const TREE_TOOL_CALL_PREVIEW_LIMIT = 5;
const AUTO_EXPANDED_OWNER_TOOL_EVENT_LIMIT = 2;
const EVENT_HIERARCHY_DEBUG_STORAGE_KEY = 'debug:event-hierarchy';

function isEventHierarchyDebugEnabled(): boolean {
  if (typeof window === 'undefined') return false;
  const debugWindow = window as unknown as { __EVENT_HIERARCHY_DEBUG__?: boolean };
  if (debugWindow.__EVENT_HIERARCHY_DEBUG__ === true) return true;
  try {
    return window.localStorage.getItem(EVENT_HIERARCHY_DEBUG_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' ? value as Record<string, unknown> : undefined;
}

function getTerminalOwnerPayload(event: PollingEvent): Record<string, unknown> | undefined {
  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  return asRecord(payload?.fields) || payload;
}

function isSyntheticFullWorkflowWrapperEvent(event: PollingEvent): boolean {
  const type = event.type || '';
  if (type !== 'orchestrator_agent_start' && type !== 'orchestrator_agent_end') return false;

  const payload = getTerminalOwnerPayload(event);
  const agentType = payload?.agent_type;
  const agentName = payload?.agent_name;

  return agentType === 'workshop-workflow-execution' && agentName === 'Full Workflow Execution';
}

function firstStringField(records: Array<Record<string, unknown> | undefined>, fields: string[]): string | undefined {
  for (const record of records) {
    if (!record) continue;
    for (const field of fields) {
      const value = record[field];
      if (typeof value === 'string' && value.trim() !== '') return value;
    }
  }
  return undefined;
}

function isExecutionOwnerEvent(event: PollingEvent): boolean {
  return !!event.type && EXECUTION_OWNER_EVENT_TYPES.has(event.type);
}

function isStatusOnlyCompletionText(value: string): boolean {
  const normalized = value
    .replace(/\s+/g, ' ')
    .trim();
  if (!normalized) return true;

  const withoutStepName = normalized
    .replace(/^Workflow step\s*->\s*.+?\s+STATUS:/i, 'STATUS:')
    .trim();
  return /^STATUS:\s*(COMPLETED|FAILED|CANCELED|CANCELLED|TERMINATED)$/i.test(withoutStepName);
}

function getExecutionCompletionText(event: PollingEvent): string {
  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  const fields = asRecord(payload?.fields);
  const error = firstStringField([fields, payload, data], ['error']);
  if (error) return error;

  const result = firstStringField([fields, payload, data], ['result', 'summary', 'message']) || '';
  return isStatusOnlyCompletionText(result) ? '' : result;
}

function isExecutionNodeStatusMirrorEvent(event: PollingEvent): boolean {
  if (getExecutionCompletionText(event) !== '') return false;
  return !!event.type && EXECUTION_NODE_STATUS_MIRROR_EVENT_TYPES.has(event.type);
}

// Only tool-detail events remain behind the collapsed panel on an owner card.
// Non-tool children stay visible inline instead of disappearing into grouped tools.
function isOwnerCollapsedDetailEvent(event: PollingEvent): boolean {
  return !!event.type && OWNER_COLLAPSED_TOOL_EVENT_TYPES.has(event.type);
}

function dedupeOwnerInlineNodes(nodes: EventNode[]): EventNode[] {
  const hasPrimaryCompletion = nodes.some(node => {
    const type = node.event.type || '';
    return OWNER_PRIMARY_COMPLETION_EVENT_TYPES.has(type);
  });

  if (!hasPrimaryCompletion) return nodes;
  return nodes.filter(node => node.event.type !== 'unified_completion');
}

function executionOwnerStatus(node: EventNode): string {
  const payload = getTerminalOwnerPayload(node.event);
  const status = payload?.status;
  if (typeof status === 'string' && status.trim() !== '') return status.trim().toLowerCase();

  const nonExecutionChildren = node.children.filter(child => !isExecutionOwnerEvent(child.event));
  for (const child of nonExecutionChildren) {
    const type = child.event.type || '';
    if (EXECUTION_FAILED_EVENT_TYPES.has(type)) return 'failed';
  }
  for (const child of nonExecutionChildren) {
    const type = child.event.type || '';
    if (EXECUTION_CANCELED_EVENT_TYPES.has(type)) return 'canceled';
  }
  for (const child of nonExecutionChildren) {
    const type = child.event.type || '';
    if (OWNER_PRIMARY_COMPLETION_EVENT_TYPES.has(type)) return 'completed';
  }

  return 'running';
}

function isCompletedExecutionOwnerNode(node: EventNode): boolean {
  return executionOwnerStatus(node) === 'completed';
}

function isTreeVisibleExecutionDetailEvent(event: PollingEvent): boolean {
  return !!event.type && TREE_VISIBLE_EXECUTION_DETAIL_TYPES.has(event.type);
}

function extractAutoNotificationExecutionId(event: PollingEvent): string | undefined {
  if (event.type !== 'user_message') return undefined;

  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  const content = firstStringField([payload, data], ['content', 'message']);
  if (!content?.startsWith('[AUTO-NOTIFICATION]')) return undefined;

  const match = content.match(/\((?:ID:\s*|id=)([^)]+)\)/i);
  const executionId = match?.[1]?.trim();
  return executionId || undefined;
}

function getEventParentExecutionId(event: PollingEvent): string | undefined {
  const eventRecord = event as unknown as Record<string, unknown>;
  const directParent = eventRecord.parent_execution_id;
  if (typeof directParent === 'string' && directParent.trim() !== '') {
    return directParent.trim();
  }

  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  const metadata = asRecord(payload?.metadata);
  const metadataParent = metadata?.parent_execution_id;
  if (typeof metadataParent === 'string' && metadataParent.trim() !== '') {
    return metadataParent.trim();
  }

  const payloadParent = payload?.parent_execution_id;
  if (typeof payloadParent === 'string' && payloadParent.trim() !== '') {
    return payloadParent.trim();
  }

  return undefined;
}

function shouldMaterializeExecutionNode(node: SessionExecutionTreeResponse['root']): boolean {
  return node.kind !== 'session' && node.kind !== 'main_agent';
}

function getBackgroundAgentName(event: PollingEvent): string {
  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  const fields = asRecord(payload?.fields) || payload;
  return firstStringField([fields, payload, data], ['name', 'agent_name']) || '';
}

function normalizedOwnerName(event: PollingEvent): string {
  return getBackgroundAgentName(event)
    .replace(/^(step|background|planner):\s*/i, '')
    .trim()
    .toLowerCase();
}

function isNoisyWorkshopWrapperEvent(event: PollingEvent): boolean {
  if (event.type !== 'background_agent_started') return false;
  return /^step-/i.test(getBackgroundAgentName(event).trim());
}

function isSyntheticExecutionOwnerEvent(event: PollingEvent): boolean {
  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  return payload?.synthetic === true || data?.synthetic === true || event.id.startsWith('synthetic-execution-owner:');
}

function isRootLikeExecutionId(executionId: string): boolean {
  return executionId.startsWith('main:') || executionId.startsWith('session:');
}

function prettyExecutionName(executionId: string): string {
  const tail = executionId.split(':').pop() || executionId;
  if (/^workflow-full-[a-z0-9]+-step-\d+-[a-z0-9]+$/i.test(tail) || /^workflow-step-\d+-[a-z0-9]+$/i.test(tail)) {
    return 'Workflow step';
  }
  return tail.replace(/^workflow-full-[a-z0-9]+-?/i, 'Workflow ').replace(/-[a-z0-9]{10,}$/gi, '').replace(/-/g, ' ').trim() || executionId;
}

function eventDerivedOwnerName(event: PollingEvent, executionId: string): string {
  const data = asRecord(event.data);
  const payload = asRecord(data?.data) || data;
  const metadata = asRecord(payload?.metadata);
  return firstStringField(
    [payload, metadata, data],
    ['agent_name', 'name', 'current_step_id', 'orchestrator_step_id', 'step_id', 'workflow_step_id', 'route_id'],
  ) || prettyExecutionName(executionId);
}

interface InferredExecutionState {
  status: string;
  completedAt?: string;
  error?: string;
}

function createSyntheticExecutionOwnerEventFromEvent(
  executionId: string,
  parentExecutionId: string | undefined,
  event: PollingEvent,
  state?: InferredExecutionState,
): PollingEvent {
  const name = eventDerivedOwnerName(event, executionId);
  const kind = event.execution_kind || (executionId.startsWith('workflow-step:') ? 'workflow_step' : 'execution');
  const status = state?.status || 'running';
  return {
    id: `synthetic-execution-owner:${executionId}`,
    type: 'background_agent_started',
    timestamp: event.timestamp,
    session_id: event.session_id,
    execution_id: executionId,
    parent_execution_id: parentExecutionId,
    execution_kind: kind,
    data: {
      type: 'background_agent_started',
      timestamp: event.timestamp,
      synthetic: true,
      data: {
        agent_id: executionId,
        name,
        status,
        kind,
        source: 'event_stream_fallback',
        completed_at: state?.completedAt,
        error: state?.error,
        fields: {
          agent_id: executionId,
          name,
          status,
          kind,
          source: 'event_stream_fallback',
          completed_at: state?.completedAt,
          error: state?.error,
        },
      },
    },
  } as PollingEvent;
}

function createSyntheticParentExecutionOwnerEvent(
  executionId: string,
  event: PollingEvent,
  state?: InferredExecutionState,
): PollingEvent {
  const name = prettyExecutionName(executionId);
  const kind = executionId.startsWith('workflow-step:') ? 'workflow_step' : 'execution';
  const status = state?.status || 'running';
  return {
    id: `synthetic-execution-owner:${executionId}`,
    type: 'background_agent_started',
    timestamp: event.timestamp,
    session_id: event.session_id,
    execution_id: executionId,
    execution_kind: kind,
    data: {
      type: 'background_agent_started',
      timestamp: event.timestamp,
      synthetic: true,
      data: {
        agent_id: executionId,
        name,
        status,
        kind,
        source: 'event_stream_parent_fallback',
        completed_at: state?.completedAt,
        error: state?.error,
        fields: {
          agent_id: executionId,
          name,
          status,
          kind,
          source: 'event_stream_parent_fallback',
          completed_at: state?.completedAt,
          error: state?.error,
        },
      },
    },
  } as PollingEvent;
}

function createSyntheticExecutionOwnerEvent(
  node: SessionExecutionTreeResponse['root'],
  state?: InferredExecutionState,
): PollingEvent {
  const status = state?.status || node.status;
  const completedAt = state?.completedAt || node.completed_at;
  const error = state?.error || node.error;
  const metadata = node.metadata || {};
  return {
    id: `synthetic-execution-owner:${node.execution_id}`,
    type: 'background_agent_started',
    timestamp: node.started_at,
    session_id: node.session_id,
    execution_id: node.execution_id,
    parent_execution_id: node.parent_execution_id,
    execution_kind: node.kind,
    data: {
      type: 'background_agent_started',
      timestamp: node.started_at,
      synthetic: true,
      data: {
        agent_id: node.execution_id,
        name: node.name || node.kind,
        status,
        kind: node.kind,
        source: node.source,
        completed_at: completedAt,
        error,
        metadata,
        ...metadata,
        fields: {
          agent_id: node.execution_id,
          name: node.name || node.kind,
          status,
          kind: node.kind,
          source: node.source,
          completed_at: completedAt,
          error,
          metadata,
          ...metadata,
        },
      },
    },
  } as PollingEvent;
}

function buildToolSummary(payload?: Record<string, unknown>): { toolName?: string; toolLabel?: string } {
  const toolName = (payload?.tool_name as string) || undefined;
  if (!toolName) return {};

  // Collapsed summaries should not leak long command/query/path parameters.
  // The expanded tool row still contains the full details when the user opens it.
  const compactToolName = toolName.split('__').filter(Boolean).pop() || toolName;
  return { toolName, toolLabel: compactToolName };
}

function selectToolGroupPreviewItems(items: FlattenedItem[], startIdx: number, endIdx: number): FlattenedItem[] {
  const selected = new Map<number, FlattenedItem>();
  for (let idx = startIdx; idx <= endIdx; idx++) {
    const item = items[idx];
    if (item.node?.event.type === 'tool_call_error') {
      selected.set(idx, item);
    }
  }

  for (let idx = endIdx; idx >= startIdx && selected.size < TREE_TOOL_CALL_PREVIEW_LIMIT; idx--) {
    const item = items[idx];
    if (item.node && TOOL_CALL_TYPES.has(item.node.event.type || '')) {
      selected.set(idx, item);
    }
  }

  return Array.from(selected.entries())
    .sort(([a], [b]) => a - b)
    .map(([, item]) => item);
}

interface EventHierarchyProps {
  events: PollingEvent[];
  executionTree?: SessionExecutionTreeResponse;
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  isApproving?: boolean  // Loading state for approve button
  compact?: boolean  // Compact mode for smaller font sizes
  tabId?: string  // Specific tab ID — avoids getActiveTab() so multi-chat panels are independent
}

interface FlattenedItem {
  node?: EventNode;
  uniqueKey: string;
  levelOffset?: number;
  isToolCallToggle?: boolean;
  isCompletedExecutionToggle?: boolean;
  toolCallToggleMode?: 'expand' | 'collapse' | 'terminal_hint';
  completedExecutionToggleMode?: 'expand' | 'collapse';
  hiddenCount?: number;   // Per-group count for the "+" label
  groupKey?: string;      // Group key for per-group expand/collapse
  completedExecutionLevel?: number;
  latestToolName?: string;  // Latest tool_call_start tool name in collapsed group
  latestToolLabel?: string;
}

export const EventHierarchy: React.FC<EventHierarchyProps> = React.memo(({
  events,
  executionTree,
  onApproveWorkflow,
  onSubmitFeedback,
  onFeedbackSubmitted,
  onSendMessage,
  isApproving,
  compact = false,
  tabId: tabIdProp,
}) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
  // Track session keys the user manually expanded — don't auto-collapse these again
  const userExpandedSessionsRef = useRef<Set<string>>(new Set());
  // Sessions we've already auto-collapsed once, so re-running the effect
  // doesn't fight the user clicking expand. Cleared only on session restart.
  const autoCollapsedSessionsRef = useRef<Set<string>>(new Set());
  // Per-group expand state for tool call groups (keyed by first event ID in group)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
  const [expandedCompletedExecutionGroups, setExpandedCompletedExecutionGroups] = useState<Set<string>>(new Set());
  const [expandedOwnedLogPanels, setExpandedOwnedLogPanels] = useState<Set<string>>(new Set());
  const [loadedOlderEvents, setLoadedOlderEvents] = useState<PollingEvent[]>([]);
  const [paginationOffset, setPaginationOffset] = useState<number>(0);
  const [isLoadingOlder, setIsLoadingOlder] = useState<boolean>(false);
  const [hasMoreOlderEvents, setHasMoreOlderEvents] = useState<boolean>(false);
  
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollParent, setScrollParent] = useState<HTMLElement | null>(null);
  // Track previous flattened item count to avoid auto-scrolling on sub-agent events.
  // Sub-agent events change displayEvents → eventTree rebuilds → new object refs, but
  // flattenedItems.length stays the same because delegation_start children aren't flattened.
  const prevFlattenedCountRef = useRef(0);
  const userScrolledUpRef = useRef(false);

  // Find the scrollable parent on mount
  useEffect(() => {
    if (containerRef.current) {
      let parent = containerRef.current.parentElement;
      while (parent) {
        const overflow = window.getComputedStyle(parent).overflowY;
        if (overflow === 'auto' || overflow === 'scroll') {
          setScrollParent(parent);
          break;
        }
        parent = parent.parentElement;
      }
    }
  }, []);

  // Track user scroll-up intent via wheel events (never fires from programmatic scrollTo)
  useEffect(() => {
    const target = scrollParent || containerRef.current;
    if (!target) return;

    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) userScrolledUpRef.current = true;
    };
    const onScroll = () => {
      const el = target;
      if (el.scrollHeight - el.scrollTop - el.clientHeight < 50) {
        userScrolledUpRef.current = false;
      }
    };

    target.addEventListener('wheel', onWheel, { passive: true });
    target.addEventListener('scroll', onScroll, { passive: true });
    return () => {
      target.removeEventListener('wheel', onWheel);
      target.removeEventListener('scroll', onScroll);
    };
  }, [scrollParent]);

  // Look up specific tab (or fall back to active tab for backwards compat)
  const activeTabId = useChatStore(state => state.activeTabId)
  const resolvedTabId = tabIdProp || activeTabId
  const tab = useChatStore(state => resolvedTabId ? state.chatTabs[resolvedTabId] : undefined)
  const setTabViewMode = useChatStore(state => state.setTabViewMode)
  // const setTabHideToolCalls = useChatStore(state => state.setTabHideToolCalls) // kept for future "show all / collapse all"
  const sessionId = tab?.sessionId
  const hideToolCalls = tab?.hideToolCalls || false
  // Holds the last returned displayEvents array for ref-stability.
  // The displayEvents memo below does heavy work (dedup, filter, sort, smart cap).
  // Even when the output is identical, it produces a new array ref — which cascades:
  //   new displayEvents ref → eventTree rebuilds (Map + parent-child linking for all events)
  //     → flattenedItems rebuilds (tree walk + tool-call grouping)
  //       → Virtuoso diffs all items
  // By returning the previous ref when output hasn't changed, this entire chain becomes a no-op.
  const displayEventsRef = useRef<PollingEvent[]>([]);
  const flattenedItemsRef = useRef<FlattenedItem[]>([]);
  // Snapshot of expandedNodes captured alongside the previous flattenedItems
  // result. Used by the togglesMatch shortcut to detect delegation-card
  // expand changes that don't change the flat list structure (delegation
  // children render inside the card, not as flat rows).
  const prevExpandedNodesRef = useRef<Set<string>>(new Set());

  // Merge loaded older events with current events — single-pass filter
  const displayEvents = useMemo(() => {
    // Avoid spread when loadedOlderEvents is empty (common case)
    const source = loadedOlderEvents.length > 0
      ? [...loadedOlderEvents, ...events]
      : events;

    const HIDDEN_STREAMING = new Set(['streaming_start', 'streaming_chunk', 'streaming_end']);
    const HIDDEN_DELEGATION_TOOLS = new Set(['delegate', 'query_agent', 'terminate_agent', 'list_agents']);

    // Single-pass: dedup + all filter conditions at once
    const seenIds = new Set<string>();
    const result: PollingEvent[] = [];

    for (let i = 0; i < source.length; i++) {
      const event = source[i];
      const type = event.type || '';

      // Dedup by ID
      if (seenIds.has(event.id)) continue;
      seenIds.add(event.id);

      // Never-display events
      if (NEVER_DISPLAY_EVENTS.has(type)) continue;

      // The background-agent lifecycle card already represents full workflow
      // execution. The orchestrator event is an internal wrapper for the same
      // execution and otherwise duplicates the visible terminal panel.
      if (isSyntheticFullWorkflowWrapperEvent(event)) continue;

      // Hidden events — never rendered, filtering here prevents them from
      // breaking consecutive tool-call groups into tiny fragments.
      if (HIDDEN_EVENTS.has(type)) continue;

      // Hidden streaming events
      if (HIDDEN_STREAMING.has(type)) continue;

      // Delegation tool events — hide raw tool_call for delegation tools
      if (TOOL_CALL_TYPES.has(type)) {
        const agentEvent = event.data as { data?: { tool_name?: string }; tool_name?: string } | undefined;
        const toolName = agentEvent?.data?.tool_name || agentEvent?.tool_name;
        if (toolName && HIDDEN_DELEGATION_TOOLS.has(toolName)) continue;
      }

      // Additional display filters
      if (type === 'token_usage') {
        const agentEvent = event.data as { data?: Record<string, unknown> } | undefined;
        const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined;
        if (payload?.context === 'conversation_total') continue;
      }
      if (type === 'large_tool_output_detected' || type === 'large_tool_output_file_written') continue;
      if (type === 'orchestrator_agent_end' || type === 'agent_end' || type === 'agent_start') {
        const agentEvent = event.data as { data?: Record<string, unknown>; agent_type?: string } | undefined;
        const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined;
        const agentType = (payload as Record<string, unknown> | undefined)?.agent_type || agentEvent?.agent_type;
        if (agentType === 'simple') continue;
      }

      result.push(event);
    }

    const sourceOrder = new Map<string, number>()
    result.forEach((event, index) => {
      sourceOrder.set(event.id, index)
    })
    result.sort((a, b) => compareEventsChronologically(a, b, sourceOrder));

    // Keep restored conversation history in the timeline.
    // The synthetic conversation_resumed event acts as a visual divider between
    // the restored history and the new follow-up turn, instead of hiding prior events.

    // REF-STABILITY: Return previous array ref when output hasn't changed,
    // preventing downstream cascade (eventTree → flattenedItems → Virtuoso).
    const returnStable = (arr: PollingEvent[]): PollingEvent[] => {
      const prev = displayEventsRef.current;
      if (
        arr.length === prev.length &&
        arr.length > 0 &&
        arr[0]?.id === prev[0]?.id &&
        arr[arr.length - 1]?.id === prev[prev.length - 1]?.id
      ) {
        return prev;
      }
      displayEventsRef.current = arr;
      return arr;
    };

    if (result.length <= MAX_EVENTS_TO_PROCESS) return returnStable(result);

    // Smart cap: preserve structural events across the whole run, then fill the
    // remaining budget with recent detail events. Refreshing a long workflow
    // should not reshape the execution tree just because older context fell out
    // of the non-structural detail budget.
    // Structural events (delegation_start/end, orchestrator boundaries) are always kept
    // because dropping them breaks the tree (orphan children, missing cards).
    // Sub-agent child events are capped per delegation since SubAgentHierarchy only renders 30.
    const STRUCTURAL_TYPES = new Set([
      'delegation_start', 'delegation_end',
      'orchestrator_agent_start', 'orchestrator_agent_end',
      'orchestrator_agent_error',
      'background_agent_started', 'background_agent_completed',
      'background_agent_failed', 'background_agent_terminated',
      'workflow_start', 'workflow_end',
      'workflow_error',
      'orchestrator_start', 'orchestrator_end',
      'batch_group_start', 'batch_group_end',
      'batch_execution_start', 'batch_execution_end', 'batch_execution_canceled',
      'pre_validation_completed',
      'routing_evaluated',
      'todo_task_route_selected', 'todo_task_item_created', 'todo_task_item_updated',
      'todo_task_item_completed', 'todo_task_step_completed', 'todo_task_status_update',
      'learn_code_script_execution',
      'request_human_feedback', 'blocking_human_feedback', 'plan_approval',
      'user_message'
    ]);

    const structuralEvents = new Map<string, PollingEvent>();
    result.forEach(event => {
      if (STRUCTURAL_TYPES.has(event.type || '')) {
        structuralEvents.set(event.id, event);
      }
    });

    const detailBudget = Math.max(MAX_EVENTS_TO_PROCESS - structuralEvents.size, 0);

    // Count children per delegation (events with a delegation- correlation_id)
    const delegationChildCounts = new Map<string, number>();
    const recentDetailEvents: PollingEvent[] = [];

    // Iterate newest-first so we keep the latest children per delegation
    for (let i = result.length - 1; i >= 0; i--) {
      const ev = result[i];
      const type = ev.type || '';

      if (STRUCTURAL_TYPES.has(type)) continue;
      if (recentDetailEvents.length >= detailBudget) break;

      // Check if this is a sub-agent child event (has delegation- correlation_id)
      let delegationId: string | undefined;
      if (ev.data && typeof ev.data === 'object') {
        const data = ev.data as Record<string, unknown>;
        const cid = data.correlation_id as string | undefined;
        if (cid && cid.startsWith('delegation-')) {
          delegationId = cid;
        }
      }

      if (delegationId) {
        const count = delegationChildCounts.get(delegationId) || 0;
        if (count >= MAX_CHILD_EVENTS_PER_DELEGATION) continue; // Over per-delegation budget
        delegationChildCounts.set(delegationId, count + 1);
      }

      recentDetailEvents.push(ev);
    }

    const capped = [...structuralEvents.values(), ...recentDetailEvents];
    capped.sort((a, b) => compareEventsChronologically(a, b, sourceOrder));
    return returnStable(capped);
  }, [events, loadedOlderEvents]);

  // Tool call grouping is done in flattenedItems (after tree building + flattening),
  // so sub-agent events — which are excluded from the flat list at delegation_start nodes —
  // are never mixed into main agent tool call groups.

  // Reset loaded older events when session changes
  useEffect(() => {
    setLoadedOlderEvents([])
    setPaginationOffset(0)
    setExpandedCompletedExecutionGroups(new Set())
    const chatStore = useChatStore.getState()
    const hasMore = sessionId ? chatStore.getTabHasMoreOlderEvents(sessionId) : false
    setHasMoreOlderEvents(hasMore)
  }, [sessionId])
  
  // Helpers to extract hierarchy info
  const getParentId = useCallback((event: PollingEvent): string | undefined => {
    // Check top-level parent_id
    if ('parent_id' in event && event.parent_id) return event.parent_id;

    if (event.data && typeof event.data === 'object') {
      const data = event.data as Record<string, unknown>;

      // Check event.data.parent_id directly (for AgentEvent structure)
      if ('parent_id' in data && typeof data.parent_id === 'string' && data.parent_id) {
        return data.parent_id;
      }

      // Check nested objects within event.data
      for (const [, value] of Object.entries(data)) {
        if (value && typeof value === 'object' && 'parent_id' in value) {
          return (value as { parent_id: string }).parent_id;
        }
      }
    }

    if (event.type === 'delegation_start') {
      const data = event.data as Record<string, unknown>
      const payload = (data.data && typeof data.data === 'object')
        ? data.data as Record<string, unknown>
        : data
      const backgroundAgentId = payload.background_agent_id
      if (typeof backgroundAgentId === 'string' && backgroundAgentId.trim() && event.session_id) {
        return `${event.session_id}_background_agent_started_${backgroundAgentId.trim()}`
      }
    }
    return undefined;
  }, []);

  const getAgentSessionKey = useCallback((event: PollingEvent): string | null => {
    if (event.type !== 'orchestrator_agent_start' && event.type !== 'orchestrator_agent_end') return null;
    let correlationId: string | undefined;
    let agentType: string | undefined;
    if (event.data && typeof event.data === 'object') {
      const data = event.data as Record<string, unknown>;
      let eventData = (data.data && typeof data.data === 'object') ? (data.data as Record<string, unknown>) : undefined;
      if (!eventData) eventData = (data.orchestrator_agent_start || data.orchestrator_agent_end) as Record<string, unknown> | undefined;
      if (!eventData) eventData = data;
      if (eventData) {
        correlationId = eventData.correlation_id as string | undefined;
        agentType = eventData.agent_type as string | undefined;
      }
    }
    return (correlationId && agentType) ? `agent_session:${correlationId}:${agentType}` : null;
  }, []);

  const getExecutionId = useCallback((event: PollingEvent): string | undefined => {
    const autoNotificationExecutionId = extractAutoNotificationExecutionId(event);
    if (autoNotificationExecutionId) return autoNotificationExecutionId;

    const direct = event.execution_id?.trim();
    if (direct) return direct;
    const data = asRecord(event.data);
    const payload = asRecord(data?.data) || data;
    return firstStringField([payload, data], ['execution_id']);
  }, []);

  const summarizeEventForOwnershipDebug = useCallback((event: PollingEvent, index?: number) => {
    const eventRecord = event as unknown as Record<string, unknown>;
    const data = asRecord(event.data);
    const payload = asRecord(data?.data) || asRecord(data?.fields) || data;
    const metadata = asRecord(data?.metadata) || asRecord(payload?.metadata);
    const toolParams = asRecord(payload?.tool_params);

    return {
      index,
      id: event.id,
      type: event.type,
      timestamp: event.timestamp ?? null,
      parentId: getParentId(event) ?? null,
      executionId: getExecutionId(event) ?? null,
      parentExecutionId: event.parent_execution_id ?? null,
      executionKind: event.execution_kind ?? null,
      correlationId: firstStringField([eventRecord, data, payload, metadata], ['correlation_id']) ?? null,
      parentCorrelationId: firstStringField([eventRecord, data, payload, metadata], ['parent_correlation_id']) ?? null,
      delegationId: firstStringField([payload, data], ['delegation_id']) ?? null,
      backgroundAgentId: firstStringField([payload, data], ['background_agent_id']) ?? null,
      agentId: firstStringField([payload, data], ['agent_id']) ?? null,
      agentType: firstStringField([payload, data], ['agent_type']) ?? null,
      workflowId: firstStringField([payload, data, metadata], ['workflow_id', 'workflow_run_id']) ?? null,
      stepId: firstStringField([payload, data, metadata], ['step_id', 'workflow_step_id']) ?? null,
      routeId: firstStringField([payload, data, metadata], ['route_id']) ?? null,
      subAgentStep: firstStringField([payload, data], ['sub_agent_step']) ?? null,
      toolName: firstStringField([payload, toolParams], ['tool_name', 'name']) ?? null,
      dataKeys: data ? Object.keys(data).slice(0, 12) : [],
      payloadKeys: payload ? Object.keys(payload).slice(0, 12) : [],
    };
  }, [getExecutionId, getParentId]);

  // Single-pass derivation: delegationStats + backgroundAgentStats + sessionEvents (was 3 separate useMemos)
  const {
    delegationStats,
    backgroundAgentStats,
    findEventsBetweenStartEnd,
    agentSessionStartIdBySpanKey,
    agentSessionSpanKeyByStartId,
  } = useMemo(() => {
    const dStats = new Map<string, DelegationStats>()
    const bgStats = new Map<string, DelegationStats>()
    const openAgentSessionStarts = new Map<string, Array<{ event: PollingEvent; index: number; spanKey: string }>>()
    const agentSessionSpans: Array<{ spanKey: string; startEvent: PollingEvent; startIndex: number; endEvent?: PollingEvent; endIndex?: number }> = []
    const agentSessionStartIdBySpanKey = new Map<string, string>()
    const agentSessionSpanKeyByStartId = new Map<string, string>()

    // Temp storage for delegation_start events (need dStats populated first for bgStats)
    const delegationStartEvents: { bgAgentId: string; delegationId: string }[] = []

    for (let i = 0; i < displayEvents.length; i++) {
      const event = displayEvents[i]
      const type = event.type

      // --- Session events (was findEventsBetweenStartEnd) ---
      if (type === 'orchestrator_agent_start' || type === 'orchestrator_agent_end') {
        const baseSessionKey = getAgentSessionKey(event)
        if (baseSessionKey) {
          if (type === 'orchestrator_agent_start') {
            const spanKey = `${baseSessionKey}:start:${event.id}`
            const starts = openAgentSessionStarts.get(baseSessionKey) || []
            starts.push({ event, index: i, spanKey })
            openAgentSessionStarts.set(baseSessionKey, starts)
            agentSessionStartIdBySpanKey.set(spanKey, event.id)
            agentSessionSpanKeyByStartId.set(event.id, spanKey)
          } else {
            const starts = openAgentSessionStarts.get(baseSessionKey) || []
            const start = starts.pop()
            if (start) {
              agentSessionSpans.push({
                spanKey: start.spanKey,
                startEvent: start.event,
                startIndex: start.index,
                endEvent: event,
                endIndex: i,
              })
            }
            if (starts.length > 0) openAgentSessionStarts.set(baseSessionKey, starts)
            else openAgentSessionStarts.delete(baseSessionKey)
          }
        }
      }

      if (!event.data || typeof event.data !== 'object') continue
      const data = event.data as Record<string, unknown>

      // --- Delegation stats ---
      const correlationId = data.correlation_id as string | undefined
      if (correlationId) {
        if (!dStats.has(correlationId)) {
          dStats.set(correlationId, { toolCalls: 0, inputTokens: 0, outputTokens: 0 })
        }
        const s = dStats.get(correlationId)!

        if (type === 'tool_call_start') {
          s.toolCalls++
          const payload = (data.data && typeof data.data === 'object') ? data.data as Record<string, unknown> : data
          const { toolName, toolLabel } = buildToolSummary(payload)
          if (toolName) s.latestToolName = toolName
          if (toolLabel) s.latestToolLabel = toolLabel
        }
        if (type === 'tool_call_end' || type === 'token_usage') {
          const payload = (data.data && typeof data.data === 'object') ? data.data as Record<string, unknown> : data
          if (type === 'token_usage') {
            s.inputTokens += (payload.input_tokens as number) || 0
            s.outputTokens += (payload.output_tokens as number) || 0
          }
          if (typeof payload.context_usage_percent === 'number') s.contextUsagePercent = payload.context_usage_percent
          if (typeof payload.context_window_usage === 'number') s.contextWindowUsage = payload.context_window_usage
          if (typeof payload.model_context_window === 'number') s.modelContextWindow = payload.model_context_window
          if (typeof payload.model_id === 'string') s.modelId = payload.model_id
        }
      }

      // Mark completed delegations
      if (type === 'delegation_end') {
        const delegationData = (data.data && typeof data.data === 'object') ? data.data as Record<string, unknown> : data
        const delegationId = (delegationData.delegation_id as string) || (data.correlation_id as string)
        if (delegationId && dStats.has(delegationId)) {
          dStats.get(delegationId)!.completed = true
        }
      }

      // Mark completed orchestrator agent sessions
      if (type === 'orchestrator_agent_end') {
        const cid = data.correlation_id as string | undefined
        if (cid && dStats.has(cid)) {
          dStats.get(cid)!.completed = true
        }
      }

      // Collect delegation_start for bgStats (resolved after loop)
      if (type === 'delegation_start') {
        const delegationData = (data.data && typeof data.data === 'object')
          ? data.data as Record<string, unknown> : data
        const bgAgentId = delegationData?.background_agent_id as string | undefined
        const delegationId = delegationData?.delegation_id as string | undefined
        if (bgAgentId && delegationId) {
          delegationStartEvents.push({ bgAgentId, delegationId })
        }
      }
    }

    // Resolve bgStats now that dStats is complete
    for (const { bgAgentId, delegationId } of delegationStartEvents) {
      const ds = dStats.get(delegationId)
      if (ds) bgStats.set(bgAgentId, ds)
    }

    openAgentSessionStarts.forEach(starts => {
      starts.forEach(start => {
        agentSessionSpans.push({
          spanKey: start.spanKey,
          startEvent: start.event,
          startIndex: start.index,
        })
      })
    })
    agentSessionSpans.sort((a, b) => a.startIndex - b.startIndex)

    // Build session events map. Keys are occurrence-scoped by start event ID,
    // not just correlation_id, because repeated workflow/step runs can reuse
    // correlation IDs inside the same chat.
    const sessionEvents = new Map<string, Set<string>>()
    agentSessionSpans.forEach(span => {
      const eventIds = new Set<string>()
      eventIds.add(span.startEvent.id)

      if (span.endEvent && span.endIndex !== undefined) {
        // Completed session: include all events between start and end by index
        for (let j = span.startIndex + 1; j < span.endIndex; j++) eventIds.add(displayEvents[j].id)
        eventIds.add(span.endEvent.id)
      } else {
        // In-progress session (no end event yet): prefer backend-owned execution_id.
        // Fallback to the older correlation-id allowlist for events emitted before the
        // backend attached execution ownership.
        const sessionExecutionId = getExecutionId(span.startEvent)
        const parts = span.spanKey.split(':')
        const sessionCorrelationId: string | undefined = parts[1] // agent_session:{correlationId}:{agentType}
        for (let j = span.startIndex + 1; j < displayEvents.length; j++) {
          const evt = displayEvents[j]
          if (sessionExecutionId && getExecutionId(evt) === sessionExecutionId) {
            eventIds.add(evt.id)
            continue
          }

          if (sessionCorrelationId) {
            if (!evt.type || !IN_PROGRESS_CHILD_TYPES.has(evt.type)) continue
            const evtData = evt.data as Record<string, unknown> | undefined
            const innerData = (evtData?.data && typeof evtData.data === 'object') ? evtData.data as Record<string, unknown> : undefined
            const evtCid = (evt as unknown as Record<string, unknown>).correlation_id
              ?? innerData?.correlation_id
              ?? evtData?.correlation_id
            if (evtCid === sessionCorrelationId) {
              eventIds.add(evt.id)
            }
          }
        }
      }

      sessionEvents.set(span.spanKey, eventIds)
    })

    return {
      delegationStats: dStats,
      backgroundAgentStats: bgStats,
      findEventsBetweenStartEnd: sessionEvents,
      agentSessionStartIdBySpanKey,
      agentSessionSpanKeyByStartId,
    }
  }, [displayEvents, getAgentSessionKey, getExecutionId]);

  // Auto-collapse agent sessions that completed cleanly — surfaces the
  // agent card + "+N tools/events" affordance instead of a wall of inner
  // event cards. Guardrail: never auto-collapse sessions that emitted an
  // error or cancellation event (the user wants to see what broke). Also
  // skip if the user explicitly expanded this session before.
  useEffect(() => {
    if (findEventsBetweenStartEnd.size === 0) return;
    const ERROR_EVENT_TYPES = new Set([
      'orchestrator_agent_error',
      'background_agent_failed',
      'conversation_error',
      'workflow_error',
      'agent_error',
      'tool_call_error',
      'context_cancelled',
      'batch_execution_canceled',
    ]);
    const eventById = new Map<string, PollingEvent>();
    displayEvents.forEach(e => eventById.set(e.id, e));

    const newlyCollapsing: string[] = [];
    findEventsBetweenStartEnd.forEach((eventIds, sessionKey) => {
      if (autoCollapsedSessionsRef.current.has(sessionKey)) return;
      if (userExpandedSessionsRef.current.has(sessionKey)) return;
      if (collapsedSessions.has(sessionKey)) return;
      // Only collapse once the session has reached its end event.
      let hasEnd = false;
      let hasError = false;
      for (const id of eventIds) {
        const evt = eventById.get(id);
        if (!evt) continue;
        if (evt.type === 'orchestrator_agent_end') hasEnd = true;
        if (evt.type && ERROR_EVENT_TYPES.has(evt.type)) { hasError = true; break; }
      }
      if (!hasEnd || hasError) return;
      newlyCollapsing.push(sessionKey);
    });

    if (newlyCollapsing.length === 0) return;
    newlyCollapsing.forEach(key => autoCollapsedSessionsRef.current.add(key));
    setCollapsedSessions(prev => {
      const next = new Set(prev);
      newlyCollapsing.forEach(key => next.add(key));
      return next;
    });
  }, [findEventsBetweenStartEnd, displayEvents, collapsedSessions]);


  const toggleNode = useCallback((eventId: string) => {
    setExpandedNodes(prev => {
      const newExpanded = new Set(prev);
      if (newExpanded.has(eventId)) newExpanded.delete(eventId);
      else newExpanded.add(eventId);
      return newExpanded;
    });
  }, []);

  const toggleAgentSession = useCallback((sessionKey: string) => {
    setCollapsedSessions(prevCollapsed => {
      const newCollapsed = new Set(prevCollapsed);
      if (newCollapsed.has(sessionKey)) {
        newCollapsed.delete(sessionKey);
        // User is manually expanding — remember so auto-collapse doesn't override
        userExpandedSessionsRef.current.add(sessionKey);
      } else {
        newCollapsed.add(sessionKey);
        userExpandedSessionsRef.current.delete(sessionKey);
      }
      return newCollapsed;
    });
  }, []);

  const toggleToolCallGroup = useCallback((groupKey: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev);
      if (!next.has(groupKey)) next.add(groupKey);
      else next.delete(groupKey);
      return next;
    });
  }, []);

  const toggleCompletedExecutionGroup = useCallback((groupKey: string) => {
    setExpandedCompletedExecutionGroups(prev => {
      const next = new Set(prev);
      if (!next.has(groupKey)) next.add(groupKey);
      else next.delete(groupKey);
      return next;
    });
  }, []);

  const switchToTerminalView = useCallback(() => {
    if (!resolvedTabId) return;
    setTabViewMode(resolvedTabId, 'terminal');
  }, [resolvedTabId, setTabViewMode]);

  const toggleOwnedLogPanel = useCallback((eventId: string, open: boolean) => {
    setExpandedOwnedLogPanels(prev => {
      const next = new Set(prev);
      if (open) next.add(eventId);
      else next.delete(eventId);
      return next;
    });
  }, []);

  const eventTree = useMemo(() => {
    const eventById = new Map<string, PollingEvent>();
    displayEvents.forEach(event => eventById.set(event.id, event));

    const eventsToFilter = new Set<string>();
    findEventsBetweenStartEnd.forEach((eventIds, sessionKey) => {
      if (collapsedSessions.has(sessionKey)) {
        eventIds.forEach(eventId => {
          const event = eventById.get(eventId);
          if (event && event.type !== 'orchestrator_agent_start' && event.type !== 'orchestrator_agent_end') {
            eventsToFilter.add(eventId);
          }
        });
      }
    });

    const realFilteredEvents = displayEvents.filter(event => !eventsToFilter.has(event.id));

    const ownershipSourceEvents = realFilteredEvents;

    const inferredStateByExecutionId = new Map<string, InferredExecutionState>();
    const markExecutionState = (executionId: string | undefined, state: InferredExecutionState) => {
      if (!executionId || isRootLikeExecutionId(executionId)) return;
      const existing = inferredStateByExecutionId.get(executionId);
      if (existing?.status === 'failed' || existing?.status === 'canceled') return;
      if (existing?.status === 'completed' && state.status === 'running') return;
      inferredStateByExecutionId.set(executionId, state);
    };

    for (const event of ownershipSourceEvents) {
      const executionId = getExecutionId(event);
      const payload = asRecord(asRecord(event.data)?.data) || asRecord(event.data);
      const error = firstStringField([payload, asRecord(payload?.fields)], ['error', 'message']);
      switch (event.type) {
        case 'orchestrator_agent_error':
        case 'background_agent_failed':
        case 'conversation_error':
        case 'workflow_error':
        case 'agent_error':
          markExecutionState(executionId, { status: 'failed', completedAt: event.timestamp, error });
          break;
        case 'context_cancelled':
        case 'batch_execution_canceled':
          markExecutionState(executionId, { status: 'canceled', completedAt: event.timestamp, error });
          break;
        case 'orchestrator_agent_end':
        case 'background_agent_completed':
        case 'background_agent_terminated':
        case 'delegation_end':
        case 'todo_task_step_completed':
        case 'batch_group_end':
        case 'batch_execution_end':
        case 'workflow_end':
        case 'unified_completion':
        case 'conversation_end':
        case 'agent_end':
          markExecutionState(executionId, { status: 'completed', completedAt: event.timestamp });
          break;
        default:
          break;
      }
    }

    const executionNodeById = new Map<string, SessionExecutionTreeResponse['root']>();
    const executionParentById = new Map<string, string>();
    const collectExecutionParents = (node?: SessionExecutionTreeResponse['root']) => {
      if (!node) return;
      if (node.execution_id) {
        executionNodeById.set(node.execution_id, node);
      }
      for (const child of node.children || []) {
        if (child.execution_id && child.parent_execution_id) {
          executionParentById.set(child.execution_id, child.parent_execution_id);
        }
        collectExecutionParents(child);
      }
    };
    collectExecutionParents(executionTree?.root);

    const executionOwnerEventById = new Map<string, PollingEvent>();
    for (const event of realFilteredEvents) {
      if (!isExecutionOwnerEvent(event)) continue;
      const executionId = getExecutionId(event);
      if (executionId && !executionOwnerEventById.has(executionId)) {
        executionOwnerEventById.set(executionId, event);
      }
    }

    const syntheticOwnerEvents: PollingEvent[] = [];
    executionNodeById.forEach((node, executionId) => {
      if (!shouldMaterializeExecutionNode(node)) return;
      if (executionOwnerEventById.has(executionId)) return;

      const syntheticEvent = createSyntheticExecutionOwnerEvent(node, inferredStateByExecutionId.get(executionId));
      syntheticOwnerEvents.push(syntheticEvent);
      executionOwnerEventById.set(executionId, syntheticEvent);
    });

    for (const event of ownershipSourceEvents) {
      const executionId = getExecutionId(event);
      if (!executionId || isRootLikeExecutionId(executionId) || executionOwnerEventById.has(executionId)) {
        continue;
      }

      const parentExecutionId = (executionParentById.get(executionId) || getEventParentExecutionId(event))?.trim();
      const syntheticEvent = createSyntheticExecutionOwnerEventFromEvent(
        executionId,
        parentExecutionId,
        event,
        inferredStateByExecutionId.get(executionId),
      );
      syntheticOwnerEvents.push(syntheticEvent);
      executionOwnerEventById.set(executionId, syntheticEvent);
    }

    for (const event of ownershipSourceEvents) {
      const parentExecutionId = getEventParentExecutionId(event)?.trim();
      if (!parentExecutionId || isRootLikeExecutionId(parentExecutionId) || executionOwnerEventById.has(parentExecutionId)) {
        continue;
      }

      const syntheticParent = createSyntheticParentExecutionOwnerEvent(
        parentExecutionId,
        event,
        inferredStateByExecutionId.get(parentExecutionId),
      );
      syntheticOwnerEvents.push(syntheticParent);
      executionOwnerEventById.set(parentExecutionId, syntheticParent);
    }

    const rawExecutionRootNodes = (executionTree?.root?.children || []).filter(shouldMaterializeExecutionNode);
    if (rawExecutionRootNodes.length > 0) {
      const ownerEventIds = new Set(Array.from(executionOwnerEventById.values()).map(event => event.id));
      const attachedEventIds = new Set<string>();
      const eventsByExecutionId = new Map<string, PollingEvent[]>();
      const executionAliasById = new Map<string, string>();
      const virtualExecutionChildrenByParentId = new Map<string, SessionExecutionTreeResponse['root'][]>();
      const reparentedExecutionIds = new Set<string>();

      const normalizeExecutionMatchName = (value: string): string => value
        .replace(/^workflow\s+step\s*->\s*/i, '')
        .replace(/^step-\d+-execution-/i, '')
        .replace(/[-_]+/g, ' ')
        .replace(/\s+/g, ' ')
        .trim()
        .toLowerCase();

      const getWorkflowStepNameFromEvent = (event: PollingEvent): string | undefined => {
        const data = asRecord(event.data);
        const payload = asRecord(data?.data) || data;
        const metadata = asRecord(payload?.metadata);
        return firstStringField(
          [payload, metadata, data],
          ['orchestrator_agent_name', 'current_step_id', 'orchestrator_step_id', 'step_id', 'workflow_step_id'],
        );
      };

      const getExecutionDisplayParentName = (value: string): string | undefined => {
        const parts = value.split(/\s+->\s+/).map(part => part.trim()).filter(Boolean);
        return parts.length > 1 ? parts[0] : undefined;
      };

      const allExecutionNodes = Array.from(executionNodeById.values()).filter(shouldMaterializeExecutionNode);
      const findMatchingWorkshopStepId = (event: PollingEvent): string | undefined => {
        const eventStepName = getWorkflowStepNameFromEvent(event);
        if (!eventStepName) return undefined;

        const normalizedEventStepName = normalizeExecutionMatchName(eventStepName);
        if (!normalizedEventStepName) return undefined;

        const eventTime = Date.parse(event.timestamp || '');
        const candidates = allExecutionNodes
          .filter(candidate => candidate.source === 'background_agent_registry' && candidate.kind === 'workshop_background')
          .filter(candidate => {
            const normalizedCandidateName = normalizeExecutionMatchName(candidate.name || '');
            return normalizedCandidateName === normalizedEventStepName ||
              normalizedCandidateName.endsWith(normalizedEventStepName) ||
              normalizedEventStepName.endsWith(normalizedCandidateName);
          })
          .filter(candidate => {
            const startedAt = Date.parse(candidate.started_at || '');
            const completedAt = Date.parse(candidate.completed_at || '');
            return Number.isNaN(eventTime) ||
              Number.isNaN(startedAt) ||
              eventTime >= startedAt - 1000 && (Number.isNaN(completedAt) || eventTime <= completedAt + 1000);
          })
          .sort((a, b) => {
            const aStartedAt = Date.parse(a.started_at || '');
            const bStartedAt = Date.parse(b.started_at || '');
            if (Number.isNaN(aStartedAt) || Number.isNaN(bStartedAt)) return 0;
            return bStartedAt - aStartedAt;
          });

        return candidates[0]?.execution_id;
      };

      for (const orphan of rawExecutionRootNodes) {
        if (orphan.kind !== 'workflow_sub_agent') continue;

        const displayParentName = getExecutionDisplayParentName(orphan.name || '');
        if (!displayParentName) continue;
        const normalizedParentName = normalizeExecutionMatchName(displayParentName);
        if (!normalizedParentName) continue;

        const orphanStartedAt = Date.parse(orphan.started_at || '');
        const candidates = allExecutionNodes
          .filter(candidate => candidate.execution_id !== orphan.execution_id)
          .filter(candidate => normalizeExecutionMatchName(candidate.name || '') === normalizedParentName)
          .filter(candidate => {
            const candidateStartedAt = Date.parse(candidate.started_at || '');
            return Number.isNaN(orphanStartedAt) ||
              Number.isNaN(candidateStartedAt) ||
              candidateStartedAt <= orphanStartedAt + 1000;
          })
          .sort((a, b) => {
            const aStartedAt = Date.parse(a.started_at || '');
            const bStartedAt = Date.parse(b.started_at || '');
            if (Number.isNaN(aStartedAt) || Number.isNaN(bStartedAt)) return 0;
            return bStartedAt - aStartedAt;
          });

        const parent = candidates[0];
        if (!parent) continue;

        const children = virtualExecutionChildrenByParentId.get(parent.execution_id) || [];
        children.push(orphan);
        virtualExecutionChildrenByParentId.set(parent.execution_id, children);
        reparentedExecutionIds.add(orphan.execution_id);
      }

      const getExecutionChildren = (node: SessionExecutionTreeResponse['root']): SessionExecutionTreeResponse['root'][] => [
        ...(node.children || []).filter(child => !reparentedExecutionIds.has(child.execution_id)),
        ...(virtualExecutionChildrenByParentId.get(node.execution_id) || []),
      ];

      const collectExecutionAliases = (node: SessionExecutionTreeResponse['root']) => {
        const children = getExecutionChildren(node);
        const workshopStepChildren = children.filter(child =>
          child.source === 'background_agent_registry' &&
          child.kind === 'workshop_background' &&
          normalizeExecutionMatchName(child.name || '') !== ''
        );

        for (const child of children) {
          if (child.source !== 'event_stream' || child.kind !== 'workflow_step') continue;
          const stepEventName = realFilteredEvents
            .filter(event => getExecutionId(event) === child.execution_id)
            .map(getWorkflowStepNameFromEvent)
            .find(Boolean);
          if (!stepEventName) continue;

          const normalizedStepEventName = normalizeExecutionMatchName(stepEventName);
          const matchingWorkshopStep = workshopStepChildren.find(candidate =>
            normalizeExecutionMatchName(candidate.name || '') === normalizedStepEventName
          );
          if (matchingWorkshopStep) {
            executionAliasById.set(child.execution_id, matchingWorkshopStep.execution_id);
          }
        }

        children.forEach(collectExecutionAliases);
      };
      collectExecutionAliases(executionTree!.root);
      const executionRootNodes = rawExecutionRootNodes.filter(node => !reparentedExecutionIds.has(node.execution_id));

      for (const event of realFilteredEvents) {
        if (ownerEventIds.has(event.id) || isExecutionOwnerEvent(event)) continue;
        if (isExecutionNodeStatusMirrorEvent(event)) continue;

        const rawExecutionId = getExecutionId(event);
        const rawParentExecutionId = getEventParentExecutionId(event)?.trim();
        const executionId = rawExecutionId
          ? (executionAliasById.get(rawExecutionId) || rawExecutionId)
          : rawParentExecutionId
            ? (executionAliasById.get(rawParentExecutionId) || rawParentExecutionId)
            : findMatchingWorkshopStepId(event);
        if (!executionId || isRootLikeExecutionId(executionId) || !executionNodeById.has(executionId)) {
          const matchedWorkshopStepId = findMatchingWorkshopStepId(event);
          if (!matchedWorkshopStepId || !executionNodeById.has(matchedWorkshopStepId)) continue;
          const eventsForExecution = eventsByExecutionId.get(matchedWorkshopStepId) || [];
          eventsForExecution.push(event);
          eventsByExecutionId.set(matchedWorkshopStepId, eventsForExecution);
          attachedEventIds.add(event.id);
          continue;
        }

        const eventsForExecution = eventsByExecutionId.get(executionId) || [];
        eventsForExecution.push(event);
        eventsByExecutionId.set(executionId, eventsForExecution);
        attachedEventIds.add(event.id);
      }

      const buildAttachedEventNode = (event: PollingEvent, depth: number): EventNode => ({
        event,
        children: [],
        level: depth,
      });

      const buildExecutionNode = (node: SessionExecutionTreeResponse['root'], depth: number): EventNode => {
        const ownerEvent = executionOwnerEventById.get(node.execution_id)
          || createSyntheticExecutionOwnerEvent(node, inferredStateByExecutionId.get(node.execution_id));

        const childExecutionNodes = getExecutionChildren(node)
          .filter(shouldMaterializeExecutionNode)
          .filter(child => !executionAliasById.has(child.execution_id))
          .map(child => buildExecutionNode(child, depth + 1));

        const attachedEventNodes = (eventsByExecutionId.get(node.execution_id) || [])
          .filter(event => event.id !== ownerEvent.id)
          .map(event => buildAttachedEventNode(event, depth + 1));

        return {
          event: ownerEvent,
          children: [...childExecutionNodes, ...attachedEventNodes],
          level: depth,
        };
      };

      const executionRoots = executionRootNodes.map(node => buildExecutionNode(node, 0));
      const fallbackRootEvents = realFilteredEvents.filter(event => {
        if (attachedEventIds.has(event.id)) return false;
        if (ownerEventIds.has(event.id) || isExecutionOwnerEvent(event)) return false;
        const executionId = getExecutionId(event);
        return !executionId || isRootLikeExecutionId(executionId) || !executionNodeById.has(executionId);
      });

      const rootNodes = [
        ...executionRoots,
        ...fallbackRootEvents.map(event => buildAttachedEventNode(event, 0)),
      ];
      return rootNodes.sort((a, b) => compareEventsChronologically(a.event, b.event, new Map()));
    }

    const sourceOrder = new Map<string, number>();
    realFilteredEvents.forEach((event, index) => sourceOrder.set(event.id, index));
    const filteredEvents = [...realFilteredEvents, ...syntheticOwnerEvents]
      .sort((a, b) => compareEventsChronologically(a, b, sourceOrder));
    const filteredEventIds = new Set(filteredEvents.map(e => e.id));

    // Build delegation_id -> delegation_start event ID map for re-parenting orphans.
    // When an intermediate parent within a delegation is evicted, its children become orphans.
    // Instead of showing them as root events in the main chat, re-parent them to delegation_start.
    const delegationIdToEventId = new Map<string, string>();
    // Build precise event membership for orchestrator agent sessions.
    // Important: correlation_id alone is not enough when a restored chat resumes later.
    // New tool calls can reuse a correlation_id from an older agent card, which would
    // incorrectly render them up in old history. Only events that actually fall inside
    // a specific orchestrator_agent_start/end span should be grouped under that card.
    const agentSessionEventToStartId = new Map<string, string>();
    for (const event of filteredEvents) {
      if (event.type === 'delegation_start' && event.data && typeof event.data === 'object') {
        const data = event.data as Record<string, unknown>;
        const delegationData = (data.data && typeof data.data === 'object')
          ? data.data as Record<string, unknown> : data;
        const delegationId = delegationData?.delegation_id as string | undefined;
        if (delegationId) {
          delegationIdToEventId.set(delegationId, event.id);
        }
      }
    }

    findEventsBetweenStartEnd.forEach((eventIds, sessionKey) => {
      const startEventId = agentSessionStartIdBySpanKey.get(sessionKey)
      if (!startEventId) return
      eventIds.forEach((eventId) => {
        if (eventId !== startEventId) {
          agentSessionEventToStartId.set(eventId, startEventId)
        }
      })
    })

    // Helper: extract correlation_id from event data
    const getCorrelationId = (event: PollingEvent): string | undefined => {
      if (!event.data || typeof event.data !== 'object') return undefined;
      const data = event.data as Record<string, unknown>;
      return (data.correlation_id as string | undefined)
        ?? ((data.data && typeof data.data === 'object') ? (data.data as Record<string, unknown>).correlation_id as string | undefined : undefined);
    };

    // Helper: extract delegation correlation_id from event
    const getDelegationCorrelationId = (event: PollingEvent): string | undefined => {
      const cid = getCorrelationId(event);
      return (cid && cid.startsWith('delegation-')) ? cid : undefined;
    };

    const childrenMap = new Map<string, PollingEvent[]>();
    const childEventIds = new Set<string>();

    const getExecutionParentEventId = (event: PollingEvent): string | undefined => {
      const executionId = getExecutionId(event);
      if (!executionId) return undefined;

      const canonicalOwner = executionOwnerEventById.get(executionId);
      if (canonicalOwner && canonicalOwner.id !== event.id) return canonicalOwner.id;

      if (isExecutionOwnerEvent(event)) {
        const parentExecutionId = (executionParentById.get(executionId) || getEventParentExecutionId(event))?.trim();
        if (parentExecutionId) {
          const parentOwner = executionOwnerEventById.get(parentExecutionId);
          if (parentOwner && parentOwner.id !== event.id) return parentOwner.id;
        }
        return undefined;
      }

      const parentExecutionId = (executionParentById.get(executionId) || getEventParentExecutionId(event))?.trim();
      if (parentExecutionId) {
        const parentOwner = executionOwnerEventById.get(parentExecutionId);
        if (parentOwner && parentOwner.id !== event.id) return parentOwner.id;
      }

      return undefined;
    };

    filteredEvents.forEach(event => {
      let parentId = getParentId(event);

      // Re-parent orphaned delegation children: if parent_id is missing from filteredEvents
      // but event belongs to a delegation, attach it to the delegation_start event.
      if (parentId && !filteredEventIds.has(parentId)) {
        const delegCid = getDelegationCorrelationId(event);
        if (delegCid) {
          const delegStartId = delegationIdToEventId.get(delegCid);
          if (delegStartId) {
            parentId = delegStartId;
          }
        }
      }

      // Parent only events that are explicitly inside an orchestrator agent session span.
      // Using correlation_id alone is too broad for restored/resumed chats because later
      // events can otherwise get sucked into an old agent card near the top of history.
      if (!parentId || !filteredEventIds.has(parentId)) {
        const agentStartId = agentSessionEventToStartId.get(event.id)
        if (agentStartId
            && event.type !== 'orchestrator_agent_end'
            && event.type !== 'learn_code_script_execution') {
          // Don't parent the agent_start under itself
          if (event.id !== agentStartId) {
            parentId = agentStartId;
          }
        }
      }

      if (!parentId || !filteredEventIds.has(parentId)) {
        const executionParentId = getExecutionParentEventId(event);
        if (executionParentId) {
          parentId = executionParentId;
        }
      }

      if (parentId && filteredEventIds.has(parentId)) {
        if (!childrenMap.has(parentId)) childrenMap.set(parentId, []);
        childrenMap.get(parentId)!.push(event);
        childEventIds.add(event.id);
      }
    });

    const buildTreeRecursive = (event: PollingEvent, depth: number): EventNode => {
      const children = childrenMap.get(event.id) || [];
      const childNodes = children.map(child => buildTreeRecursive(child, depth + 1));
      return {
        event,
        children: childNodes,
        level: depth,
        // isExpanded is intentionally NOT baked in — consumers read
        // expandedNodes.has(node.event.id) directly so toggles don't
        // invalidate the structural tree memo.
      };
    };

    const rootEvents = filteredEvents.filter(event => {
      if (childEventIds.has(event.id)) return false;

      // Check if this event was parented under an agent session or delegation via childrenMap
      const cid = getCorrelationId(event);

      // Never promote delegation child events to root — they belong inside delegation cards.
      if (cid && cid.startsWith('delegation-')) {
        const delegStartId = delegationIdToEventId.get(cid);
        if (delegStartId && event.id !== delegStartId) return false;
      }

      // Never promote true agent-session child events to root — they belong inside agent cards.
      // orchestrator_agent_end is excluded: it should show at the top level after the agent card.
      const agentStartId = agentSessionEventToStartId.get(event.id)
      if (agentStartId
          && event.type !== 'orchestrator_agent_end'
          && event.type !== 'learn_code_script_execution') {
        if (event.id !== agentStartId) return false; // Child of agent session, not root
      }

      const parentId = getParentId(event);
      // Standard root check: no parent or parent not in filtered set
      const isOrphan = !parentId || !filteredEventIds.has(parentId);
      if (!isOrphan) return false;

      return true;
    });

    return rootEvents.map(event => buildTreeRecursive(event, 0));
  }, [displayEvents, collapsedSessions, findEventsBetweenStartEnd, executionTree, agentSessionStartIdBySpanKey, getExecutionId, getParentId]);

  useEffect(() => {
    if (!isEventHierarchyDebugEnabled()) return;

    const eventById = new Map(displayEvents.map(event => [event.id, event]));
    const treeRows: Array<Record<string, unknown>> = [];
    const walk = (nodes: EventNode[], depth: number, path: string) => {
      nodes.forEach((node, index) => {
        treeRows.push({
          ...summarizeEventForOwnershipDebug(node.event),
          depth,
          path: path ? `${path}.${index}` : `${index}`,
          childCount: node.children.length,
          isExpanded: expandedNodes.has(node.event.id),
        });
        walk(node.children, depth + 1, path ? `${path}.${index}` : `${index}`);
      });
    };
    walk(eventTree, 0, '');

    const sessionMembership = Array.from(findEventsBetweenStartEnd.entries()).map(([sessionKey, eventIds]) => ({
      sessionKey,
      count: eventIds.size,
      events: Array.from(eventIds).map(eventId => {
        const event = eventById.get(eventId);
        return event ? `${event.type}:${event.id}` : `missing:${eventId}`;
      }),
    }));

    const delegationStatsRows = Array.from(delegationStats.entries()).map(([id, stats]) => ({ id, ...stats }));
    const backgroundAgentStatsRows = Array.from(backgroundAgentStats.entries()).map(([id, stats]) => ({ id, ...stats }));
    const eventRows = displayEvents.map((event, index) => summarizeEventForOwnershipDebug(event, index));
    const interestingRows = eventRows.filter(row =>
      row.type === 'user_message' ||
      row.type === 'tool_call_start' ||
      row.type === 'tool_call_end' ||
      row.type === 'delegation_start' ||
      row.type === 'delegation_end' ||
      row.type === 'orchestrator_agent_start' ||
      row.type === 'orchestrator_agent_end' ||
      row.type === 'background_agent_started' ||
      row.type === 'background_agent_completed' ||
      row.type === 'learn_code_script_execution'
    );

    console.groupCollapsed(
      `[EVENT_HIERARCHY_DEBUG] tab=${tabIdProp ?? activeTabId ?? 'unknown'} events=${displayEvents.length} roots=${eventTree.length}`
    );
    console.log('Enable/disable with localStorage key:', EVENT_HIERARCHY_DEBUG_STORAGE_KEY);
    console.log('interestingEvents', interestingRows);
    console.log('allEventOwnershipRows', eventRows);
    console.log('computedTreeRows', treeRows);
    console.log('sessionMembership', sessionMembership);
    console.log('delegationStats', delegationStatsRows);
    console.log('backgroundAgentStats', backgroundAgentStatsRows);
    console.log('rawRecentEvents', displayEvents.slice(-50));
    console.log('renderBoundaryRecent', interestingRows.slice(-20).map(row => {
      const event = eventById.get(row.id as string);
      return event ? summarizeEventForDebug(event) : row;
    }));
    console.groupEnd();
  }, [
    activeTabId,
    backgroundAgentStats,
    delegationStats,
    displayEvents,
    eventTree,
    findEventsBetweenStartEnd,
    summarizeEventForOwnershipDebug,
    tabIdProp,
  ]);

  useEffect(() => {
    if (!isEventHierarchyDebugEnabled()) return;
    const scriptedEvents = displayEvents.filter(event => event.type === 'learn_code_script_execution');
    if (scriptedEvents.length === 0) return;

    const rootIds = new Set(
      eventTree
        .filter(node => node.event.type === 'learn_code_script_execution')
        .map(node => node.event.id)
    );

    console.log('[FIX_LEARN_CODE_UI] hierarchy_state', {
      tabId: tabIdProp ?? null,
      scriptedEvents: scriptedEvents.map(event => {
        const agentEvent = event.data as Record<string, unknown> | undefined;
        const payload = (agentEvent?.data && typeof agentEvent.data === 'object')
          ? agentEvent.data as Record<string, unknown>
          : agentEvent;
        return {
          eventId: event.id,
          stepId: payload?.step_id ?? null,
          fixIteration: payload?.fix_iteration ?? null,
          correlationId: (event as unknown as Record<string, unknown>).correlation_id ?? agentEvent?.correlation_id ?? null,
          isRoot: rootIds.has(event.id),
        };
      }),
    });
  }, [displayEvents, eventTree, tabIdProp]);

  const flattenedItems = useMemo(() => {
    const list: FlattenedItem[] = [];

    const flatten = (node: EventNode, key: string, levelOffset = 0) => {
      if (isSyntheticExecutionOwnerEvent(node.event)) {
        const ownerChildren = node.children.filter(child => isExecutionOwnerEvent(child.event));
        const nonOwnerChildren = node.children.filter(child => !isExecutionOwnerEvent(child.event));
        if (
          ownerChildren.length === 1 &&
          normalizedOwnerName(node.event) !== '' &&
          normalizedOwnerName(node.event) === normalizedOwnerName(ownerChildren[0].event)
        ) {
          const mergedChild: EventNode = {
            ...ownerChildren[0],
            level: node.level,
            children: [...nonOwnerChildren, ...ownerChildren[0].children],
          };
          flatten(mergedChild, `${key}-dedup-child`, levelOffset);
          return;
        }
      }

      if (node.event.type === 'background_agent_started') {
        const ownerChildren = node.children.filter(child => isExecutionOwnerEvent(child.event));
        const nonOwnerChildren = node.children.filter(child => !isExecutionOwnerEvent(child.event));
        if (
          ownerChildren.length === 1 &&
          ownerChildren[0].event.type === 'orchestrator_agent_start' &&
          normalizedOwnerName(node.event) !== '' &&
          normalizedOwnerName(node.event) === normalizedOwnerName(ownerChildren[0].event)
        ) {
          const mergedChild: EventNode = {
            ...ownerChildren[0],
            level: node.level,
            children: [...nonOwnerChildren, ...ownerChildren[0].children],
          };
          flatten(mergedChild, `${key}-background-owner-child`, levelOffset);
          return;
        }
      }

      if (isNoisyWorkshopWrapperEvent(node.event)) {
        const ownerChildren = node.children.filter(child => isExecutionOwnerEvent(child.event));
        const nonOwnerChildren = node.children.filter(child => !isExecutionOwnerEvent(child.event));

        if (ownerChildren.length === 1) {
          const mergedChild: EventNode = {
            ...ownerChildren[0],
            level: node.level,
            children: [...nonOwnerChildren, ...ownerChildren[0].children],
          };
          flatten(mergedChild, `${key}-wrapper-child-0`, levelOffset - 1);
        } else {
          ownerChildren.forEach((child, index) => {
            flatten(child, `${key}-wrapper-child-${index}`, levelOffset - 1);
          });
          nonOwnerChildren.forEach((child, index) => {
            flatten(child, `${key}-wrapper-extra-${index}`, levelOffset - 1);
          });
        }
        return;
      }

      list.push({ node, uniqueKey: key, levelOffset });

      if (isExecutionOwnerEvent(node.event)) {
        // Show nested execution owners as tree rows, but keep raw logs/tool events
        // inside the owner's expandable log panel. Keep completed execution
        // siblings visible so the workflow reads as a run timeline, not just
        // the active tail of the trace.
        const ownerChildren = node.children.filter(child => isExecutionOwnerEvent(child.event));
        const visibleDetailChildren = node.children.filter(child =>
          !isExecutionOwnerEvent(child.event) && isTreeVisibleExecutionDetailEvent(child.event)
        );
        const visibleChildren = [...ownerChildren, ...visibleDetailChildren]
          .sort((a, b) => compareEventsChronologically(a.event, b.event, new Map()));
        visibleChildren.forEach((child, index) => {
          flatten(child, `${key}-owner-child-${index}`, levelOffset);
        });
        return;
      }

      // Respect the user's explicit expand/collapse state for normal hierarchy nodes.
      const shouldExpand = expandedNodes.has(node.event.id);
      if (shouldExpand && node.children.length > 0) {
        node.children.forEach((child, index) => {
          flatten(child, `${key}-child-${index}`, levelOffset);
        });
      }
    };
    eventTree.forEach((node, index) => {
      flatten(node, `${node.event.id}-root-${index}`);
    });

    // Tool call grouping — operates on the flat list so only main-agent events are affected.
    // Sub-agent events were excluded above (flattening stops at delegation_start).
    if (hideToolCalls) {
      // Identify consecutive runs of TOOL_CALL_TYPES in the flat list.
      // DELEGATION_BRIDGE_TYPES (delegation_start/end) are allowed inside a group so they
      // don't break runs in workflow/multi-agent mode — they appear between tool_call_start
      // and tool_call_end when a sub-agent is spawned. A group must start and end on a
      // TOOL_CALL_TYPE event (not a bridge event).
      const groups: { startIdx: number; endIdx: number; groupKey: string }[] = [];
      let i = 0;
      while (i < list.length) {
        const item = list[i];
        if (item.node && TOOL_CALL_TYPES.has(item.node.event.type || '')) {
          const startIdx = i;
          const groupKey = item.node.event.id;
          let lastToolCallIdx = i;
          i++;
          while (i < list.length && list[i].node) {
            const t = list[i].node!.event.type || '';
            if (TOOL_CALL_TYPES.has(t)) { lastToolCallIdx = i; i++; }
            else if (DELEGATION_BRIDGE_TYPES.has(t)) { i++; } // bridge — continue scanning
            else break;
          }
          groups.push({ startIdx, endIdx: lastToolCallIdx, groupKey });
          i = lastToolCallIdx + 1;
        } else {
          i++;
        }
      }

      // Replace each group: expanded → keep items + add collapse sentinel, hidden → replace with expand sentinel
      // Process in reverse to keep indices stable
      for (let g = groups.length - 1; g >= 0; g--) {
        const group = groups[g];
        const totalCount = group.endIdx - group.startIdx + 1;
        if (expandedGroups.has(group.groupKey)) {
          const visibleItems = selectToolGroupPreviewItems(list, group.startIdx, group.endIdx);
          const hiddenCount = Math.max(0, totalCount - visibleItems.length);
          const replacementItems: FlattenedItem[] = [];

          replacementItems.push(...visibleItems);
          if (hiddenCount > 0) {
            replacementItems.push({
              uniqueKey: `tool-call-terminal-hint-${group.groupKey}-${hiddenCount}`,
              isToolCallToggle: true,
              toolCallToggleMode: 'terminal_hint',
              hiddenCount,
              groupKey: group.groupKey,
            });
          }
          replacementItems.push({
            uniqueKey: `tool-call-collapse-${group.groupKey}`,
            isToolCallToggle: true,
            toolCallToggleMode: 'collapse',
            groupKey: group.groupKey,
          });

          list.splice(group.startIdx, totalCount, ...replacementItems);
        } else {
          // Replace entire group with a single "+ N tools" sentinel
          const count = totalCount;
          // Find the latest tool_call_start in the group for preview info
          let latestToolName: string | undefined;
          let latestToolLabel: string | undefined;
          for (let idx = group.endIdx; idx >= group.startIdx; idx--) {
            const n = list[idx].node;
            if (n && n.event.type === 'tool_call_start') {
              const d = n.event.data as Record<string, unknown> | undefined;
              const payload = (d?.data && typeof d.data === 'object') ? d.data as Record<string, unknown> : d;
              const summary = buildToolSummary(payload);
              latestToolName = summary.toolName;
              latestToolLabel = summary.toolLabel;
              break;
            }
          }
          list.splice(group.startIdx, count, {
            uniqueKey: `tool-call-expand-${group.groupKey}`,
            isToolCallToggle: true,
            toolCallToggleMode: 'expand',
            hiddenCount: count,
            groupKey: group.groupKey,
            latestToolName,
            latestToolLabel,
          });
        }
      }
    }

    // --- PERF: flattenedItems ref-stability ---
    // Virtuoso diffs the entire data array on every render. If the array ref changes,
    // Virtuoso walks all items even if the content is identical.
    //
    // When sub-agent events are added, eventTree changes (new children inside agent cards),
    // but flattenedItems stays the same (sub-agent children aren't flattened — they render
    // inside the agent card's scrollable area). Without this check, every sub-agent event
    // would cause a full Virtuoso re-diff of 100+ items.
    //
    // We compare length + first/last uniqueKey. If all match, return the previous array ref.
    // BUG RISK: if uniqueKeys change for existing items (shouldn't happen — they're derived
    // from event IDs which are immutable), this would return stale data.
    // Symptom: events appear but don't update. Fix: remove this stability check.
    const prev = flattenedItemsRef.current;
    if (
      list.length === prev.length &&
      list.length > 0 &&
      list[0]?.uniqueKey === prev[0]?.uniqueKey &&
      list[list.length - 1]?.uniqueKey === prev[prev.length - 1]?.uniqueKey
    ) {
      // Also check tool-call toggle sentinels — their hiddenCount changes as new tool calls arrive.
      // Without this, the "+N tool calls" counter would show stale values.
      let togglesMatch = true
      for (let i = 0; i < list.length; i++) {
        if (list[i].isToolCallToggle && list[i].hiddenCount !== prev[i]?.hiddenCount) {
          togglesMatch = false
          break
        }
        if (list[i].isToolCallToggle && list[i].toolCallToggleMode !== prev[i]?.toolCallToggleMode) {
          togglesMatch = false
          break
        }
        if (list[i].isCompletedExecutionToggle && list[i].hiddenCount !== prev[i]?.hiddenCount) {
          togglesMatch = false
          break
        }
        if (list[i].isCompletedExecutionToggle && list[i].completedExecutionToggleMode !== prev[i]?.completedExecutionToggleMode) {
          togglesMatch = false
          break
        }
        // Owner-card children render inside the card, not as flat rows. Changes to
        // these child arrays must still re-render the owning card so summary events
        // and collapsed log counts stay live.
        const node = list[i].node
        const prevNode = prev[i]?.node
        if (node && prevNode && isExecutionOwnerEvent(node.event) && node.children.length !== prevNode.children.length) {
          togglesMatch = false
          break
        }
        if (
          node && prevNode && isExecutionOwnerEvent(node.event) &&
          node.children.some((child, childIndex) =>
            child.event.type === 'live_execution_streaming' &&
            child.event !== prevNode.children[childIndex]?.event
          )
        ) {
          togglesMatch = false
          break
        }
        if (node && prevNode && isExecutionOwnerEvent(node.event) && node.event !== prevNode.event) {
          togglesMatch = false
          break
        }
        if (node && prevNode && node.event.type === 'delegation_start' &&
            expandedNodes.has(node.event.id) !== prevExpandedNodesRef.current.has(node.event.id)) {
          togglesMatch = false
          break
        }
      }
      if (togglesMatch) return prev;
    }
    flattenedItemsRef.current = list;
    prevExpandedNodesRef.current = expandedNodes;
    return list;
  }, [eventTree, expandedNodes, hideToolCalls, expandedGroups, expandedCompletedExecutionGroups]);

  // --- Render tracking (filter by [Render] or [Memo] in console) ---
  useRenderLogger('EventHierarchy', {
    eventsIn: events.length,
    displayEvents: displayEvents.length,
    eventTree: eventTree.length,
    flattenedItems: flattenedItems.length,
    expandedNodes: expandedNodes.size,
    collapsedSessions: collapsedSessions.size,
  })
  useMemoLogger('EH.displayEvents', displayEvents, displayEvents.length)
  useMemoLogger('EH.combinedStats', delegationStats, Object.keys(delegationStats).length + ' delegations')
  useMemoLogger('EH.eventTree', eventTree, eventTree.length)
  useMemoLogger('EH.flattenedItems', flattenedItems, flattenedItems.length)

  const terminalOwnerPreference = useMemo(() => {
    const preferred = new Map<string, { eventId: string; depth: number; index: number }>();

    flattenedItems.forEach((item, index) => {
      const node = item.node;
      if (!node || !isExecutionOwnerEvent(node.event)) return;

      const keys = getOwnedTerminalOwnerKeys(node.event, getTerminalOwnerPayload(node.event));
      if (keys.length === 0) return;

      const depth = Math.max(0, node.level + (item.levelOffset ?? 0));
      for (const key of keys) {
        const current = preferred.get(key);
        if (!current || depth > current.depth || (depth === current.depth && index > current.index)) {
          preferred.set(key, { eventId: node.event.id, depth, index });
        }
      }
    });

    return new Map(Array.from(preferred.entries()).map(([key, value]) => [key, value.eventId]));
  }, [flattenedItems]);

  const handleLoadMore = useCallback(async () => {
    if (!sessionId || isLoadingOlder) return;
    setIsLoadingOlder(true);
    try {
      const response = await agentApi.getSessionEvents(sessionId, undefined, {
        limit: 50,
        offset: paginationOffset,
      });
      if (response.events.length > 0) {
        setLoadedOlderEvents(prev => [...response.events, ...prev]);
        setPaginationOffset(prev => prev + response.events.length);
        setHasMoreOlderEvents(response.has_more);
      } else setHasMoreOlderEvents(false);
    } catch (error) {
      console.error('[EventHierarchy] Failed to load older events:', error);
      setHasMoreOlderEvents(false);
    } finally {
      setIsLoadingOlder(false);
    }
  }, [sessionId, paginationOffset, isLoadingOlder]);

  const renderItem = useCallback((_index: number, item: FlattenedItem) => {
    if (!item) return null;

    if (item.isCompletedExecutionToggle) {
      const count = item.hiddenCount || 0;
      const key = item.groupKey;
      const mode = item.completedExecutionToggleMode || 'expand';
      const indentSize = 18;
      const level = Math.max(0, item.completedExecutionLevel ?? 0);
      return (
        <div className="event-tree-node relative">
          {level > 0 && Array.from({ length: level }).map((_, i) => (
            <div
              key={i}
              className="absolute top-0 bottom-0 border-l border-gray-200/30 dark:border-gray-700/30"
              style={{ left: `${(i + 1) * indentSize - 9}px` }}
            />
          ))}
          <div className="event-tree-item relative z-10 py-0.5" style={{ paddingLeft: `${level * indentSize}px` }}>
            <button
              type="button"
              onClick={() => {
                if (!key) return;
                toggleCompletedExecutionGroup(key);
              }}
              className="flex min-w-0 max-w-full items-center gap-2 rounded border border-emerald-800/35 bg-emerald-950/10 px-2 py-1 text-left text-[11px] leading-tight text-emerald-300/80 transition-colors hover:border-emerald-700/60 hover:bg-emerald-950/20 hover:text-emerald-200"
            >
              <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500/70" />
              <span className="shrink-0 font-medium">
                {mode === 'expand' ? `Show ${count} completed step${count !== 1 ? 's' : ''}` : `Hide completed steps`}
              </span>
              <span className="min-w-0 truncate text-emerald-400/55">
                {mode === 'expand' ? 'Done work is collapsed to keep the active path clear' : `${count} completed step${count !== 1 ? 's' : ''}`}
              </span>
              <span className="shrink-0 text-emerald-400/70">
                {mode === 'expand' ? '▸' : '▾'}
              </span>
            </button>
          </div>
        </div>
      );
    }

    // Inline tool-call toggle sentinel
    if (item.isToolCallToggle) {
      const count = item.hiddenCount || 0;
      const key = item.groupKey;
      const mode = item.toolCallToggleMode || 'expand';
      if (mode === 'terminal_hint') {
        return (
          <div className="flex items-center py-0.5 pl-5 min-w-0">
            <button
              type="button"
              onClick={switchToTerminalView}
              disabled={!resolvedTabId}
              className="flex min-w-0 max-w-full items-center gap-1.5 rounded px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 transition-colors hover:bg-muted/30 hover:text-muted-foreground disabled:cursor-default disabled:hover:bg-transparent disabled:hover:text-muted-foreground/60"
            >
              <span className="shrink-0">
                {count} older tool event{count !== 1 ? 's' : ''} hidden
              </span>
              <span className="truncate opacity-70">
                · View full trace in Terminal
              </span>
            </button>
          </div>
        );
      }
      return (
        <div className="flex items-center py-0.5 pl-5 min-w-0">
          <button
            onClick={() => {
              if (!key) return;
              toggleToolCallGroup(key);
            }}
            className="flex items-center gap-1.5 min-w-0 max-w-full px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
          >
            <span className="flex-shrink-0 font-medium">
              {count > 0
                ? `+ ${count} tool${count !== 1 ? 's' : ''}`
                : `− collapse`}
            </span>
            {mode === 'expand' && count > 0 && (item.latestToolLabel || item.latestToolName) && (
              <span className="truncate opacity-60">
                • {item.latestToolLabel || item.latestToolName}
              </span>
            )}
          </button>
        </div>
      );
    }

    const { node, uniqueKey } = item;
    if (!node) return null;
    const { event, children } = node;
    // Read expand state from the store directly — buildTreeRecursive no
    // longer bakes isExpanded into the EventNode so toggles don't
    // invalidate the structural tree memo.
    const isExpanded = expandedNodes.has(event.id);
    const level = Math.max(0, node.level + (item.levelOffset ?? 0));
    const hasChildren = children.length > 0;
    const ownsInternalLogPanel = event.type === 'delegation_start' || event.type === 'orchestrator_agent_start' || event.type === 'background_agent_started';
    const isOwnedLogPanelOpen = ownsInternalLogPanel && expandedOwnedLogPanels.has(event.id);
    const terminalOwnerKeys = ownsInternalLogPanel
      ? getOwnedTerminalOwnerKeys(event, getTerminalOwnerPayload(event))
      : [];
    const showOwnedTerminal = !ownsInternalLogPanel ||
      terminalOwnerKeys.length === 0 ||
      terminalOwnerKeys.some(key => terminalOwnerPreference.get(key) === event.id);
    const ownerNonExecutionChildren = ownsInternalLogPanel
      ? children.filter(child => !isExecutionOwnerEvent(child.event))
      : [];
    const ownedInlineChildren = ownsInternalLogPanel
      ? dedupeOwnerInlineNodes(ownerNonExecutionChildren.filter(child =>
        !isOwnerCollapsedDetailEvent(child.event) && !isTreeVisibleExecutionDetailEvent(child.event)
      ))
      : [];
    const ownedLogChildren = ownsInternalLogPanel
      ? ownerNonExecutionChildren.filter(child => isOwnerCollapsedDetailEvent(child.event))
      : children;
    const autoExpandOwnedLogPanel = ownsInternalLogPanel
      && ownedLogChildren.length > 0
      && ownedLogChildren.length <= AUTO_EXPANDED_OWNER_TOOL_EVENT_LIMIT;

    // Refresh-safe tool-count derived from the event tree. The "+N tools"
    // chip in EventDispatcher falls back to this when liveStats (in-memory
    // delegation/background-agent stats) is empty after a page reload.
    const countToolCallStarts = (nodes: EventNode[]): number => {
      let total = 0;
      for (const node of nodes) {
        if (node.event.type === 'tool_call_start') total += 1;
        if (node.children?.length) total += countToolCallStarts(node.children);
      }
      return total;
    };
    const toolCallSource = ownsInternalLogPanel ? ownedLogChildren : children;
    const childrenToolCount = countToolCallStarts(toolCallSource);
    
    // Only nested rows get tree indentation. Root rows should align with the feed.
    const indentLevel = level;
    const indentSize = 18;
    const indent = indentLevel * indentSize;
    
    const sessionKey = event.type === 'orchestrator_agent_start'
      ? agentSessionSpanKeyByStartId.get(event.id)
      : undefined;
    const isCollapsed = sessionKey ? collapsedSessions.has(sessionKey) : false;
    const eventCount = sessionKey && findEventsBetweenStartEnd.has(sessionKey)
      ? findEventsBetweenStartEnd.get(sessionKey)!.size - 2 : undefined;
    const onToggleCollapse = sessionKey ? () => toggleAgentSession(sessionKey) : undefined;
    
    return (
      <div key={uniqueKey} className="event-tree-node relative" data-event-type={event.type}>
        {/* Subtle hierarchy connectors. Root rows should not get a dominant rail. */}
        {level > 0 && Array.from({ length: level }).map((_, i) => (
          <div
            key={i}
            className="absolute top-0 bottom-0 border-l border-gray-200/40 dark:border-gray-700/35"
            style={{ left: `${(i + 1) * indentSize - 9}px` }}
          />
        ))}
        {level > 0 && (
          <div
            className="absolute top-5 h-px border-t border-gray-200/50 dark:border-gray-700/45"
            style={{
              left: `${level * indentSize - 9}px`,
              width: `${indentSize - 5}px`,
            }}
          />
        )}

        <div 
          className="event-tree-item relative z-10"
          style={{ paddingLeft: `${indent}px` }}
        >
          {hasChildren && !ownsInternalLogPanel && (
            <button
              onClick={() => toggleNode(event.id)}
              className="expand-button"
              aria-label={isExpanded ? 'Collapse' : 'Expand'}
              style={{ position: 'absolute', left: `${Math.max(indent - 22, 4)}px`, top: '10px' }}
            >
              <span className={`expand-icon ${isExpanded ? 'expanded' : ''}`}>
                {isExpanded ? '▼' : '▶'}
              </span>
            </button>
          )}
          
          <div className="event-content">
            <div className="event-details">
              <EventDispatcher
                event={event}
                onApproveWorkflow={onApproveWorkflow}
                onSubmitFeedback={onSubmitFeedback}
                onFeedbackSubmitted={onFeedbackSubmitted}
                onSendMessage={onSendMessage}
                isApproving={isApproving}
                isCollapsed={isCollapsed}
                eventCount={eventCount}
                onToggleCollapse={onToggleCollapse}
                compact={compact}
                delegationStats={delegationStats}
                backgroundAgentStats={backgroundAgentStats}
                summaryNodes={ownsInternalLogPanel ? ownedInlineChildren : undefined}
                summaryCount={ownsInternalLogPanel ? ownedInlineChildren.length : undefined}
                childrenNodes={ownsInternalLogPanel ? ((isOwnedLogPanelOpen || autoExpandOwnedLogPanel) ? ownedLogChildren : undefined) : (isExpanded ? children : undefined)}
                childrenCount={ownsInternalLogPanel ? ownedLogChildren.length : children.length}
                childrenToolCount={childrenToolCount}
                onToggleNode={toggleNode}
                ownedLogPanelOpen={isOwnedLogPanelOpen || autoExpandOwnedLogPanel}
                autoExpandedOwnedLogPanel={autoExpandOwnedLogPanel}
                onToggleOwnedLogPanel={ownsInternalLogPanel ? (open) => toggleOwnedLogPanel(event.id, open) : undefined}
                showOwnedTerminal={showOwnedTerminal}
                tabId={resolvedTabId || undefined}
              />
            </div>
          </div>
        </div>
      </div>
    );
  }, [collapsedSessions, expandedNodes, expandedOwnedLogPanels, findEventsBetweenStartEnd, agentSessionSpanKeyByStartId, terminalOwnerPreference, toggleAgentSession, toggleNode, toggleOwnedLogPanel, toggleCompletedExecutionGroup, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, onSendMessage, isApproving, compact, delegationStats, backgroundAgentStats, switchToTerminalView, toggleToolCallGroup, resolvedTabId]);

  // Only auto-scroll when new top-level items are added (not when sub-agent events update internals).
  // Sub-agent events change displayEvents but don't add items to flattenedItems.
  // Return `true` (instant) instead of `'smooth'` — the parent ChatArea container already
  // manages smooth auto-scroll. Two concurrent smooth-scroll callers on the same element
  // interrupt each other, producing visible jank.
  const handleFollowOutput = useCallback((isAtBottom: boolean): false | true => {
    const current = flattenedItems.length;
    const prev = prevFlattenedCountRef.current;
    prevFlattenedCountRef.current = current;
    if (current > prev && isAtBottom && !userScrolledUpRef.current) return true;
    return false;
  }, [flattenedItems.length]);

  if (flattenedItems.length === 0) {
    return <div className="text-gray-500 text-center py-4">No hierarchical events to display</div>;
  }

  return (
    <div ref={containerRef} className="event-hierarchy w-full">
      <Virtuoso
        ref={virtuosoRef}
        data={flattenedItems}
        customScrollParent={scrollParent || undefined}
        useWindowScroll={!scrollParent}
        increaseViewportBy={300}
        followOutput={handleFollowOutput}
        itemContent={renderItem}
        components={{
          Header: () => {
            if (!hasMoreOlderEvents) return null;
            return (
              <div className="flex items-center justify-center gap-3 py-2">
                <button
                  onClick={handleLoadMore}
                  disabled={isLoadingOlder}
                  className="px-3 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground bg-muted/40 hover:bg-muted/70 border border-border/50 rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  {isLoadingOlder ? 'Loading...' : 'Load Older Events'}
                </button>
              </div>
            );
          }
        }}
      />
    </div>
  );
});

EventHierarchy.displayName = 'EventHierarchy';
