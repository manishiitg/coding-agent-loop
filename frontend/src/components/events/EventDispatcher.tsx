import React from 'react'
import type { PollingEvent } from '../../services/api-types'
import { useEventMode } from './useEventMode'
import { EventHierarchy } from './EventHierarchy'
import { EventWithOrchestratorContext } from './common/EventWithOrchestratorContext'

// Utility function to extract event data, handling nested structure
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  function extractEventData<T>(eventData: Record<string, any>): T {
    // With the unified event system, events now have a simple structure:
    // { id, type, timestamp, data: AgentEvent, error?, session_id? }
    // The AgentEvent contains all the actual event data
    
    if (eventData && typeof eventData === 'object' && eventData.data) {
      return eventData.data as T
    }

    // Fallback: return the event data as-is (for backward compatibility)
    return eventData as T
  }

// Helper function to wrap any event component with Deep Search context
function wrapWithOrchestratorContext<T extends { metadata?: { [k: string]: unknown } }>(
  Component: React.ComponentType<{ event: T; compact?: boolean }>,
  eventData: T,
  compact?: boolean
) {
  // Get metadata from the extracted event data
  const metadata = eventData.metadata;
  
  return (
    <EventWithOrchestratorContext metadata={metadata}>
      <Component event={eventData} compact={compact} />
    </EventWithOrchestratorContext>
  )
}
import type {
  AgentErrorEvent,
  LLMGenerationWithRetryEvent,
  MCPServerSelectionEvent,
  MCPServerDiscoveryEvent,
  MCPServerConnectionEvent,
  ConversationStartEvent,
  ConversationEndEvent,
  ConversationErrorEvent,
  ConversationTurnEvent,

  LLMGenerationStartEvent,
  LLMGenerationEndEvent,
  LLMGenerationErrorEvent,

  ToolCallStartEvent,
  ToolCallEndEvent,
  ToolCallErrorEvent,
  
  SystemPromptEvent,

  LargeToolOutputDetectedEvent,
  LargeToolOutputFileWrittenEvent,
  FallbackAttemptEvent,
  ModelChangeEvent,

  ThrottlingDetectedEvent,
  FallbackModelUsedEvent,
  TokenLimitExceededEvent,
  TokenUsageEvent,
  MaxTurnsReachedEvent,
  ContextCancelledEvent,
  OrchestratorStartEvent,
  OrchestratorEndEvent,
  OrchestratorErrorEvent,
  OrchestratorAgentStartEvent,
  OrchestratorAgentEndEvent,
  OrchestratorAgentErrorEvent,

  CacheEvent,
  ComprehensiveCacheEvent,
  SmartRoutingStartEvent,
  SmartRoutingEndEvent,
  AgentStartEvent,
  AgentEndEvent
} from '../../generated/events'

// Import from the new organized component structure
import {
  AgentErrorEventDisplay,
  LLMGenerationWithRetryEventDisplay,
  AgentStartEventComponent,
  AgentEndEventComponent
} from './agents'

import {
  MCPServerSelectionEventDisplay,
  MCPServerDiscoveryEventDisplay,
  MCPServerConnectionEventDisplay
} from './mcp'

import {
  ConversationStartEventDisplay,
  ConversationEndEventDisplay,
  ConversationErrorEventDisplay,
  ConversationTurnEventDisplay,

} from './conversation'

import {
  LLMGenerationStartEventDisplay,
  LLMGenerationEndEventDisplay,
  LLMGenerationErrorEventDisplay,

} from './llm'

import {
  ToolCallStartEventDisplay,
  ToolCallEndEventDisplay,
  ToolCallErrorEventDisplay
} from './tools'

import {
  SystemPromptEventDisplay,
  UserMessageEventDisplay
} from './system'



