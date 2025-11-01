import React, { useState, useCallback } from 'react';
import type { ReactNode } from 'react';
import { EventModeContext, type EventMode } from './EventContext';
import { useAppStore } from '../../stores/useAppStore';

// Advanced mode events - events that are hidden in basic mode
const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_end',
  'llm_generation_with_retry',
  'system_prompt',
  'conversation_start',
  'conversation_turn',
  'cache_event',
  'comprehensive_cache_event',
  // 'orchestrator_start',
  // 'orchestrator_end',
  // Add more advanced events here as needed
]);

// Deep Search mode events - only show Deep Search-specific events
const ORCHESTRATOR_MODE_EVENTS = new Set([
  'orchestrator_start',
  'orchestrator_end',
  'orchestrator_error',
  'orchestrator_agent_start',
  'orchestrator_agent_end',
  'orchestrator_agent_error',
]);

// Workflow mode events - only show workflow-specific events
const WORKFLOW_MODE_EVENTS = new Set([
  'orchestrator_agent_start',
  'orchestrator_agent_end',
  'orchestrator_agent_error',
]);

export const EventModeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [mode, setMode] = useState<EventMode>('basic');
  const { agentMode } = useAppStore();

  const shouldShowEvent = useCallback((eventType: string): boolean => {
    if (mode === 'advanced') {
      return true; // Show all events in advanced mode
    }

    if (mode === 'orchestrator') {
      // In Deep Search mode, only show Deep Search-specific events
      const shouldShow = ORCHESTRATOR_MODE_EVENTS.has(eventType);
      return shouldShow;
    }

    if (mode === 'workflow') {
      // In workflow mode, only show workflow-specific events
      const shouldShow = WORKFLOW_MODE_EVENTS.has(eventType);
      return shouldShow;
    }

    // In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
    const shouldShow = !ADVANCED_MODE_EVENTS.has(eventType);
    return shouldShow;
  }, [mode, agentMode]);

  // Expose global function for event mode cycling with conditional logic

  React.useEffect(() => {
    // Expose global function for event mode cycling
    (window as Window & { cycleEventMode?: () => void }).cycleEventMode = () => {
      setMode(prev => {
        // Context-aware cycling based on current mode and agent mode
        switch (prev) {
          case 'basic':
            return 'advanced';
          case 'advanced':
            // Context-aware cycling based on agent mode
            if (agentMode === 'workflow') {
              return 'workflow';
            } else if (agentMode === 'orchestrator') {
              return 'orchestrator';
            } else {
              // For simple/ReAct agent modes, cycle back to basic
              return 'basic';
            }
          case 'workflow':
            return 'basic'; // Cycle back to basic from workflow
          case 'orchestrator':
            return 'basic'; // Cycle back to basic from Deep Search
          default:
            return 'basic';
        }
      });
    };
    
    return () => {
      delete (window as Window & { cycleEventMode?: () => void }).cycleEventMode;
    };
  }, [agentMode]); // Add agentMode as dependency

  return (
    <EventModeContext.Provider value={{ mode, setMode, shouldShowEvent }}>
      {children}
    </EventModeContext.Provider>
  );
};

 