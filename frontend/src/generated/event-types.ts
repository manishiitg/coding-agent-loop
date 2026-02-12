/**
 * Event Type System - Discriminated Union Types
 * 
 * This file provides type-safe discriminated union types for the event system.
 * It builds on top of the generated events-bridge.ts and provides proper
 * type narrowing based on event.data.type.
 * 
 * USAGE:
 * ```typescript
 * import { getEventData, isEventType } from './event-types';
 * 
 * // Type-safe access with type guard
 * if (isEventType(event, 'tool_call_start')) {
 *   const data = getEventData(event);
 *   console.log(data.tool_name);  // Fully typed!
 * }
 * ```
 */

// Re-export all event types from generated file
export * from './events-bridge';

import type {
  // Event wrapper types
  PollingEventSchema,
  AgentEventForSchema,
  
  // Individual event types
  AgentStartEvent,
  AgentEndEvent,
  AgentErrorEvent,
  ConversationStartEvent,
  ConversationEndEvent,
  ConversationErrorEvent,
  ConversationTurnEvent,
  LLMGenerationStartEvent,
  LLMGenerationEndEvent,
  LLMGenerationErrorEvent,
  LLMGenerationWithRetryEvent,
  ToolCallStartEvent,
  ToolCallEndEvent,
  ToolCallErrorEvent,
  ToolExecutionEvent,
  ToolOutputEvent,
  ToolResponseEvent,
  WorkspaceFileOperationEvent,
  MCPServerConnectionEvent,
  MCPServerDiscoveryEvent,
  MCPServerSelectionEvent,
  SystemPromptEvent,
  UserMessageEvent,
  TokenUsageEvent,
  ErrorDetailEvent,
  MaxTurnsReachedEvent,
  ContextCancelledEvent,
  ContextSummarizationStartedEvent,
  ContextSummarizationCompletedEvent,
  ContextSummarizationErrorEvent,
  ContextEditingCompletedEvent,
  ContextEditingErrorEvent,
  LargeToolOutputDetectedEvent,
  LargeToolOutputFileWrittenEvent,
  LargeToolOutputFileWriteErrorEvent,
  LargeToolOutputServerUnavailableEvent,
  ModelChangeEvent,
  FallbackModelUsedEvent,
  FallbackAttemptEvent,
  ThrottlingDetectedEvent,
  TokenLimitExceededEvent,
  CacheEvent,
  ComprehensiveCacheEvent,
  SmartRoutingStartEvent,
  SmartRoutingEndEvent,
  UnifiedCompletionEvent,
  OrchestratorStartEvent,
  OrchestratorEndEvent,
  OrchestratorErrorEvent,
  OrchestratorAgentStartEvent,
  OrchestratorAgentEndEvent,
  OrchestratorAgentErrorEvent,
  StepTokenUsageEvent,
  StepProgressUpdatedEvent,
  DecisionEvaluatedEvent,
  PrerequisiteNavigationEvent,
  TodoStepsExtractedEvent,
  VariablesExtractedEvent,
  IndependentStepsSelectedEvent,
  RequestHumanFeedbackEvent,
  BlockingHumanFeedbackEvent,
  BlockingHumanQuestionsEvent,
  BlockingHumanQuestionsQuestion,
  HumanVerificationResponseEvent,
  StructuredOutputStartEvent,
  StructuredOutputEndEvent,
  StructuredOutputErrorEvent,
  // New Streaming Events
  StreamingStartEvent,
  StreamingChunkEvent,
  StreamingEndEvent,
  StreamingErrorEvent,
  StreamingProgressEvent,
  StreamingConnectionLostEvent,
  // New Cache Detail Events
  CacheHitEvent,
  CacheMissEvent,
  CacheWriteEvent,
  CacheExpiredEvent,
  CacheCleanupEvent,
  CacheErrorEvent,
  CacheOperationStartEvent,
  // New MCP Server Connection Events
  MCPServerConnectionStartEvent,
  MCPServerConnectionEndEvent,
  MCPServerConnectionErrorEvent,
  // New JSON Validation Events
  JSONValidationStartEvent,
  JSONValidationEndEvent,
  // New Other Events
  ConversationThinkingEvent,
  LLMMessagesEvent,
  ToolCallProgressEvent,
  DebugEvent,
  PerformanceEvent,
  LLMTokenUsageEvent,
  AgentProcessingEvent,
  // Batch Execution Events
  BatchExecutionStartEvent,
  BatchGroupStartEvent,
  BatchGroupEndEvent,
  BatchExecutionEndEvent,
} from './events-bridge';

