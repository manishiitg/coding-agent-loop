import type { EventMode } from './EventContext';

// Events that are never displayed in any mode (they drive non-chat UI like canvas node status)
export const NEVER_DISPLAY_EVENTS = new Set([
  'step_progress_updated',
]);

// Advanced-only events - events that are only shown in advanced mode
export const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  'conversation_start',
  'conversation_turn',
  // Cache events - still filter on frontend as safety net (old events may be in database)
  'cache_event',
  'comprehensive_cache_event',
]);

// Tiny mode additional events - events hidden in tiny/micro mode beyond what advanced mode hides
// Note: user_message is NOT filtered — essential for conversation display on restore
export const TINY_MODE_ADDITIONAL_EVENTS = new Set([
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
    // In tiny/micro mode, hide ADVANCED_MODE_EVENTS + TINY_MODE_ADDITIONAL_EVENTS
    return !ADVANCED_MODE_EVENTS.has(eventType) && !TINY_MODE_ADDITIONAL_EVENTS.has(eventType)
  }
  // Fallback: treat unknown modes as tiny
  return !ADVANCED_MODE_EVENTS.has(eventType) && !TINY_MODE_ADDITIONAL_EVENTS.has(eventType)
}

