import React, { useState, useRef } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import type { PollingEvent } from '../../services/api-types';
import { EventDispatcher } from './EventDispatcher';
import './EventHierarchy.css';

interface EventHierarchyProps {
  events: PollingEvent[];
  // onApproveWorkflow?: (requestId: string) => void
  // isApproving?: boolean  // Loading state for approve button
}

interface EventNode {
  event: PollingEvent;
  children: EventNode[];
  level: number;
  isExpanded: boolean;
}

export const EventHierarchy: React.FC<EventHierarchyProps> = React.memo(({ events }) => {
  const [expandedNodes, setExpandedNodes] = useState<Set<string>>(new Set());
  const parentRef = useRef<HTMLDivElement>(null);
  
  // No longer need MAX_EVENTS limit - virtualization handles performance
  const displayEvents = events;

  // Extract parent_id from event data
  const getParentId = (event: PollingEvent): string | undefined => {
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
  };

  // Extract hierarchy_level from event data
  const getHierarchyLevel = (event: PollingEvent): number => {
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
  };

  // Build event tree from flat list
  const buildEventTree = (events: PollingEvent[]): EventNode[] => {
    const eventMap = new Map<string, PollingEvent>();
    const childrenMap = new Map<string, PollingEvent[]>();
    
    
    // Build maps
    events.forEach(event => {
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
    
    // Show ALL events without any filtering
    // This ensures all events including tool_call_start, tool_call_end, etc. are visible
    
    return events.map(event => buildTreeRecursive(event));
  };

  const toggleNode = (eventId: string) => {
    const newExpanded = new Set(expandedNodes);
    if (newExpanded.has(eventId)) {
      newExpanded.delete(eventId);
    } else {
      newExpanded.add(eventId);
    }
    setExpandedNodes(newExpanded);
  };

  const renderEventNode = (node: EventNode): React.ReactNode => {
    const { event, children, level, isExpanded } = node;
    const hasChildren = children.length > 0;
    // Support up to L10: L0 = 10px, L1 = 20px, ..., L10 = 110px
    const indent = Math.min((level + 1) * 10, 110); // Cap at L10 (110px)
    
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

  const eventTree = buildEventTree(displayEvents);

  // Setup virtualizer for performance with large event lists
  const virtualizer = useVirtualizer({
    count: eventTree.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 80, // Average event height in pixels
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
                ref={virtualizer.measureElement}
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