export interface BatchExecutionCanceledEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  total_groups?: number;
  completed_groups?: number;
  canceled_group_id?: string;
  remaining_group_ids?: string[];
  reason?: string;
}

// Import event types that exist in events.ts but not in events-bridge.ts
import type {
  PreValidationCompletedEvent,
} from './events';

// =============================================================================
// EVENT TYPE CONSTANTS
// =============================================================================

/**
 * All valid event type string literals
 */
export type EventTypeString =
  | 'agent_start'
  | 'agent_end'
  | 'agent_error'
  | 'conversation_start'
  | 'conversation_end'
  | 'conversation_error'
  | 'conversation_turn'
  | 'llm_generation_start'
  | 'llm_generation_end'
  | 'llm_generation_error'
  | 'llm_generation_with_retry'
  | 'tool_call_start'
  | 'tool_call_end'
  | 'tool_call_error'
  | 'tool_execution'
  | 'tool_output'
  | 'tool_response'
  | 'workspace_file_operation'
  | 'mcp_server_connection'
  | 'mcp_server_connection_error'
  | 'mcp_server_discovery'
  | 'mcp_server_selection'
  | 'system_prompt'
  | 'user_message'
  | 'token_usage'
  | 'error_detail'
  | 'max_turns_reached'
  | 'context_cancelled'
  | 'context_summarization_started'
  | 'context_summarization_completed'
  | 'context_summarization_error'
  | 'context_editing_completed'
  | 'context_editing_error'
  | 'large_tool_output_detected'
  | 'large_tool_output_file_written'
  | 'large_tool_output_file_write_error'
  | 'large_tool_output_server_unavailable'
  | 'model_change'
  | 'fallback_model_used'
  | 'fallback_attempt'
  | 'throttling_detected'
  | 'token_limit_exceeded'
  | 'cache_event'
  | 'comprehensive_cache_event'
  | 'smart_routing_start'
  | 'smart_routing_end'
  | 'unified_completion'
  | 'orchestrator_start'
  | 'orchestrator_end'
  | 'orchestrator_error'
  | 'orchestrator_agent_start'
  | 'orchestrator_agent_end'
  | 'orchestrator_agent_error'
  | 'step_token_usage'
  | 'step_progress_updated'
  | 'decision_evaluated'
  | 'prerequisite_navigation'
  | 'pre_validation_completed'
  | 'todo_steps_extracted'
  | 'variables_extracted'
  | 'independent_steps_selected'
  | 'request_human_feedback'
  | 'blocking_human_feedback'
  | 'human_verification_response'
  | 'structured_output_start'
  | 'structured_output_end'
  | 'structured_output_error'
  // Streaming Events
  | 'streaming_start'
  | 'streaming_chunk'
  | 'streaming_end'
  | 'streaming_error'
  | 'streaming_progress'
  | 'streaming_connection_lost'
  // Cache Detail Events
  | 'cache_hit'
  | 'cache_miss'
  | 'cache_write'
  | 'cache_expired'
  | 'cache_cleanup'
  | 'cache_error'
  | 'cache_operation_start'
  // MCP Server Connection Detail Events
  | 'mcp_server_connection_start'
  | 'mcp_server_connection_end'
  // Note: mcp_server_connection_error already exists above
  // JSON Validation Events
  | 'json_validation_start'
  | 'json_validation_end'
  // Other Events
  | 'conversation_thinking'
  | 'llm_messages'
  | 'tool_call_progress'
  | 'debug'
  | 'performance'
  | 'llm_token_usage'
  | 'agent_processing'
  // Workflow Events
  | 'workflow_start'
  | 'workflow_progress'
  | 'workflow_end'
  // Batch Execution Events
  | 'batch_execution_start'
  | 'batch_group_start'
  | 'batch_group_end'
  | 'batch_execution_end'
  | 'batch_execution_canceled'
  // Todo Task Events
  | 'todo_task_route_selected'
  | 'todo_task_item_created'
  | 'todo_task_item_updated'
  | 'todo_task_item_completed'
  | 'todo_task_step_completed'
  // Delegation Events
  | 'delegation_start'
  | 'delegation_end';

