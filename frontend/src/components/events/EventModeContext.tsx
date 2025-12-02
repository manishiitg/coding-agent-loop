import React, { useState, useCallback } from 'react';
import type { ReactNode } from 'react';
import { EventModeContext, type EventMode } from './EventContext';

// Advanced mode events - events that are hidden in basic mode
const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  'system_prompt',
  'conversation_start',
  'conversation_turn',
  'cache_event',
  'comprehensive_cache_event',
  'step_execution_start',
  'step_execution_end',
  'step_execution_failed',
  'step_progress_updated',
  // Add more advanced events here as needed
]);

export const EventModeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [mode, setMode] = useState<EventMode>('basic');

  const shouldShowEvent = useCallback((eventType: string): boolean => {
    if (mode === 'advanced') {
      return true; // Show all events in advanced mode
    }

    // In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
    const shouldShow = !ADVANCED_MODE_EVENTS.has(eventType);
    return shouldShow;
  }, [mode]);

  React.useEffect(() => {
    // Expose global function for event mode cycling
    (window as Window & { cycleEventMode?: () => void }).cycleEventMode = () => {
      setMode(prev => {
        // Simple toggle between basic and advanced
        return prev === 'basic' ? 'advanced' : 'basic';
      });
    };
    
    return () => {
      delete (window as Window & { cycleEventMode?: () => void }).cycleEventMode;
    };
  }, []);

  return (
    <EventModeContext.Provider value={{ mode, setMode, shouldShowEvent }}>
      {children}
    </EventModeContext.Provider>
  );
};