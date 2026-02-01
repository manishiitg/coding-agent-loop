import { createContext } from 'react';

export type EventMode = 'basic' | 'advanced' | 'tiny' | 'micro';

interface EventModeContextType {
  mode: EventMode;
  setMode: (mode: EventMode) => void;
}

export const EventModeContext = createContext<EventModeContextType | undefined>(undefined); 