// =============================================================================
// EVENT TYPE TO DATA TYPE MAPPING
// =============================================================================

/**
 * Maps event type strings to their corresponding typed event data
 */
export interface EventTypeToDataMap {
  'agent_start': AgentStartEvent;
  'agent_end': AgentEndEvent;
  'agent_error': AgentErrorEvent;
  'conversation_start': ConversationStartEvent;
  'conversation_end': ConversationEndEvent;
  'conversation_error': ConversationErrorEvent;
  'conversation_turn': ConversationTurnEvent;
  'llm_generation_start': LLMGenerationStartEvent;
  'llm_generation_end': LLMGenerationEndEvent;
  'llm_generation_error': LLMGenerationErrorEvent;
  'llm_generation_with_retry': LLMGenerationWithRetryEvent;
  'tool_call_start': ToolCallStartEvent;
  'tool_call_end': ToolCallEndEvent;
  'tool_call_error': ToolCallErrorEvent;
  'tool_execution': ToolExecutionEvent;
  'tool_output': ToolOutputEvent;
  'tool_response': ToolResponseEvent;
  'workspace_file_operation': WorkspaceFileOperationEvent;
  'mcp_server_connection': MCPServerConnectionEvent;
  'mcp_server_connection_error': MCPServerConnectionErrorEvent;
  'mcp_server_discovery': MCPServerDiscoveryEvent;
  'mcp_server_selection': MCPServerSelectionEvent;
  'system_prompt': SystemPromptEvent;
  'user_message': UserMessageEvent;
  'token_usage': TokenUsageEvent;
  'error_detail': ErrorDetailEvent;
  'max_turns_reached': MaxTurnsReachedEvent;
  'context_cancelled': ContextCancelledEvent;
  'context_summarization_started': ContextSummarizationStartedEvent;
  'context_summarization_completed': ContextSummarizationCompletedEvent;
  'context_summarization_error': ContextSummarizationErrorEvent;
  'context_editing_completed': ContextEditingCompletedEvent;
  'context_editing_error': ContextEditingErrorEvent;
  'large_tool_output_detected': LargeToolOutputDetectedEvent;
  'large_tool_output_file_written': LargeToolOutputFileWrittenEvent;
  'large_tool_output_file_write_error': LargeToolOutputFileWriteErrorEvent;
  'large_tool_output_server_unavailable': LargeToolOutputServerUnavailableEvent;
  'model_change': ModelChangeEvent;
  'fallback_model_used': FallbackModelUsedEvent;
  'fallback_attempt': FallbackAttemptEvent;
  'throttling_detected': ThrottlingDetectedEvent;
  'token_limit_exceeded': TokenLimitExceededEvent;
  'cache_event': CacheEvent;
  'comprehensive_cache_event': ComprehensiveCacheEvent;
  'smart_routing_start': SmartRoutingStartEvent;
  'smart_routing_end': SmartRoutingEndEvent;
  'unified_completion': UnifiedCompletionEvent;
  'orchestrator_start': OrchestratorStartEvent;
  'orchestrator_end': OrchestratorEndEvent;
  'orchestrator_error': OrchestratorErrorEvent;
  'orchestrator_agent_start': OrchestratorAgentStartEvent;
  'orchestrator_agent_end': OrchestratorAgentEndEvent;
  'orchestrator_agent_error': OrchestratorAgentErrorEvent;
  'step_token_usage': StepTokenUsageEvent;
  'step_progress_updated': StepProgressUpdatedEvent;
  'decision_evaluated': DecisionEvaluatedEvent;
  'prerequisite_navigation': PrerequisiteNavigationEvent;
  'pre_validation_completed': PreValidationCompletedEvent;
  'todo_steps_extracted': TodoStepsExtractedEvent;
  'variables_extracted': VariablesExtractedEvent;
  'independent_steps_selected': IndependentStepsSelectedEvent;
  'request_human_feedback': RequestHumanFeedbackEvent;
  'blocking_human_feedback': BlockingHumanFeedbackEvent;
  'human_verification_response': HumanVerificationResponseEvent;
  'structured_output_start': StructuredOutputStartEvent;
  'structured_output_end': StructuredOutputEndEvent;
  'structured_output_error': StructuredOutputErrorEvent;
  // Streaming Events
  'streaming_start': StreamingStartEvent;
  'streaming_chunk': StreamingChunkEvent;
  'streaming_end': StreamingEndEvent;
  'streaming_error': StreamingErrorEvent;
  'streaming_progress': StreamingProgressEvent;
  'streaming_connection_lost': StreamingConnectionLostEvent;
  // Cache Detail Events
  'cache_hit': CacheHitEvent;
  'cache_miss': CacheMissEvent;
  'cache_write': CacheWriteEvent;
  'cache_expired': CacheExpiredEvent;
  'cache_cleanup': CacheCleanupEvent;
  'cache_error': CacheErrorEvent;
  'cache_operation_start': CacheOperationStartEvent;
  // MCP Server Connection Detail Events
  'mcp_server_connection_start': MCPServerConnectionStartEvent;
  'mcp_server_connection_end': MCPServerConnectionEndEvent;
  // JSON Validation Events
  'json_validation_start': JSONValidationStartEvent;
  'json_validation_end': JSONValidationEndEvent;
  // Other Events
  'conversation_thinking': ConversationThinkingEvent;
  'llm_messages': LLMMessagesEvent;
  'tool_call_progress': ToolCallProgressEvent;
  'debug': DebugEvent;
  'performance': PerformanceEvent;
  'llm_token_usage': LLMTokenUsageEvent;
  'agent_processing': AgentProcessingEvent;
  // Workflow Events
  'workflow_start': WorkflowStartEventData;
  'workflow_progress': WorkflowProgressEventData;
  'workflow_end': WorkflowEndEventData;
  // Batch Execution Events
  'batch_execution_start': BatchExecutionStartEvent;
  'batch_group_start': BatchGroupStartEvent;
  'batch_group_end': BatchGroupEndEvent;
  'batch_execution_end': BatchExecutionEndEvent;
  'batch_execution_canceled': BatchExecutionCanceledEvent;
  // Todo Task Events
  'todo_task_route_selected': TodoTaskRouteSelectedEvent;
  'todo_task_item_created': TodoTaskItemCreatedEvent;
  'todo_task_item_updated': TodoTaskItemUpdatedEvent;
  'todo_task_item_completed': TodoTaskItemCompletedEvent;
  'todo_task_step_completed': TodoTaskStepCompletedEvent;
  // Delegation Events
  'delegation_start': DelegationStartEvent;
  'delegation_end': DelegationEndEvent;
}

