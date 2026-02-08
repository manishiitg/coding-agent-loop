import React from 'react'
import { Code2, Sparkles, Search } from 'lucide-react'
import type { PollingEvent } from '../../services/api-types'
import { EventHierarchy } from './EventHierarchy'
import { EventWithOrchestratorContext } from './common/EventWithOrchestratorContext'

// Import the type-safe helpers from the new event-types module
import {
  isEventType,
  getEventData,
  type WorkflowStartEventData,
  type WorkflowProgressEventData,
  type WorkflowEndEventData,
  type PrerequisiteNavigationEvent,
  type TodoTaskRouteSelectedEvent,
  type TodoTaskItemCreatedEvent,
  type TodoTaskItemUpdatedEvent,
  type TodoTaskItemCompletedEvent,
  type TodoTaskStepCompletedEvent,
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
import type { UserMessageEvent } from '../../generated/events'

import {
  OrchestratorStartEventDisplay,
  OrchestratorEndEventDisplay,
  OrchestratorErrorEventDisplay,
  OrchestratorAgentStartEventDisplay,
  OrchestratorAgentEndEventDisplay,
  OrchestratorAgentErrorEventDisplay,
  IndependentStepsSelectedEventDisplay,
  TodoStepsExtractedEventDisplay,
  StepProgressUpdatedEventDisplay,
  DecisionEvaluatedEventDisplay,
  PreValidationCompletedEventDisplay,
  PrerequisiteNavigationEventDisplay,
  TodoTaskRouteSelectedEventDisplay,
  TodoTaskItemCreatedEventDisplay,
  TodoTaskItemUpdatedEventDisplay,
  TodoTaskItemCompletedEventDisplay,
  TodoTaskStepCompletedEventDisplay
} from './orchestrator'
import { StepTokenUsageEventDisplay } from './orchestrator/StepTokenUsageEvent'
import { VariablesExtractedEventDisplay } from './orchestrator/VariablesExtractedEvent'

import {
  WorkflowStartEvent,
  WorkflowProgressEvent,
  WorkflowEndEvent,
  BatchGroupStartEvent,
  BatchGroupEndEvent,
  BatchExecutionStartEventDisplay,
  BatchExecutionEndEventDisplay,
  BatchExecutionCanceledEventDisplay
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
  ContextSummarizationStartedEventDisplay,
  ContextSummarizationCompletedEventDisplay,
  ContextSummarizationErrorEventDisplay,
  ContextEditingCompletedEventDisplay,
  ContextEditingErrorEventDisplay,
  TempLLMSkippedEventDisplay
} from './debug'
import { UnifiedCompletionEventDisplay } from './debug/UnifiedCompletionEvent'
import { HumanVerificationDisplay } from './HumanVerificationDisplay'
import { BlockingHumanFeedbackDisplay } from './BlockingHumanFeedbackDisplay'

export interface DelegationStats {
  toolCalls: number
  inputTokens: number
  outputTokens: number
  latestToolName?: string
  completed?: boolean
}

// Live elapsed timer for running delegation events
const ElapsedTimer: React.FC<{ startTimestamp: string; className?: string }> = ({ startTimestamp, className }) => {
  const [elapsed, setElapsed] = React.useState('')

  React.useEffect(() => {
    const startTime = new Date(startTimestamp).getTime()
    if (isNaN(startTime)) return

    const update = () => {
      const seconds = Math.floor((Date.now() - startTime) / 1000)
      if (seconds < 60) {
        setElapsed(`${seconds}s`)
      } else {
        const m = Math.floor(seconds / 60)
        const s = seconds % 60
        setElapsed(`${m}m${s.toString().padStart(2, '0')}s`)
      }
    }
    update()
    const interval = setInterval(update, 1000)
    return () => clearInterval(interval)
  }, [startTimestamp])

  if (!elapsed) return null
  return <span className={className}>{elapsed}</span>
}

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
  delegationStats?: Map<string, DelegationStats>
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
  compact = false,
  delegationStats
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
  // Note: delegate tool events are filtered out at EventHierarchy level
  if (isEventType(event, 'tool_call_start')) {
    return <CompactWrapper><WithContext Component={ToolCallStartEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_end')) {
    return <CompactWrapper><WithContext Component={ToolCallEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_error')) {
    return <CompactWrapper><WithContext Component={ToolCallErrorEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // System Events
  if (isEventType(event, 'system_prompt')) {
    return <CompactWrapper><WithContext Component={SystemPromptEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'user_message')) {
    const data = getEventData(event)
    // Always render - UserMessageEventDisplay handles missing content gracefully
    // Log warning if content is missing for debugging
    if (!data.content) {
      console.warn('USERMSG_DEBUG - EventDispatcher - user_message event has no content, but rendering anyway', data)
    }
    return <CompactWrapper><WithContext Component={UserMessageEventDisplay} data={data} compact={compact} /></CompactWrapper>
  }
  
  // Fallback: Try to handle user_message events even if type check fails
  // This handles cases where event structure might be slightly different
  if (event.type === 'user_message' && event.data) {
    try {
      // Try to extract data from nested structure
      const agentEvent = event.data as { data?: unknown; type?: string }
      const eventData = (agentEvent?.data || event.data) as UserMessageEvent
      if (eventData) {
        console.log('USERMSG_DEBUG - EventDispatcher - Using fallback for user_message event', eventData)
        return <CompactWrapper><WithContext Component={UserMessageEventDisplay} data={eventData} compact={compact} /></CompactWrapper>
      }
    } catch (error) {
      console.error('USERMSG_DEBUG - EventDispatcher - Error in fallback handler', error, event)
    }
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
  if (isEventType(event, 'prerequisite_navigation')) {
    return <CompactWrapper><PrerequisiteNavigationEventDisplay event={getEventData(event) as PrerequisiteNavigationEvent} compact={compact} /></CompactWrapper>
  }
  // Batch execution events
  if (isEventType(event, 'batch_group_start')) {
    return <CompactWrapper><BatchGroupStartEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_group_end')) {
    return <CompactWrapper><BatchGroupEndEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_start')) {
    return <CompactWrapper><BatchExecutionStartEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_end')) {
    return <CompactWrapper><BatchExecutionEndEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_canceled')) {
    return <CompactWrapper><BatchExecutionCanceledEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Todo Task Events
  if (isEventType(event, 'todo_task_route_selected')) {
    const data = getEventData(event) as TodoTaskRouteSelectedEvent
    const actionColors: Record<string, string> = {
      delegate: 'text-blue-600 dark:text-blue-400',
      complete: 'text-green-600 dark:text-green-400',
      continue: 'text-yellow-600 dark:text-yellow-400',
    }
    return (
      <CompactWrapper>
        <div className={`bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2 mb-2">
            <span className="text-lg">📋</span>
            <span className={`font-medium ${compact ? 'text-xs' : 'text-sm'} text-purple-700 dark:text-purple-300`}>
              Todo Task: Route Selected
            </span>
            {data.iteration && (
              <span className={`${compact ? 'text-[10px]' : 'text-xs'} bg-purple-200 dark:bg-purple-800 px-1.5 py-0.5 rounded text-purple-700 dark:text-purple-300`}>
                Iteration {data.iteration}
              </span>
            )}
          </div>
          <div className={`space-y-1 ${compact ? 'text-xs' : 'text-sm'}`}>
            <div className="flex items-center gap-2">
              <span className="text-gray-500 dark:text-gray-400">Action:</span>
              <span className={`font-medium ${actionColors[data.next_action || ''] || 'text-gray-700 dark:text-gray-300'}`}>
                {data.next_action || 'unknown'}
              </span>
            </div>
            {data.selected_route_name && (
              <div className="flex items-center gap-2">
                <span className="text-gray-500 dark:text-gray-400">Agent:</span>
                <span className="text-purple-600 dark:text-purple-400">{data.selected_route_name}</span>
              </div>
            )}
            {data.use_generic_agent && (
              <div className="flex items-center gap-2">
                <span className="text-gray-500 dark:text-gray-400">Agent:</span>
                <span className="text-purple-600 dark:text-purple-400">Generic Agent</span>
              </div>
            )}
            {data.todo_title && (
              <div className="flex items-center gap-2">
                <span className="text-gray-500 dark:text-gray-400">Todo:</span>
                <span className="text-gray-700 dark:text-gray-300">{data.todo_title}</span>
              </div>
            )}
            {data.progress_summary && (
              <div className="flex items-center gap-2">
                <span className="text-gray-500 dark:text-gray-400">Progress:</span>
                <span className="text-gray-700 dark:text-gray-300">{data.progress_summary}</span>
              </div>
            )}
          </div>
        </div>
      </CompactWrapper>
    )
  }

  if (isEventType(event, 'todo_task_item_created')) {
    const data = getEventData(event) as TodoTaskItemCreatedEvent
    return (
      <CompactWrapper>
        <div className={`bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2 mb-1">
            <span className="text-lg">➕</span>
            <span className={`font-medium ${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300`}>
              Todo Created: {data.title}
            </span>
            {data.priority && (
              <span className={`${compact ? 'text-[10px]' : 'text-xs'} px-1.5 py-0.5 rounded ${
                data.priority === 'high' ? 'bg-red-200 dark:bg-red-800 text-red-700 dark:text-red-300' :
                data.priority === 'medium' ? 'bg-yellow-200 dark:bg-yellow-800 text-yellow-700 dark:text-yellow-300' :
                'bg-gray-200 dark:bg-gray-700 text-gray-600 dark:text-gray-400'
              }`}>
                {data.priority}
              </span>
            )}
          </div>
          {data.description && (
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400 mt-1`}>
              {data.description}
            </div>
          )}
        </div>
      </CompactWrapper>
    )
  }

  if (isEventType(event, 'todo_task_item_updated')) {
    const data = getEventData(event) as TodoTaskItemUpdatedEvent
    return (
      <CompactWrapper>
        <div className={`bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2">
            <span className="text-lg">🔄</span>
            <span className={`font-medium ${compact ? 'text-xs' : 'text-sm'} text-blue-700 dark:text-blue-300`}>
              Todo Updated: {data.title}
            </span>
            <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400`}>
              {data.old_status} → {data.new_status}
            </span>
          </div>
          {data.notes && (
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-blue-600 dark:text-blue-400 mt-1`}>
              {data.notes}
            </div>
          )}
        </div>
      </CompactWrapper>
    )
  }

  if (isEventType(event, 'todo_task_item_completed')) {
    const data = getEventData(event) as TodoTaskItemCompletedEvent
    return (
      <CompactWrapper>
        <div className={`bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2">
            <span className="text-lg">✅</span>
            <span className={`font-medium ${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300`}>
              Todo Completed: {data.title}
            </span>
          </div>
          {data.result && (
            <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400 mt-1`}>
              {data.result}
            </div>
          )}
        </div>
      </CompactWrapper>
    )
  }

  if (isEventType(event, 'todo_task_step_completed')) {
    const data = getEventData(event) as TodoTaskStepCompletedEvent
    return (
      <CompactWrapper>
        <div className={`bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2 mb-2">
            <span className="text-lg">🎉</span>
            <span className={`font-medium ${compact ? 'text-xs' : 'text-sm'} text-purple-700 dark:text-purple-300`}>
              Todo Task Step Completed: {data.step_title}
            </span>
          </div>
          <div className={`space-y-1 ${compact ? 'text-xs' : 'text-sm'}`}>
            <div className="flex items-center gap-2">
              <span className="text-gray-500 dark:text-gray-400">Todos:</span>
              <span className="text-purple-600 dark:text-purple-400">
                {data.completed_count}/{data.total_todos_count} completed
              </span>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-gray-500 dark:text-gray-400">Iterations:</span>
              <span className="text-purple-600 dark:text-purple-400">{data.total_iterations}</span>
            </div>
            {data.completion_reason && (
              <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-600 dark:text-gray-400 italic mt-1`}>
                {data.completion_reason}
              </div>
            )}
          </div>
        </div>
      </CompactWrapper>
    )
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

  // Context Summarization Events
  if (isEventType(event, 'context_summarization_started')) {
    return <CompactWrapper><ContextSummarizationStartedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_summarization_completed')) {
    return <CompactWrapper><ContextSummarizationCompletedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_summarization_error')) {
    return <CompactWrapper><ContextSummarizationErrorEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Context Editing Events
  if (isEventType(event, 'context_editing_completed')) {
    return <CompactWrapper><ContextEditingCompletedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_editing_error')) {
    return <CompactWrapper><ContextEditingErrorEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Temp LLM Skipped Event
  if (event.type === 'temp_llm_skipped') {
    const data = event.data as {
      timestamp?: string
      hierarchy_level?: number
      component?: string
      metadata?: {
        orchestrator_agent_name?: string
        orchestrator_phase?: string
        orchestrator_step?: number
      }
      step_id?: string
      step_index?: number
      step_title?: string
      step_path?: string
      is_branch_step?: boolean
      reason?: string
      temp_llm_provider?: string
      temp_llm_model?: string
      learnings_path?: string
      run_folder?: string
      workspace_path?: string
    }
    return (
      <CompactWrapper>
        <EventWithOrchestratorContext metadata={data?.metadata}>
          <TempLLMSkippedEventDisplay event={data || {}} compact={compact} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
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

  // Todo Task Events
  if (isEventType(event, 'todo_task_route_selected')) {
    return (
      <CompactWrapper>
        <TodoTaskRouteSelectedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_created')) {
    return (
      <CompactWrapper>
        <TodoTaskItemCreatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_updated')) {
    return (
      <CompactWrapper>
        <TodoTaskItemUpdatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_completed')) {
    return (
      <CompactWrapper>
        <TodoTaskItemCompletedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_step_completed')) {
    return (
      <CompactWrapper>
        <TodoTaskStepCompletedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Pre-Validation Completed Event
  if (isEventType(event, 'pre_validation_completed')) {
    return (
      <CompactWrapper>
        <PreValidationCompletedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Workflow Error Event
  if (event.type === 'workflow_error') {
    const data = event.data as {
      data?: {
        error?: string
        error_chain?: string
        query_id?: string
        [key: string]: unknown
      }
      error?: string
      timestamp?: string
      trace_id?: string
      correlation_id?: string
      [key: string]: unknown
    }
    
    // Extract error from nested structure - handle both nested and flat structures
    const nestedData = data?.data
    const rootCauseError = 
      (typeof nestedData === 'object' && nestedData !== null && 'error' in nestedData && typeof nestedData.error === 'string' && nestedData.error) ||
      (typeof data?.error === 'string' && data.error) ||
      'Unknown workflow error'
    
    const fullErrorChain = 
      (typeof nestedData === 'object' && nestedData !== null && 'error_chain' in nestedData && typeof nestedData.error_chain === 'string' && nestedData.error_chain) ||
      undefined
    
    const queryId = 
      (typeof nestedData === 'object' && nestedData !== null && 'query_id' in nestedData && typeof nestedData.query_id === 'string' && nestedData.query_id) ||
      undefined
    
    const hasFullChain = fullErrorChain && fullErrorChain !== rootCauseError
    
    return (
      <CompactWrapper>
        <div className={`bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg ${compact ? 'p-2' : 'p-3'}`}>
          <div className="space-y-2">
            {/* Header */}
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2">
                <span className="text-lg">❌</span>
                <div className={`${compact ? 'text-xs' : 'text-sm'} font-medium text-red-700 dark:text-red-300`}>
                  Workflow Error
                </div>
              </div>
              {event.timestamp && (
                <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 flex-shrink-0`}>
                  {new Date(event.timestamp).toLocaleTimeString()}
                </div>
              )}
            </div>
            
            {/* Query ID */}
            {queryId && (
              <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400`}>
                <span className="font-medium">Query ID:</span>{' '}
                <code className="bg-red-100 dark:bg-red-800 px-1 rounded">{queryId}</code>
              </div>
            )}
            
            {/* Root Cause Error - highlighted prominently */}
            <div className="bg-red-200 dark:bg-red-900 border-2 border-red-300 dark:border-red-700 rounded-md p-2">
              <div className={`${compact ? 'text-[10px]' : 'text-xs'} font-bold text-red-900 dark:text-red-100 mb-1 flex items-center gap-1`}>
                <span>🔍</span>
                <span>Root Cause:</span>
              </div>
              <div className={`${compact ? 'text-xs' : 'text-sm'} text-red-950 dark:text-red-50 whitespace-pre-wrap break-words font-mono font-semibold`}>
                {rootCauseError}
              </div>
            </div>
            
            {/* Full Error Chain - shown if different from root cause */}
            {hasFullChain && (
              <details className="bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded-md p-2">
                <summary className={`${compact ? 'text-[10px]' : 'text-xs'} font-medium text-red-800 dark:text-red-200 cursor-pointer`}>
                  Full Error Chain (click to expand)
                </summary>
                <div className={`${compact ? 'text-xs' : 'text-sm'} text-red-900 dark:text-red-100 whitespace-pre-wrap break-words font-mono mt-2`}>
                  {fullErrorChain}
                </div>
              </details>
            )}
            
            {/* Additional metadata */}
            {(data?.trace_id || data?.correlation_id) && (
              <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 space-y-1`}>
                {data.trace_id && (
                  <div>
                    <span className="font-medium">Trace ID:</span>{' '}
                    <code className="bg-red-100 dark:bg-red-800 px-1 rounded">{data.trace_id}</code>
                  </div>
                )}
                {data.correlation_id && (
                  <div>
                    <span className="font-medium">Correlation ID:</span>{' '}
                    <code className="bg-red-100 dark:bg-red-800 px-1 rounded">{data.correlation_id}</code>
                  </div>
                )}
              </div>
            )}
            
            {/* Show full data structure if available (for debugging) */}
            {compact && Object.keys(data?.data || {}).length > 2 && (
              <details className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400`}>
                <summary className="cursor-pointer font-medium">Show full error data</summary>
                <pre className="mt-1 bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded p-2 overflow-x-auto text-[10px]">
                  {JSON.stringify(data, null, 2)}
                </pre>
              </details>
            )}
          </div>
        </div>
      </CompactWrapper>
    )
  }

  // Delegation Start Event
  if (event.type === 'delegation_start') {
    const data = event.data as {
      data?: {
        delegation_id?: string
        depth?: number
        instruction?: string
        reasoning_level?: string
        model_id?: string
        tool_mode?: string
      }
      delegation_id?: string
      depth?: number
      instruction?: string
      reasoning_level?: string
      model_id?: string
      tool_mode?: string
      timestamp?: string
    }

    const delegationData = data?.data || data
    const instruction = delegationData?.instruction || 'No instruction provided'
    const depth = delegationData?.depth
    const delegationId = delegationData?.delegation_id
    const reasoningLevel = delegationData?.reasoning_level
    const modelId = delegationData?.model_id
    const toolMode = delegationData?.tool_mode

    const reasoningColors: Record<string, string> = {
      high: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300',
      medium: 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300',
      low: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-300',
    }

    // Get live stats for this delegation from child events
    const liveStats = delegationId ? delegationStats?.get(delegationId) : undefined
    const hasLiveStats = liveStats && (liveStats.toolCalls > 0 || liveStats.inputTokens > 0)
    const isCompleted = liveStats?.completed

    return (
      <CompactWrapper>
        <details className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded px-2 py-1.5 group">
          <summary className="flex items-center gap-2 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <span className="text-sm">🔀</span>
            <span className="text-[10px] text-purple-400 group-open:hidden">+</span>
            <span className="text-[10px] text-purple-400 hidden group-open:inline">−</span>
            <div className="text-xs font-medium text-purple-700 dark:text-purple-300 flex-1 truncate" title={instruction}>
              Sub-agent: {instruction.length > 80 ? instruction.substring(0, 80) + '...' : instruction}
            </div>
            <div className="flex items-center gap-1.5 flex-shrink-0">
              {hasLiveStats && (
                <span className={`text-[10px] text-purple-500 dark:text-purple-400${isCompleted ? '' : ' animate-pulse'}`}>
                  {liveStats.toolCalls ? `${liveStats.toolCalls} tools` : ''}
                  {liveStats.latestToolName ? ` · ${liveStats.latestToolName}` : ''}
                  {liveStats.inputTokens ? ` · ${((liveStats.inputTokens + liveStats.outputTokens) / 1000).toFixed(1)}k tok` : ''}
                </span>
              )}
              {event.timestamp && !isCompleted && (
                <ElapsedTimer startTimestamp={event.timestamp} className="text-[10px] text-purple-500 dark:text-purple-400 animate-pulse font-mono" />
              )}
              {reasoningLevel && (
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${reasoningColors[reasoningLevel] || 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'}`}>
                  {reasoningLevel}
                </span>
              )}
              <span className="relative group/mode cursor-default flex items-center">
                {toolMode === 'code_execution' ? (
                  <Code2 className="w-3.5 h-3.5 text-orange-500 dark:text-orange-400" />
                ) : toolMode === 'tool_search' ? (
                  <Search className="w-3.5 h-3.5 text-cyan-500 dark:text-cyan-400" />
                ) : (
                  <Sparkles className="w-3.5 h-3.5 text-purple-500 dark:text-purple-400" />
                )}
                <span className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 px-2 py-1 rounded bg-gray-900 dark:bg-gray-100 text-white dark:text-gray-900 text-[10px] font-medium whitespace-nowrap opacity-0 pointer-events-none group-hover/mode:opacity-100 transition-opacity z-50">
                  {toolMode === 'code_execution' ? 'Code Execution' : toolMode === 'tool_search' ? 'Tool Search' : 'Simple'}
                </span>
              </span>
              {event.timestamp && (
                <span className="text-[10px] text-purple-500 dark:text-purple-400">
                  {new Date(event.timestamp).toLocaleTimeString()}
                </span>
              )}
            </div>
          </summary>
          <div className="mt-2 pt-2 border-t border-purple-200 dark:border-purple-700 space-y-1.5">
            <div className="text-xs text-purple-700 dark:text-purple-300 whitespace-pre-wrap break-words">
              {instruction}
            </div>
            <div className="flex items-center gap-3 text-[10px] text-purple-500 dark:text-purple-400">
              {reasoningLevel && <span>Reasoning: {reasoningLevel}</span>}
              <span>Mode: {!toolMode || toolMode === 'simple' ? 'Simple' : toolMode === 'code_execution' ? 'Code Execution' : 'Tool Search'}</span>
              {modelId && <span>Model: {modelId}</span>}
              {depth !== undefined && <span>Depth: {depth}</span>}
              {delegationId && <span className="font-mono">{delegationId}</span>}
            </div>
            {hasLiveStats && (
              <div className="flex items-center gap-3 text-[10px] text-purple-500 dark:text-purple-400">
                {liveStats.inputTokens > 0 && <span>In: {liveStats.inputTokens.toLocaleString()} tokens</span>}
                {liveStats.outputTokens > 0 && <span>Out: {liveStats.outputTokens.toLocaleString()} tokens</span>}
                {liveStats.toolCalls > 0 && <span>Tool calls: {liveStats.toolCalls}</span>}
                {liveStats.latestToolName && <span>Latest: {liveStats.latestToolName}</span>}
                {event.timestamp && !isCompleted && (
                  <span>Elapsed: <ElapsedTimer startTimestamp={event.timestamp} className="font-mono" /></span>
                )}
              </div>
            )}
          </div>
        </details>
      </CompactWrapper>
    )
  }

  // Delegation End Event
  if (event.type === 'delegation_end') {
    const data = event.data as {
      data?: {
        delegation_id?: string
        depth?: number
        result?: string
        error?: string
        duration?: string
        input_tokens?: number
        output_tokens?: number
        tool_calls?: number
      }
      delegation_id?: string
      depth?: number
      result?: string
      error?: string
      duration?: string
      input_tokens?: number
      output_tokens?: number
      tool_calls?: number
      timestamp?: string
    }

    const delegationData = data?.data || data
    const resultText = delegationData?.result
    const error = delegationData?.error
    const rawDuration = delegationData?.duration || ''
    const isSuccess = !error
    const inputTokens = delegationData?.input_tokens
    const outputTokens = delegationData?.output_tokens
    const toolCalls = delegationData?.tool_calls
    const hasStats = inputTokens || outputTokens || toolCalls

    // Format Go duration (e.g. "45.123456789s", "2m34.567s") to concise form
    const formatDuration = (d: string): string => {
      if (!d) return ''
      // Match Go duration formats: "Xm", "Xs", "XmYs", "XmY.Zs"
      const match = d.match(/^(?:(\d+)m)?(\d+(?:\.\d+)?)s$/)
      if (match) {
        const mins = match[1] ? parseInt(match[1]) : 0
        const secs = parseFloat(match[2])
        if (mins > 0) return `${mins}m${Math.round(secs).toString().padStart(2, '0')}s`
        return `${secs.toFixed(1)}s`
      }
      return d
    }
    const duration = formatDuration(rawDuration)

    const colorClasses = isSuccess
      ? { bg: 'bg-green-50 dark:bg-green-900/20', border: 'border-green-200 dark:border-green-800', text: 'text-green-700 dark:text-green-300', muted: 'text-green-500 dark:text-green-400', divider: 'border-green-200 dark:border-green-700' }
      : { bg: 'bg-red-50 dark:bg-red-900/20', border: 'border-red-200 dark:border-red-800', text: 'text-red-700 dark:text-red-300', muted: 'text-red-500 dark:text-red-400', divider: 'border-red-200 dark:border-red-700' }

    return (
      <CompactWrapper>
        <details className={`${colorClasses.bg} border ${colorClasses.border} rounded px-2 py-1.5 group`}>
          <summary className="flex items-center gap-2 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <span className="text-sm">{isSuccess ? '✅' : '❌'}</span>
            <span className={`text-[10px] ${colorClasses.muted} group-open:hidden`}>+</span>
            <span className={`text-[10px] ${colorClasses.muted} hidden group-open:inline`}>−</span>
            <div className={`text-xs font-medium flex-1 ${colorClasses.text}`}>
              {isSuccess ? 'Sub-agent done' : 'Sub-agent failed'}
              {error && <span className="font-normal ml-1">- {error.length > 50 ? error.substring(0, 50) + '...' : error}</span>}
            </div>
            <div className="flex items-center gap-1.5 text-[10px] flex-shrink-0">
              {hasStats && (
                <span className={colorClasses.muted}>
                  {inputTokens ? `${((inputTokens + (outputTokens || 0)) / 1000).toFixed(1)}k tok` : ''}
                  {toolCalls ? ` · ${toolCalls} tools` : ''}
                </span>
              )}
              {duration && (
                <span className={colorClasses.muted}>{duration}</span>
              )}
              {event.timestamp && (
                <span className={colorClasses.muted}>
                  {new Date(event.timestamp).toLocaleTimeString()}
                </span>
              )}
            </div>
          </summary>
          <div className={`mt-2 pt-2 border-t ${colorClasses.divider} space-y-1.5`}>
            {error && (
              <div className="text-xs text-red-700 dark:text-red-300 whitespace-pre-wrap break-words">
                <span className="font-medium">Error: </span>{error}
              </div>
            )}
            {resultText && (
              <div className={`text-xs ${colorClasses.text} whitespace-pre-wrap break-words max-h-40 overflow-y-auto`}>
                {resultText}
              </div>
            )}
            {hasStats && (
              <div className={`flex items-center gap-3 text-[10px] ${colorClasses.muted}`}>
                {inputTokens !== undefined && <span>In: {inputTokens.toLocaleString()} tokens</span>}
                {outputTokens !== undefined && <span>Out: {outputTokens.toLocaleString()} tokens</span>}
                {toolCalls !== undefined && <span>Tool calls: {toolCalls}</span>}
              </div>
            )}
            {duration && (
              <div className={`text-[10px] ${colorClasses.muted}`}>Duration: {duration}</div>
            )}
          </div>
        </details>
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
  if (prevProps.event.id !== nextProps.event.id ||
      prevProps.mode !== nextProps.mode ||
      prevProps.isApproving !== nextProps.isApproving ||
      prevProps.isCollapsed !== nextProps.isCollapsed ||
      prevProps.eventCount !== nextProps.eventCount) {
    return false
  }
  // For delegation_start events, also compare live stats so they re-render with updated tool counts/names
  if (prevProps.event.type === 'delegation_start' && prevProps.delegationStats !== nextProps.delegationStats) {
    return false
  }
  return true
})

// Event list component for displaying multiple events
// NOTE: Event filtering is now done on the backend based on event_mode
// Frontend no longer filters events - backend returns pre-filtered events
export const EventList: React.FC<{ 
  events: PollingEvent[]
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean
  compact?: boolean
  flatHierarchy?: boolean
}> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, isApproving, compact = false, flatHierarchy = false }) => {
  if (events.length === 0) {
    return <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-500 text-center ${compact ? 'py-2' : 'py-4'}`}>No events to display</div>
  }
  
  return (
    <EventHierarchy 
      events={events} 
      onApproveWorkflow={onApproveWorkflow}
      onSubmitFeedback={onSubmitFeedback}
      onFeedbackSubmitted={onFeedbackSubmitted}
      isApproving={isApproving}
      compact={compact}
      flatHierarchy={flatHierarchy}
    />
  )
})
