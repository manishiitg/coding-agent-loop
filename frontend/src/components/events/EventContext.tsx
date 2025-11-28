import { createContext } from 'react';

export type EventMode = 'basic' | 'advanced';

interface EventModeContextType {
  mode: EventMode;
  setMode: (mode: EventMode) => void;
  shouldShowEvent: (eventType: string) => boolean;
}

export const EventModeContext = createContext<EventModeContextType | undefined>(undefined); 