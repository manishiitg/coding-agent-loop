import { createContext } from 'react';

export type EventMode = 'advanced' | 'tiny' | 'micro';

interface EventModeContextType {
  mode: EventMode;
  setMode: (mode: EventMode) => void;
}

export const EventModeContext = createContext<EventModeContextType | undefined>(undefined); 