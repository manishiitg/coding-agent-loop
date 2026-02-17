import React from 'react';
import { useChatStore } from '../../stores/useChatStore';
import { Settings, ListFilter } from 'lucide-react';
import { cn } from '@/lib/utils';

export const EventModeToggle: React.FC = () => {
  // Get active tab's event mode directly from store
  const activeTab = useChatStore(state => state.getActiveTab());
  const mode = activeTab?.eventMode || 'micro';
  const setTabEventMode = useChatStore(state => state.setTabEventMode);

  const cycleMode = () => {
    // Toggle: micro ↔ advanced
    if (activeTab) {
      const newMode: 'advanced' | 'micro' = mode === 'micro' ? 'advanced' : 'micro';
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
      case 'advanced':
        return { icon: Settings, label: 'Advanced' };
      case 'micro':
        return { icon: ListFilter, label: 'Micro' };
      default:
        return { icon: ListFilter, label: 'Micro' };
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
        "flex items-center justify-center h-4 w-4 p-0 border-gray-300 dark:border-gray-600"
      )}
      title={`Event Mode: ${label} (click to toggle)`}
      data-testid="event-mode-toggle"
    >
      <Icon className="w-2.5 h-2.5" />
    </div>
  );
}; 