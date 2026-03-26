import { useState, useEffect, useCallback } from 'react'
import { Monitor, Trash2, RefreshCw, X } from 'lucide-react'
import { agentApi } from '../../services/api'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

interface BrowserProcess {
  pid: number
  cpu: number
  mem_mb: number
  started_at: string
  user_data_dir: string
  type: string
}

interface BrowserSessionTracking {
  browser_session: string
  chat_session: string
  age: string
  idle: string
}

export default function BrowserProcesses() {
  const [processes, setProcesses] = useState<BrowserProcess[]>([])
  const [tracking, setTracking] = useState<BrowserSessionTracking[]>([])
  const [loading, setLoading] = useState(false)
  const [cleaning, setCleaning] = useState(false)
  const [showDetails, setShowDetails] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const fetchProcesses = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      // Fetch both process info (from workspace-api) and session tracking (from agent_go) in parallel
      const [procResponse, trackResponse] = await Promise.all([
        agentApi.getBrowserProcesses().catch(() => ({ success: false, processes: [] as BrowserProcess[], count: 0 })),
        agentApi.getBrowserSessionTracking().catch(() => ({ sessions: [] as BrowserSessionTracking[], count: 0 })),
      ])
      if (procResponse.success) {
        setProcesses(procResponse.processes || [])
      } else {
        setError('Failed to fetch browser processes')
      }
      setTracking(trackResponse.sessions || [])
    } catch (err) {
      console.error('Failed to fetch browser processes:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch')
    } finally {
      setLoading(false)
    }
  }, [])

  // Fetch on first open
  useEffect(() => {
    if (showDetails) {
      fetchProcesses()
    }
  }, [showDetails, fetchProcesses])

  // ESC to close
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && showDetails) {
        setShowDetails(false)
      }
    }
    if (showDetails) {
      document.addEventListener('keydown', handleKeyDown)
    }
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [showDetails])

  const handleCleanupAll = async () => {
    try {
      setCleaning(true)
      const response = await agentApi.cleanupBrowserProcesses()
      if (response.success) {
        await fetchProcesses()
      }
    } catch (err) {
      console.error('Cleanup failed:', err)
      setError(err instanceof Error ? err.message : 'Cleanup failed')
    } finally {
      setCleaning(false)
    }
  }

  const handleKillProcess = async (pid: number) => {
    try {
      await agentApi.cleanupBrowserProcesses([pid])
      await fetchProcesses()
    } catch (err) {
      console.error('Kill failed:', err)
    }
  }

  // Group processes by session (user_data_dir)
  const sessions = processes.reduce<Record<string, BrowserProcess[]>>((acc, proc) => {
    const key = proc.user_data_dir || 'unknown'
    if (!acc[key]) acc[key] = []
    acc[key].push(proc)
    return acc
  }, {})

  // Build a lookup from browser session name → tracking info
  const trackingBySession = new Map<string, BrowserSessionTracking>()
  for (const t of tracking) {
    trackingBySession.set(t.browser_session, t)
  }

  const sessionCount = Object.keys(sessions).length
  const trackedCount = tracking.length
  const totalCpu = processes.reduce((sum, p) => sum + p.cpu, 0)
  const totalMem = processes.reduce((sum, p) => sum + p.mem_mb, 0)

  return (
    <TooltipProvider>
      <div className="flex items-center gap-2">
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => setShowDetails(!showDetails)}
              className={`p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded transition-colors relative ${
                processes.length > 0 ? 'text-blue-500' : 'text-gray-400'
              }`}
            >
              <Monitor className="w-4 h-4" />
              {processes.length > 0 && (
                <span className="absolute -top-2 -right-2 flex items-center justify-center min-w-[16px] h-4 px-0.5 text-[10px] font-bold text-white bg-blue-500 rounded-full">
                  {sessionCount}
                </span>
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Browser Sessions ({sessionCount})</p>
          </TooltipContent>
        </Tooltip>

        {showDetails && (
          <div className="absolute top-full right-0 mt-1 w-80 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50 p-3">
            <div className="flex justify-between items-center mb-3">
              <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                Browser Sessions
              </h3>
              <div className="flex items-center gap-1">
                <button
                  onClick={fetchProcesses}
                  disabled={loading}
                  className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 disabled:opacity-50"
                  title="Refresh"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
                </button>
                <button
                  onClick={() => setShowDetails(false)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>

            {error && (
              <div className="mb-2 p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded text-xs text-red-700 dark:text-red-300">
                {error}
              </div>
            )}

            {loading && processes.length === 0 ? (
              <div className="text-xs text-gray-500 text-center py-4">Loading...</div>
            ) : processes.length === 0 ? (
              <div className="text-xs text-gray-500 text-center py-4">No browser sessions running</div>
            ) : (
              <>
                {/* Summary */}
                <div className="mb-3 p-2 bg-gray-50 dark:bg-gray-700/50 rounded text-xs space-y-1">
                  <div className="flex justify-between">
                    <span className="text-gray-600 dark:text-gray-400">Sessions:</span>
                    <span className="font-medium">{sessionCount} process{sessionCount !== 1 ? ' groups' : ' group'}{trackedCount > 0 ? ` (${trackedCount} tracked)` : ''}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-600 dark:text-gray-400">Processes:</span>
                    <span className="font-medium">{processes.length}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-600 dark:text-gray-400">CPU:</span>
                    <span className={`font-medium ${totalCpu > 50 ? 'text-red-500' : ''}`}>
                      {totalCpu.toFixed(1)}%
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-gray-600 dark:text-gray-400">Memory:</span>
                    <span className="font-medium">{totalMem.toFixed(0)} MB</span>
                  </div>
                </div>

                {/* Session list */}
                <div className="max-h-48 overflow-y-auto space-y-2 mb-3">
                  {Object.entries(sessions).map(([sessionId, procs]) => {
                    const sessionCpu = procs.reduce((s, p) => s + p.cpu, 0)
                    const sessionMem = procs.reduce((s, p) => s + p.mem_mb, 0)
                    const mainProc = procs.find(p => p.type === 'main')
                    const startedAt = mainProc?.started_at || procs[0]?.started_at || '?'

                    // Try to match this process group to a tracked session by user_data_dir
                    // The user_data_dir often contains the session name
                    const sessionName = sessionId.split('/').pop() || sessionId
                    const trackInfo = trackingBySession.get(sessionName)

                    return (
                      <div
                        key={sessionId}
                        className="p-2 bg-gray-50 dark:bg-gray-700/50 rounded text-xs"
                      >
                        <div className="flex justify-between items-start">
                          <div className="flex-1 min-w-0">
                            <div className="font-mono text-gray-700 dark:text-gray-300 truncate" title={sessionId}>
                              {sessionName.length > 20 ? sessionName.slice(0, 20) + '...' : sessionName}
                            </div>
                            <div className="text-gray-500 mt-0.5">
                              {procs.length} proc{procs.length > 1 ? 's' : ''} | CPU: {sessionCpu.toFixed(1)}% | {sessionMem.toFixed(0)} MB | started: {startedAt}
                            </div>
                            {trackInfo && (
                              <div className="text-blue-500 dark:text-blue-400 mt-0.5">
                                chat: {trackInfo.chat_session.slice(0, 8)}... | age: {trackInfo.age} | idle: {trackInfo.idle}
                              </div>
                            )}
                            {!trackInfo && (
                              <div className="text-orange-500 dark:text-orange-400 mt-0.5">
                                orphan (not tracked by any session)
                              </div>
                            )}
                          </div>
                          <button
                            onClick={() => {
                              const pids = procs.map(p => p.pid)
                              agentApi.cleanupBrowserProcesses(pids).then(() => fetchProcesses())
                            }}
                            className="ml-2 p-1 text-red-400 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/30 rounded"
                            title="Kill this session"
                          >
                            <Trash2 className="w-3 h-3" />
                          </button>
                        </div>
                      </div>
                    )
                  })}
                </div>

                {/* Tracked sessions without running processes (stale tracker entries) */}
                {tracking.filter(t => !Object.keys(sessions).some(s => s.split('/').pop() === t.browser_session)).length > 0 && (
                  <div className="mb-3">
                    <div className="text-[10px] font-medium text-orange-600 dark:text-orange-400 mb-1">Tracked but no process:</div>
                    {tracking.filter(t => !Object.keys(sessions).some(s => s.split('/').pop() === t.browser_session)).map(t => (
                      <div key={t.browser_session} className="text-[10px] text-gray-500 pl-2">
                        {t.browser_session} → chat: {t.chat_session.slice(0, 8)}... (age: {t.age})
                      </div>
                    ))}
                  </div>
                )}

                {/* Cleanup all button */}
                <button
                  onClick={handleCleanupAll}
                  disabled={cleaning}
                  className="w-full py-1.5 px-3 text-xs font-medium text-red-600 bg-red-50 hover:bg-red-100 dark:text-red-400 dark:bg-red-900/20 dark:hover:bg-red-900/40 rounded transition-colors disabled:opacity-50"
                >
                  {cleaning ? 'Cleaning up...' : `Kill All Sessions (${sessionCount})`}
                </button>
              </>
            )}
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
