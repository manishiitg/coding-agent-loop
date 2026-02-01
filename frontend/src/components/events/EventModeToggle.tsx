import React from 'react';
import { useChatStore } from '../../stores/useChatStore';
import { Filter, Settings, Minus, Minimize2 } from 'lucide-react';
import { cn } from '@/lib/utils';

export const EventModeToggle: React.FC = () => {
  // Get active tab's event mode directly from store
  const activeTab = useChatStore(state => state.getActiveTab());
  const mode = activeTab?.eventMode || 'basic';
  const setTabEventMode = useChatStore(state => state.setTabEventMode);

  const cycleMode = () => {
    // Cycle through: basic → advanced → tiny → micro → basic
    if (activeTab) {
      let newMode: 'basic' | 'advanced' | 'tiny' | 'micro';
      if (mode === 'basic') {
        newMode = 'advanced';
      } else if (mode === 'advanced') {
        newMode = 'tiny';
      } else if (mode === 'tiny') {
        newMode = 'micro';
      } else {
        newMode = 'basic';
      }
      setTabEventMode(activeTab.tabId, newMode);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      cycleMode();
    }
  };

  const getModeDisplay = () => {
    switch (mode) {
      case 'basic':
        return { icon: Filter, label: 'Basic' };
      case 'advanced':
        return { icon: Settings, label: 'Advanced' };
      case 'tiny':
        return { icon: Minus, label: 'Tiny' };
      case 'micro':
        return { icon: Minimize2, label: 'Micro' };
      default:
        return { icon: Filter, label: 'Basic' };
    }
  };

  const { icon: Icon, label } = getModeDisplay();

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={cycleMode}
      onKeyDown={handleKeyDown}
      className={cn(
        "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors",
        "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
        "border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground",
        "cursor-pointer",
        "flex items-center justify-center h-6 w-6 p-0 border-gray-300 dark:border-gray-600"
      )}
      title={`Event Mode: ${label} (click to toggle)`}
      data-testid="event-mode-toggle"
    >
      <Icon className="w-3.5 h-3.5" />
    </div>
  );
}; 