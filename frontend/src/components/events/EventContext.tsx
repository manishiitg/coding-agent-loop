import { createContext } from 'react';

export type EventMode = 'basic' | 'advanced';

interface EventModeContextType {
  mode: EventMode;
  setMode: (mode: EventMode) => void;
}

export const EventModeContext = createContext<EventModeContextType | undefined>(undefined); 