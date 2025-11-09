import React, { useState, useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher } from './EventDispatcher';
import './EventHierarchy.css';

interface EventHierarchyProps {
  events: PollingEvent[];
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean  // Loading state for approve button
}

interface EventNode {
  event: PollingEvent;
  children: EventNode[];
  level: number;
  isExpanded: boolean;
}

export const EventHierarchy: React.FC<EventHierarchyProps> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving }) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const [collapsedSessions, setCollapsedSessions] = useState<Set<string>>(new Set());
  const parentRef = useRef<HTMLDivElement>(null);
  
  // No longer need MAX_EVENTS limit - virtualization handles performance
  const displayEvents = events;

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
  const findEventsBetweenStartEnd = React.useMemo(() => {
    const sessionEvents = new Map<string, Set<string>>(); // sessionKey -> Set of event IDs
    
    // First pass: identify all orchestrator_agent_start and orchestrator_agent_end events and their session keys
    const startEvents = new Map<string, { event: PollingEvent; index: number }>(); // sessionKey -> start event
    const endEvents = new Map<string, { event: PollingEvent; index: number }>(); // sessionKey -> end event

    events.forEach((event, index) => {
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
        eventIds.add(events[i].id);
      }
      
      // Include end event
      eventIds.add(endInfo.event.id);

      sessionEvents.set(sessionKey, eventIds);
    });

    return sessionEvents;
  }, [events, getAgentSessionKey]);

  // Initialize collapsed sessions with all found session keys (all collapsed by default)
  // This runs only when new sessions are found (not when collapsedSessions changes)
  React.useEffect(() => {
    if (findEventsBetweenStartEnd.size > 0) {
      const allSessionKeys = Array.from(findEventsBetweenStartEnd.keys());
      const newCollapsed = new Set(collapsedSessions);
      let hasNewSessions = false;
      
      // Add any new session keys that aren't already in collapsedSessions
      // This preserves user's manual expand/collapse choices
      allSessionKeys.forEach(key => {
        if (!newCollapsed.has(key)) {
          newCollapsed.add(key);
          hasNewSessions = true;
        }
      });
      
      // Only update if there are new sessions to collapse
      if (hasNewSessions) {
        setCollapsedSessions(newCollapsed);
      }
    }
    // Only depend on findEventsBetweenStartEnd, not collapsedSessions
    // This prevents re-adding sessions that the user explicitly expanded
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [findEventsBetweenStartEnd]);


  const toggleNode = (eventId: string) => {
    const newExpanded = new Set(expandedNodes);
    if (newExpanded.has(eventId)) {
      newExpanded.delete(eventId);
    } else {
      newExpanded.add(eventId);
    }
    setExpandedNodes(newExpanded);
  };

  const toggleAgentSession = (sessionKey: string) => {
    const newCollapsed = new Set(collapsedSessions);
    if (newCollapsed.has(sessionKey)) {
      newCollapsed.delete(sessionKey);
    } else {
      newCollapsed.add(sessionKey);
    }
    setCollapsedSessions(newCollapsed);
  };

  const renderEventNode = (node: EventNode): React.ReactNode => {
    const { event, children, level, isExpanded } = node;
    const hasChildren = children.length > 0;
    // Support up to L10: L0 = 10px, L1 = 20px, ..., L10 = 110px
    const indent = Math.min((level + 1) * 10, 110); // Cap at L10 (110px)
    
    // Get session info for orchestrator_agent_start events
    const sessionKey = getAgentSessionKey(event);
    const isCollapsed = sessionKey ? collapsedSessions.has(sessionKey) : false;
    const eventCount = sessionKey && findEventsBetweenStartEnd.has(sessionKey)
      ? findEventsBetweenStartEnd.get(sessionKey)!.size - 2 // Subtract start and end events
      : undefined;
    const onToggleCollapse = sessionKey ? () => toggleAgentSession(sessionKey) : undefined;
    
    return (
      <div key={event.id} className="event-tree-node">
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
  };

  // Build event tree from flat list - memoized to react to collapsedSessions changes
  const eventTree = React.useMemo(() => {
    // Determine which events should be filtered out (collapsed sessions)
    const eventsToFilter = new Set<string>();
    findEventsBetweenStartEnd.forEach((eventIds, sessionKey) => {
      const isCollapsed = collapsedSessions.has(sessionKey);

      if (isCollapsed) {
        // Filter out all events in this session except the start and end events
        eventIds.forEach(eventId => {
          const event = displayEvents.find(e => e.id === eventId);
          if (event && event.type !== 'orchestrator_agent_start' && event.type !== 'orchestrator_agent_end') {
            eventsToFilter.add(eventId);
          }
        });
      }
    });

    // Filter events: remove collapsed events but keep start/end events
    const filteredEvents = displayEvents.filter(event => !eventsToFilter.has(event.id));

    const eventMap = new Map<string, PollingEvent>();
    const childrenMap = new Map<string, PollingEvent[]>();
    
    
    // Build maps
    filteredEvents.forEach(event => {
      eventMap.set(event.id, event);
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
  }, [displayEvents, collapsedSessions, findEventsBetweenStartEnd, expandedNodes, getParentId, getHierarchyLevel]);

  // Setup virtualizer for performance with large event lists
  const virtualizer = useVirtualizer({
    count: eventTree.length,
    getScrollElement: () => parentRef.current,
    estimateSize: (index) => {
      // Estimate size based on event type and content
      const node = eventTree[index];
      if (!node) return 80;
      
      // Basic estimation based on event type
      const baseSize = 80;
      const hasChildren = node.children.length > 0;
      const isExpanded = expandedNodes.has(node.event.id);
      
      // Add extra height for expanded nodes with children
      if (hasChildren && isExpanded) {
        return baseSize + (node.children.length * 60);
      }
      
      return baseSize;
    },
    overscan: 5, // Render 5 extra items above/below viewport for smooth scrolling
  });

  if (eventTree.length === 0) {
    return (
      <div className="text-gray-500 text-center py-4">
        No hierarchical events to display
      </div>
    );
  }

  return (
    <div className="event-hierarchy">
      {/* Event count info */}
      {events.length > 100 && (
        <div className="mb-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
          <div className="flex items-center justify-between">
            <div className="text-sm text-blue-700 dark:text-blue-300">
              Showing all {events.length} events (virtualized for performance)
            </div>
          </div>
        </div>
      )}
      
      {/* Virtualized event tree */}
      <div
        ref={parentRef}
        className="event-tree-container"
        style={{
          height: '100%',
          overflow: 'auto'
        }}
      >
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: '100%',
            position: 'relative'
          }}
        >
          {virtualizer.getVirtualItems().map((virtualRow) => {
            const node = eventTree[virtualRow.index];
            return (
              <div
                key={node.event.id}
                data-index={virtualRow.index}
                ref={(el) => {
                  if (el) {
                    virtualizer.measureElement(el);
                  }
                }}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${virtualRow.start}px)`
                }}
              >
                {renderEventNode(node)}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
});
