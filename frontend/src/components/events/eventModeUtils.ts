import type { EventMode } from './EventContext';

// Advanced mode events - events that are hidden in basic mode
export const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  // 'system_prompt' - removed: now shown in basic mode
  'conversation_start',
  'conversation_turn',
  'cache_event',
  'comprehensive_cache_event',
  'step_execution_start',
  'step_execution_end',
  'step_execution_failed',
  'step_progress_updated',
  'workspace_file_operation', // File operations for debugging
  // Add more advanced events here as needed
]);

// Helper function to check if an event should be shown based on event mode
export const shouldShowEventByMode = (eventType: string, mode: EventMode): boolean => {
  if (!eventType) return false
  if (mode === 'advanced') {
    return true // Show all events in advanced mode
  }
  // In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
  return !ADVANCED_MODE_EVENTS.has(eventType)
}

