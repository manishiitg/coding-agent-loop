import React from 'react';
import { useChatStore } from '../../stores/useChatStore';
import { Wrench } from 'lucide-react';
import { cn } from '@/lib/utils';

export const ToolCallToggle: React.FC = () => {
  const activeTab = useChatStore(state => state.getActiveTab());
  const hideToolCalls = activeTab?.hideToolCalls || false;
  const setTabHideToolCalls = useChatStore(state => state.setTabHideToolCalls);

  const toggle = () => {
    if (activeTab) {
      setTabHideToolCalls(activeTab.tabId, !hideToolCalls);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      toggle();
    }
  };

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={toggle}
      onKeyDown={handleKeyDown}
      className={cn(
        "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors",
        "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
        "border border-input bg-background shadow-sm hover:bg-accent hover:text-accent-foreground",
        "cursor-pointer",
        "flex items-center justify-center h-5 w-5 p-0 border-gray-300 dark:border-gray-600",
        hideToolCalls && "opacity-40"
      )}
      title={hideToolCalls ? 'Tool calls hidden (click to show)' : 'Tool calls visible (click to hide)'}
      data-testid="tool-call-toggle"
    >
      <Wrench className="w-3 h-3" />
    </div>
  );
};
