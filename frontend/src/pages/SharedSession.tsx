import { useEffect, useState, useMemo } from 'react'
import { sessionShareApi, type SharedSessionResponse } from '../services/api'
import { Loader2, Share2, Clock, ExternalLink } from 'lucide-react'
import { EventList } from '../components/events'
import { shouldShowEventByMode } from '../components/events/eventModeUtils'
import type { PollingEvent } from '../services/api-types'
import '../components/events/EventHierarchy.css'

interface SharedSessionProps {
  shareToken: string
  onBack?: () => void
}

export function SharedSession({ shareToken, onBack }: SharedSessionProps) {
  const [session, setSession] = useState<SharedSessionResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const loadSession = async () => {
      try {
        setLoading(true)
        setError(null)
        const data = await sessionShareApi.getSharedSession(shareToken)
        setSession(data)
      } catch (err) {
        console.error('Failed to load shared session:', err)
        setError('Failed to load shared session. The link may be invalid or expired.')
      } finally {
        setLoading(false)
      }
    }

    loadSession()
  }, [shareToken])

  // Transform raw DB events and apply micro-mode filtering
  const events = useMemo(() => {
    if (!session?.events || !Array.isArray(session.events)) return []

    return session.events
      .map((raw: unknown, index: number) => {
        const r = raw as Record<string, unknown>
        return {
          id: (r.id as string) || `shared-event-${index}`,
          type: (r.type as string) || (r.event_type as string) || 'unknown',
          data: (r.data as Record<string, unknown>) || (r.event_data as Record<string, unknown>) || {},
          timestamp: (r.timestamp as string) || '',
          session_id: (r.session_id as string) || session.session_id,
          parent_id: (r.parent_id as string) || undefined,
        } as PollingEvent
      })
      .filter(e => shouldShowEventByMode(e.type || ''))
  }, [session])

  if (loading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="text-center">
          <Loader2 className="h-8 w-8 animate-spin mx-auto text-blue-500" />
          <p className="mt-4 text-gray-600 dark:text-gray-400">Loading shared session...</p>
        </div>
      </div>
    )
  }

  if (error) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
        <div className="max-w-md w-full p-8 bg-white dark:bg-gray-800 rounded-lg shadow-lg text-center">
          <div className="text-red-500 dark:text-red-400 text-6xl mb-4">!</div>
          <h2 className="text-xl font-bold text-gray-900 dark:text-white mb-2">
            Session Not Found
          </h2>
          <p className="text-gray-600 dark:text-gray-400 mb-4">
            {error}
          </p>
          {onBack && (
            <button
              onClick={onBack}
              className="text-blue-600 dark:text-blue-400 hover:underline"
            >
              Go back
            </button>
          )}
        </div>
      </div>
    )
  }

  if (!session) {
    return null
  }

  return (
    <div className="min-h-screen bg-gray-100 dark:bg-gray-900">
      {/* Header */}
      <div className="bg-white dark:bg-gray-800 shadow">
        <div className="px-4 sm:px-6 py-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-3">
              <Share2 className="h-5 w-5 text-blue-500" />
              <div>
                <h1 className="text-lg font-bold text-gray-900 dark:text-white">
                  {session.title || 'Shared Session'}
                </h1>
                <div className="flex items-center space-x-4 text-xs text-gray-500 dark:text-gray-400">
                  <span className="flex items-center">
                    <Clock className="h-3 w-3 mr-1" />
                    {new Date(session.created_at).toLocaleString()}
                  </span>
                  <span className="bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-400 px-2 py-0.5 rounded text-xs font-medium">
                    Read-only
                  </span>
                  <span className="text-gray-400">
                    {session.agent_mode}
                  </span>
                  <span className={`px-2 py-0.5 rounded text-xs font-medium ${
                    session.status === 'completed'
                      ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400'
                      : session.status === 'active'
                      ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-400'
                      : 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-400'
                  }`}>
                    {session.status}
                  </span>
                  <span className="text-gray-400">
                    {events.length} events
                  </span>
                </div>
              </div>
            </div>
            {onBack && (
              <button
                onClick={onBack}
                className="text-blue-600 dark:text-blue-400 hover:underline inline-flex items-center text-sm"
              >
                Return to app <ExternalLink className="h-3 w-3 ml-1" />
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Content - full width, minimal padding */}
      <div className="px-2 sm:px-4 py-2 h-[calc(100vh-64px)] overflow-y-auto">
        {events.length > 0 ? (
            <EventList
              events={events}
              compact
              flatHierarchy
            />
        ) : (
          <div className="text-center py-8 text-gray-500 dark:text-gray-400">
            No events to display
          </div>
        )}
      </div>
    </div>
  )
}
