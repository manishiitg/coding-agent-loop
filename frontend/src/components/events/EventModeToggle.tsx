import React from 'react';
import { Button } from '../ui/Button';
import { useEventMode } from './useEventMode';
import { Eye, EyeOff } from 'lucide-react';

export const EventModeToggle: React.FC = () => {
  const { mode, setMode } = useEventMode();

  const cycleMode = () => {
    // Simple toggle between basic and advanced
    setMode(mode === 'basic' ? 'advanced' : 'basic');
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
    <div className="flex items-center gap-1">
      <span className="text-xs text-gray-600 dark:text-gray-400">
        Event Mode:
      </span>
      <Button
        variant="outline"
        size="sm"
        onClick={cycleMode}
        className="flex items-center gap-1 text-xs h-7 px-2"
      >
        <Icon className="w-3 h-3" />
        {label}
      </Button>
    </div>
  );
}; 