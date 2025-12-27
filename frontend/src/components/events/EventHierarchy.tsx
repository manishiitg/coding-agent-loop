import React, { useState } from 'react';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher } from './EventDispatcher';
import { agentApi } from '../../services/api';
import { useChatStore } from '../../stores/useChatStore';
import { MAX_EVENTS_TO_PROCESS } from '../../constants/events';
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

export const EventHierarchy: React.FC<EventHierarchyProps> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact = false, flatHierarchy = false }) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
  // Track loaded older events from backend (prepended to props events)
  const [loadedOlderEvents, setLoadedOlderEvents] = useState<PollingEvent[]>([]);
  // Track pagination offset for loading older events
  const [paginationOffset, setPaginationOffset] = useState<number>(0);
  // Track loading state
  const [isLoadingOlder, setIsLoadingOlder] = useState<boolean>(false);
  // Track if there are more events to load
  const [hasMoreOlderEvents, setHasMoreOlderEvents] = useState<boolean>(false);
  // Track last event count to detect new events
  const lastEventCountRef = React.useRef<number>(0);
  
  // Get active tab for sessionId and eventMode
  const activeTab = useChatStore(state => state.getActiveTab())
  const sessionId = activeTab?.sessionId
  const eventMode: 'basic' | 'advanced' = (activeTab?.eventMode || 'basic') as 'basic' | 'advanced'
  
  // Merge loaded older events with current events (older events first, then current events)
  const displayEvents = React.useMemo(() => {
    // Combine: older events (loaded from backend) + current events (from props/store)
    const allEvents = [...loadedOlderEvents, ...events];
    
    // Limit to prevent browser freeze
    if (allEvents.length <= MAX_EVENTS_TO_PROCESS) {
      return allEvents;
    }
    // Only process the most recent MAX_EVENTS_TO_PROCESS events
    console.warn(`[PERF] Limiting events from ${allEvents.length} to ${MAX_EVENTS_TO_PROCESS} to prevent freeze`);
    return allEvents.slice(-MAX_EVENTS_TO_PROCESS);
  }, [events, loadedOlderEvents]);
  
  // Reset loaded older events when session or event mode changes
  // (since filtering happens on backend, we need to reload when mode changes)
  React.useEffect(() => {
    setLoadedOlderEvents([])
    setPaginationOffset(0)
    // Get hasMoreOlderEvents from store (set by ChatArea when initial fetch completes)
    const chatStore = useChatStore.getState()
    const hasMore = sessionId ? chatStore.getTabHasMoreOlderEvents(sessionId) : false
    setHasMoreOlderEvents(hasMore)
  }, [sessionId, eventMode])
  
  // Track event count changes
  React.useEffect(() => {
    lastEventCountRef.current = displayEvents.length;
  }, [displayEvents.length]);
  
  // All events are visible (no startIndex needed - we use pagination instead)
  const visibleEvents = displayEvents;

  // Extract parent_id from event data
  const getParentId = React.useCallback((event: PollingEvent): string | undefined => {
    // First check top-level parent_id
    if ('parent_id' in event && event.parent_id) {
      return event.parent_id;
    }
    
    // Fallback: check nested data
    if (event.data && typeof event.data === 'object') {
      for (const [, value] of Object.entries(event.data)) {
        if (value && typeof value === 'object' && 'parent_id' in value) {
          return (value as { parent_id: string }).parent_id;
        }
      }
    }
    return undefined;
  }, []);

  // Extract hierarchy_level from event data
  const getHierarchyLevel = React.useCallback((event: PollingEvent): number => {
    // Debug: Log the event structure to see what fields are available
    
    // First check top-level hierarchy_level
    if ('hierarchy_level' in event && typeof event.hierarchy_level === 'number') {
      // Found hierarchy_level at top level
      return event.hierarchy_level;
    }
    
    // Fallback: check nested data
    if (event.data && typeof event.data === 'object') {
      for (const [, value] of Object.entries(event.data)) {
        if (value && typeof value === 'object' && 'hierarchy_level' in value) {
          const level = (value as { hierarchy_level: number }).hierarchy_level;
          // Found hierarchy_level in nested data
          return level;
        }
      }
    }
    
    // Always default to L-1 if hierarchy_level not found - ensures events are always visible
    return -1;
  }, []);

  // Extract agent session key from orchestrator_agent_start/end events for matching
  const getAgentSessionKey = React.useCallback((event: PollingEvent): string | null => {
    // Only process orchestrator_agent_start and orchestrator_agent_end events
    if (event.type !== 'orchestrator_agent_start' && event.type !== 'orchestrator_agent_end') {
      return null;
    }

    // Extract correlation_id and agent_type from event data
    // The data structure can be:
    // 1. event.data.data (nested data field)
    // 2. event.data.orchestrator_agent_start or event.data.orchestrator_agent_end (nested by type)
    // 3. event.data (direct - data itself is the event)
    let correlationId: string | undefined;
    let agentType: string | undefined;

    if (event.data && typeof event.data === 'object') {
      const data = event.data as Record<string, unknown>;
      
      // Try nested data field first (matches extractEventData pattern)
      let eventData = (data.data && typeof data.data === 'object') 
        ? (data.data as Record<string, unknown>)
        : undefined;
      
      // If not found, try nested structure by type
      if (!eventData || typeof eventData !== 'object') {
        eventData = (data.orchestrator_agent_start || data.orchestrator_agent_end) as Record<string, unknown> | undefined;
      }
      
      // If still not found, try direct structure
      if (!eventData || typeof eventData !== 'object') {
        eventData = data;
      }
      
      if (eventData && typeof eventData === 'object') {
        correlationId = eventData.correlation_id as string | undefined;
        agentType = eventData.agent_type as string | undefined;
      }
    }

    // Generate session key: correlation_id + agent_type
    if (correlationId && agentType) {
      return `agent_session:${correlationId}:${agentType}`;
    }

    // Fallback: use trace_id + agent_type
    let traceId: string | undefined;
    if (event.data && typeof event.data === 'object') {
      const data = event.data as Record<string, unknown>;
      
      // Try nested data field first
      let eventData = (data.data && typeof data.data === 'object') 
        ? (data.data as Record<string, unknown>)
        : undefined;
      
      // If not found, try nested structure by type
      if (!eventData || typeof eventData !== 'object') {
        eventData = (data.orchestrator_agent_start || data.orchestrator_agent_end) as Record<string, unknown> | undefined;
      }
      
      // If still not found, try direct structure
      if (!eventData || typeof eventData !== 'object') {
        eventData = data;
      }
      
      if (eventData && typeof eventData === 'object') {
        traceId = eventData.trace_id as string | undefined;
        if (!agentType) {
          agentType = eventData.agent_type as string | undefined;
        }
      }
    }

    if (traceId && agentType) {
      return `agent_session:${traceId}:${agentType}`;
    }

    return null;
  }, []);

  // Find all events between orchestrator_agent_start and orchestrator_agent_end
  // OPTIMIZATION: Only process visibleEvents to reduce computation
  const findEventsBetweenStartEnd = React.useMemo(() => {
    const sessionEvents = new Map<string, Set<string>>(); // sessionKey -> Set of event IDs
    
    // First pass: identify all orchestrator_agent_start and orchestrator_agent_end events and their session keys
    const startEvents = new Map<string, { event: PollingEvent; index: number }>(); // sessionKey -> start event
    const endEvents = new Map<string, { event: PollingEvent; index: number }>(); // sessionKey -> end event

    visibleEvents.forEach((event, index) => {
      const sessionKey = getAgentSessionKey(event);
      if (!sessionKey) return;

      if (event.type === 'orchestrator_agent_start') {
        startEvents.set(sessionKey, { event, index });
      } else if (event.type === 'orchestrator_agent_end') {
        endEvents.set(sessionKey, { event, index });
      }
    });

    // Second pass: for each matched start/end pair, collect all events between them
    startEvents.forEach((startInfo, sessionKey) => {
      const endInfo = endEvents.get(sessionKey);
      if (!endInfo) {
        return; // No matching end event found
      }

      const eventIds = new Set<string>();
      // Include start event
      eventIds.add(startInfo.event.id);
      
      // Include all events between start and end (exclusive of end)
      for (let i = startInfo.index + 1; i < endInfo.index; i++) {
        eventIds.add(visibleEvents[i].id);
      }
      
      // Include end event
      eventIds.add(endInfo.event.id);

      sessionEvents.set(sessionKey, eventIds);
    });

    return sessionEvents;
  }, [visibleEvents, getAgentSessionKey]);

  const toggleNode = React.useCallback((eventId: string) => {
    setExpandedNodes(prev => {
      const newExpanded = new Set(prev);
      if (newExpanded.has(eventId)) {
        newExpanded.delete(eventId);
      } else {
        newExpanded.add(eventId);
      }
      return newExpanded;
    });
  }, []);

  const toggleAgentSession = React.useCallback((sessionKey: string) => {
    setCollapsedSessions(prevCollapsed => {
      const newCollapsed = new Set(prevCollapsed);
      if (newCollapsed.has(sessionKey)) {
        newCollapsed.delete(sessionKey);
      } else {
        newCollapsed.add(sessionKey);
      }
      return newCollapsed;
    });
  }, []);

  // Memoized event node renderer to prevent unnecessary re-renders
  const renderEventNode = React.useCallback((node: EventNode): React.ReactNode => {
    const { event, children, level, isExpanded } = node;
    const hasChildren = children.length > 0;
    // Support up to L10: L0 = 10px, L1 = 20px, ..., L10 = 110px
    // If flatHierarchy is true, no indentation is applied
    const indent = flatHierarchy ? 0 : Math.min((level + 1) * 10, 110); // Cap at L10 (110px)
    
    // Get session info for orchestrator_agent_start events
    const sessionKey = getAgentSessionKey(event);
    const isCollapsed = sessionKey ? collapsedSessions.has(sessionKey) : false;
    const eventCount = sessionKey && findEventsBetweenStartEnd.has(sessionKey)
      ? findEventsBetweenStartEnd.get(sessionKey)!.size - 2 // Subtract start and end events
      : undefined;
    const onToggleCollapse = sessionKey ? () => toggleAgentSession(sessionKey) : undefined;
    
    return (
      <div key={event.id} className="event-tree-node" data-event-type={event.type}>
        <div 
          className="event-tree-item"
          style={{ marginLeft: `${indent}px` }}
        >
          {/* Expand/Collapse Button */}
          {hasChildren && (
            <button
              onClick={() => toggleNode(event.id)}
              className="expand-button"
              aria-label={isExpanded ? 'Collapse' : 'Expand'}
            >
              <span className={`expand-icon ${isExpanded ? 'expanded' : ''}`}>
                {isExpanded ? '▼' : '▶'}
              </span>
            </button>
          )}
          
          
          {/* Event Content */}
          <div className="event-content">
            {/* Full Event Details */}
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
        
        {/* Render children if expanded */}
        {isExpanded && hasChildren && (
          <div className="event-children">
            {children.map(child => renderEventNode(child))}
          </div>
        )}
      </div>
    );
  }, [collapsedSessions, findEventsBetweenStartEnd, getAgentSessionKey, toggleAgentSession, toggleNode, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact, flatHierarchy]);

  // Build event tree from flat list - memoized to react to collapsedSessions changes
  // OPTIMIZATION: Filter collapsed events early to reduce processing
  const eventTree = React.useMemo(() => {
    // Early return: if no collapsed sessions, use all visible events (skip filtering overhead)
    if (collapsedSessions.size === 0) {
      const filteredEvents = visibleEvents;
      
      const childrenMap = new Map<string, PollingEvent[]>();
      
      // Build parent-child map
      filteredEvents.forEach(event => {
        const parentId = getParentId(event);
        if (parentId) {
          if (!childrenMap.has(parentId)) {
            childrenMap.set(parentId, []);
          }
          childrenMap.get(parentId)!.push(event);
        }
      });
      
      // Build trees recursively
      const buildTreeRecursive = (event: PollingEvent): EventNode => {
        const children = childrenMap.get(event.id) || [];
        const childNodes = children.map(child => buildTreeRecursive(child));
        
        return {
          event,
          children: childNodes,
          level: getHierarchyLevel(event),
          isExpanded: expandedNodes.has(event.id)
        };
      };
      
      return filteredEvents.map(event => buildTreeRecursive(event));
    }

    // Step 1: Build a map of event ID -> event for O(1) lookup (instead of O(n) find)
    const eventById = new Map<string, PollingEvent>();
    visibleEvents.forEach(event => {
      eventById.set(event.id, event);
    });

    // Step 2: Determine which events should be filtered out (collapsed sessions)
    // Use Set for O(1) lookup instead of array.find() which is O(n)
    const eventsToFilter = new Set<string>();
    findEventsBetweenStartEnd.forEach((eventIds, sessionKey) => {
      const isCollapsed = collapsedSessions.has(sessionKey);

      if (isCollapsed) {
        // Filter out all events in this session except the start and end events
        eventIds.forEach(eventId => {
          const event = eventById.get(eventId);
          if (event && event.type !== 'orchestrator_agent_start' && event.type !== 'orchestrator_agent_end') {
            eventsToFilter.add(eventId);
          }
        });
      }
    });

    // Step 3: Filter events: remove collapsed events but keep start/end events
    // This reduces the number of events processed in tree building significantly
    const filteredEvents = visibleEvents.filter(event => !eventsToFilter.has(event.id));

    const childrenMap = new Map<string, PollingEvent[]>();
    
    // Build parent-child map (only for filtered events)
    filteredEvents.forEach(event => {
      const parentId = getParentId(event);
      if (parentId) {
        if (!childrenMap.has(parentId)) {
          childrenMap.set(parentId, []);
        }
        childrenMap.get(parentId)!.push(event);
      }
    });
    
    // Build trees recursively
    const buildTreeRecursive = (event: PollingEvent): EventNode => {
      const children = childrenMap.get(event.id) || [];
      const childNodes = children.map(child => buildTreeRecursive(child));
      
      return {
        event,
        children: childNodes,
        level: getHierarchyLevel(event), // Use actual hierarchy level from event data
        isExpanded: expandedNodes.has(event.id)
      };
    };
    
    return filteredEvents.map(event => buildTreeRecursive(event));
  }, [visibleEvents, collapsedSessions, findEventsBetweenStartEnd, expandedNodes, getParentId, getHierarchyLevel]);

  // Load more events handler - fetches older events from backend
  const handleLoadMore = React.useCallback(async () => {
    if (!sessionId || isLoadingOlder) {
      return;
    }
    
    setIsLoadingOlder(true);
    try {
      // Fetch older events using pagination (limit=50, offset=current offset)
      const response = await agentApi.getSessionEvents(sessionId, undefined, {
        limit: 50,
        offset: paginationOffset,
        eventMode
      });
      
      if (response.events.length > 0) {
        // Events come from backend in chronological order (oldest first) already
        // Prepend older events to the beginning of our loaded events
        setLoadedOlderEvents(prev => [...response.events, ...prev]);
        // Update offset: add the number of events we just loaded
        setPaginationOffset(prev => prev + response.events.length);
        setHasMoreOlderEvents(response.has_more);
      } else {
        setHasMoreOlderEvents(false);
      }
    } catch (error) {
      console.error('[EventHierarchy] Failed to load older events:', error);
      setHasMoreOlderEvents(false);
    } finally {
      setIsLoadingOlder(false);
    }
  }, [sessionId, paginationOffset, eventMode, isLoadingOlder]);

  // Check if there are more events to load from backend
  const hasMoreEvents = hasMoreOlderEvents;

  if (eventTree.length === 0) {
    return (
      <div className="text-gray-500 text-center py-4">
        No hierarchical events to display
      </div>
    );
  }

  return (
    <div className="event-hierarchy">
      {/* Event tree */}
      <div
        className="event-tree-container"
      >
        {/* Load older events button at TOP (to load events that appear above current view) */}
        {hasMoreEvents && (
          <div className="flex justify-center py-4">
            <button
              onClick={handleLoadMore}
              disabled={isLoadingOlder}
              className="px-4 py-2 text-sm font-medium text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 hover:bg-blue-100 dark:hover:bg-blue-900/30 border border-blue-200 dark:border-blue-800 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isLoadingOlder ? 'Loading...' : 'Load Older Events'}
            </button>
          </div>
        )}
        
        {/* Render events in chronological order (oldest first, latest at bottom) */}
        {/* Performance: Only render top-level nodes initially to prevent freeze */}
        {eventTree.map((node) => (
          <div key={node.event.id}>
            {renderEventNode(node)}
          </div>
        ))}
      </div>
    </div>
  );
});