// Todo Task event data types (not in generated schema)
export interface TodoTaskRouteSelectedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  step_index?: number;
  step_path?: string;
  step_id?: string;
  step_title?: string;
  iteration?: number;
  next_action?: string; // "delegate", "complete", "continue"
  selected_route_id?: string;
  selected_route_name?: string;
  use_generic_agent?: boolean;
  todo_id_to_execute?: string;
  todo_title?: string;
  instructions_to_sub_agent?: string;
  selection_reasoning?: string;
  all_tasks_complete?: boolean;
  completion_reason?: string;
  progress_summary?: string;
  model?: string;
}

export interface TodoTaskItemCreatedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  step_index?: number;
  step_path?: string;
  step_id?: string;
  todo_id?: string;
  title?: string;
  description?: string;
  priority?: string;
  created_by?: string;
}

export interface TodoTaskItemUpdatedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  step_index?: number;
  step_path?: string;
  step_id?: string;
  todo_id?: string;
  title?: string;
  old_status?: string;
  new_status?: string;
  updated_by?: string;
  notes?: string;
}

export interface TodoTaskItemCompletedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  step_index?: number;
  step_path?: string;
  step_id?: string;
  todo_id?: string;
  title?: string;
  result?: string;
  completed_by?: string;
}

export interface TodoTaskStepCompletedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  step_index?: number;
  step_path?: string;
  step_id?: string;
  step_title?: string;
  total_iterations?: number;
  total_todos_count?: number;
  completed_count?: number;
  completion_reason?: string;
  next_step_id?: string;
}

