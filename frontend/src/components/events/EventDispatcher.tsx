import React from 'react'
import { Code2, Sparkles, Search, ChevronDown, ChevronRight } from 'lucide-react'
import type { PollingEvent } from '../../services/api-types'
import { EventHierarchy } from './EventHierarchy'
import { EventWithOrchestratorContext } from './common/EventWithOrchestratorContext'

/**
 * Node structure for hierarchical event rendering
 */
export interface EventNode {
  event: PollingEvent;
  children: EventNode[];
  level: number;
  isExpanded: boolean;
}

// Import the type-safe helpers from the new event-types module
import {
  isEventType,
  getEventData,
  type WorkflowStartEventData,
  type WorkflowProgressEventData,
  type WorkflowEndEventData,
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
  RoutingEvaluatedEventDisplay,
  PreValidationCompletedEventDisplay,
  TodoTaskRouteSelectedEventDisplay,
  TodoTaskItemCreatedEventDisplay,
  TodoTaskItemUpdatedEventDisplay,
  TodoTaskItemCompletedEventDisplay,
  TodoTaskStepCompletedEventDisplay,
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
  BrokenPipeEventDisplay,
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
  ContextEditingErrorEventDisplay
} from './debug'
import { UnifiedCompletionEventDisplay } from './debug/UnifiedCompletionEvent'
import { HumanVerificationDisplay } from './HumanVerificationDisplay'
import { BlockingHumanFeedbackDisplay } from './BlockingHumanFeedbackDisplay'
import { PlanApprovalDisplay } from './PlanApprovalDisplay'
import { BlockingHumanQuestionsDisplay } from './BlockingHumanQuestionsDisplay'
import { useChatStore } from '../../stores/useChatStore'
import { MarkdownRenderer } from '../ui/MarkdownRenderer'
import { CircularProgress } from '../ui/CircularProgress'
import { TooltipProvider } from '../ui/tooltip'

// Sub-agent live streaming text display (subscribes to delegation streaming store independently)
const DelegationStreamingCard: React.FC<{ delegationId: string }> = ({ delegationId }) => {
  const text = useChatStore(state => state.delegationStreamingText[delegationId] || '')
  if (!text) return null
  return (
    <div className="mt-2 border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-900/20 rounded p-2">
      <div className="flex items-center gap-1.5 mb-1">
        <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse" />
        <span className="text-[10px] text-blue-600 dark:text-blue-400 font-medium">
          Working...
        </span>
      </div>
      <div className="text-xs max-h-60 overflow-y-auto custom-scrollbar overscroll-y-contain">
        <MarkdownRenderer content={text} className="text-xs" />
        <span className="inline-block w-1.5 h-3 bg-blue-500 animate-pulse ml-0.5" />
      </div>
    </div>
  )
}

export interface DelegationStats {
  toolCalls: number
  inputTokens: number
  outputTokens: number
  latestToolName?: string
  latestToolLabel?: string
  completed?: boolean
  contextUsagePercent?: number
  contextWindowUsage?: number
  modelContextWindow?: number
  modelId?: string
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
  onSendMessage?: (msg: string) => void
  isApproving?: boolean
  isCollapsed?: boolean
  eventCount?: number
  onToggleCollapse?: () => void
  compact?: boolean
  delegationStats?: Map<string, DelegationStats>
  backgroundAgentStats?: Map<string, DelegationStats>
  // Hierarchy props for sub-agent log containment
  childrenNodes?: EventNode[]
  childrenCount?: number // Total children count (available even when collapsed)
  onToggleNode?: (eventId: string) => void
}

/**
 * Internal component to render the hierarchical logs of a sub-agent
 * in a simplified, non-virtualized list within a scrollable area.
 */
const MAX_SUBAGENT_CHILDREN = 20

// Event types grouped and collapsed inside sub-agent logs (mirrors EventHierarchy's TOOL_CALL_TYPES)
const SUB_AGENT_TOOL_CALL_TYPES = new Set(['tool_call_start', 'tool_call_end', 'tool_call_error', 'token_usage', 'llm_generation_end'])

