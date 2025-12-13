import React from 'react'
import type { PollingEvent } from '../../services/api-types'
import { useEventMode } from './useEventMode'
import { EventHierarchy } from './EventHierarchy'
import { EventWithOrchestratorContext } from './common/EventWithOrchestratorContext'

// Import the type-safe helpers from the new event-types module
import {
  isEventType,
  getEventData,
  type WorkflowStartEventData,
  type WorkflowProgressEventData,
  type WorkflowEndEventData,
} from '../../generated/event-types'

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
  StepProgressUpdatedEventDisplay,
  DecisionEvaluatedEventDisplay
} from './orchestrator'
import { StepTokenUsageEventDisplay } from './orchestrator/StepTokenUsageEvent'
import { VariablesExtractedEventDisplay } from './orchestrator/VariablesExtractedEvent'

import {
  WorkflowStartEvent,
  WorkflowProgressEvent,
  WorkflowEndEvent,
  BatchGroupStartEvent,
  BatchGroupEndEvent
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
  SmartRoutingStartEventDisplay,
  SmartRoutingEndEventDisplay,
  CacheEventDisplay,
  ComprehensiveCacheEventDisplay,
  StructuredOutputStartEventDisplay,
  StructuredOutputEndEventDisplay,
  WorkspaceFileOperationEventDisplay
} from './debug'
import { UnifiedCompletionEventDisplay } from './debug/UnifiedCompletionEvent'
import { HumanVerificationDisplay } from './HumanVerificationDisplay'
import { BlockingHumanFeedbackDisplay } from './BlockingHumanFeedbackDisplay'

interface EventDispatcherProps {
  event: PollingEvent
  mode?: 'compact' | 'detailed'
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean
  isCollapsed?: boolean
  eventCount?: number
  onToggleCollapse?: () => void
  compact?: boolean
}

// Helper function to wrap event component with orchestrator context
function WithContext<T extends { metadata?: Record<string, unknown> }>({
  Component,
  data,
  compact
}: {
  Component: React.ComponentType<{ event: T; compact?: boolean }>
  data: T
  compact?: boolean
}) {
  return (
    <EventWithOrchestratorContext metadata={data.metadata}>
      <Component event={data} compact={compact} />
    </EventWithOrchestratorContext>
  )
}

