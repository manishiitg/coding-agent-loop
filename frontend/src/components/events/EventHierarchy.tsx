import React, { useState, useMemo, useCallback, useRef, useEffect } from 'react';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher } from './EventDispatcher';
import { agentApi } from '../../services/api';
import { useChatStore } from '../../stores/useChatStore';
import { MAX_EVENTS_TO_PROCESS } from '../../constants/events';
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso';
import './EventHierarchy.css';

interface EventHierarchyProps {
  events: PollingEvent[];
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean  // Loading state for approve button
  compact?: boolean  // Compact mode for smaller font sizes
  flatHierarchy?: boolean  // If true, removes left padding/indentation for hierarchy levels
}

interface EventNode {
  event: PollingEvent;
  children: EventNode[];
  level: number;
  isExpanded: boolean;
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
  isApproving, 
  compact = false, 
  flatHierarchy = false 
}) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
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

  // Get active tab for sessionId and eventMode
  const activeTab = useChatStore(state => state.getActiveTab())
  const sessionId = activeTab?.sessionId
  const eventMode: 'basic' | 'advanced' | 'tiny' | 'micro' = (activeTab?.eventMode || 'basic') as 'basic' | 'advanced' | 'tiny' | 'micro'
  
  // Merge loaded older events with current events
  const displayEvents = useMemo(() => {
    let allEvents = [...loadedOlderEvents, ...events];

    // Deduplicate events by ID to prevent visual duplicates
    const seenIds = new Set<string>();
    allEvents = allEvents.filter(event => {
      if (seenIds.has(event.id)) return false;
      seenIds.add(event.id);
      return true;
    });

    // Filter out streaming events in all modes - these are internal events for UI streaming
    const HIDDEN_STREAMING_EVENTS = ['streaming_start', 'streaming_chunk', 'streaming_end'];
    allEvents = allEvents.filter(event => !HIDDEN_STREAMING_EVENTS.includes(event.type));

    // Filter out "Total Token Usage" and "Context Offloading" events in tiny/micro mode
    if (eventMode === 'tiny' || eventMode === 'micro') {
      allEvents = allEvents.filter(event => {
        if (event.type === 'token_usage') {
          // Check if it's a total token usage event
          // events-bridge structure: event.data.data holds the actual event payload
          const agentEvent = event.data as { data?: Record<string, unknown> } | undefined
          const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined

          if (payload?.context === 'conversation_total') {
            return false
          }
        }

        // Hide Context Offloading events in tiny mode
        if (event.type === 'large_tool_output_detected' || event.type === 'large_tool_output_file_written') {
          return false
        }

        // Hide agent_end, agent_start, and orchestrator_agent_end events with agent_type "simple" in micro mode
        if (eventMode === 'micro' && (event.type === 'orchestrator_agent_end' || event.type === 'agent_end' || event.type === 'agent_start')) {
          const agentEvent = event.data as { data?: Record<string, unknown>; agent_type?: string } | undefined
          const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined
          const agentType = (payload as Record<string, unknown> | undefined)?.agent_type || agentEvent?.agent_type
          if (agentType === 'simple') {
            return false
          }
        }

        return true
      })
    }

    const sortedEvents = allEvents.sort((a, b) => {
      const timeA = a.timestamp ? (Date.parse(a.timestamp) || 0) : 0;
      const timeB = b.timestamp ? (Date.parse(b.timestamp) || 0) : 0;
      return timeA - timeB;
    });
    
    if (sortedEvents.length > MAX_EVENTS_TO_PROCESS) {
      return sortedEvents.slice(-MAX_EVENTS_TO_PROCESS);
    }
    return sortedEvents;
  }, [events, loadedOlderEvents, eventMode]);
  
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
    if ('parent_id' in event && event.parent_id) return event.parent_id;
    if (event.data && typeof event.data === 'object') {
      for (const [, value] of Object.entries(event.data)) {
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

  const findEventsBetweenStartEnd = useMemo(() => {
    const sessionEvents = new Map<string, Set<string>>();
    const startEvents = new Map<string, { event: PollingEvent; index: number }>();
    const endEvents = new Map<string, { event: PollingEvent; index: number }>();

    displayEvents.forEach((event, index) => {
      const sessionKey = getAgentSessionKey(event);
      if (!sessionKey) return;
      if (event.type === 'orchestrator_agent_start') startEvents.set(sessionKey, { event, index });
      else if (event.type === 'orchestrator_agent_end') endEvents.set(sessionKey, { event, index });
    });

    startEvents.forEach((startInfo, sessionKey) => {
      const endInfo = endEvents.get(sessionKey);
      if (!endInfo) return;
      const eventIds = new Set<string>();
      eventIds.add(startInfo.event.id);
      for (let i = startInfo.index + 1; i < endInfo.index; i++) eventIds.add(displayEvents[i].id);
      eventIds.add(endInfo.event.id);
      sessionEvents.set(sessionKey, eventIds);
    });
    return sessionEvents;
  }, [displayEvents, getAgentSessionKey]);

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
      if (newCollapsed.has(sessionKey)) newCollapsed.delete(sessionKey);
      else newCollapsed.add(sessionKey);
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
    const flatten = (node: EventNode, key: string) => {
      list.push({ node, uniqueKey: key });
      if (node.isExpanded && node.children.length > 0) {
        node.children.forEach((child, index) => {
          flatten(child, `${key}-child-${index}`);
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
                isApproving={isApproving}
                isCollapsed={isCollapsed}
                eventCount={eventCount}
                onToggleCollapse={onToggleCollapse}
                compact={compact}
              />
            </div>
          </div>
        </div>
      </div>
    );
  }, [collapsedSessions, findEventsBetweenStartEnd, getAgentSessionKey, toggleAgentSession, toggleNode, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact, flatHierarchy]);

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
