import React, { useRef, useEffect, useState, useCallback } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Wrench,
  MessageSquare,
  AlertCircle,
  CheckCircle,
  Loader2,
  Eye,
  EyeOff,
  Trash2
} from 'lucide-react'
import { Virtuoso, type VirtuosoHandle } from 'react-virtuoso'
import type { PollingEvent } from '../../services/api-types'

interface EventViewerProps {
  events: PollingEvent[]
  isRunning: boolean
  onClear?: () => void
  className?: string
}

// Event type colors and icons
const eventConfig: Record<string, { icon: React.ReactNode; color: string; label: string }> = {
  tool_call_start: { 
    icon: <Wrench className="w-3.5 h-3.5" />, 
    color: 'text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-800', 
    label: 'Tool Call' 
  },
  tool_call_end: { 
    icon: <CheckCircle className="w-3.5 h-3.5" />, 
    color: 'text-green-500 bg-green-50 dark:bg-green-900/20', 
    label: 'Tool Complete' 
  },
  tool_call_error: { 
    icon: <AlertCircle className="w-3.5 h-3.5" />, 
    color: 'text-red-500 bg-red-50 dark:bg-red-900/20', 
    label: 'Tool Error' 
  },
  llm_generation_start: { 
    icon: <MessageSquare className="w-3.5 h-3.5" />, 
    color: 'text-purple-500 bg-purple-50 dark:bg-purple-900/20', 
    label: 'LLM Start' 
  },
  llm_generation_end: { 
    icon: <MessageSquare className="w-3.5 h-3.5" />, 
    color: 'text-purple-500 bg-purple-50 dark:bg-purple-900/20', 
    label: 'LLM Response' 
  },
  agent_start: { 
    icon: <Loader2 className="w-3.5 h-3.5" />, 
    color: 'text-indigo-500 bg-indigo-50 dark:bg-indigo-900/20', 
    label: 'Agent Start' 
  },
  agent_end: { 
    icon: <CheckCircle className="w-3.5 h-3.5" />, 
    color: 'text-indigo-500 bg-indigo-50 dark:bg-indigo-900/20', 
    label: 'Agent End' 
  },
  workflow_end: { 
    icon: <CheckCircle className="w-3.5 h-3.5" />, 
    color: 'text-green-500 bg-green-50 dark:bg-green-900/20', 
    label: 'Automation Complete'
  },
  orchestrator_step_start: { 
    icon: <Loader2 className="w-3.5 h-3.5 animate-spin" />, 
    color: 'text-indigo-500 bg-indigo-50 dark:bg-indigo-900/20', 
    label: 'Step Start' 
  },
  orchestrator_step_end: { 
    icon: <CheckCircle className="w-3.5 h-3.5" />, 
    color: 'text-indigo-500 bg-indigo-50 dark:bg-indigo-900/20', 
    label: 'Step End' 
  },
  request_human_feedback: { 
    icon: <AlertCircle className="w-3.5 h-3.5" />, 
    color: 'text-amber-500 bg-amber-50 dark:bg-amber-900/20', 
    label: 'Feedback Required' 
  }
}

const defaultEventConfig = {
  icon: <MessageSquare className="w-3.5 h-3.5" />,
  color: 'text-gray-500 bg-gray-50 dark:bg-gray-800',
  label: 'Event'
}

