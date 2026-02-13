import type { EventMode } from './EventContext';

// Events that are never displayed in any mode (they drive non-chat UI like canvas node status)
export const NEVER_DISPLAY_EVENTS = new Set([
  'step_progress_updated',
]);

// Advanced mode events - events that are hidden in basic mode
export const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  'conversation_start',
  'conversation_turn',
  // Cache events - still filter on frontend as safety net (old events may be in database)
  'cache_event',
  'comprehensive_cache_event',
]);

// Tiny mode additional events - events hidden in tiny mode beyond what basic mode hides
// Tiny mode hides everything basic mode hides PLUS user messages, system prompts, and agent lifecycle events
export const TINY_MODE_ADDITIONAL_EVENTS = new Set([
  'user_message',
  'system_prompt',
  'agent_start',
  'agent_end',
  'agent_error',
  'llm_generation_end',
  'batch_execution_canceled',
]);

// Helper function to check if an event should be shown based on event mode
export const shouldShowEventByMode = (eventType: string, mode: EventMode): boolean => {
  if (!eventType) return false
  if (NEVER_DISPLAY_EVENTS.has(eventType)) return false
  if (mode === 'advanced') {
    return true // Show all events in advanced mode
  }
  if (mode === 'tiny' || mode === 'micro') {
    // In tiny/micro mode, hide everything basic mode hides PLUS user_message and system_prompt
    // So hide if it's in ADVANCED_MODE_EVENTS OR in TINY_MODE_ADDITIONAL_EVENTS
    return !ADVANCED_MODE_EVENTS.has(eventType) && !TINY_MODE_ADDITIONAL_EVENTS.has(eventType)
  }
  // In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
  return !ADVANCED_MODE_EVENTS.has(eventType)
}

