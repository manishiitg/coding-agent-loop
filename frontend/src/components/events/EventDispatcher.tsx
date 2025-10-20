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
  Component: React.ComponentType<{ event: T }>,
  eventData: T
) {
  // Get metadata from the extracted event data
  const metadata = eventData.metadata;
  
  return (
    <EventWithOrchestratorContext metadata={metadata}>
      <Component event={eventData} />
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

  ReActReasoningStartEvent,
  ReActReasoningStepEvent,
  ReActReasoningFinalEvent,
  ReActReasoningEndEvent,
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
  IndependentStepsSelectedEventDisplay
} from './orchestrator'

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
  ReActReasoningStartEventDisplay,
  ReActReasoningEventDisplay,
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
import type { RequestHumanFeedbackEvent } from '../../generated/events'


interface EventDispatcherProps {
  event: PollingEvent
  mode?: 'compact' | 'detailed'
  onApproveWorkflow?: (requestId: string) => void
  isApproving?: boolean  // Loading state for approve button
}

export const EventDispatcher: React.FC<EventDispatcherProps> = React.memo(({ event, mode, onApproveWorkflow, isApproving }) => {
  if (!event.type || !event.data) {
    return (
      <div className="bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md p-3">
        <div className="text-sm text-yellow-700 dark:text-yellow-300">
          Invalid event: missing type or data
        </div>
      </div>
    )
  }

  switch (event.type) {
    // Agent Events
    case 'agent_error':
      return <AgentErrorEventDisplay event={extractEventData<AgentErrorEvent>(event.data)} />
    case 'llm_generation_with_retry':
      return <LLMGenerationWithRetryEventDisplay event={extractEventData<LLMGenerationWithRetryEvent>(event.data)} />

    // MCP Server Events
          case 'mcp_server_selection':
        return wrapWithOrchestratorContext(MCPServerSelectionEventDisplay, extractEventData<MCPServerSelectionEvent>(event.data))
      case 'mcp_server_discovery':
        return wrapWithOrchestratorContext(MCPServerDiscoveryEventDisplay, extractEventData<MCPServerDiscoveryEvent>(event.data))
      case 'mcp_server_connection':
        return wrapWithOrchestratorContext(MCPServerConnectionEventDisplay, extractEventData<MCPServerConnectionEvent>(event.data))
      case 'mcp_server_connection_error':
        return wrapWithOrchestratorContext(MCPServerConnectionEventDisplay, extractEventData<MCPServerConnectionEvent>(event.data))

    // Conversation Events
          case 'conversation_start':
        return wrapWithOrchestratorContext(ConversationStartEventDisplay, extractEventData<ConversationStartEvent>(event.data))
      case 'conversation_end':
        return wrapWithOrchestratorContext(ConversationEndEventDisplay, extractEventData<ConversationEndEvent>(event.data))
      case 'conversation_error':
        return wrapWithOrchestratorContext(ConversationErrorEventDisplay, extractEventData<ConversationErrorEvent>(event.data))
      case 'conversation_turn':
        return wrapWithOrchestratorContext(
          (props) => <ConversationTurnEventDisplay {...props} compact={true} />, 
          extractEventData<ConversationTurnEvent>(event.data)
        )


    // Agent Events
    case 'agent_start':
      return wrapWithOrchestratorContext(AgentStartEventComponent, extractEventData<AgentStartEvent>(event.data))
    case 'agent_end':
      return wrapWithOrchestratorContext(AgentEndEventComponent, extractEventData<AgentEndEvent>(event.data))

    // LLM Events
          case 'llm_generation_start':
        return wrapWithOrchestratorContext(
          (props) => <LLMGenerationStartEventDisplay {...props} mode={mode} />, 
          extractEventData<LLMGenerationStartEvent>(event.data)
        )
      case 'llm_generation_end':
        return wrapWithOrchestratorContext(LLMGenerationEndEventDisplay, extractEventData<LLMGenerationEndEvent>(event.data))
      case 'llm_generation_error':
        return wrapWithOrchestratorContext(
          (props) => <LLMGenerationErrorEventDisplay {...props} mode={mode} />, 
          extractEventData<LLMGenerationErrorEvent>(event.data)
        )


    // Tool Events
    case 'tool_call_start':
      return wrapWithOrchestratorContext(ToolCallStartEventDisplay, extractEventData<ToolCallStartEvent>(event.data))
    case 'tool_call_end':
      return wrapWithOrchestratorContext(ToolCallEndEventDisplay, extractEventData<ToolCallEndEvent>(event.data))
    case 'tool_call_error':
      return wrapWithOrchestratorContext(ToolCallErrorEventDisplay, extractEventData<ToolCallErrorEvent>(event.data))

    // System Events
    case 'system_prompt':
      return wrapWithOrchestratorContext(SystemPromptEventDisplay, extractEventData<SystemPromptEvent>(event.data))
    case 'user_message': {
      const userMessageData = event.data?.user_message
      if (!userMessageData) {
        console.error('USERMSG_DEBUG - EventDispatcher - no user_message data found')
        return null
      }
      return wrapWithOrchestratorContext(UserMessageEventDisplay, userMessageData)
    }

    // Step Events (Deep Search step execution)
    // Deep Search Events (individual agent events for debugging)
    case 'orchestrator_start':
      return <OrchestratorStartEventDisplay event={extractEventData<OrchestratorStartEvent>(event.data)} />
    case 'orchestrator_end':
      return <OrchestratorEndEventDisplay event={extractEventData<OrchestratorEndEvent>(event.data)} />
    case 'orchestrator_error':
      return <OrchestratorErrorEventDisplay event={extractEventData<OrchestratorErrorEvent>(event.data)} />
    case 'orchestrator_agent_start':
      return <OrchestratorAgentStartEventDisplay event={extractEventData<OrchestratorAgentStartEvent>(event.data)} />
    case 'orchestrator_agent_end':
      return <OrchestratorAgentEndEventDisplay event={extractEventData<OrchestratorAgentEndEvent>(event.data)} />
    case 'orchestrator_agent_error':
      return <OrchestratorAgentErrorEventDisplay event={extractEventData<OrchestratorAgentErrorEvent>(event.data)} />

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
        isApproving={isApproving}
      />

    // Workflow Events
    case 'workflow_start':
      return <WorkflowStartEvent event={extractEventData<{workflow_id?: string, objective?: string, message?: string, timestamp?: number}>(event.data)} />

    case 'workflow_progress':
      return <WorkflowProgressEvent event={extractEventData<{phase?: string, message?: string, timestamp?: number}>(event.data)} />

    case 'workflow_end':
      return <WorkflowEndEvent event={extractEventData<{workflow_id?: string, result?: string, status?: string, message?: string, timestamp?: number}>(event.data)} />

    // ReAct Reasoning Events
    case 'react_reasoning_start':
      return <ReActReasoningStartEventDisplay event={extractEventData<ReActReasoningStartEvent>(event.data)} />

    // Debug Events
    case 'token_usage':
      return <TokenUsageEventDisplay event={extractEventData<TokenUsageEvent>(event.data)} />
    case 'throttling_detected':
      return <ThrottlingDetectedEventDisplay event={extractEventData<ThrottlingDetectedEvent>(event.data)} />
    case 'fallback_model_used':
      return <FallbackModelUsedEventDisplay event={extractEventData<FallbackModelUsedEvent>(event.data)} />
    case 'fallback_attempt':
      return <FallbackAttemptEventDisplay event={extractEventData<FallbackAttemptEvent>(event.data)} />
    case 'token_limit_exceeded':
      return <TokenLimitExceededEventDisplay event={extractEventData<TokenLimitExceededEvent>(event.data)} />
    case 'large_tool_output_detected':
      return <LargeToolOutputDetectedEventDisplay event={extractEventData<LargeToolOutputDetectedEvent>(event.data)} />
    case 'large_tool_output_file_written':
      return <LargeToolOutputFileWrittenEventDisplay event={extractEventData<LargeToolOutputFileWrittenEvent>(event.data)} />
    case 'react_reasoning_step':
      return <ReActReasoningEventDisplay event={extractEventData<ReActReasoningStepEvent>(event.data)} />
    case 'react_reasoning_final':
      return <ReActReasoningEventDisplay event={extractEventData<ReActReasoningFinalEvent>(event.data)} />
    case 'react_reasoning_end':
      return <ReActReasoningEventDisplay event={extractEventData<ReActReasoningEndEvent>(event.data)} />
    case 'model_change':
      return <ModelChangeEventDisplay event={extractEventData<ModelChangeEvent>(event.data)} />
    case 'max_turns_reached':
      return <MaxTurnsReachedEventDisplay event={extractEventData<MaxTurnsReachedEvent>(event.data)} />
    case 'context_cancelled':
      return <ContextCancelledEventDisplay event={extractEventData<ContextCancelledEvent>(event.data)} />

    // Cache Events - Only comprehensive cache events
    case 'cache_event':
      return <CacheEventDisplay event={extractEventData<CacheEvent>(event.data)} />
    case 'comprehensive_cache_event':
      return <ComprehensiveCacheEventDisplay event={extractEventData<ComprehensiveCacheEvent>(event.data)} />

    // Smart Routing Events
    case 'smart_routing_start':
      return <SmartRoutingStartEventDisplay event={extractEventData<SmartRoutingStartEvent>(event.data)} />
    case 'smart_routing_end':
      return <SmartRoutingEndEventDisplay event={extractEventData<SmartRoutingEndEvent>(event.data)} />

    // Unified Completion Events
    case 'unified_completion':
      return <UnifiedCompletionEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} />

    // Structured Output Events
    case 'structured_output_start':
      return <StructuredOutputStartEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} />
    case 'structured_output_end':
      return <StructuredOutputEndEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} />

    // Independent Steps Events
    case 'independent_steps_selected':
      return <IndependentStepsSelectedEventDisplay event={extractEventData<Record<string, unknown>>(event.data)} />

    // Default case for unknown event types
    default:
      return (
        <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded-md p-3">
          <div className="text-sm text-gray-700 dark:text-gray-300">
            <div className="font-medium">Unknown Event Type: {event.type}</div>
            <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
              Event data: {JSON.stringify(event.data, null, 2)}
            </div>
          </div>
        </div>
      )
  }
}, (prevProps, nextProps) => {
  // Custom comparison to prevent unnecessary re-renders
  // Only re-render if event ID, mode, or approving state changes
  return prevProps.event.id === nextProps.event.id &&
         prevProps.mode === nextProps.mode &&
         prevProps.isApproving === nextProps.isApproving
})

// Event list component for displaying multiple events
export const EventList: React.FC<{ 
  events: PollingEvent[]
  // onApproveWorkflow?: (requestId: string) => void
  // isApproving?: boolean  // Loading state for approve button
}> = React.memo(({ events }) => {
  const { shouldShowEvent, mode } = useEventMode()
  
  // Filter events based on current mode (basic/advanced) - memoized
  const filteredEvents = React.useMemo(() => {
    const filtered = events.filter(event => {
      if (!event.type) return false
      return shouldShowEvent(event.type)
    })
    return filtered
  }, [events, shouldShowEvent])
  
  if (events.length === 0) {
    return <div className="text-gray-500 text-center py-4">No events to display</div>
  }
  
  if (filteredEvents.length === 0) {
    return (
      <div className="text-gray-500 text-center py-4">
        No events to display in {mode} mode
        {mode === 'basic' && (
          <div className="text-xs mt-2">
            Switch to Advanced mode to see all events
          </div>
        )}
      </div>
    )
  }
  
  return <EventHierarchy 
    events={filteredEvents} 
  />
}) 