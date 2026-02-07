import { useEffect, useState } from 'react'
import { sessionShareApi, type SharedSessionResponse } from '../services/api'
import { Loader2, Share2, Clock, ExternalLink } from 'lucide-react'

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
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-3">
              <Share2 className="h-6 w-6 text-blue-500" />
              <div>
                <h1 className="text-xl font-bold text-gray-900 dark:text-white">
                  {session.title || 'Shared Session'}
                </h1>
                <div className="flex items-center space-x-4 text-sm text-gray-500 dark:text-gray-400">
                  <span className="flex items-center">
                    <Clock className="h-4 w-4 mr-1" />
                    {new Date(session.created_at).toLocaleString()}
                  </span>
                  <span className="bg-yellow-100 dark:bg-yellow-900/30 text-yellow-800 dark:text-yellow-400 px-2 py-0.5 rounded text-xs font-medium">
                    Shared Session (Read-only)
                  </span>
                </div>
              </div>
            </div>
            <div className="flex items-center space-x-2">
              <span className="text-sm text-gray-500 dark:text-gray-400">
                Mode: <span className="font-medium">{session.agent_mode}</span>
              </span>
              <span className={`px-2 py-1 rounded text-xs font-medium ${
                session.status === 'completed'
                  ? 'bg-green-100 dark:bg-green-900/30 text-green-800 dark:text-green-400'
                  : session.status === 'active'
                  ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-800 dark:text-blue-400'
                  : 'bg-gray-100 dark:bg-gray-700 text-gray-800 dark:text-gray-400'
              }`}>
                {session.status}
              </span>
            </div>
          </div>
        </div>
      </div>

      {/* Content */}
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        <div className="bg-white dark:bg-gray-800 rounded-lg shadow overflow-hidden">
          <div className="p-4 border-b border-gray-200 dark:border-gray-700">
            <h2 className="text-lg font-medium text-gray-900 dark:text-white">
              Session Events
            </h2>
          </div>
          <div className="p-4 max-h-[calc(100vh-300px)] overflow-y-auto">
            {session.events && Array.isArray(session.events) && session.events.length > 0 ? (
              <div className="space-y-4">
                {session.events.map((event: unknown, index: number) => {
                  const e = event as Record<string, unknown>
                  return (
                    <div
                      key={index}
                      className="p-4 bg-gray-50 dark:bg-gray-700/50 rounded-lg"
                    >
                      <div className="flex items-center justify-between mb-2">
                        <span className="text-sm font-medium text-gray-900 dark:text-white">
                          {String(e.event_type || e.type || 'Event')}
                        </span>
                        <span className="text-xs text-gray-500 dark:text-gray-400">
                          {e.timestamp ? new Date(String(e.timestamp)).toLocaleTimeString() : ''}
                        </span>
                      </div>
                      <pre className="text-xs text-gray-600 dark:text-gray-300 overflow-x-auto whitespace-pre-wrap">
                        {JSON.stringify(e, null, 2)}
                      </pre>
                    </div>
                  )
                })}
              </div>
            ) : (
              <div className="text-center py-8 text-gray-500 dark:text-gray-400">
                No events to display
              </div>
            )}
          </div>
        </div>
      </div>

      {/* Footer */}
      <div className="fixed bottom-0 left-0 right-0 bg-white dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 py-3">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 text-center text-sm text-gray-500 dark:text-gray-400">
          This is a read-only view of a shared session.{' '}
          {onBack && (
            <button
              onClick={onBack}
              className="text-blue-600 dark:text-blue-400 hover:underline inline-flex items-center"
            >
              Return to app <ExternalLink className="h-3 w-3 ml-1" />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