// Delegation event data types (not in generated schema)
export interface DelegationStartEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  delegation_id?: string;
  depth?: number;
  instruction?: string;
}

export interface DelegationEndEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  is_end_event?: boolean;
  correlation_id?: string;
  hierarchy_level?: number;
  session_id?: string;
  component?: string;
  metadata?: {
    [k: string]: unknown;
  };
  delegation_id?: string;
  depth?: number;
  result?: string;
  error?: string;
  duration?: string;
}

// Workflow event data types (not in generated schema)
export interface WorkflowStartEventData {
  workflow_id?: string;
  objective?: string;
  message?: string;
  timestamp?: number;
}

export interface WorkflowProgressEventData {
  phase?: string;
  message?: string;
  timestamp?: number;
}

export interface WorkflowEndEventData {
  workflow_id?: string;
  result?: string;
  status?: string;
  message?: string;
  timestamp?: number;
}

// =============================================================================
// TYPED EVENT INTERFACE
// =============================================================================

/**
 * A typed polling event where we know the event type
 */
export interface TypedEvent<T extends EventTypeString> {
  id: string;
  type: T;
  timestamp?: string;
  session_id?: string;
  error?: string;
  data: {
    type: T;
    timestamp?: string;
    event_index?: number;
    trace_id?: string;
    span_id?: string;
    parent_id?: string;
    correlation_id?: string;
    hierarchy_level?: number;
    session_id?: string;
    component?: string;
    data: EventTypeToDataMap[T];
  };
}

// =============================================================================
// TYPE GUARDS - The core of type-safe event handling
// =============================================================================

/**
 * Type guard to check if an event is of a specific type.
 * After this check, you can use getEventData() to get typed data.
 * 
 * @example
 * if (isEventType(event, 'tool_call_start')) {
 *   const data = getEventData(event);
 *   console.log(data.tool_name);  // TypeScript knows this exists!
 * }
 */
export function isEventType<T extends EventTypeString>(
  event: PollingEventSchema | undefined | null,
  eventType: T
): event is PollingEventSchema & { type: T; data: AgentEventForSchema & { type: T; data: EventTypeToDataMap[T] } } {
  const agentEvent = event?.data as AgentEventForSchema | undefined;
  return event?.type === eventType && agentEvent?.type === eventType;
}

/**
 * Get the typed event data from a typed event.
 * Use this AFTER checking with isEventType()
 * 
 * @example
 * if (isEventType(event, 'agent_start')) {
 *   const data = getEventData(event);  // Returns AgentStartEvent
 *   console.log(data.agent_type);
 * }
 */
export function getEventData<T extends EventTypeString>(
  event: PollingEventSchema & { type: T; data: { type: T; data: EventTypeToDataMap[T] } }
): EventTypeToDataMap[T] {
  return event.data.data as EventTypeToDataMap[T];
}

/**
 * Combined type guard and data extraction.
 * Returns undefined if event doesn't match, otherwise returns typed data.
 * 
 * @example
 * const data = getTypedEventData(event, 'tool_call_start');
 * if (data) {
 *   console.log(data.tool_name);  // TypeScript knows the type!
 * }
 */
