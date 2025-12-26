import React from 'react';
import { Button } from '../ui/Button';
import { useChatStore } from '../../stores/useChatStore';
import { Eye, EyeOff } from 'lucide-react';

export const EventModeToggle: React.FC = () => {
  // Get active tab's event mode directly from store
  const activeTab = useChatStore(state => state.getActiveTab());
  const mode = activeTab?.eventMode || 'basic';
  const setTabEventMode = useChatStore(state => state.setTabEventMode);

  const cycleMode = () => {
    // Simple toggle between basic and advanced
    if (activeTab) {
      const newMode = mode === 'basic' ? 'advanced' : 'basic';
      setTabEventMode(activeTab.tabId, newMode);
    }
  };

  const getModeDisplay = () => {
    switch (mode) {
      case 'basic':
        return { icon: Eye, label: 'Basic' };
      case 'advanced':
        return { icon: EyeOff, label: 'Advanced' };
      default:
        return { icon: Eye, label: 'Basic' };
    }
  };

  const { icon: Icon, label } = getModeDisplay();

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={cycleMode}
      className="flex items-center gap-1 text-xs h-6 px-1.5 border-gray-300 dark:border-gray-600"
      title={`Event Mode: ${label} (click to toggle)`}
      data-testid="event-mode-toggle"
    >
      <Icon className="w-3 h-3" />
      <span className="text-[10px]">{label}</span>
    </Button>
  );
}; 