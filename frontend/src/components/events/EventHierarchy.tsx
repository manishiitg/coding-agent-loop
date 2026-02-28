import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher, type DelegationStats, type EventNode } from './EventDispatcher';
import { agentApi } from '../../services/api';
import { useChatStore } from '../../stores/useChatStore';
import { MAX_EVENTS_TO_PROCESS, MAX_CHILD_EVENTS_PER_DELEGATION } from '../../constants/events';
import { NEVER_DISPLAY_EVENTS } from './eventModeUtils';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import { useRenderLogger, useMemoLogger } from '../../utils/renderLogger';
import './EventHierarchy.css';

// Event types that get grouped and collapsed together as "tool calls".
// llm_generation_end naturally occurs between tool call batches — including it
// prevents many tiny "+ 1 tool call" groups from forming.
const TOOL_CALL_TYPES = new Set(['tool_call_start', 'tool_call_end', 'tool_call_error', 'token_usage', 'llm_generation_end']);

interface EventHierarchyProps {
  events: PollingEvent[];
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  isApproving?: boolean  // Loading state for approve button
  compact?: boolean  // Compact mode for smaller font sizes
  flatHierarchy?: boolean  // If true, removes left padding/indentation for hierarchy levels
  tabId?: string  // Specific tab ID — avoids getActiveTab() so multi-chat panels are independent
}

interface HiddenGroup {
  count: number;
  groupKey: string;     // Stable key (first event ID in the group)
  beforeId?: string;    // Insert sentinel before this event ID (undefined = append)
}

interface FlattenedItem {
  node?: EventNode;
  uniqueKey: string;
  isToolCallToggle?: boolean;
  hiddenCount?: number;   // Per-group count for the "+" label
  groupKey?: string;      // Group key for per-group expand/collapse
}

