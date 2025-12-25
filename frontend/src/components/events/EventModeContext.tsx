import React, { useState, useCallback, useEffect } from 'react';
import type { ReactNode } from 'react';
import { EventModeContext, type EventMode } from './EventContext';
import { useChatStore } from '../../stores/useChatStore';

// Advanced mode events - events that are hidden in basic mode
const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  // 'system_prompt' - removed: now shown in basic mode
  'conversation_start',
  'conversation_turn',
  'cache_event',
  'comprehensive_cache_event',
  'step_execution_start',
  'step_execution_end',
  'step_execution_failed',
  'step_progress_updated',
  'workspace_file_operation', // File operations for debugging
  // Add more advanced events here as needed
]);

export const EventModeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  // Get active tab's event mode, fallback to 'basic' if no tab
  const activeTab = useChatStore(state => state.getActiveTab())
  const tabEventMode = activeTab?.eventMode || 'basic'
  const [mode, setMode] = useState<EventMode>(tabEventMode)
  
  // Update mode when active tab changes
  useEffect(() => {
    setMode(tabEventMode)
  }, [tabEventMode])

  const shouldShowEvent = useCallback((eventType: string): boolean => {
    if (mode === 'advanced') {
      return true; // Show all events in advanced mode
    }

    // In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
    const shouldShow = !ADVANCED_MODE_EVENTS.has(eventType);
    return shouldShow;
  }, [mode]);
  
  // Custom setMode that updates the active tab's event mode
  const setTabMode = useCallback((newMode: EventMode) => {
    setMode(newMode)
    const activeTab = useChatStore.getState().getActiveTab()
    if (activeTab) {
      useChatStore.getState().setTabEventMode(activeTab.tabId, newMode)
    }
  }, [])

  React.useEffect(() => {
    // Expose global function for event mode cycling
    (window as Window & { cycleEventMode?: () => void }).cycleEventMode = () => {
      const newMode = mode === 'basic' ? 'advanced' : 'basic'
      setTabMode(newMode)
    };
    
    return () => {
      delete (window as Window & { cycleEventMode?: () => void }).cycleEventMode;
    };
  }, [mode, setTabMode]);

  return (
    <EventModeContext.Provider value={{ mode, setMode: setTabMode, shouldShowEvent }}>
      {children}
    </EventModeContext.Provider>
  );
};
