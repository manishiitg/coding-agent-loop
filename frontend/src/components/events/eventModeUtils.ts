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

// Summary mode allowlist — only these event types are shown when viewMode='summary'.
// This dramatically reduces the event count (500 → ~20-30) for background workflows
// where the user only cares about agent progress, not execution details.
//
// Categories:
//   Agent lifecycle:    orchestrator start/end, delegation start/end, background agents
//   Task progress:      todo items (created/updated/completed), step completion, status updates
//   Batch progress:     batch group start/end
//   Workflow lifecycle:  workflow start/end/error
//   User interaction:   user messages, human feedback, plan approval
//   Errors/completion:  conversation end/error, context cancelled, unified completion
//   Session:            conversation resumed (separator)
export const SUMMARY_MODE_EVENTS = new Set([
  // Agent lifecycle
  'orchestrator_agent_start',
  'orchestrator_agent_end',
  'delegation_start',
  'delegation_end',
  'background_agent_started',
  'background_agent_completed',
  'background_agent_terminated',

  // Learn code mode — script execution results
  'learn_code_script_execution',

  // Task & step progress
  'todo_task_route_selected',
  'todo_task_item_created',
  'todo_task_item_updated',
  'todo_task_item_completed',
  'todo_task_step_completed',
  'todo_task_status_update',
  'step_progress_updated',
  'batch_group_start',
  'batch_group_end',
  'pre_validation_completed',

  // Workflow lifecycle
  'workflow_start',
  'workflow_end',
  'workflow_error',

  // User interaction — must always be visible so user can respond
  'user_message',
  'request_human_feedback',
  'blocking_human_feedback',
  'plan_approval',

  // Completion & errors
  'unified_completion',
  'conversation_end',
  'conversation_error',
  'context_cancelled',
  'conversation_resumed',
]);

// Helper function to check if an event should be shown
export const shouldShowEventByMode = (eventType: string, _mode?: unknown): boolean => {
  if (!eventType) return false
  if (NEVER_DISPLAY_EVENTS.has(eventType)) return false

  return !HIDDEN_EVENTS.has(eventType)
}