const SubAgentHierarchy: React.FC<{
  nodes: EventNode[]
  onToggleNode: (eventId: string) => void
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  isApproving?: boolean
  delegationStats?: Map<string, DelegationStats>
  backgroundAgentStats?: Map<string, DelegationStats>
  compact?: boolean
}> = ({ nodes, onToggleNode, ...props }) => {
  const [showAll, setShowAll] = React.useState(false)
  const [expandedGroups, setExpandedGroups] = React.useState<Set<string>>(new Set())

  // Read hideToolCalls from active tab
  const hideToolCalls = useChatStore(state => {
    const tab = state.activeTabId ? state.chatTabs[state.activeTabId] : undefined
    return tab?.hideToolCalls ?? true
  })

  // Cap to most recent children to prevent unbounded DOM growth
  const isCapped = !showAll && nodes.length > MAX_SUBAGENT_CHILDREN
  const visibleNodes = isCapped ? nodes.slice(-MAX_SUBAGENT_CHILDREN) : nodes
  const hiddenCount = nodes.length - visibleNodes.length

  // Build grouped render list when hideToolCalls is on
  type RenderItem = { type: 'node'; node: EventNode } | { type: 'group'; groupKey: string; count: number; nodes: EventNode[] }
  const renderItems: RenderItem[] = React.useMemo(() => {
    if (!hideToolCalls) return visibleNodes.map(n => ({ type: 'node' as const, node: n }))

    const items: RenderItem[] = []
    let i = 0
    while (i < visibleNodes.length) {
      const node = visibleNodes[i]
      if (SUB_AGENT_TOOL_CALL_TYPES.has(node.event.type || '')) {
        const groupKey = node.event.id
        const groupNodes: EventNode[] = []
        let lastToolIdx = i
        let j = i
        while (j < visibleNodes.length) {
          const t = visibleNodes[j].event.type || ''
          if (SUB_AGENT_TOOL_CALL_TYPES.has(t)) { groupNodes.push(visibleNodes[j]); lastToolIdx = j; j++ }
          else break
        }
        items.push({ type: 'group', groupKey, count: groupNodes.length, nodes: groupNodes })
        i = lastToolIdx + 1
      } else {
        items.push({ type: 'node', node })
        i++
      }
    }
    return items
  }, [visibleNodes, hideToolCalls])

  const renderNode = (node: EventNode) => (
    <div key={node.event.id} className="relative group/node">
      <div className="flex items-start gap-1">
        {node.children.length > 0 ? (
          <button
            onClick={(e) => { e.preventDefault(); e.stopPropagation(); onToggleNode(node.event.id) }}
            className="mt-1.5 p-0.5 hover:bg-gray-100 dark:hover:bg-gray-800 rounded transition-colors flex-shrink-0"
          >
            {node.isExpanded ? <ChevronDown className="w-3 h-3 text-gray-400" /> : <ChevronRight className="w-3 h-3 text-gray-400" />}
          </button>
        ) : (
          <div className="w-4 flex-shrink-0" />
        )}
        <div className="flex-1 min-w-0">
          <EventDispatcher
            event={node.event}
            compact={true}
            {...props}
            onToggleNode={onToggleNode}
            childrenNodes={node.isExpanded ? node.children : undefined}
          />
        </div>
      </div>
      {node.isExpanded && node.children.length > 0 && node.event.type !== 'delegation_start' && (
        <div className="ml-2 pl-3 mt-1">
          <SubAgentHierarchy nodes={node.children} onToggleNode={onToggleNode} {...props} />
        </div>
      )}
    </div>
  )

  return (
    <div className="space-y-2">
      {isCapped && (
        <button
          onClick={() => setShowAll(true)}
          className="text-xs text-blue-500 hover:text-blue-600 dark:text-blue-400 dark:hover:text-blue-300 px-2 py-1"
        >
          Show {hiddenCount} older events...
        </button>
      )}
      {renderItems.map((item) => {
        if (item.type === 'node') return renderNode(item.node)

        // Tool call group sentinel
        const isExpanded = expandedGroups.has(item.groupKey)
        return (
          <div key={item.groupKey}>
            <button
              onClick={() => setExpandedGroups(prev => {
                const next = new Set(prev)
                if (next.has(item.groupKey)) next.delete(item.groupKey)
                else next.add(item.groupKey)
                return next
              })}
              className="px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
            >
              {isExpanded ? `− collapse` : `+ ${item.count} tool call${item.count !== 1 ? 's' : ''}`}
            </button>
            {isExpanded && item.nodes.map(n => renderNode(n))}
          </div>
        )
      })}
    </div>
  )
}

