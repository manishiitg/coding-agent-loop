// Events that are never displayed in any mode (they drive non-chat UI like canvas node status)
export const NEVER_DISPLAY_EVENTS = new Set([
  'step_progress_updated',
]);

// Hidden events - events hidden in chat view
export const HIDDEN_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  'conversation_start',
  'conversation_turn',
  'cache_event',
  'comprehensive_cache_event',
  'system_prompt',
  'agent_start',
  'agent_end',
  'agent_error',
  'llm_generation_end',
  'batch_execution_canceled',
]);

// Helper function to check if an event should be shown
export const shouldShowEventByMode = (eventType: string, _mode?: unknown): boolean => {
  if (!eventType) return false
  if (NEVER_DISPLAY_EVENTS.has(eventType)) return false

  return !HIDDEN_EVENTS.has(eventType)
}
