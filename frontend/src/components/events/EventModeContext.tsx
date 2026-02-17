import React, { useState, useCallback, useEffect } from 'react';
import type { ReactNode } from 'react';
import { EventModeContext, type EventMode } from './EventContext';
import { useChatStore } from '../../stores/useChatStore';

export const EventModeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  // Get active tab's event mode, fallback to 'micro' if no tab
  const activeTab = useChatStore(state => state.getActiveTab())
  const tabEventMode = activeTab?.eventMode || 'micro'
  const [mode, setMode] = useState<EventMode>(tabEventMode)
  
  // Update mode when active tab changes
  useEffect(() => {
    setMode(tabEventMode)
  }, [tabEventMode])
  
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
    // Toggle: micro ↔ advanced
    (window as Window & { cycleEventMode?: () => void }).cycleEventMode = () => {
      const newMode: EventMode = mode === 'micro' ? 'advanced' : 'micro';
      setTabMode(newMode)
    };
    
    return () => {
      delete (window as Window & { cycleEventMode?: () => void }).cycleEventMode;
    };
  }, [mode, setTabMode]);

  return (
    <EventModeContext.Provider value={{ mode, setMode: setTabMode }}>
      {children}
    </EventModeContext.Provider>
  );
};