// Stable compact styling wrapper — defined outside EventDispatcher to prevent
// component identity changes on re-render (which would unmount children and lose state).
const CompactWrapper: React.FC<{ compact?: boolean; children: React.ReactNode }> = ({ compact, children }) => {
  if (!compact) return <>{children}</>
  return <div className="text-xs [&>*]:text-xs [&_h1]:!text-sm [&_h2]:!text-xs [&_h3]:!text-[11px] [&_p]:!text-xs [&_code]:!text-[10px] [&_span]:!text-xs [&_div]:!text-xs">{children}</div>
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
  onSendMessage,
  isApproving,
  isCollapsed,
  eventCount,
  onToggleCollapse,
  compact = false,
  delegationStats,
  backgroundAgentStats,
  childrenNodes,
  childrenCount,
  onToggleNode
}) => {
  // Ref for auto-scrolling sub-agent logs
  const scrollRef = React.useRef<HTMLDivElement>(null)
  const isAutoScrollingRef = React.useRef(false)
  const userScrolledUpRef = React.useRef(false)
  const prevChildrenLengthRef = React.useRef(0)

  // Handle scroll events to detect if user scrolled up manually
  const handleScroll = React.useCallback(() => {
    const div = scrollRef.current
    if (!div || isAutoScrollingRef.current) return
    
    const { scrollTop, scrollHeight, clientHeight } = div
    // User is considered "at bottom" if within 50px of the bottom (increased tolerance)
    const isAtBottom = scrollHeight - scrollTop - clientHeight < 50
    userScrolledUpRef.current = !isAtBottom
  }, [])

  // Auto-scroll to bottom when childrenNodes change (new events added)
  React.useEffect(() => {
    if (event.type === 'delegation_start' && childrenNodes && scrollRef.current) {
      const div = scrollRef.current
      
      const isFirstLoad = prevChildrenLengthRef.current === 0
      
      // Auto-scroll if it's the first load OR if user hasn't scrolled up away from bottom
      // This handles both new events AND streaming content updates within existing events
      if (isFirstLoad || !userScrolledUpRef.current) {
        isAutoScrollingRef.current = true
        
        const scroll = () => {
          if (div) div.scrollTop = div.scrollHeight
        }
        
        // Scroll immediately and after render frame for robustness
        scroll()
        requestAnimationFrame(() => {
          scroll()
          // Reset flag after delay to allow scroll event to fire without setting userScrolledUp
          setTimeout(() => {
            isAutoScrollingRef.current = false
          }, 100)
        })
      }
      prevChildrenLengthRef.current = childrenNodes.length
    }
  }, [event.type, childrenNodes]) // Re-run when children nodes array changes

  // Attach scroll listener
  React.useEffect(() => {
    const div = scrollRef.current
    if (div) {
      div.addEventListener('scroll', handleScroll)
      return () => div.removeEventListener('scroll', handleScroll)
    }
  }, [childrenNodes, handleScroll]) // Re-attach if childrenNodes causes re-render of the div

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
    return <CompactWrapper compact={compact}><AgentErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'llm_generation_with_retry')) {
    return <CompactWrapper compact={compact}><LLMGenerationWithRetryEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'agent_start')) {
    return <CompactWrapper compact={compact}><WithContext Component={AgentStartEventComponent} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'agent_end')) {
    return <CompactWrapper compact={compact}><WithContext Component={AgentEndEventComponent} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // MCP Server Events
  if (isEventType(event, 'mcp_server_selection')) {
    return <CompactWrapper compact={compact}><WithContext Component={MCPServerSelectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_discovery')) {
    return <CompactWrapper compact={compact}><WithContext Component={MCPServerDiscoveryEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_connection')) {
    return <CompactWrapper compact={compact}><WithContext Component={MCPServerConnectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'mcp_server_connection_error')) {
    return <CompactWrapper compact={compact}><WithContext Component={MCPServerConnectionEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Conversation Events
  if (isEventType(event, 'conversation_start')) {
    return <CompactWrapper compact={compact}><WithContext Component={ConversationStartEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_end')) {
    return <CompactWrapper compact={compact}><WithContext Component={ConversationEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_error')) {
    return <CompactWrapper compact={compact}><WithContext Component={ConversationErrorEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'conversation_turn')) {
    const data = getEventData(event)
    return (
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
        <EventWithOrchestratorContext metadata={data.metadata}>
          <LLMGenerationStartEventDisplay event={data} mode={compact ? 'compact' : mode} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'llm_generation_end')) {
    return <CompactWrapper compact={compact}><WithContext Component={LLMGenerationEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'llm_generation_error')) {
    const data = getEventData(event)
    return (
      <CompactWrapper compact={compact}>
        <EventWithOrchestratorContext metadata={data.metadata}>
          <LLMGenerationErrorEventDisplay event={data} mode={compact ? 'compact' : mode} />
        </EventWithOrchestratorContext>
      </CompactWrapper>
    )
  }

  // Tool Events
  // Note: delegate tool events are filtered out at EventHierarchy level
  if (isEventType(event, 'tool_call_start')) {
    return <CompactWrapper compact={compact}><WithContext Component={ToolCallStartEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_end')) {
    return <CompactWrapper compact={compact}><WithContext Component={ToolCallEndEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'tool_call_error')) {
    return <CompactWrapper compact={compact}><WithContext Component={ToolCallErrorEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // System Events
  if (isEventType(event, 'system_prompt')) {
    return <CompactWrapper compact={compact}><WithContext Component={SystemPromptEventDisplay} data={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (event.type === 'conversation_resumed') {
    const agentEvent = event.data as { data?: { previous_event_count?: number } } | undefined
    const count = agentEvent?.data?.previous_event_count ?? 0
    return (
      <div className={`flex items-center gap-2 ${compact ? 'py-1' : 'py-2'} ${compact ? 'text-[10px]' : 'text-xs'} text-gray-400 dark:text-gray-500`}>
        <div className="flex-1 border-t border-gray-200 dark:border-gray-700" />
        <span className="shrink-0 px-2">Previous conversation{count > 0 ? ` (${count} events)` : ''}</span>
        <div className="flex-1 border-t border-gray-200 dark:border-gray-700" />
      </div>
    )
  }
  if (isEventType(event, 'user_message')) {
    const data = getEventData(event)
    // Always render - UserMessageEventDisplay handles missing content gracefully
    // Log warning if content is missing for debugging
    if (!data.content) {
      console.warn('USERMSG_DEBUG - EventDispatcher - user_message event has no content, but rendering anyway', data)
    }
    return <CompactWrapper compact={compact}><WithContext Component={UserMessageEventDisplay} data={data} compact={compact} /></CompactWrapper>
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
        return <CompactWrapper compact={compact}><WithContext Component={UserMessageEventDisplay} data={eventData} compact={compact} /></CompactWrapper>
      }
    } catch (error) {
      console.error('USERMSG_DEBUG - EventDispatcher - Error in fallback handler', error, event)
    }
  }

  // Orchestrator Events
  if (isEventType(event, 'orchestrator_start')) {
    return <CompactWrapper compact={compact}><OrchestratorStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_end')) {
    return <CompactWrapper compact={compact}><OrchestratorEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_error')) {
    return <CompactWrapper compact={compact}><OrchestratorErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_agent_start')) {
    const data = getEventData(event)
    const liveStats = data.correlation_id ? delegationStats?.get(data.correlation_id) : undefined
    const childCount = childrenCount ?? 0
    const toolCount = liveStats?.toolCalls ?? 0
    const nonToolChildCount = Math.max(0, childCount - toolCount)
    return (
      <CompactWrapper compact={compact}>
        <OrchestratorAgentStartEventDisplay
          event={data}
          isCollapsed={isCollapsed}
          eventCount={eventCount}
          onToggleCollapse={onToggleCollapse}
          toolCallCount={liveStats?.toolCalls}
          latestToolLabel={liveStats?.latestToolLabel || liveStats?.latestToolName}
        />
        {!childrenNodes && (childrenCount ?? 0) > 0 && onToggleNode && (
          <div className="mt-1 ml-1">
            <button
              onClick={() => onToggleNode(event.id)}
              className="px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
            >
              <span className="font-medium">
                + {childCount} log{childCount !== 1 ? 's' : ''}
              </span>
              {toolCount > 0 && (
                <span className="truncate opacity-60">
                  • {toolCount} tool{toolCount !== 1 ? 's' : ''}
                  {nonToolChildCount > 0 ? `, ${nonToolChildCount} other` : ''}
                </span>
              )}
              {!toolCount && nonToolChildCount > 0 && (
                <span className="truncate opacity-60">• agent/background events</span>
              )}
            </button>
          </div>
        )}
        {/* Render children (tool calls) when agent has grouped events via correlation_id */}
        {childrenNodes && childrenNodes.length > 0 && onToggleNode && (
          <div className="mt-1 ml-1">
            <button
              onClick={() => onToggleNode(event.id)}
              className="mb-1 px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
            >
              <span className="font-medium">− collapse logs</span>
            </button>
            <div
              className="overflow-y-auto overflow-x-hidden pl-4 pr-1 py-1 custom-scrollbar break-words overscroll-y-contain"
              style={{ maxHeight: '50vh' }}
            >
              <SubAgentHierarchy
                nodes={childrenNodes}
                onToggleNode={onToggleNode}
                onApproveWorkflow={onApproveWorkflow}
                onSubmitFeedback={onSubmitFeedback}
                onFeedbackSubmitted={onFeedbackSubmitted}
                isApproving={isApproving}
                delegationStats={delegationStats}
                backgroundAgentStats={backgroundAgentStats}
                compact={true}
              />
            </div>
          </div>
        )}
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'orchestrator_agent_end')) {
    return <CompactWrapper compact={compact}><OrchestratorAgentEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'orchestrator_agent_error')) {
    return <CompactWrapper compact={compact}><OrchestratorAgentErrorEventDisplay event={getEventData(event)} /></CompactWrapper>
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

  if (event.type === 'blocking_human_questions') {
    const data = event.data as { data?: Record<string, unknown> } | undefined
    const payload = (data?.data || event.data) as Record<string, unknown>
    return (
      <BlockingHumanQuestionsDisplay
        event={{
          type: event.type,
          data: {
            request_id: (payload?.request_id as string) || `request_${Date.now()}`,
            questions: (payload?.questions as Array<{ id: string; question: string }>) || [],
            session_id: (payload?.session_id as string) || '',
          },
          timestamp: event.timestamp || new Date().toISOString()
        }}
        onSubmitFeedback={onSubmitFeedback}
        onFeedbackSubmitted={onFeedbackSubmitted}
      />
    )
  }

  // Plan Approval Event (non-blocking — sends response as chat message)
  if (event.type === 'plan_approval') {
    const data = event.data as { data?: Record<string, unknown> } | undefined
    const payload = (data?.data || event.data) as Record<string, unknown>
    return (
      <PlanApprovalDisplay
        event={{
          type: event.type,
          data: {
            question: (payload?.question as string) || 'Plan is ready for review.',
            context: (payload?.context as string) || '',
            yes_label: (payload?.yes_label as string) || 'Approve & Execute',
          },
          timestamp: event.timestamp || new Date().toISOString()
        }}
        onSendMessage={onSendMessage || (() => {})}
      />
    )
  }

  // Workflow Events
  if (isEventType(event, 'workflow_start')) {
    return <CompactWrapper compact={compact}><WorkflowStartEvent event={getEventData(event) as WorkflowStartEventData} /></CompactWrapper>
  }
  if (isEventType(event, 'workflow_progress')) {
    return <CompactWrapper compact={compact}><WorkflowProgressEvent event={getEventData(event) as WorkflowProgressEventData} /></CompactWrapper>
  }
  if (isEventType(event, 'workflow_end')) {
    return <CompactWrapper compact={compact}><WorkflowEndEvent event={getEventData(event) as WorkflowEndEventData} /></CompactWrapper>
  }
  // Batch execution events
  if (isEventType(event, 'batch_group_start')) {
    return <CompactWrapper compact={compact}><BatchGroupStartEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_group_end')) {
    return <CompactWrapper compact={compact}><BatchGroupEndEvent event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_start')) {
    return <CompactWrapper compact={compact}><BatchExecutionStartEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_end')) {
    return <CompactWrapper compact={compact}><BatchExecutionEndEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'batch_execution_canceled')) {
    return <CompactWrapper compact={compact}><BatchExecutionCanceledEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
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
      <CompactWrapper compact={compact}>
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
                {data.preferred_tier_label && (
                  <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-medium ${
                    data.preferred_tier === 1 ? 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300' :
                    data.preferred_tier === 2 ? 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300' :
                    'bg-green-100 dark:bg-green-900/50 text-green-700 dark:text-green-300'
                  }`}>
                    {data.preferred_tier_label}
                  </span>
                )}
              </div>
            )}
            {data.use_generic_agent && (
              <div className="flex items-center gap-2">
                <span className="text-gray-500 dark:text-gray-400">Agent:</span>
                <span className="text-purple-600 dark:text-purple-400">Generic Agent</span>
                {data.preferred_tier_label && (
                  <span className={`text-[10px] px-1.5 py-0.5 rounded-full font-medium ${
                    data.preferred_tier === 1 ? 'bg-purple-100 dark:bg-purple-900/50 text-purple-700 dark:text-purple-300' :
                    data.preferred_tier === 2 ? 'bg-blue-100 dark:bg-blue-900/50 text-blue-700 dark:text-blue-300' :
                    'bg-green-100 dark:bg-green-900/50 text-green-700 dark:text-green-300'
                  }`}>
                    {data.preferred_tier_label}
                  </span>
                )}
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
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
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
    return <CompactWrapper compact={compact}><TokenUsageEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'throttling_detected')) {
    return <CompactWrapper compact={compact}><ThrottlingDetectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'fallback_model_used')) {
    return <CompactWrapper compact={compact}><FallbackModelUsedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'fallback_attempt')) {
    return <CompactWrapper compact={compact}><FallbackAttemptEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'broken_pipe')) {
    return <CompactWrapper compact={compact}><BrokenPipeEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'token_limit_exceeded')) {
    return <CompactWrapper compact={compact}><TokenLimitExceededEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'large_tool_output_detected')) {
    return <CompactWrapper compact={compact}><LargeToolOutputDetectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'large_tool_output_file_written')) {
    return <CompactWrapper compact={compact}><LargeToolOutputFileWrittenEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'model_change')) {
    return <CompactWrapper compact={compact}><ModelChangeEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'max_turns_reached')) {
    return <CompactWrapper compact={compact}><MaxTurnsReachedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'context_cancelled')) {
    return <CompactWrapper compact={compact}><ContextCancelledEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Cache Events
  if (isEventType(event, 'cache_event')) {
    return <CompactWrapper compact={compact}><CacheEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'comprehensive_cache_event')) {
    return <CompactWrapper compact={compact}><ComprehensiveCacheEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Smart Routing Events
  if (isEventType(event, 'smart_routing_start')) {
    return <CompactWrapper compact={compact}><SmartRoutingStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'smart_routing_end')) {
    return <CompactWrapper compact={compact}><SmartRoutingEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Unified Completion Events
  if (isEventType(event, 'unified_completion')) {
    return <CompactWrapper compact={compact}><UnifiedCompletionEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Structured Output Events
  if (isEventType(event, 'structured_output_start')) {
    return <CompactWrapper compact={compact}><StructuredOutputStartEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'structured_output_end')) {
    return <CompactWrapper compact={compact}><StructuredOutputEndEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Context Summarization Events
  if (isEventType(event, 'context_summarization_started')) {
    return <CompactWrapper compact={compact}><ContextSummarizationStartedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_summarization_completed')) {
    return <CompactWrapper compact={compact}><ContextSummarizationCompletedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_summarization_error')) {
    return <CompactWrapper compact={compact}><ContextSummarizationErrorEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Context Editing Events
  if (isEventType(event, 'context_editing_completed')) {
    return <CompactWrapper compact={compact}><ContextEditingCompletedEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }
  if (isEventType(event, 'context_editing_error')) {
    return <CompactWrapper compact={compact}><ContextEditingErrorEventDisplay event={getEventData(event)} compact={compact} /></CompactWrapper>
  }

  // Planning Events
  if (isEventType(event, 'independent_steps_selected')) {
    return <CompactWrapper compact={compact}><IndependentStepsSelectedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'todo_steps_extracted')) {
    return <CompactWrapper compact={compact}><TodoStepsExtractedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }
  if (isEventType(event, 'variables_extracted')) {
    return <CompactWrapper compact={compact}><VariablesExtractedEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Step Token Usage Events
  if (isEventType(event, 'step_token_usage')) {
    return <CompactWrapper compact={compact}><StepTokenUsageEventDisplay event={getEventData(event)} /></CompactWrapper>
  }

  // Routing Evaluated Event
  if (isEventType(event, 'routing_evaluated')) {
    return (
      <CompactWrapper compact={compact}>
        <RoutingEvaluatedEventDisplay
          event={getEventData(event) as Record<string, unknown>}
          compact={compact}
        />
      </CompactWrapper>
    )
  }

  // Todo Task Events
  if (isEventType(event, 'todo_task_route_selected')) {
    return (
      <CompactWrapper compact={compact}>
        <TodoTaskRouteSelectedEventDisplay
          event={getEventData(event)}
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_created')) {
    return (
      <CompactWrapper compact={compact}>
        <TodoTaskItemCreatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_updated')) {
    return (
      <CompactWrapper compact={compact}>
        <TodoTaskItemUpdatedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_item_completed')) {
    return (
      <CompactWrapper compact={compact}>
        <TodoTaskItemCompletedEventDisplay 
          event={getEventData(event)} 
          compact={compact}
        />
      </CompactWrapper>
    )
  }
  if (isEventType(event, 'todo_task_step_completed')) {
    return (
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
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
      <CompactWrapper compact={compact}>
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
        servers?: string[]
        agent_template?: string
      }
      delegation_id?: string
      depth?: number
      instruction?: string
      reasoning_level?: string
      model_id?: string
      servers?: string[]
      agent_template?: string
      timestamp?: string
    }

    const delegationData = data?.data || data
    const instruction = delegationData?.instruction || 'No instruction provided'
    const delegationId = delegationData?.delegation_id
    const reasoningLevel = delegationData?.reasoning_level
    const modelId = delegationData?.model_id
    const servers = delegationData?.servers
    const agentTemplate = delegationData?.agent_template

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
      <CompactWrapper compact={compact}>
        <details className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded px-2 py-1.5 group">
          <summary className="flex items-center gap-2 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <span className="text-sm">🔀</span>
            <span className="text-[10px] text-purple-400 group-open:hidden">+</span>
            <span className="text-[10px] text-purple-400 hidden group-open:inline">−</span>
            <div className="text-xs font-medium text-purple-700 dark:text-purple-300 flex-1 truncate" title={instruction}>
              {instruction.length > 80 ? instruction.substring(0, 80) + '...' : instruction}
            </div>
            <div className="flex items-center gap-1.5 flex-shrink-0">
              {hasLiveStats && (
                <span className={`text-[10px] text-purple-500 dark:text-purple-400${isCompleted ? '' : ' animate-pulse'}`}>
                  {liveStats.toolCalls ? `${liveStats.toolCalls} tools` : ''}
                  {liveStats.latestToolLabel || liveStats.latestToolName ? ` · ${liveStats.latestToolLabel || liveStats.latestToolName}` : ''}
                  {liveStats.inputTokens ? ` · ${((liveStats.inputTokens + liveStats.outputTokens) / 1000).toFixed(1)}k tok` : ''}
                </span>
              )}
              {liveStats?.contextUsagePercent !== undefined && liveStats.contextUsagePercent > 0 && (
                <TooltipProvider>
                  <CircularProgress percentage={liveStats.contextUsagePercent} size={16} strokeWidth={2.5}
                    tokenUsage={{ context_usage_percent: liveStats.contextUsagePercent,
                      model_context_window: liveStats.modelContextWindow,
                      context_window_usage: liveStats.contextWindowUsage, model_id: liveStats.modelId }} />
                </TooltipProvider>
              )}
              {event.timestamp && !isCompleted && (
                <ElapsedTimer startTimestamp={event.timestamp} className="text-[10px] text-purple-500 dark:text-purple-400 animate-pulse font-mono" />
              )}
              {agentTemplate && (
                <span className="text-[10px] px-1.5 py-0.5 rounded font-medium bg-indigo-100 dark:bg-indigo-900/40 text-indigo-700 dark:text-indigo-300" title={`Agent template: ${agentTemplate}`}>
                  {agentTemplate}
                </span>
              )}
              {reasoningLevel && (
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${reasoningColors[reasoningLevel] || 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'}`}>
                  {reasoningLevel}
                </span>
              )}
              <span className="relative group/mode cursor-default flex items-center">
                <Code2 className="w-3.5 h-3.5 text-orange-500 dark:text-orange-400" />
                <span className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 px-2 py-1 rounded bg-gray-900 dark:bg-gray-100 text-white dark:text-gray-900 text-[10px] font-medium whitespace-nowrap opacity-0 pointer-events-none group-hover/mode:opacity-100 transition-opacity z-50">
                  Code Execution
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
            <div className="flex items-center gap-3 text-[10px] text-purple-500 dark:text-purple-400 flex-wrap">
              {agentTemplate && <span>Template: {agentTemplate}</span>}
              {reasoningLevel && <span>Reasoning: {reasoningLevel}</span>}
              {modelId && <span>Model: {modelId}</span>}
              {servers && servers.length > 0 && <span>Servers: {servers.join(', ')}</span>}
            </div>

            {hasLiveStats && (
              <div className="flex items-center gap-3 text-[10px] text-purple-500 dark:text-purple-400">
                {liveStats.inputTokens > 0 && <span>In: {liveStats.inputTokens.toLocaleString()} tokens</span>}
                {liveStats.outputTokens > 0 && <span>Out: {liveStats.outputTokens.toLocaleString()} tokens</span>}
                {liveStats.toolCalls > 0 && <span>Tool calls: {liveStats.toolCalls}</span>}
                {(liveStats.latestToolLabel || liveStats.latestToolName) && <span>Tool: {liveStats.latestToolLabel || liveStats.latestToolName}</span>}
                {event.timestamp && !isCompleted && (
                  <span>Elapsed: <ElapsedTimer startTimestamp={event.timestamp} className="font-mono" /></span>
                )}
              </div>
            )}
            {delegationId && !isCompleted && (
              <DelegationStreamingCard delegationId={delegationId} />
            )}
          </div>
        </details>

        {/* Tool calls toggle — show "+ N tool calls" when collapsed, full list when expanded */}
        {onToggleNode && !childrenNodes && (childrenCount ?? 0) > 0 && (
          <div className="mt-1 ml-1">
            <button
              onClick={() => onToggleNode(event.id)}
              className="px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors"
            >
              + {childrenCount} event{childrenCount !== 1 ? 's' : ''}
              {liveStats?.latestToolLabel || liveStats?.latestToolName ? ` · ${liveStats.latestToolLabel || liveStats.latestToolName}` : ''}
            </button>
          </div>
        )}
        {/* Hierarchical Execution Logs - Shown when expanded via hierarchy arrow */}
        {childrenNodes && childrenNodes.length > 0 && onToggleNode && (
          <div className="mt-1 ml-1">
            <button
              onClick={() => onToggleNode(event.id)}
              className="px-1.5 py-px text-[10px] leading-tight text-muted-foreground/60 hover:text-muted-foreground hover:bg-muted/30 rounded transition-colors mb-1"
            >
              − collapse
            </button>
            <div
              ref={scrollRef}
              className="overflow-y-auto overflow-x-hidden pl-4 pr-1 py-1 custom-scrollbar break-words overscroll-y-contain"
              style={{ maxHeight: '50vh' }}
            >
              <SubAgentHierarchy
                nodes={childrenNodes}
                onToggleNode={onToggleNode}
                onApproveWorkflow={onApproveWorkflow}
                onSubmitFeedback={onSubmitFeedback}
                onFeedbackSubmitted={onFeedbackSubmitted}
                isApproving={isApproving}
                delegationStats={delegationStats}
                backgroundAgentStats={backgroundAgentStats}
                compact={true}
              />
            </div>
          </div>
        )}
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
    const delegationId = delegationData?.delegation_id
    const endStats = delegationId ? delegationStats?.get(delegationId) : undefined

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
      <CompactWrapper compact={compact}>
        <details className={`${colorClasses.bg} border ${colorClasses.border} rounded px-2 py-1.5 group`}>
          <summary className="flex items-center gap-2 cursor-pointer list-none [&::-webkit-details-marker]:hidden">
            <span className="text-sm">{isSuccess ? '✅' : '❌'}</span>
            <span className={`text-[10px] ${colorClasses.muted} group-open:hidden`}>+</span>
            <span className={`text-[10px] ${colorClasses.muted} hidden group-open:inline`}>−</span>
            <div className={`text-xs font-medium flex-1 ${colorClasses.text}`}>
              {isSuccess ? 'Task completed' : 'Task failed'}
              {error && <span className="font-normal ml-1">- {error.length > 50 ? error.substring(0, 50) + '...' : error}</span>}
            </div>
            <div className="flex items-center gap-1.5 text-[10px] flex-shrink-0">
              {hasStats && (
                <span className={colorClasses.muted}>
                  {inputTokens ? `${((inputTokens + (outputTokens || 0)) / 1000).toFixed(1)}k tok` : ''}
                  {toolCalls ? ` · ${toolCalls} tools` : ''}
                </span>
              )}
              {endStats?.contextUsagePercent !== undefined && endStats.contextUsagePercent > 0 && (
                <TooltipProvider>
                  <CircularProgress percentage={endStats.contextUsagePercent} size={16} strokeWidth={2.5}
                    tokenUsage={{ context_usage_percent: endStats.contextUsagePercent,
                      model_context_window: endStats.modelContextWindow,
                      context_window_usage: endStats.contextWindowUsage, model_id: endStats.modelId }} />
                </TooltipProvider>
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
              <div className={`text-xs ${colorClasses.text} whitespace-pre-wrap break-words max-h-40 overflow-y-auto overscroll-y-contain`}>
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

  // Background Agent Started Event
  if (event.type === 'background_agent_started') {
    const data = event.data as {
      data?: { agent_id?: string; name?: string; instruction?: string; fields?: { agent_id?: string; name?: string; instruction?: string } }
      agent_id?: string
      name?: string
      instruction?: string
    }
    const fields = data?.data?.fields || data?.data || data
    const agentId = fields?.agent_id || ''
    const rawName = fields?.name || ''
    // Strip internal prefixes like "Planner: " for user-facing display
    const displayName = rawName.replace(/^Planner:\s*/i, '').trim() || 'Task'

    // Look up live stats via background agent ID → delegation stats mapping
    const liveStats = agentId ? backgroundAgentStats?.get(agentId) : undefined
    const hasLiveStats = liveStats && (liveStats.toolCalls > 0 || liveStats.inputTokens > 0)
    return (
      <CompactWrapper compact={compact}>
        <div className={`bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2">
            <span className="inline-block w-2 h-2 rounded-full bg-blue-500 animate-pulse" />
            <span className={`${compact ? 'text-xs' : 'text-sm'} font-medium text-blue-700 dark:text-blue-300`}>
              {displayName}
            </span>
            {hasLiveStats && (
              <span className="text-[10px] text-blue-500 dark:text-blue-400 animate-pulse">
                {liveStats.toolCalls ? `${liveStats.toolCalls} tools` : ''}
                {liveStats.latestToolLabel || liveStats.latestToolName ? ` · ${liveStats.latestToolLabel || liveStats.latestToolName}` : ''}
              </span>
            )}
            {!hasLiveStats && (
              <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-blue-500 dark:text-blue-400`}>
                in progress...
              </span>
            )}
            {liveStats?.contextUsagePercent !== undefined && liveStats.contextUsagePercent > 0 && (
              <TooltipProvider>
                <CircularProgress percentage={liveStats.contextUsagePercent} size={16} strokeWidth={2.5}
                  tokenUsage={{ context_usage_percent: liveStats.contextUsagePercent,
                    model_context_window: liveStats.modelContextWindow,
                    context_window_usage: liveStats.contextWindowUsage, model_id: liveStats.modelId }} />
              </TooltipProvider>
            )}
            {event.timestamp && (
              <ElapsedTimer startTimestamp={event.timestamp} className="text-[10px] text-blue-500 dark:text-blue-400 animate-pulse font-mono" />
            )}
          </div>
        </div>
      </CompactWrapper>
    )
  }

  // Background Agent Completed Event
  if (event.type === 'background_agent_completed') {
    const data = event.data as {
      data?: { agent_id?: string; name?: string; status?: string; result?: string; error?: string; duration?: string; fields?: { agent_id?: string; name?: string; status?: string; result?: string; error?: string; duration?: string } }
      agent_id?: string
      name?: string
      status?: string
      result?: string
      error?: string
      duration?: string
    }
    const fields = data?.data?.fields || data?.data || data
    const rawName = fields?.name || ''
    const displayName = rawName.replace(/^Planner:\s*/i, '').trim() || 'Task'
    const status = fields?.status || 'completed'
    const duration = fields?.duration || ''
    const result = fields?.result || ''
    const error = fields?.error || ''
    const isSuccess = status === 'completed'
    const isFailed = status === 'failed'

    const bgColor = isSuccess ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' :
                    isFailed ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800' :
                    'bg-gray-50 dark:bg-gray-900/20 border-gray-200 dark:border-gray-800'
    const textColor = isSuccess ? 'text-green-700 dark:text-green-300' :
                      isFailed ? 'text-red-700 dark:text-red-300' :
                      'text-gray-700 dark:text-gray-300'
    const dotColor = isSuccess ? 'bg-green-500' : isFailed ? 'bg-red-500' : 'bg-gray-400'
    const statusLabel = isSuccess ? 'completed' : isFailed ? 'failed' : status

    return (
      <CompactWrapper compact={compact}>
        <details className={`${bgColor} border rounded-md ${compact ? 'p-2' : 'p-3'}`}>
          <summary className="cursor-pointer flex items-center gap-2">
            <span className={`inline-block w-2 h-2 rounded-full ${dotColor}`} />
            <span className={`${compact ? 'text-xs' : 'text-sm'} font-medium ${textColor}`}>
              {displayName}
            </span>
            <span className={`${compact ? 'text-[10px]' : 'text-xs'} ${textColor} opacity-75`}>
              {statusLabel}{duration ? ` (${duration})` : ''}
            </span>
          </summary>
          <div className={`mt-2 ${compact ? 'text-[10px]' : 'text-xs'}`}>
            {error && (
              <div className="text-red-600 dark:text-red-400 whitespace-pre-wrap">{error}</div>
            )}
            {result && (
              <div className="text-gray-600 dark:text-gray-400 max-h-40 overflow-y-auto overscroll-y-contain">
                <MarkdownRenderer content={result} />
              </div>
            )}
          </div>
        </details>
      </CompactWrapper>
    )
  }

  // Background Agent Terminated Event
  if (event.type === 'background_agent_terminated') {
    const data = event.data as {
      data?: { agent_id?: string; name?: string; fields?: { agent_id?: string; name?: string } }
      agent_id?: string
      name?: string
    }
    const fields = data?.data?.fields || data?.data || data
    const rawName = fields?.name || ''
    const displayName = rawName.replace(/^Planner:\s*/i, '').trim() || 'Task'

    return (
      <CompactWrapper compact={compact}>
        <div className={`bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2">
            <span className="inline-block w-2 h-2 rounded-full bg-gray-400" />
            <span className={`${compact ? 'text-xs' : 'text-sm'} font-medium text-gray-500 dark:text-gray-400`}>
              {displayName}
            </span>
            <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-400 dark:text-gray-500`}>
              cancelled
            </span>
          </div>
        </div>
      </CompactWrapper>
    )
  }

  // Synthetic Turn Ready Event (shown when a background task has completed and results are being processed)
  if (event.type === 'synthetic_turn_ready') {
    return (
      <CompactWrapper compact={compact}>
        <div className={`bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2">
            <span className="inline-block w-2 h-2 rounded-full bg-blue-500 animate-pulse" />
            <span className={`${compact ? 'text-xs' : 'text-sm'} text-blue-700 dark:text-blue-300`}>
              Processing results...
            </span>
          </div>
        </div>
      </CompactWrapper>
    )
  }

  // Learn Code Script Execution Event
  if (event.type === 'learn_code_script_execution') {
    const wrapper = event.data as { data?: unknown } | undefined
    const d = (wrapper?.data || event.data) as {
      step_id: string; step_title: string; step_path: string
      script_path: string; script_content: string; success: boolean; exit_code: number
      output: string; error: string; fix_iteration: number; is_saved_script: boolean
    }
    const isSaved = d?.is_saved_script
    const success = d?.success
    const fixIter = d?.fix_iteration ?? 0
    let label: string
    if (isSaved) {
      label = '🐍 Script (saved)'
    } else if (fixIter === 0) {
      label = '🐍 Script (new)'
    } else {
      label = `🐍 Script (fix #${fixIter})`
    }
    const exitCode = d?.exit_code
    const exitLabel = exitCode == null || exitCode < 0 ? 'failed' : `exit ${exitCode}`
    const statusColor = success
      ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'
      : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
    const textColor = success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'
    const failDetail = !success ? (d?.error || d?.output) : null
    const successOutput = success ? d?.output : null
    return (
      <CompactWrapper compact={compact}>
        <div className={`border rounded-md ${statusColor} ${compact ? 'p-2' : 'p-3'}`}>
          <div className="flex items-center gap-2 flex-wrap">
            <span className={`${compact ? 'text-xs' : 'text-sm'} font-medium ${textColor}`}>{label}</span>
            <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400`}>{d?.step_title || d?.step_path}</span>
            {success
              ? <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400`}>✓ passed</span>
              : <span className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400`}>✗ {exitLabel}</span>
            }
          </div>
          {failDetail && (() => {
            const preview = failDetail.slice(0, 600)
            const isTruncated = failDetail.length > 600
            return (
              <div className="mt-1">
                <div className={`font-mono ${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 whitespace-pre-wrap break-all`}>
                  {preview}{isTruncated ? '…' : ''}
                </div>
                {isTruncated && (
                  <details className="mt-2">
                    <summary className={`cursor-pointer ${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400 select-none`}>
                      View full error
                    </summary>
                    <pre className={`mt-1 font-mono ${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 whitespace-pre-wrap break-all bg-red-50 dark:bg-red-950/20 rounded p-2 max-h-64 overflow-y-auto`}>
                      {failDetail}
                    </pre>
                  </details>
                )}
              </div>
            )
          })()}
          {successOutput && (
            <details className="mt-2">
              <summary className={`cursor-pointer ${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400 select-none`}>
                Output
              </summary>
              <pre className={`mt-1 font-mono ${compact ? 'text-[10px]' : 'text-xs'} text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-all bg-gray-50 dark:bg-gray-800 rounded p-2 max-h-64 overflow-y-auto`}>
                {successOutput}
              </pre>
            </details>
          )}
          {d?.script_content && (
            <details className="mt-1">
              <summary className={`cursor-pointer ${compact ? 'text-[10px]' : 'text-xs'} text-gray-500 dark:text-gray-400 select-none`}>
                View main.py
              </summary>
              <pre className={`mt-1 font-mono ${compact ? 'text-[10px]' : 'text-xs'} text-gray-700 dark:text-gray-300 whitespace-pre-wrap break-all bg-gray-50 dark:bg-gray-800 rounded p-2 max-h-64 overflow-y-auto`}>
                {d.script_content}
              </pre>
            </details>
          )}
        </div>
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
  // For delegation and background agent events, also compare live stats so they re-render
  if ((prevProps.event.type === 'delegation_start' || prevProps.event.type === 'delegation_end' ||
       prevProps.event.type === 'background_agent_started' || prevProps.event.type === 'background_agent_completed') && prevProps.delegationStats !== nextProps.delegationStats) {
    return false
  }
  if ((prevProps.event.type === 'background_agent_started') && prevProps.backgroundAgentStats !== nextProps.backgroundAgentStats) {
    return false
  }
  // Check if childrenNodes or childrenCount changed (for sub-agent hierarchy expansion)
  if (prevProps.childrenNodes !== nextProps.childrenNodes || prevProps.childrenCount !== nextProps.childrenCount) {
    return false
  }
  return true
})

// Event list component for displaying multiple events
// NOTE: Event filtering is now done on the backend
// Frontend no longer filters events - backend returns pre-filtered events
export const EventList: React.FC<{
  events: PollingEvent[]
  onApproveWorkflow?: (requestId: string) => void
  onSubmitFeedback?: (requestId: string, feedback: string) => void
  onFeedbackSubmitted?: () => void
  onSendMessage?: (msg: string) => void
  isApproving?: boolean
  compact?: boolean
  flatHierarchy?: boolean
  tabId?: string
}> = React.memo(({ events, onApproveWorkflow, onSubmitFeedback, onFeedbackSubmitted, onSendMessage, isApproving, compact = false, flatHierarchy = false, tabId }) => {
  if (events.length === 0) {
    return <div className={`${compact ? 'text-xs' : 'text-sm'} text-gray-500 text-center ${compact ? 'py-2' : 'py-4'}`}>No events to display</div>
  }

  return (
    <EventHierarchy
      events={events}
      onApproveWorkflow={onApproveWorkflow}
      onSubmitFeedback={onSubmitFeedback}
      onFeedbackSubmitted={onFeedbackSubmitted}
      onSendMessage={onSendMessage}
      isApproving={isApproving}
      compact={compact}
      flatHierarchy={flatHierarchy}
      tabId={tabId}
    />
  )
})
