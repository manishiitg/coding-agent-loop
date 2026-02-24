import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher, type DelegationStats, type EventNode } from './EventDispatcher';
import { agentApi } from '../../services/api';
import { useChatStore } from '../../stores/useChatStore';
import { MAX_EVENTS_TO_PROCESS } from '../../constants/events';
import { NEVER_DISPLAY_EVENTS, shouldShowEventByMode } from './eventModeUtils';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import './EventHierarchy.css';

interface EventHierarchyProps {
  events: PollingEvent[];
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  isApproving?: boolean  // Loading state for approve button
  compact?: boolean  // Compact mode for smaller font sizes
  flatHierarchy?: boolean  // If true, removes left padding/indentation for hierarchy levels
  eventMode?: 'advanced' | 'micro'  // Override event mode (e.g. for shared sessions with no active tab)
}

interface FlattenedItem {
  node: EventNode;
  uniqueKey: string;
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
  eventMode: eventModeProp
}) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
  // Track session keys the user manually expanded — don't auto-collapse these again
  const userExpandedSessionsRef = useRef<Set<string>>(new Set());
  const [loadedOlderEvents, setLoadedOlderEvents] = useState<PollingEvent[]>([]);
  const [paginationOffset, setPaginationOffset] = useState<number>(0);
  const [isLoadingOlder, setIsLoadingOlder] = useState<boolean>(false);
  const [hasMoreOlderEvents, setHasMoreOlderEvents] = useState<boolean>(false);
  
  const virtuosoRef = useRef<VirtuosoHandle>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [scrollParent, setScrollParent] = useState<HTMLElement | null>(null);
  
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

  // Get active tab for sessionId, eventMode, and hideToolCalls
  const activeTab = useChatStore(state => state.getActiveTab())
  const sessionId = activeTab?.sessionId
  const eventMode: 'advanced' | 'micro' = eventModeProp || (activeTab?.eventMode || 'micro') as 'advanced' | 'micro'
  const hideToolCalls = activeTab?.hideToolCalls || false
  
  // Merge loaded older events with current events — single-pass filter
  const displayEvents = useMemo(() => {
    // Avoid spread when loadedOlderEvents is empty (common case)
    const source = loadedOlderEvents.length > 0
      ? [...loadedOlderEvents, ...events]
      : events;

    const HIDDEN_STREAMING = new Set(['streaming_start', 'streaming_chunk', 'streaming_end']);
    const DELEGATE_TOOL_EVENTS = new Set(['tool_call_start', 'tool_call_end', 'tool_call_error']);
    const HIDDEN_DELEGATION_TOOLS = new Set(['delegate', 'confirm_plan_execution', 'query_agent', 'terminate_agent', 'list_agents']);
    const isMicro = eventMode === 'micro';

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
      if (DELEGATE_TOOL_EVENTS.has(type)) {
        const agentEvent = event.data as { data?: { tool_name?: string }; tool_name?: string } | undefined;
        const toolName = agentEvent?.data?.tool_name || agentEvent?.tool_name;
        if (toolName && HIDDEN_DELEGATION_TOOLS.has(toolName)) continue;
      }

      // Hide all tool call events when toggle is on
      if (hideToolCalls && DELEGATE_TOOL_EVENTS.has(type)) continue;

      // Micro-mode filters
      if (isMicro) {
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
      }

      result.push(event);
    }

    result.sort((a, b) => {
      const timeA = a.timestamp ? (Date.parse(a.timestamp) || 0) : 0;
      const timeB = b.timestamp ? (Date.parse(b.timestamp) || 0) : 0;
      return timeA - timeB;
    });

    if (result.length > MAX_EVENTS_TO_PROCESS) {
      return result.slice(-MAX_EVENTS_TO_PROCESS);
    }
    return result;
  }, [events, loadedOlderEvents, eventMode, hideToolCalls]);
  
  // Reset loaded older events when session or event mode changes
  useEffect(() => {
    setLoadedOlderEvents([])
    setPaginationOffset(0)
    const chatStore = useChatStore.getState()
    const hasMore = sessionId ? chatStore.getTabHasMoreOlderEvents(sessionId) : false
    setHasMoreOlderEvents(hasMore)
  }, [sessionId, eventMode])
  
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

    const filteredEvents = displayEvents.filter(event => !eventsToFilter.has(event.id));
    const filteredEventIds = new Set(filteredEvents.map(e => e.id));
    const childrenMap = new Map<string, PollingEvent[]>();

    filteredEvents.forEach(event => {
      const parentId = getParentId(event);
      if (parentId) {
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
      return !parentId || !filteredEventIds.has(parentId);
    });

    return rootEvents.map(event => buildTreeRecursive(event, 0));
  }, [displayEvents, collapsedSessions, findEventsBetweenStartEnd, expandedNodes, getParentId]); // Removed getHierarchyLevel dependency

  const flattenedItems = useMemo(() => {
    const list: FlattenedItem[] = [];
    const flatten = (node: EventNode, key: string, isWithinSubAgent = false) => {
      list.push({ node, uniqueKey: key });
      
      // If this is a delegation_start (sub-agent), we STOP flattening its children into the main list.
      // They will be rendered internally by the sub-agent card's scrollable logs area.
      // We only do this if we are not ALREADY inside a sub-agent log area (to keep nested ones contained too).
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
    return list;
  }, [eventTree]);

  const handleLoadMore = useCallback(async () => {
    if (!sessionId || isLoadingOlder) return;
    setIsLoadingOlder(true);
    try {
      const response = await agentApi.getSessionEvents(sessionId, undefined, {
        limit: 50,
        offset: paginationOffset,
        eventMode
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
  }, [sessionId, paginationOffset, eventMode, isLoadingOlder]);

  const renderItem = useCallback((_index: number, item: FlattenedItem) => {
    const { node, uniqueKey } = item;
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
        followOutput="smooth"
        itemContent={renderItem}
        components={{
          Header: () => hasMoreOlderEvents ? (
            <div className="flex justify-center py-4">
              <button
                onClick={handleLoadMore}
                disabled={isLoadingOlder}
                className="px-4 py-2 text-sm font-medium text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 hover:bg-blue-100 dark:hover:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {isLoadingOlder ? 'Loading...' : 'Load Older Events'}
              </button>
            </div>
          ) : null
        }}
      />
    </div>
  );
});

EventHierarchy.displayName = 'EventHierarchy';