import {
  OrchestratorStartEventDisplay,
  OrchestratorEndEventDisplay,
  OrchestratorErrorEventDisplay,
  OrchestratorAgentStartEventDisplay,
  OrchestratorAgentEndEventDisplay,
  OrchestratorAgentErrorEventDisplay,
  IndependentStepsSelectedEventDisplay,
  TodoStepsExtractedEventDisplay,
  StepExecutionEventDisplay,
  StepProgressUpdatedEventDisplay
} from './orchestrator'
import { StepTokenUsageEventDisplay } from './orchestrator/StepTokenUsageEvent'
import { VariablesExtractedEventDisplay } from './orchestrator/VariablesExtractedEvent'

import {
  WorkflowStartEvent,
  WorkflowProgressEvent,
  WorkflowEndEvent
} from './workflow'

import {
  TokenUsageEventDisplay,
  ThrottlingDetectedEventDisplay,
  FallbackModelUsedEventDisplay,
  FallbackAttemptEventDisplay,
  TokenLimitExceededEventDisplay,
  LargeToolOutputDetectedEventDisplay,
  LargeToolOutputFileWrittenEventDisplay,
  ModelChangeEventDisplay,
  MaxTurnsReachedEventDisplay,
  ContextCancelledEventDisplay,
  // Smart Routing event components
  SmartRoutingStartEventDisplay,
  SmartRoutingEndEventDisplay,
  // Cache event components
  CacheEventDisplay,
  ComprehensiveCacheEventDisplay,
  // Structured output event components
  StructuredOutputStartEventDisplay,
  StructuredOutputEndEventDisplay
} from './debug'
import { UnifiedCompletionEventDisplay } from './debug/UnifiedCompletionEvent'
import { HumanVerificationDisplay } from './HumanVerificationDisplay'
import { BlockingHumanFeedbackDisplay, type BlockingHumanFeedbackEvent } from './BlockingHumanFeedbackDisplay'
import type { RequestHumanFeedbackEvent } from '../../generated/events'
// Import TodoStepsExtractedEvent type from the component that uses it
type TodoStepsExtractedEvent = {
  timestamp?: string;
  total_steps_extracted?: number;
  extracted_steps?: unknown[];
  extraction_method?: string;
  plan_source?: string;
  workspace_path?: string;
  [key: string]: unknown;
}


interface EventDispatcherProps {
  event: PollingEvent
  mode?: 'compact' | 'detailed'
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean  // Loading state for approve button
  isCollapsed?: boolean  // Whether the session is collapsed
  eventCount?: number  // Number of events in the session (excluding start/end)
  onToggleCollapse?: () => void  // Callback to toggle collapse state
  compact?: boolean  // Compact mode for smaller font sizes (used in workflow layout)
}