export const EventHierarchy: React.FC<EventHierarchyProps> = React.memo(({
  events,
  onApproveWorkflow,
  onSubmitFeedback,
  onFeedbackSubmitted,
  onSendMessage,
  isApproving,
  compact = false,
  flatHierarchy = false,
  tabId: tabIdProp,
}) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
  // Track session keys the user manually expanded — don't auto-collapse these again
  const userExpandedSessionsRef = useRef<Set<string>>(new Set());
  // Per-group expand state for tool call groups (keyed by first event ID in group)
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());
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

  // Look up specific tab (or fall back to active tab for backwards compat)
  const activeTabId = useChatStore(state => state.activeTabId)
  const resolvedTabId = tabIdProp || activeTabId
  const tab = useChatStore(state => resolvedTabId ? state.chatTabs[resolvedTabId] : undefined)
  // const setTabHideToolCalls = useChatStore(state => state.setTabHideToolCalls) // kept for future "show all / collapse all"
  const sessionId = tab?.sessionId
  const hideToolCalls = tab?.hideToolCalls || false
  
  // Merge loaded older events with current events — single-pass filter
  const displayEvents = useMemo(() => {
    // Avoid spread when loadedOlderEvents is empty (common case)
    const source = loadedOlderEvents.length > 0
      ? [...loadedOlderEvents, ...events]
      : events;

    const HIDDEN_STREAMING = new Set(['streaming_start', 'streaming_chunk', 'streaming_end']);
    const HIDDEN_DELEGATION_TOOLS = new Set(['delegate', 'confirm_plan_execution', 'query_agent', 'terminate_agent', 'list_agents']);

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

    result.sort((a, b) => {
      const timeA = a.timestamp ? (Date.parse(a.timestamp) || 0) : 0;
      const timeB = b.timestamp ? (Date.parse(b.timestamp) || 0) : 0;
      return timeA - timeB;
    });

    if (result.length <= MAX_EVENTS_TO_PROCESS) return result;

    // Smart cap: preserve structural events, cap sub-agent children per delegation.
    // Structural events (delegation_start/end, orchestrator boundaries) are always kept
    // because dropping them breaks the tree (orphan children, missing cards).
    // Sub-agent child events are capped per delegation since SubAgentHierarchy only renders 30.
    const STRUCTURAL_TYPES = new Set([
      'delegation_start', 'delegation_end',
      'orchestrator_agent_start', 'orchestrator_agent_end',
      'workflow_start', 'workflow_end',
      'orchestrator_start', 'orchestrator_end',
      'request_human_feedback', 'blocking_human_feedback',
      'user_message'
    ]);

    // Count children per delegation (events with a delegation- correlation_id)
    const delegationChildCounts = new Map<string, number>();
    const capped: PollingEvent[] = [];

    // Iterate newest-first so we keep the latest children per delegation
    for (let i = result.length - 1; i >= 0; i--) {
      const ev = result[i];
      const type = ev.type || '';

      // Always keep structural events
      if (STRUCTURAL_TYPES.has(type)) {
        capped.push(ev);
        continue;
      }

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

      capped.push(ev);
      // Stop once we have enough
      if (capped.length >= MAX_EVENTS_TO_PROCESS) break;
    }

    // Reverse back to chronological order
    capped.reverse();
    return capped;
  }, [events, loadedOlderEvents]);

  // Filter tool call / token_usage groups from displayEvents when hide mode is on.
  // Done at this level (before tree construction) so tool calls that are children of
  // collapsed parent nodes are also removed — filtering inside flatten() missed those.
  const { filteredDisplayEvents, hiddenGroups, totalHiddenCount } = useMemo(() => {
    const emptyResult = { filteredDisplayEvents: displayEvents, hiddenGroups: [] as HiddenGroup[], totalHiddenCount: 0 };
    if (!hideToolCalls) return emptyResult;

    // Helper: check if event belongs to a sub-agent delegation (not main agent).
    // Sub-agent events have a correlation_id starting with "delegation-" at
    // event.data.correlation_id or event.data.data.correlation_id (nested AgentEvent).
    const isDelegationChild = (event: PollingEvent): boolean => {
      if (!event.data || typeof event.data !== 'object') return false;
      const data = event.data as Record<string, unknown>;
      // Check top-level correlation_id
      const cid = data.correlation_id as string | undefined;
      if (cid && cid.startsWith('delegation-')) return true;
      // Check nested data.data.correlation_id (AgentEvent wrapper)
      if (data.data && typeof data.data === 'object') {
        const nested = data.data as Record<string, unknown>;
        const ncid = nested.correlation_id as string | undefined;
        if (ncid && ncid.startsWith('delegation-')) return true;
      }
      return false;
    };

    // Identify consecutive groups of TOOL_CALL_TYPES events (main agent only).
    // Sub-agent events are left alone — the tree builder re-parents them into delegation cards.
    const groups: { startIdx: number; endIdx: number }[] = [];
    let i = 0;
    while (i < displayEvents.length) {
      const ev = displayEvents[i];
      if (TOOL_CALL_TYPES.has(ev.type || '') && !isDelegationChild(ev)) {
        const startIdx = i;
        while (i < displayEvents.length && TOOL_CALL_TYPES.has(displayEvents[i].type || '') && !isDelegationChild(displayEvents[i])) i++;
        groups.push({ startIdx, endIdx: i - 1 });
      } else {
        i++;
      }
    }

    if (groups.length === 0) return emptyResult;

    const hideIds = new Set<string>();
    const hiddenGroupInfo: HiddenGroup[] = [];

    for (const group of groups) {
      const groupKey = displayEvents[group.startIdx].id;
      // Skip groups the user has individually expanded
      if (expandedGroups.has(groupKey)) continue;

      const count = group.endIdx - group.startIdx + 1;
      for (let j = group.startIdx; j <= group.endIdx; j++) {
        hideIds.add(displayEvents[j].id);
      }
      // Sentinel goes before the first non-hidden event after this group
      const afterIdx = group.endIdx + 1;
      const beforeId = afterIdx < displayEvents.length ? displayEvents[afterIdx].id : undefined;
      hiddenGroupInfo.push({ count, groupKey, beforeId });
    }

    const filtered = displayEvents.filter(e => !hideIds.has(e.id));
    return { filteredDisplayEvents: filtered, hiddenGroups: hiddenGroupInfo, totalHiddenCount: hideIds.size };
  }, [displayEvents, hideToolCalls, expandedGroups]);

  // Reset loaded older events when session changes
  useEffect(() => {
    setLoadedOlderEvents([])
    setPaginationOffset(0)
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

  // Single-pass derivation: delegationStats + backgroundAgentStats + sessionEvents (was 3 separate useMemos)
  const { delegationStats, backgroundAgentStats, findEventsBetweenStartEnd } = useMemo(() => {
    const dStats = new Map<string, DelegationStats>()
    const bgStats = new Map<string, DelegationStats>()
    const startEvents = new Map<string, { event: PollingEvent; index: number }>()
    const endEvents = new Map<string, { event: PollingEvent; index: number }>()

    // Temp storage for delegation_start events (need dStats populated first for bgStats)
    const delegationStartEvents: { bgAgentId: string; delegationId: string }[] = []

    for (let i = 0; i < displayEvents.length; i++) {
      const event = displayEvents[i]
      const type = event.type

      // --- Session events (was findEventsBetweenStartEnd) ---
      if (type === 'orchestrator_agent_start' || type === 'orchestrator_agent_end') {
        const sessionKey = getAgentSessionKey(event)
        if (sessionKey) {
          if (type === 'orchestrator_agent_start') startEvents.set(sessionKey, { event, index: i })
          else endEvents.set(sessionKey, { event, index: i })
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
          const toolName = (payload.tool_name as string) || undefined
          if (toolName) s.latestToolName = toolName
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

    // Build session events map
    const sessionEvents = new Map<string, Set<string>>()
    startEvents.forEach((startInfo, sessionKey) => {
      const endInfo = endEvents.get(sessionKey)
      if (!endInfo) return
      const eventIds = new Set<string>()
      eventIds.add(startInfo.event.id)
      for (let j = startInfo.index + 1; j < endInfo.index; j++) eventIds.add(displayEvents[j].id)
      eventIds.add(endInfo.event.id)
      sessionEvents.set(sessionKey, eventIds)
    })

    return { delegationStats: dStats, backgroundAgentStats: bgStats, findEventsBetweenStartEnd: sessionEvents }
  }, [displayEvents, getAgentSessionKey]);

  // Fix 5: Auto-collapse completed workflow steps.
  // When orchestrator_agent_end fires for a session, auto-add it to collapsedSessions
  // (unless the user manually expanded it).
  useEffect(() => {
    const completedKeys: string[] = []
    for (const event of displayEvents) {
      if (event.type !== 'orchestrator_agent_end') continue
      const sessionKey = getAgentSessionKey(event)
      if (sessionKey && !userExpandedSessionsRef.current.has(sessionKey) && !collapsedSessions.has(sessionKey)) {
        completedKeys.push(sessionKey)
      }
    }
    if (completedKeys.length > 0) {
      setCollapsedSessions(prev => {
        const next = new Set(prev)
        for (const k of completedKeys) next.add(k)
        return next
      })
    }
  }, [displayEvents, getAgentSessionKey, collapsedSessions]);

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

  const eventTree = useMemo(() => {
    const eventById = new Map<string, PollingEvent>();
    filteredDisplayEvents.forEach(event => eventById.set(event.id, event));

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

    const filteredEvents = filteredDisplayEvents.filter(event => !eventsToFilter.has(event.id));
    const filteredEventIds = new Set(filteredEvents.map(e => e.id));

    // Build delegation_id -> delegation_start event ID map for re-parenting orphans.
    // When an intermediate parent within a delegation is evicted, its children become orphans.
    // Instead of showing them as root events in the main chat, re-parent them to delegation_start.
    const delegationIdToEventId = new Map<string, string>();
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

    // Helper: extract delegation correlation_id from event
    const getDelegationCorrelationId = (event: PollingEvent): string | undefined => {
      if (!event.data || typeof event.data !== 'object') return undefined;
      const data = event.data as Record<string, unknown>;
      const cid = data.correlation_id as string | undefined;
      return (cid && cid.startsWith('delegation-')) ? cid : undefined;
    };

    const childrenMap = new Map<string, PollingEvent[]>();

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

      if (parentId && filteredEventIds.has(parentId)) {
        if (!childrenMap.has(parentId)) childrenMap.set(parentId, []);
        childrenMap.get(parentId)!.push(event);
      }
    });

    const buildTreeRecursive = (event: PollingEvent, depth: number): EventNode => {
      const children = childrenMap.get(event.id) || [];
      const childNodes = children.map(child => buildTreeRecursive(child, depth + 1));
      return {
        event,
        children: childNodes,
        level: depth,
        isExpanded: expandedNodes.has(event.id)
      };
    };

    const rootEvents = filteredEvents.filter(event => {
      const parentId = getParentId(event);
      // Standard root check: no parent or parent not in filtered set
      const isOrphan = !parentId || !filteredEventIds.has(parentId);
      if (!isOrphan) return false;

      // Never promote delegation child events to root — they belong inside delegation cards.
      // If their delegation_start was evicted (shouldn't happen with structural preservation),
      // hide them rather than polluting the main chat.
      if (parentId) {
        const delegCid = getDelegationCorrelationId(event);
        if (delegCid) {
          // Check if this event was re-parented to a delegation_start in childrenMap
          const delegStartId = delegationIdToEventId.get(delegCid);
          if (delegStartId) return false; // Already re-parented, don't show as root
          // delegation_start was evicted entirely — suppress orphan
          return false;
        }
      }

      return true;
    });

    return rootEvents.map(event => buildTreeRecursive(event, 0));
  }, [filteredDisplayEvents, collapsedSessions, findEventsBetweenStartEnd, expandedNodes, getParentId]);

  const hasToolCalls = useMemo(
    () => displayEvents.some(e => TOOL_CALL_TYPES.has(e.type || '')),
    [displayEvents]
  );

  const flattenedItems = useMemo(() => {
    const list: FlattenedItem[] = [];

    const flatten = (node: EventNode, key: string, isWithinSubAgent = false) => {
      list.push({ node, uniqueKey: key });

      // If this is a delegation_start (sub-agent), we STOP flattening its children into the main list.
      // They will be rendered internally by the sub-agent card's scrollable logs area.
      if (node.event.type === 'delegation_start') {
        return;
      }

      if (node.isExpanded && node.children.length > 0) {
        node.children.forEach((child, index) => {
          flatten(child, `${key}-child-${index}`, isWithinSubAgent || node.event.type === 'delegation_start');
        });
      }
    };
    eventTree.forEach((node, index) => flatten(node, `${node.event.id}-root-${index}`));

    // Insert per-group sentinels (reverse order to keep indices stable)
    if (hideToolCalls) {
      // "+" sentinels for hidden groups
      for (let g = hiddenGroups.length - 1; g >= 0; g--) {
        const group = hiddenGroups[g];
        const sentinel: FlattenedItem = { uniqueKey: `tool-call-expand-${group.groupKey}`, isToolCallToggle: true, hiddenCount: group.count, groupKey: group.groupKey };
        if (group.beforeId) {
          const insertIdx = list.findIndex(item => item.node?.event.id === group.beforeId);
          if (insertIdx >= 0) {
            list.splice(insertIdx, 0, sentinel);
          } else {
            list.push(sentinel);
          }
        } else {
          list.push(sentinel);
        }
      }

      // "−" sentinels after each individually expanded group
      if (expandedGroups.size > 0) {
        // Find runs of consecutive tool call events and check if they match an expanded group key
        for (let i = list.length - 1; i >= 0; i--) {
          const item = list[i];
          if (!item.node || !TOOL_CALL_TYPES.has(item.node.event.type || '')) continue;
          // Check if next item is NOT a tool call (end of a group)
          const nextIsToolCall = i + 1 < list.length && list[i + 1].node && TOOL_CALL_TYPES.has(list[i + 1].node!.event.type || '');
          if (nextIsToolCall) continue;
          // Walk backward to find group start
          let start = i;
          while (start > 0 && list[start - 1].node && TOOL_CALL_TYPES.has(list[start - 1].node!.event.type || '')) start--;
          const firstEventId = list[start].node!.event.id;
          if (expandedGroups.has(firstEventId)) {
            list.splice(i + 1, 0, { uniqueKey: `tool-call-collapse-${firstEventId}`, isToolCallToggle: true, groupKey: firstEventId });
          }
          i = start; // Skip past this group
        }
      }
    }

    return list;
  }, [eventTree, hideToolCalls, hiddenGroups, expandedGroups]);

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

    // Inline tool-call toggle sentinel
    if (item.isToolCallToggle) {
      const count = item.hiddenCount || 0;
      const key = item.groupKey;
      return (
        <div className="flex items-center py-0.5 pl-5">
          <button
            onClick={() => {
              if (!key) return;
              setExpandedGroups(prev => {
                const next = new Set(prev);
                if (next.has(key)) next.delete(key);
                else next.add(key);
                return next;
              });
            }}
            className="px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
          >
            {count > 0
              ? `+ ${count} tool call${count !== 1 ? 's' : ''}`
              : `− collapse`}
          </button>
        </div>
      );
    }

    const { node, uniqueKey } = item;
    if (!node) return null;
    const { event, children, level, isExpanded } = node;
    const hasChildren = children.length > 0;
    
    // Base indentation - use level + 1 to ensure at least one level of indent (20px) for visibility
    const indentLevel = level + 1;
    const indentSize = flatHierarchy ? 0 : 20;
    const indent = indentLevel * indentSize;
    
    const sessionKey = getAgentSessionKey(event);
    const isCollapsed = sessionKey ? collapsedSessions.has(sessionKey) : false;
    const eventCount = sessionKey && findEventsBetweenStartEnd.has(sessionKey)
      ? findEventsBetweenStartEnd.get(sessionKey)!.size - 2 : undefined;
    const onToggleCollapse = sessionKey ? () => toggleAgentSession(sessionKey) : undefined;
    
    return (
      <div key={uniqueKey} className="event-tree-node relative" data-event-type={event.type}>
        {/* Hierarchy Guide Lines */}
        {!flatHierarchy && level >= 0 && Array.from({ length: level + 1 }).map((_, i) => (
          <div 
            key={i} 
            className="absolute top-0 bottom-0 border-l-2 border-gray-200 dark:border-gray-800"
            style={{ left: `${(i + 1) * indentSize - 10}px` }}
          />
        ))}

        <div 
          className="event-tree-item relative z-10"
          style={{ paddingLeft: `${indent}px` }}
        >
          {hasChildren && (
            <button
              onClick={() => toggleNode(event.id)}
              className="expand-button"
              aria-label={isExpanded ? 'Collapse' : 'Expand'}
              style={{ position: 'absolute', left: `${indent - 25}px`, top: '10px' }}
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
                childrenNodes={isExpanded ? children : undefined}
                onToggleNode={toggleNode}
              />
            </div>
          </div>
        </div>
      </div>
    );
  }, [collapsedSessions, findEventsBetweenStartEnd, getAgentSessionKey, toggleAgentSession, toggleNode, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, onSendMessage, isApproving, compact, flatHierarchy, delegationStats, backgroundAgentStats]);

  // Only auto-scroll when new top-level items are added (not when sub-agent events update internals).
  // Sub-agent events change displayEvents but don't add items to flattenedItems.
  const handleFollowOutput = useCallback((isAtBottom: boolean): false | 'smooth' => {
    const current = flattenedItems.length;
    const prev = prevFlattenedCountRef.current;
    prevFlattenedCountRef.current = current;
    if (current > prev && isAtBottom) return 'smooth';
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