// Individual event item component
const EventItem: React.FC<{ event: PollingEvent }> = ({ event }) => {
  const [expanded, setExpanded] = useState(false)
  const eventType = event.type || 'unknown'
  const config = eventConfig[eventType] || defaultEventConfig

  // Extract relevant info from event data
  const getEventSummary = () => {
    const data = event.data
    if (!data) return null

    if (eventType === 'tool_call_start' || eventType === 'tool_call_end') {
      const toolData = data as { tool_name?: string; tool_call?: { name?: string } }
      const toolName = toolData.tool_name || toolData.tool_call?.name
      return toolName ? `${toolName}` : null
    }

    if (eventType === 'llm_generation_end') {
      const llmData = data as { content?: string; result?: string }
      const content = llmData.content || llmData.result
      if (content && typeof content === 'string') {
        return content.length > 100 ? `${content.substring(0, 100)}...` : content
      }
    }

    if (eventType === 'orchestrator_step_start' || eventType === 'orchestrator_step_end') {
      const stepData = data as { step_title?: string; step_id?: string }
      return stepData.step_title || stepData.step_id
    }

    return null
  }

  const summary = getEventSummary()
  const timestamp = event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : ''

  return (
    <div className="border-b border-gray-100 dark:border-gray-800 last:border-b-0">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-start gap-2 p-2 hover:bg-gray-50 dark:hover:bg-gray-800/50 transition-colors text-left"
      >
        <div className={`flex-shrink-0 p-1 rounded ${config.color}`}>
          {config.icon}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
              {config.label}
            </span>
            <span className="text-xs text-gray-400 dark:text-gray-500">
              {timestamp}
            </span>
          </div>
          {summary && (
            <p className="text-xs text-gray-500 dark:text-gray-400 truncate mt-0.5">
              {summary}
            </p>
          )}
        </div>
        <div className="flex-shrink-0 text-gray-400 dark:text-gray-500">
          {expanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
        </div>
      </button>

      {/* Expanded content */}
      {expanded && event.data && (
        <div className="px-2 pb-2">
          <pre className="text-xs bg-gray-100 dark:bg-gray-800 p-2 rounded overflow-x-auto max-h-40 overflow-y-auto">
            {JSON.stringify(event.data, null, 2)}
          </pre>
        </div>
      )}
    </div>
  )
}

export const EventViewer: React.FC<EventViewerProps> = ({
  events,
  isRunning,
  onClear,
  className = ''
}) => {
  const virtuosoRef = useRef<VirtuosoHandle>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const [autoScroll, setAutoScroll] = useState(true)
  const [showFilters, setShowFilters] = useState(false)
  const [hiddenTypes, setHiddenTypes] = useState<Set<string>>(new Set())

  // Auto-scroll via Virtuoso followOutput
  const atBottomStateChange = useCallback((atBottom: boolean) => {
    setAutoScroll(atBottom)
  }, [])

  // Toggle event type visibility
  const toggleEventType = (type: string) => {
    setHiddenTypes(prev => {
      const next = new Set(prev)
      if (next.has(type)) {
        next.delete(type)
      } else {
        next.add(type)
      }
      return next
    })
  }

  // Filter events - handle both undefined and known types
  const filteredEvents = events.filter(e => {
    const eventType = e.type || 'unknown'
    return !hiddenTypes.has(eventType)
  })

  // Get unique event types for filter
  const eventTypes = Array.from(new Set(events.map(e => e.type || 'unknown')))

  const renderItem = useCallback((_index: number, event: PollingEvent) => (
    <EventItem event={event} />
  ), [])

  return (
    <div className={`flex flex-col h-full bg-white dark:bg-gray-900 ${className}`}>
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Events
          </span>
          {isRunning && (
            <div className="flex items-center gap-1">
              <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse" />
              <span className="text-xs text-green-600 dark:text-green-400">Live</span>
            </div>
          )}
          <span className="text-xs text-gray-400 dark:text-gray-500">
            ({filteredEvents.length})
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => setShowFilters(!showFilters)}
            className={`p-1.5 rounded transition-colors ${
              showFilters
                ? 'bg-gray-200 dark:bg-gray-700 text-gray-900 dark:text-gray-100'
                : 'hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 dark:text-gray-400'
            }`}
            title="Filter events"
          >
            {showFilters ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
          </button>
          {onClear && (
            <button
              onClick={onClear}
              className="p-1.5 rounded hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-500 dark:text-gray-400 transition-colors"
              title="Clear events"
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
          )}
        </div>
      </div>

      {/* Filters */}
      {showFilters && (
        <div className="px-3 py-2 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
          <div className="flex flex-wrap gap-1">
            {eventTypes.map(type => {
              const typeKey = type || 'unknown'
              const config = eventConfig[typeKey] || defaultEventConfig
              const isHidden = hiddenTypes.has(typeKey)
              return (
                <button
                  key={typeKey}
                  onClick={() => toggleEventType(typeKey)}
                  className={`flex items-center gap-1 px-2 py-0.5 rounded text-xs transition-colors ${
                    isHidden
                      ? 'bg-gray-200 dark:bg-gray-700 text-gray-400 dark:text-gray-500 line-through'
                      : config.color
                  }`}
                >
                  {config.icon}
                  {config.label}
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Events list — virtualized */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto relative">
        {filteredEvents.length === 0 ? (
          <div className="flex flex-col items-center justify-center h-full text-center p-4">
            <MessageSquare className="w-8 h-8 text-gray-300 dark:text-gray-600 mb-2" />
            <span className="text-sm text-gray-500 dark:text-gray-400">
              {events.length === 0 ? 'No events yet' : 'All events filtered'}
            </span>
            <span className="text-xs text-gray-400 dark:text-gray-500 mt-1">
              {events.length === 0 ? 'Events will appear here when the automation runs' : 'Adjust filters to see events'}
            </span>
          </div>
        ) : (
          <Virtuoso
            ref={virtuosoRef}
            data={filteredEvents}
            customScrollParent={scrollRef.current || undefined}
            followOutput="smooth"
            atBottomStateChange={atBottomStateChange}
            increaseViewportBy={200}
            itemContent={renderItem}
          />
        )}

        {/* Auto-scroll indicator */}
        {!autoScroll && events.length > 0 && (
          <button
            onClick={() => {
              setAutoScroll(true)
              virtuosoRef.current?.scrollToIndex({ index: filteredEvents.length - 1, behavior: 'smooth' })
            }}
            className="sticky bottom-4 left-1/2 -translate-x-1/2 flex items-center gap-1 px-3 py-1.5 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs rounded-full shadow-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
          >
            <ChevronDown className="w-3 h-3" />
            Scroll to latest
          </button>
        )}
      </div>
    </div>
  )
}

export default EventViewer