export const EventDispatcher: React.FC<EventDispatcherProps> = React.memo(({ event, mode, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, isCollapsed, eventCount, onToggleCollapse, compact = false }) => {
  
  // Wrapper component to apply compact styling to events that don't support compact prop
  const CompactWrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    if (!compact) return <>{children}</>
    return <div className="text-xs [&>*]:text-xs [&_h1]:!text-sm [&_h2]:!text-xs [&_h3]:!text-[11px] [&_p]:!text-xs [&_code]:!text-[10px] [&_span]:!text-xs [&_div]:!text-xs">{children}</div>
  }
  
  if (!event.type || !event.data) {
    return (
      <div className={`bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
        <div className={`${compact ? 'text-xs' : 'text-sm'} text-yellow-700 dark:text-yellow-300`}>
          Invalid event: missing type or data
        </div>
      </div>
    )
  }

  switch (event.type) {
    // Agent Events
    case 'agent_error':
      return <CompactWrapper><AgentErrorEventDisplay event={extractEventData<AgentErrorEvent>(event.data)} /></CompactWrapper>
    case 'llm_generation_with_retry':
      return <CompactWrapper><LLMGenerationWithRetryEventDisplay event={extractEventData<LLMGenerationWithRetryEvent>(event.data)} /></CompactWrapper>

    // MCP Server Events
          case 'mcp_server_selection':
        return <CompactWrapper>{wrapWithOrchestratorContext(MCPServerSelectionEventDisplay, extractEventData<MCPServerSelectionEvent>(event.data), compact)}</CompactWrapper>
      case 'mcp_server_discovery':
        return <CompactWrapper>{wrapWithOrchestratorContext(MCPServerDiscoveryEventDisplay, extractEventData<MCPServerDiscoveryEvent>(event.data), compact)}</CompactWrapper>
      case 'mcp_server_connection':
        return <CompactWrapper>{wrapWithOrchestratorContext(MCPServerConnectionEventDisplay, extractEventData<MCPServerConnectionEvent>(event.data), compact)}</CompactWrapper>
      case 'mcp_server_connection_error':
        return <CompactWrapper>{wrapWithOrchestratorContext(MCPServerConnectionEventDisplay, extractEventData<MCPServerConnectionEvent>(event.data), compact)}</CompactWrapper>

    // Conversation Events
          case 'conversation_start':
        return <CompactWrapper>{wrapWithOrchestratorContext(ConversationStartEventDisplay, extractEventData<ConversationStartEvent>(event.data), compact)}</CompactWrapper>
      case 'conversation_end':
        return <CompactWrapper>{wrapWithOrchestratorContext(ConversationEndEventDisplay, extractEventData<ConversationEndEvent>(event.data), compact)}</CompactWrapper>
      case 'conversation_error':
        return <CompactWrapper>{wrapWithOrchestratorContext(ConversationErrorEventDisplay, extractEventData<ConversationErrorEvent>(event.data), compact)}</CompactWrapper>
      case 'conversation_turn':
        return <CompactWrapper>{wrapWithOrchestratorContext(
          (props) => <ConversationTurnEventDisplay {...props} compact={compact} />, 
          extractEventData<ConversationTurnEvent>(event.data),
          compact
        )}</CompactWrapper>


    // Agent Events
    case 'agent_start':
      return <CompactWrapper>{wrapWithOrchestratorContext(AgentStartEventComponent, extractEventData<AgentStartEvent>(event.data), compact)}</CompactWrapper>
    case 'agent_end':
      return <CompactWrapper>{wrapWithOrchestratorContext(AgentEndEventComponent, extractEventData<AgentEndEvent>(event.data), compact)}</CompactWrapper>

    // LLM Events
          case 'llm_generation_start':
        return <CompactWrapper>{wrapWithOrchestratorContext(
          (props) => <LLMGenerationStartEventDisplay {...props} mode={compact ? 'compact' : mode} />, 
          extractEventData<LLMGenerationStartEvent>(event.data),
          compact
        )}</CompactWrapper>
      case 'llm_generation_end':
        return <CompactWrapper>{wrapWithOrchestratorContext(LLMGenerationEndEventDisplay, extractEventData<LLMGenerationEndEvent>(event.data), compact)}</CompactWrapper>
      case 'llm_generation_error':
        return <CompactWrapper>{wrapWithOrchestratorContext(
          (props) => <LLMGenerationErrorEventDisplay {...props} mode={compact ? 'compact' : mode} />, 
          extractEventData<LLMGenerationErrorEvent>(event.data),
          compact
        )}</CompactWrapper>


    // Tool Events
    case 'tool_call_start':
      return <CompactWrapper>{wrapWithOrchestratorContext(ToolCallStartEventDisplay, extractEventData<ToolCallStartEvent>(event.data), compact)}</CompactWrapper>
    case 'tool_call_end':
      return <CompactWrapper>{wrapWithOrchestratorContext(ToolCallEndEventDisplay, extractEventData<ToolCallEndEvent>(event.data), compact)}</CompactWrapper>
    case 'tool_call_error':
      return <CompactWrapper>{wrapWithOrchestratorContext(ToolCallErrorEventDisplay, extractEventData<ToolCallErrorEvent>(event.data), compact)}</CompactWrapper>

    // System Events
    case 'system_prompt':
      return <CompactWrapper>{wrapWithOrchestratorContext(SystemPromptEventDisplay, extractEventData<SystemPromptEvent>(event.data), compact)}</CompactWrapper>
    case 'user_message': {
      const userMessageData = event.data?.user_message
      if (!userMessageData) {
        console.error('USERMSG_DEBUG - EventDispatcher - no user_message data found')
        return null
      }
      return <CompactWrapper>{wrapWithOrchestratorContext(UserMessageEventDisplay, userMessageData, compact)}</CompactWrapper>
    }

    // Step Events (Deep Search step execution)
    // Deep Search Events (individual agent events for debugging)
    case 'orchestrator_start':
      return <CompactWrapper><OrchestratorStartEventDisplay event={extractEventData<OrchestratorStartEvent>(event.data)} /></CompactWrapper>
    case 'orchestrator_end':
      return <CompactWrapper><OrchestratorEndEventDisplay event={extractEventData<OrchestratorEndEvent>(event.data)} /></CompactWrapper>
    case 'orchestrator_error':
      return <CompactWrapper><OrchestratorErrorEventDisplay event={extractEventData<OrchestratorErrorEvent>(event.data)} /></CompactWrapper>
    case 'orchestrator_agent_start':
      return <CompactWrapper><OrchestratorAgentStartEventDisplay 
        event={extractEventData<OrchestratorAgentStartEvent>(event.data)} 
        isCollapsed={isCollapsed}
        eventCount={eventCount}
        onToggleCollapse={onToggleCollapse}
      /></CompactWrapper>
    case 'orchestrator_agent_end':
      return <CompactWrapper><OrchestratorAgentEndEventDisplay event={extractEventData<OrchestratorAgentEndEvent>(event.data)} /></CompactWrapper>
    case 'orchestrator_agent_error':
      return <CompactWrapper><OrchestratorAgentErrorEventDisplay event={extractEventData<OrchestratorAgentErrorEvent>(event.data)} /></CompactWrapper>

    // Human Verification Events
    case 'request_human_feedback':
      return <HumanVerificationDisplay 
        event={{
          type: event.type,
          data: {
            ...extractEventData<RequestHumanFeedbackEvent>(event.data),
            objective: extractEventData<RequestHumanFeedbackEvent>(event.data).objective || '',
            todo_list_markdown: extractEventData<RequestHumanFeedbackEvent>(event.data).todo_list_markdown || '',
            request_id: extractEventData<RequestHumanFeedbackEvent>(event.data).request_id || `request_${Date.now()}`,
            // Pass through dynamic fields
            verification_type: extractEventData<RequestHumanFeedbackEvent>(event.data).verification_type,
            next_phase: extractEventData<RequestHumanFeedbackEvent>(event.data).next_phase,
            action_label: extractEventData<RequestHumanFeedbackEvent>(event.data).action_label,
            action_description: extractEventData<RequestHumanFeedbackEvent>(event.data).action_description
          },
          timestamp: event.timestamp || new Date().toISOString()
        }} 
        onApprove={onApproveWorkflow || (() => {})}
        onFeedbackSubmitted={onFeedbackSubmitted}
        isApproving={isApproving}
      />

    case 'blocking_human_feedback':
      return <BlockingHumanFeedbackDisplay 
        event={{
          type: event.type,
          data: {
            ...extractEventData<BlockingHumanFeedbackEvent>(event.data),
            question: extractEventData<BlockingHumanFeedbackEvent>(event.data).question || 'Do you want to continue?',
            allow_feedback: extractEventData<BlockingHumanFeedbackEvent>(event.data).allow_feedback || false,
            context: extractEventData<BlockingHumanFeedbackEvent>(event.data).context || '',
            session_id: extractEventData<BlockingHumanFeedbackEvent>(event.data).session_id || '',
            workflow_id: extractEventData<BlockingHumanFeedbackEvent>(event.data).workflow_id || '',
            request_id: extractEventData<BlockingHumanFeedbackEvent>(event.data).request_id || `request_${Date.now()}`
          },
          timestamp: event.timestamp || new Date().toISOString()
        }} 
        onApprove={onApproveWorkflow || (() => {})}
        onSubmitFeedback={onSubmitFeedback}
        onFeedbackSubmitted={onFeedbackSubmitted}
        isApproving={isApproving}
      />

    // Workflow Events
    case 'workflow_start':
      return <CompactWrapper><WorkflowStartEvent event={extractEventData<{workflow_id?: string, objective?: string, message?: string, timestamp?: number}>(event.data)} /></CompactWrapper>

    case 'workflow_progress':
      return <CompactWrapper><WorkflowProgressEvent event={extractEventData<{phase?: string, message?: string, timestamp?: number}>(event.data)} /></CompactWrapper>

    case 'workflow_end':
      return <CompactWrapper><WorkflowEndEvent event={extractEventData<{workflow_id?: string, result?: string, status?: string, message?: string, timestamp?: number}>(event.data)} /></CompactWrapper>

    // Debug Events
    case 'token_usage':
      return <CompactWrapper><TokenUsageEventDisplay event={extractEventData<TokenUsageEvent>(event.data)} /></CompactWrapper>
    case 'throttling_detected':
      return <CompactWrapper><ThrottlingDetectedEventDisplay event={extractEventData<ThrottlingDetectedEvent>(event.data)} /></CompactWrapper>
    case 'fallback_model_used':
      return <CompactWrapper><FallbackModelUsedEventDisplay event={extractEventData<FallbackModelUsedEvent>(event.data)} /></CompactWrapper>
    case 'fallback_attempt':
      return <CompactWrapper><FallbackAttemptEventDisplay event={extractEventData<FallbackAttemptEvent>(event.data)} /></CompactWrapper>
    case 'token_limit_exceeded':
      return <CompactWrapper><TokenLimitExceededEventDisplay event={extractEventData<TokenLimitExceededEvent>(event.data)} /></CompactWrapper>
    case 'large_tool_output_detected':
      return <CompactWrapper><LargeToolOutputDetectedEventDisplay event={extractEventData<LargeToolOutputDetectedEvent>(event.data)} /></CompactWrapper>
    case 'large_tool_output_file_written':
      return <CompactWrapper><LargeToolOutputFileWrittenEventDisplay event={extractEventData<LargeToolOutputFileWrittenEvent>(event.data)} /></CompactWrapper>
    case 'model_change':
      return <CompactWrapper><ModelChangeEventDisplay event={extractEventData<ModelChangeEvent>(event.data)} /></CompactWrapper>
    case 'max_turns_reached':
      return <CompactWrapper><MaxTurnsReachedEventDisplay event={extractEventData<MaxTurnsReachedEvent>(event.data)} /></CompactWrapper>
    case 'context_cancelled':
      return <CompactWrapper><ContextCancelledEventDisplay event={extractEventData<ContextCancelledEvent>(event.data)} /></CompactWrapper>

    // Cache Events - Only comprehensive cache events
    case 'cache_event':
      return <CompactWrapper><CacheEventDisplay event={extractEventData<CacheEvent>(event.data)} /></CompactWrapper>
    case 'comprehensive_cache_event':
      return <CompactWrapper><ComprehensiveCacheEventDisplay event={extractEventData<ComprehensiveCacheEvent>(event.data)} /></CompactWrapper>

    // Smart Routing Events
    case 'smart_routing_start':
      return <CompactWrapper><SmartRoutingStartEventDisplay event={extractEventData<SmartRoutingStartEvent>(event.data)} /></CompactWrapper>
    case 'smart_routing_end':
      return <CompactWrapper><SmartRoutingEndEventDisplay event={extractEventData<SmartRoutingEndEvent>(event.data)} /></CompactWrapper>

    // Unified Completion Events
    case 'unified_completion':
      return <CompactWrapper><UnifiedCompletionEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} /></CompactWrapper>

    // Structured Output Events
    case 'structured_output_start':
      return <CompactWrapper><StructuredOutputStartEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} /></CompactWrapper>
    case 'structured_output_end':
      return <CompactWrapper><StructuredOutputEndEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} /></CompactWrapper>

    // Independent Steps Events
    case 'independent_steps_selected':
      return <CompactWrapper><IndependentStepsSelectedEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} /></CompactWrapper>

    // Todo Steps Events
    case 'todo_steps_extracted':
      return <CompactWrapper><TodoStepsExtractedEventDisplay event={extractEventData<TodoStepsExtractedEvent>(event.data)} /></CompactWrapper>

    // Variables Events
    case 'variables_extracted':
      return <CompactWrapper><VariablesExtractedEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} /></CompactWrapper>

    // Step Token Usage Events
    case 'step_token_usage':
      return <CompactWrapper><StepTokenUsageEventDisplay event={extractEventData<{
        timestamp?: string
        phase: string
        step: number
        step_title?: string
        prompt_tokens: number
        completion_tokens: number
        total_tokens: number
        cache_tokens: number
        reasoning_tokens: number
        llm_call_count: number
        cache_enabled_call_count: number
        average_cache_discount: number
      }>(event.data)} /></CompactWrapper>

    // Step Execution Events
    case 'step_execution_start':
    case 'step_execution_end':
    case 'step_execution_failed': {
      const stepData = extractEventData<{
        step_id?: string
        step_index?: number
        step_title?: string
        step_path?: string
        is_branch_step?: boolean
        error?: string
      }>(event.data)
      return (
        <CompactWrapper>
          <StepExecutionEventDisplay 
            event={stepData} 
            eventType={event.type as 'step_execution_start' | 'step_execution_end' | 'step_execution_failed'}
            compact={compact}
          />
        </CompactWrapper>
      )
    }

    // Step Progress Updated Event
    case 'step_progress_updated': {
      const progressData = extractEventData<{
        completed_step_indices?: number[]
        total_steps?: number
        last_completed_step?: number
        workspace_path?: string
        run_folder?: string
        metadata?: {
          orchestrator_agent_name?: string
          orchestrator_iteration?: number
          orchestrator_phase?: string
          orchestrator_step?: number
        }
      }>(event.data)
      return (
        <CompactWrapper>
          <StepProgressUpdatedEventDisplay 
            event={progressData} 
            compact={compact}
          />
        </CompactWrapper>
      )
    }

    // Default case for unknown event types
    default:
      return (
        <div className={`bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
          <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-700 dark:text-gray-300`}>
            <div className="font-medium">Unknown Event Type: {event.type}</div>
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400 mt-1`}>
              Event data: {JSON.stringify(event.data, null, 2)}
            </div>
          </div>
        </div>
      )
  }
}, (prevProps, nextProps) => {
  // Custom comparison to prevent unnecessary re-renders
  // Only re-render if event ID, mode, approving state, or collapse state changes
  return prevProps.event.id === nextProps.event.id &&
         prevProps.mode === nextProps.mode &&
         prevProps.isApproving === nextProps.isApproving &&
         prevProps.isCollapsed === nextProps.isCollapsed &&
         prevProps.eventCount === nextProps.eventCount
})