export const EventDispatcher: React.FC<EventDispatcherProps> = React.memo(({ 
  event, 
  mode, 
  onApproveWorkflow, 
  onSubmitFeedback, 
  onFeedbackSubmitted, 
  isApproving, 
  isCollapsed, 
  eventCount, 
  onToggleCollapse, 
  compact = false 
}) => {
  // Wrapper component to apply compact styling
  const CompactWrapper: React.FC<{ children: React.ReactNode }> = ({ children }) => {
    if (!compact) return <>{children}</>
    return <div className="text-xs [&>*]:text-xs [&_h1]:!text-sm [&_h2]:!text-xs [&_h3]:!text-[11px] [&_p]:!text-xs [&_code]:!text-[10px] [&_span]:!text-xs [&_div]:!text-xs">{children}</div>
  }

  // Invalid event check
  if (!event.type || !event.data) {
    return (
      <div className={`bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
        <div className={`${compact ? 'text-xs' : 'text-sm'} text-yellow-700 dark:text-yellow-300`}>
          Invalid event: missing type or data
        </div>
      </div>
    )
  }

  // Type-safe event rendering using discriminated unions
  // Each case uses isEventType for type narrowing, then getEventData for typed access

  // Agent Events
  if (isEventType(event, 'agent_error')) {
    return <CompactWrapper><AgentErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'llm_generation_with_retry')) {
    return <CompactWrapper><LLMGenerationWithRetryEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'agent_start')) {
    return <CompactWrapper><WithContext Component={AgentStartEventComponent} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'agent_end')) {
    return <CompactWrapper><WithContext Component={AgentEndEventComponent} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // MCP Server Events
  if (isEventType(event, 'mcp_server_selection')) {
    return <CompactWrapper><WithContext Component={MCPServerSelectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_discovery')) {
    return <CompactWrapper><WithContext Component={MCPServerDiscoveryEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_connection')) {
    return <CompactWrapper><WithContext Component={MCPServerConnectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_connection_error')) {
    return <CompactWrapper><WithContext Component={MCPServerConnectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Conversation Events
  if (isEventType(event, 'conversation_start')) {
    return <CompactWrapper><WithContext Component={ConversationStartEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_end')) {
    return <CompactWrapper><WithContext Component={ConversationEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_error')) {
    return <CompactWrapper><WithContext Component={ConversationErrorEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_turn')) {
    const data = getEventData(event)
    return (
      <CompactWrapper>
        <EventWithOrchestratorContext metadata={data.metadata}>
          <ConversationTurnEventDisplay event={data} compact={compact} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
  }

  // LLM Events
  if (isEventType(event, 'llm_generation_start')) {
    const data = getEventData(event)
    return (
      <CompactWrapper>
        <EventWithOrchestratorContext metadata={data.metadata}>
          <LLMGenerationStartEventDisplay event={data} mode={compact ? 'compact' : mode} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'llm_generation_end')) {
    return <CompactWrapper><WithContext Component={LLMGenerationEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'llm_generation_error')) {
    const data = getEventData(event)
    return (
      <CompactWrapper>
        <EventWithOrchestratorContext metadata={data.metadata}>
          <LLMGenerationErrorEventDisplay event={data} mode={compact ? 'compact' : mode} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
  }

  // Tool Events
  if (isEventType(event, 'tool_call_start')) {
    return <CompactWrapper><WithContext Component={ToolCallStartEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_end')) {
    return <CompactWrapper><WithContext Component={ToolCallEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_error')) {
    return <CompactWrapper><WithContext Component={ToolCallErrorEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  // Workspace file operation events - shown in advanced mode only
  if (isEventType(event, 'workspace_file_operation')) {
    return <CompactWrapper><WorkspaceFileOperationEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // System Events
  if (isEventType(event, 'system_prompt')) {
    return <CompactWrapper><WithContext Component={SystemPromptEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'user_message')) {
    const data = getEventData(event)
    if (!data.content) {
      console.error('USERMSG_DEBUG - EventDispatcher - no user_message content found')
      return null
    }
    return <CompactWrapper><WithContext Component={UserMessageEventDisplay} data={data} compact={compact} /></CompactWrapper>
  }

  // Orchestrator Events
  if (isEventType(event, 'orchestrator_start')) {
    return <CompactWrapper><OrchestratorStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_end')) {
    return <CompactWrapper><OrchestratorEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_error')) {
    return <CompactWrapper><OrchestratorErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_agent_start')) {
    return (
      <CompactWrapper>
        <OrchestratorAgentStartEventDisplay 
          event={getEventData(event)} 
          isCollapsed={isCollapsed}
          eventCount={eventCount}
          onToggleCollapse={onToggleCollapse}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'orchestrator_agent_end')) {
    return <CompactWrapper><OrchestratorAgentEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_agent_error')) {
    return <CompactWrapper><OrchestratorAgentErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Human Verification Events
  if (isEventType(event, 'request_human_feedback')) {
    const data = getEventData(event)
    return (
      <HumanVerificationDisplay 
        event={{
          type: event.type,
          data: {
            ...data,
            objective: data.objective || '',
            todo_list_markdown: data.todo_list_markdown || '',
            request_id: data.request_id || `request_${Date.now()}`,
          },
          timestamp: event.timestamp || new Date().toISOString()
        }} 
        onApprove={onApproveWorkflow || (() => {})}
        onFeedbackSubmitted={onFeedbackSubmitted}
        isApproving={isApproving}
      />
    )
  }
  if (isEventType(event, 'blocking_human_feedback')) {
    const data = getEventData(event)
    return (
      <BlockingHumanFeedbackDisplay 
        event={{
          type: event.type,
          data: {
            ...data,
            question: data.question || 'Do you want to continue?',
            allow_feedback: data.allow_feedback || false,
            context: data.context || '',
            session_id: data.session_id || '',
            workflow_id: data.workflow_id || '',
            request_id: data.request_id || `request_${Date.now()}`
          },
          timestamp: event.timestamp || new Date().toISOString()
        }} 
        onApprove={onApproveWorkflow || (() => {})}
        onSubmitFeedback={onSubmitFeedback}
        onFeedbackSubmitted={onFeedbackSubmitted}
        isApproving={isApproving}
      />
    )
  }

  // Workflow Events
  if (isEventType(event, 'workflow_start')) {
    return <CompactWrapper><WorkflowStartEvent event={getEventData(event) as WorkflowStartEventData} /></CompactWrapper>
  }
  if (isEventType(event, 'workflow_progress')) {
    return <CompactWrapper><WorkflowProgressEvent event={getEventData(event) as WorkflowProgressEventData} /></CompactWrapper>
  }
  if (isEventType(event, 'workflow_end')) {
    return <CompactWrapper><WorkflowEndEvent event={getEventData(event) as WorkflowEndEventData} /></CompactWrapper>
  }
  // Batch execution events
  if (isEventType(event, 'batch_group_start')) {
    return <CompactWrapper><BatchGroupStartEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_group_end')) {
    return <CompactWrapper><BatchGroupEndEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Debug Events
  if (isEventType(event, 'token_usage')) {
    return <CompactWrapper><TokenUsageEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'throttling_detected')) {
    return <CompactWrapper><ThrottlingDetectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'fallback_model_used')) {
    return <CompactWrapper><FallbackModelUsedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'fallback_attempt')) {
    return <CompactWrapper><FallbackAttemptEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'token_limit_exceeded')) {
    return <CompactWrapper><TokenLimitExceededEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'large_tool_output_detected')) {
    return <CompactWrapper><LargeToolOutputDetectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'large_tool_output_file_written')) {
    return <CompactWrapper><LargeToolOutputFileWrittenEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'model_change')) {
    return <CompactWrapper><ModelChangeEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'max_turns_reached')) {
    return <CompactWrapper><MaxTurnsReachedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'context_cancelled')) {
    return <CompactWrapper><ContextCancelledEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Cache Events
  if (isEventType(event, 'cache_event')) {
    return <CompactWrapper><CacheEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'comprehensive_cache_event')) {
    return <CompactWrapper><ComprehensiveCacheEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Smart Routing Events
  if (isEventType(event, 'smart_routing_start')) {
    return <CompactWrapper><SmartRoutingStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'smart_routing_end')) {
    return <CompactWrapper><SmartRoutingEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Unified Completion Events
  if (isEventType(event, 'unified_completion')) {
    return <CompactWrapper><UnifiedCompletionEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Structured Output Events
  if (isEventType(event, 'structured_output_start')) {
    return <CompactWrapper><StructuredOutputStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'structured_output_end')) {
    return <CompactWrapper><StructuredOutputEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Planning Events
  if (isEventType(event, 'independent_steps_selected')) {
    return <CompactWrapper><IndependentStepsSelectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'todo_steps_extracted')) {
    return <CompactWrapper><TodoStepsExtractedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'variables_extracted')) {
    return <CompactWrapper><VariablesExtractedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Step Token Usage Events
  if (isEventType(event, 'step_token_usage')) {
    return <CompactWrapper><StepTokenUsageEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Step Execution Events
  if (isEventType(event, 'step_execution_start')) {
    return (
      <CompactWrapper>
        <StepExecutionEventDisplay 
          event={getEventData(event)} 
          eventType="step_execution_start"
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'step_execution_end')) {
    return (
      <CompactWrapper>
        <StepExecutionEventDisplay 
          event={getEventData(event)} 
          eventType="step_execution_end"
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'step_execution_failed')) {
    return (
      <CompactWrapper>
        <StepExecutionEventDisplay 
          event={getEventData(event)} 
          eventType="step_execution_failed"
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Step Progress Updated Event
  if (isEventType(event, 'step_progress_updated')) {
    return (
      <CompactWrapper>
        <StepProgressUpdatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Decision Evaluated Event
  if (isEventType(event, 'decision_evaluated')) {
    return (
      <CompactWrapper>
        <DecisionEvaluatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Default case for unknown event types
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
}, (prevProps, nextProps) => {
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
  isApproving?: boolean
  compact?: boolean
}> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact = false }) => {
  const { shouldShowEvent, mode } = useEventMode()
  
  const filteredEvents = React.useMemo(() => {
    return events.filter(event => {
      if (!event.type) return false
      return shouldShowEvent(event.type)
    })
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
  
  return (
    <EventHierarchy 
      events={filteredEvents} 
      onApproveWorkflow={onApproveWorkflow}
      onSubmitFeedback={onSubmitFeedback}
      onFeedbackSubmitted={onFeedbackSubmitted}
      isApproving={isApproving}
      compact={compact}
    />
  )
})
