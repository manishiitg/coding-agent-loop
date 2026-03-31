import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import ReactDOM from 'react-dom'
import { X, Radio } from 'lucide-react'
import { SSEConnection } from '../../services/sse'
import { EventList } from '../events'
import { shouldShowEventByMode } from '../events/eventModeUtils'
import { useChatStore } from '../../stores'
import type { PollingEvent, SSEEventMessage } from '../../services/api-types'
import { getTypedEventData } from '../../generated/event-types'

interface ScheduleLiveEventsPopupProps {
  sessionId: string
  jobName: string
  onClose: () => void
}

export default function ScheduleLiveEventsPopup({ sessionId, jobName, onClose }: ScheduleLiveEventsPopupProps) {
  const [allEvents, setAllEvents] = useState<PollingEvent[]>([])
  const [connected, setConnected] = useState(false)
  const [sessionStatus, setSessionStatus] = useState<string>('')
  const eventsEndRef = useRef<HTMLDivElement>(null)
  const appendStreamingChunk = useChatStore(state => state.appendStreamingChunk)

  // Match the same visibility rules the regular chat uses.
  const displayEvents = useMemo(
    () => allEvents.filter(e => !!e.type && e.type !== 'workspace_file_operation' && shouldShowEventByMode(e.type)),
    [allEvents]
  )

  const handleMessage = useCallback((msg: SSEEventMessage) => {
    if (msg.events && msg.events.length > 0) {
      setAllEvents(prev => [...prev, ...msg.events])

      for (const evt of msg.events) {
        if (evt.type === 'streaming_chunk') {
          const chunkData = getTypedEventData(evt, 'streaming_chunk')
          if (chunkData?.content) {
            appendStreamingChunk(sessionId, chunkData.chunk_index ?? -1, chunkData.content)
          }
        }
      }
    }
    if (msg.session_status) {
      setSessionStatus(msg.session_status)
    }
  }, [sessionId, appendStreamingChunk])

  useEffect(() => {
    const conn = new SSEConnection(sessionId, 0, {
      onMessage: handleMessage,
      onStatusUpdate: (msg) => {
        if (msg.session_status) setSessionStatus(msg.session_status)
      },
      onOpen: () => setConnected(true),
      onError: () => setConnected(false),
    })

    return () => {
      conn.close()
      useChatStore.getState().clearStreamingText?.(sessionId)
    }
  }, [sessionId, handleMessage])

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    eventsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [displayEvents.length])

  const popup = (
    <div
      className="fixed inset-0 flex items-center justify-center bg-black/50"
      style={{ zIndex: 9999 }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl w-[90vw] max-w-5xl h-[80vh] flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/50">
          <div className="flex items-center gap-3">
            <Radio className={`w-4 h-4 ${connected ? 'text-green-500 animate-pulse' : 'text-gray-400'}`} />
            <div>
              <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                Live Execution — {jobName}
              </h3>
              <p className="text-xs text-gray-500 dark:text-gray-400">
                Session: <span className="font-mono">{sessionId}</span>
                {sessionStatus && <span className="ml-2 text-amber-500">({sessionStatus})</span>}
                <span className="ml-2">{displayEvents.length} events ({allEvents.length} total)</span>
              </p>
            </div>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700">
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Events */}
        <div className="flex-1 overflow-y-auto p-4">
          {displayEvents.length === 0 ? (
            <div className="flex items-center justify-center h-full text-sm text-gray-400">
              {!connected
                ? 'Connecting...'
                : sessionStatus === 'error' || sessionStatus === 'stopped'
                ? 'Run ended — no events recorded'
                : sessionStatus === 'completed'
                ? 'Run completed — no events recorded'
                : 'Waiting for events...'}
            </div>
          ) : (
            <>
              <EventList
                events={displayEvents}
                compact
                flatHierarchy={false}
              />
              <div ref={eventsEndRef} />
            </>
          )}
        </div>
      </div>
    </div>
  )

  return ReactDOM.createPortal(popup, document.body)
}