// Event list component for displaying multiple events
export const EventList: React.FC<{ 
  events: PollingEvent[]
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean  // Loading state for approve button
  compact?: boolean  // Compact mode for smaller font sizes
}> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact = false }) => {
  const { shouldShowEvent, mode } = useEventMode()
  
  // Filter events based on current mode (basic/advanced) - memoized
  const filteredEvents = React.useMemo(() => {
    const filtered = events.filter(event => {
      if (!event.type) {
        return false
      }
      const shouldShow = shouldShowEvent(event.type)
      return shouldShow
    })
    return filtered
  }, [events, shouldShowEvent])
  
  if (events.length === 0) {
    return <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-500 text-center ${compact ? 'py-2' : 'py-4'}`}>No events to display</div>
  }
  
  if (filteredEvents.length === 0) {
    return (
      <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-500 text-center ${compact ? 'py-2' : 'py-4'}`}>
        No events to display in {mode} mode
        {mode === 'basic' && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} mt-2`}>
            Switch to Advanced mode to see all events
          </div>
        )}
      </div>
    )
  }
  
  return <EventHierarchy 
    events={filteredEvents} 
    onApproveWorkflow={onApproveWorkflow}
    onSubmitFeedback={onSubmitFeedback}
    onFeedbackSubmitted={onFeedbackSubmitted}
    isApproving={isApproving}
    compact={compact}
  />
}) 