export function getTypedEventData<T extends EventTypeString>(
  event: PollingEventSchema | undefined | null,
  eventType: T
): EventTypeToDataMap[T] | undefined {
  if (isEventType(event, eventType)) {
    return getEventData(event);
  }
  return undefined;
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

/**
 * Get the raw inner data from an event (event.data.data)
 * Returns unknown type - use type guards for type safety
 */
export function getRawEventData(
  event: PollingEventSchema | undefined | null
): unknown {
  if (!event?.data) return undefined;
  const agentEvent = event.data as AgentEventForSchema;
  return agentEvent.data;
}

/**
 * Check if event has inner data
 */
export function hasEventData(
  event: PollingEventSchema | undefined | null
): boolean {
  if (!event?.data) return false;
  const agentEvent = event.data as AgentEventForSchema;
  return agentEvent.data !== undefined;
}

/**
 * Get the event type from a polling event
 */
export function getEventType(
  event: PollingEventSchema | undefined | null
): EventTypeString | undefined {
  return event?.type as EventTypeString | undefined;
}

/**
 * Assert that an event is of a specific type.
 * Throws if the assertion fails. Use when you're certain of the type.
 * 
 * @example
 * // In a switch case where you know the type
 * case 'tool_call_start':
 *   return <ToolCallDisplay data={assertEventType(event, 'tool_call_start')} />
 */
export function assertEventType<T extends EventTypeString>(
  event: PollingEventSchema | undefined | null,
  eventType: T
): EventTypeToDataMap[T] {
  if (!isEventType(event, eventType)) {
    throw new Error(`Expected event type '${eventType}' but got '${event?.type}'`);
  }
  return getEventData(event);
}

/**
 * Safe assertion - returns the typed data or a fallback value
 * Use this in render functions where you can't throw
 */
export function getEventDataOrDefault<T extends EventTypeString>(
  event: PollingEventSchema | undefined | null,
  eventType: T,
  defaultValue: EventTypeToDataMap[T]
): EventTypeToDataMap[T] {
  if (isEventType(event, eventType)) {
    return getEventData(event);
  }
  return defaultValue;
}

// =============================================================================
// RE-EXPORTS FOR CONVENIENCE
// =============================================================================

// Export the individual event types for direct use
export type {
  AgentStartEvent,
  AgentEndEvent,
  AgentErrorEvent,
  ConversationStartEvent,
  ConversationEndEvent,
  ConversationErrorEvent,
  ConversationTurnEvent,
  LLMGenerationStartEvent,
  LLMGenerationEndEvent,
  LLMGenerationErrorEvent,
  LLMGenerationWithRetryEvent,
  ToolCallStartEvent,
  ToolCallEndEvent,
  ToolCallErrorEvent,
  ToolExecutionEvent,
  ToolOutputEvent,
  ToolResponseEvent,
  WorkspaceFileOperationEvent,
  MCPServerConnectionEvent,
  MCPServerDiscoveryEvent,
  MCPServerSelectionEvent,
  SystemPromptEvent,
  UserMessageEvent,
  TokenUsageEvent,
  ErrorDetailEvent,
  MaxTurnsReachedEvent,
  ContextCancelledEvent,
  ContextSummarizationStartedEvent,
  ContextSummarizationCompletedEvent,
  ContextSummarizationErrorEvent,
  ContextEditingCompletedEvent,
  ContextEditingErrorEvent,
  LargeToolOutputDetectedEvent,
  LargeToolOutputFileWrittenEvent,
  LargeToolOutputFileWriteErrorEvent,
  LargeToolOutputServerUnavailableEvent,
  ModelChangeEvent,
  FallbackModelUsedEvent,
  FallbackAttemptEvent,
  ThrottlingDetectedEvent,
  TokenLimitExceededEvent,
  CacheEvent,
  ComprehensiveCacheEvent,
  SmartRoutingStartEvent,
  SmartRoutingEndEvent,
  UnifiedCompletionEvent,
  OrchestratorStartEvent,
  OrchestratorEndEvent,
  OrchestratorErrorEvent,
  OrchestratorAgentStartEvent,
  OrchestratorAgentEndEvent,
  OrchestratorAgentErrorEvent,
  StepTokenUsageEvent,
  StepProgressUpdatedEvent,
  DecisionEvaluatedEvent,
  PrerequisiteNavigationEvent,
  TodoStepsExtractedEvent,
  VariablesExtractedEvent,
  IndependentStepsSelectedEvent,
  RequestHumanFeedbackEvent,
  BlockingHumanFeedbackEvent,
  BlockingHumanQuestionsEvent,
  BlockingHumanQuestionsQuestion,
  HumanVerificationResponseEvent,
  StructuredOutputStartEvent,
  StructuredOutputEndEvent,
  StructuredOutputErrorEvent,
  // Batch Execution Events
  BatchExecutionStartEvent,
  BatchGroupStartEvent,
  BatchGroupEndEvent,
  BatchExecutionEndEvent,
} from './events-bridge';

// Export nested types from events.ts (used by event types but not in events-bridge.ts)
export type {
  FileCheckResultForEvent,
  JSONCheckResultForEvent,
  ValidationErrorForEvent,
  TodoStep,
} from './events';
