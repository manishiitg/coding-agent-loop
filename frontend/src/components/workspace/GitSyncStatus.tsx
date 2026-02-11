import { useState, useEffect, useCallback, useRef } from 'react'
import { RefreshCw, GitBranch, Pause, Play, ArrowUp } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { GitSyncStatus } from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

interface GitSyncStatusProps {
  onSync?: () => void
  isVisible?: boolean // Whether the workspace is visible (not minimized)
}

interface SyncAPIResponse {
  success: boolean
  message?: string
  error?: string
  data?: unknown
}

export default function GitSyncStatus({ onSync, isVisible = true }: GitSyncStatusProps) {
  const [status, setStatus] = useState<GitSyncStatus | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [syncing, setSyncing] = useState<boolean>(false)
  const [error, setError] = useState<string | null>(null)
  const [showDetails, setShowDetails] = useState<boolean>(false)
  const [commitMessage, setCommitMessage] = useState<string>('')
  const [showConflictResolution, setShowConflictResolution] = useState<boolean>(false)
  const [conflictError, setConflictError] = useState<string | null>(null)
  
  // Polling state
  const [isPolling, setIsPolling] = useState<boolean>(true)
  const [lastPollTime, setLastPollTime] = useState<Date | null>(null)
  const [pollingEnabled, setPollingEnabled] = useState<boolean>(true)
  const pollingIntervalRef = useRef<NodeJS.Timeout | null>(null)
  const lastStatusRef = useRef<GitSyncStatus | null>(null)

  // Fetch Git sync status with change detection
  const fetchStatus = useCallback(async (isPolling = false) => {
    try {
      if (!isPolling) setLoading(true)
      setError(null)
      const response = await agentApi.getGitSyncStatus()
      if (response.success && response.data) {
        const newStatus = response.data
        
        // Only update if status actually changed (for polling efficiency)
        if (isPolling && lastStatusRef.current) {
          const hasChanged = JSON.stringify(newStatus) !== JSON.stringify(lastStatusRef.current)
          if (!hasChanged) {
            setLastPollTime(new Date())
            return // No change, skip update
          }
        }
        
        setStatus(newStatus)
        lastStatusRef.current = newStatus
        setLastPollTime(new Date())
      } else {
        setError(response.message || 'Failed to fetch Git status')
      }
    } catch (err) {
      console.error('Failed to fetch Git status:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch Git status')
    } finally {
      if (!isPolling) setLoading(false)
    }
  }, [])

  // Manual sync with polling pause
  const handleSync = async () => {
    try {
      setIsPolling(false) // Pause polling during sync
      setSyncing(true)
      setError(null)
      setConflictError(null)
      setShowConflictResolution(false)
      
      const response = await agentApi.syncWithGitHub(false, commitMessage || undefined) as SyncAPIResponse
      if (response.success) {
        // Refresh status after sync
        await fetchStatus()
        onSync?.()
        setCommitMessage('') // Clear commit message after successful sync
      } else {
        // Extract error message from response
        const errorMsg = response.error || response.message || 'Sync failed'
        setError(errorMsg)
      }
    } catch (err: unknown) {
      console.error('Sync failed:', err)
      
      // Check if it's a conflict error (409 status)
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { status?: number; data?: { message?: string; error?: string } } }
        if (axiosError.response?.status === 409) {
          const conflictMsg = axiosError.response.data?.message || axiosError.response.data?.error || 'Merge conflicts detected'
          setConflictError(conflictMsg)
          setShowConflictResolution(true)
        } else {
          // Extract detailed error message from API response
          const errorMsg = axiosError.response?.data?.error || axiosError.response?.data?.message || (err instanceof Error ? err.message : 'Sync failed')
          setError(errorMsg)
        }
      } else {
        setError(err instanceof Error ? err.message : 'Sync failed')
      }
    } finally {
      setSyncing(false)
      setIsPolling(true) // Resume polling after sync
    }
  }

  // Force push local changes
  const handleForcePushLocal = async () => {
    try {
      setIsPolling(false)
      setSyncing(true)
      setError(null)
      setConflictError(null)
      setShowConflictResolution(false)
      
      const response = await agentApi.forcePushLocal(commitMessage || undefined) as SyncAPIResponse
      if (response.success) {
        await fetchStatus()
        onSync?.()
        setCommitMessage('')
      } else {
        const errorMsg = response.error || response.message || 'Force push failed'
        setError(errorMsg)
      }
    } catch (err) {
      console.error('Force push failed:', err)
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { data?: { error?: string; message?: string } } }
        const errorMsg = axiosError.response?.data?.error || axiosError.response?.data?.message || (err instanceof Error ? err.message : 'Force push failed')
        setError(errorMsg)
      } else {
        setError(err instanceof Error ? err.message : 'Force push failed')
      }
    } finally {
      setSyncing(false)
      setIsPolling(true)
    }
  }

  // Force pull remote changes
  const handleForcePullRemote = async () => {
    try {
      setIsPolling(false)
      setSyncing(true)
      setError(null)
      setConflictError(null)
      setShowConflictResolution(false)
      
      const response = await agentApi.forcePullRemote() as SyncAPIResponse
      if (response.success) {
        await fetchStatus()
        onSync?.()
        setCommitMessage('')
      } else {
        const errorMsg = response.error || response.message || 'Force pull failed'
        setError(errorMsg)
      }
    } catch (err) {
      console.error('Force pull failed:', err)
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { data?: { error?: string; message?: string } } }
        const errorMsg = axiosError.response?.data?.error || axiosError.response?.data?.message || (err instanceof Error ? err.message : 'Force pull failed')
        setError(errorMsg)
      } else {
        setError(err instanceof Error ? err.message : 'Force pull failed')
      }
    } finally {
      setSyncing(false)
      setIsPolling(true)
    }
  }

  // Load status on mount
  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  // Polling effect - only poll when visible and enabled
  useEffect(() => {
    if (isVisible && pollingEnabled && isPolling) {
      const interval = setInterval(() => {
        fetchStatus(true) // Pass true to indicate this is a polling call
      }, 60000) // 60 seconds
      
      pollingIntervalRef.current = interval
      
      return () => {
        if (interval) {
          clearInterval(interval)
          pollingIntervalRef.current = null
        }
      }
    } else {
      // Clear interval if not visible or polling disabled
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current)
        pollingIntervalRef.current = null
      }
    }
  }, [isVisible, pollingEnabled, isPolling, fetchStatus])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current)
      }
    }
  }, [])

  // Toggle polling
  const togglePolling = () => {
    setPollingEnabled(!pollingEnabled)
  }

  // Format time since last poll
  const getTimeSinceLastPoll = () => {
    if (!lastPollTime) return 'Never'
    const now = new Date()
    const diffMs = now.getTime() - lastPollTime.getTime()
    const diffSeconds = Math.floor(diffMs / 1000)
    
    if (diffSeconds < 60) return `${diffSeconds}s ago`
    const diffMinutes = Math.floor(diffSeconds / 60)
    if (diffMinutes < 60) return `${diffMinutes}m ago`
    const diffHours = Math.floor(diffMinutes / 60)
    return `${diffHours}h ago`
  }

  // Handle ESC key to close popup
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && showDetails) {
        setShowDetails(false)
      }
    }

    if (showDetails) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [showDetails])


  const getStatusColor = () => {
    if (loading || syncing) return 'text-blue-500'
    if (error) return 'text-red-500'
    if (!status?.is_connected) return 'text-yellow-500'
    if (status.pending_changes > 0) return 'text-orange-500'
    return 'text-green-500'
  }

  const getPollingStatus = () => {
    if (!isVisible) return 'Paused (hidden)'
    if (!pollingEnabled) return 'Disabled'
    if (syncing) return 'Paused (syncing)'
    return 'Active'
  }

  return (
    <TooltipProvider>
      <div className="flex items-center gap-2">
        {/* Git Sync Status - Settings Icon with Status Color */}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => setShowDetails(!showDetails)}
              className={`p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded transition-colors ${getStatusColor()} relative`}
            >
              {loading || syncing ? (
                <RefreshCw className="w-4 h-4 animate-spin" />
              ) : (
                <GitBranch className="w-4 h-4" />
              )}
              {/* Unpushed commits badge */}
              {!loading && !syncing && status && status.unpushed_commits > 0 && (
                <span className="absolute -top-2 -right-2 flex items-center justify-center min-w-[16px] h-4 px-0.5 text-[10px] font-bold text-white bg-orange-500 rounded-full animate-pulse">
                  {status.unpushed_commits > 9 ? '9+' : status.unpushed_commits}
                </span>
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <div className="text-center">
              <p className="font-medium">Git Sync Status</p>
              {status && (
                <div className="text-xs mt-1 space-y-1">
                  <p>Repo: {status.repository || 'Not configured'}</p>
                  <p>Branch: {status.branch || 'main'}</p>
                  <p>Status: {status.is_connected ? 'Connected' : 'Disconnected'}</p>
                  {status.pending_changes > 0 && (
                    <p>Pending: {status.pending_changes} changes</p>
                  )}
                  {status.unpushed_commits > 0 && (
                    <p className="text-orange-400">Unpushed: {status.unpushed_commits} commit{status.unpushed_commits !== 1 ? 's' : ''}</p>
                  )}
                  {status.file_statuses && status.file_statuses.length > 0 && (
                    <div className="mt-1">
                      <p className="text-xs">Files: {status.file_statuses.slice(0, 3).map(fs => `${fs.file} (${fs.status})`).join(', ')}</p>
                      {status.file_statuses.length > 3 && (
                        <p className="text-xs text-gray-500">... and {status.file_statuses.length - 3} more</p>
                      )}
                    </div>
                  )}
                  {status.last_sync && (
                    <p>Last sync: {new Date(status.last_sync).toLocaleString()}</p>
                  )}
                  <div className="mt-2 pt-1 border-t border-gray-200 dark:border-gray-600">
                    <p className="text-xs text-gray-500">
                      Auto-refresh: {getPollingStatus()}
                    </p>
                    <p className="text-xs text-gray-500">
                      Last checked: {getTimeSinceLastPoll()}
                    </p>
                  </div>
                </div>
              )}
            </div>
          </TooltipContent>
        </Tooltip>

        {/* Details Panel */}
        {showDetails && status && (
          <div className="absolute top-full right-0 mt-2 w-80 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50 p-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                  Git Sync Details
                </h3>
                <button
                  onClick={() => setShowDetails(false)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  ×
                </button>
              </div>

              <div className="space-y-2 text-xs">
                <div className="flex justify-between">
                  <span className="text-gray-600 dark:text-gray-400">Repository:</span>
                  <span className="text-gray-900 dark:text-gray-100 font-mono">
                    {status.repository || 'Not configured'}
                  </span>
                </div>
                
                <div className="flex justify-between">
                  <span className="text-gray-600 dark:text-gray-400">Branch:</span>
                  <span className="text-gray-900 dark:text-gray-100 font-mono">
                    {status.branch || 'main'}
                  </span>
                </div>
                
                <div className="flex justify-between">
                  <span className="text-gray-600 dark:text-gray-400">Status:</span>
                  <span className={`font-medium ${getStatusColor()}`}>
                    {status.is_connected ? 'Connected' : 'Disconnected'}
                  </span>
                </div>
                
                <div className="flex justify-between">
                  <span className="text-gray-600 dark:text-gray-400">Pending Changes:</span>
                  <span className="text-gray-900 dark:text-gray-100">
                    {status.pending_changes}
                  </span>
                </div>

                {status.unpushed_commits > 0 && (
                  <div className="flex justify-between items-center">
                    <span className="text-gray-600 dark:text-gray-400">Unpushed Commits:</span>
                    <span className="flex items-center gap-1 text-orange-500 font-medium">
                      <ArrowUp className="w-3 h-3" />
                      {status.unpushed_commits}
                    </span>
                  </div>
                )}
                
                {status.file_statuses && status.file_statuses.length > 0 && (
                  <div className="mt-3">
                    <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                      Pending Files ({status.file_statuses.length})
                    </h4>
                    <div className="max-h-32 overflow-y-auto space-y-1">
                      {status.file_statuses.map((fileStatus, index) => (
                        <div key={index} className="flex items-center justify-between text-xs bg-gray-50 dark:bg-gray-700 px-2 py-1 rounded">
                          <span className="font-mono text-gray-600 dark:text-gray-400 flex-1 truncate">
                            {fileStatus.file}
                          </span>
                          <div className="flex items-center gap-1 ml-2">
                            <span className={`px-1 py-0.5 rounded text-xs font-medium ${
                              fileStatus.status === 'Modified' ? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200' :
                              fileStatus.status === 'Added' ? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200' :
                              fileStatus.status === 'Deleted' ? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200' :
                              fileStatus.status === 'Untracked' ? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200' :
                              fileStatus.status === 'Renamed' ? 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200' :
                              'bg-gray-100 text-gray-800 dark:bg-gray-600 dark:text-gray-200'
                            }`}>
                              {fileStatus.status}
                            </span>
                            {fileStatus.staged && (
                              <span className="px-1 py-0.5 bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200 rounded text-xs">
                                Staged
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                
                {status.last_sync && (
                  <div className="flex justify-between">
                    <span className="text-gray-600 dark:text-gray-400">Last Sync:</span>
                    <span className="text-gray-900 dark:text-gray-100">
                      {new Date(status.last_sync).toLocaleString()}
                    </span>
                  </div>
                )}
              </div>

              {status.conflicts && status.conflicts.length > 0 && (
                <div className="mt-3">
                  <h4 className="text-xs font-semibold text-red-600 dark:text-red-400 mb-2">
                    Conflicts ({status.conflicts?.length || 0})
                  </h4>
                  <div className="space-y-1">
                    {status.conflicts?.map((conflict, index) => (
                      <div key={index} className="text-xs text-red-600 dark:text-red-400">
                        <div className="font-mono">{conflict.file}</div>
                        <div className="text-gray-500">{conflict.message}</div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Polling Status Section */}
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs font-semibold text-gray-700 dark:text-gray-300">
                    Auto-refresh Status
                  </span>
                  <div className="flex items-center gap-2">
                    <span className="text-xs text-gray-500">
                      {getTimeSinceLastPoll()}
                    </span>
                    <button
                      onClick={togglePolling}
                      className={`p-1 rounded transition-colors ${
                        pollingEnabled 
                          ? 'text-green-600 hover:bg-green-100 dark:hover:bg-green-900' 
                          : 'text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700'
                      }`}
                      title={pollingEnabled ? 'Disable auto-refresh' : 'Enable auto-refresh'}
                    >
                      {pollingEnabled ? (
                        <Pause className="w-3 h-3" />
                      ) : (
                        <Play className="w-3 h-3" />
                      )}
                    </button>
                  </div>
                </div>
                <div className="text-xs text-gray-500 mb-2">
                  Status: {getPollingStatus()}
                  {isVisible && pollingEnabled && !syncing && (
                    <span className="ml-1 text-green-600">•</span>
                  )}
                </div>
              </div>

              {/* Error Display */}
              {error && (
                <div className="mt-3 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded">
                  <div className="flex items-start gap-2">
                    <div className="w-2 h-2 bg-red-500 rounded-full mt-1.5 flex-shrink-0"></div>
                    <div className="flex-1">
                      <h4 className="text-xs font-semibold text-red-800 dark:text-red-200 mb-1">
                        Sync Error
                      </h4>
                      <p className="text-xs text-red-700 dark:text-red-300 whitespace-pre-wrap break-words">
                        {error}
                      </p>
                    </div>
                    <button
                      onClick={() => setError(null)}
                      className="text-red-400 hover:text-red-600 dark:hover:text-red-300 text-xs"
                      title="Dismiss error"
                    >
                      ×
                    </button>
                  </div>
                </div>
              )}

              {/* Commit Message Input */}
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <label className="block text-xs font-semibold text-gray-700 dark:text-gray-300 mb-1">
                  Commit Message (Optional)
                </label>
                <input
                  type="text"
                  value={commitMessage}
                  onChange={(e) => setCommitMessage(e.target.value)}
                  placeholder="Enter commit message..."
                  className="w-full px-2 py-1 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                />
              </div>

              <div className="flex gap-2 pt-2">
                <button
                  onClick={handleSync}
                  disabled={syncing}
                  className="flex-1 px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50 transition-colors"
                >
                  {syncing ? 'Syncing...' : 'Sync Now'}
                </button>
                <button
                  onClick={() => fetchStatus()}
                  disabled={loading}
                  className="px-3 py-1 text-xs bg-gray-500 text-white rounded hover:bg-gray-600 disabled:opacity-50 transition-colors"
                >
                  Refresh
                </button>
              </div>

              {/* Conflict Resolution Section */}
              {showConflictResolution && (
                <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded">
                  <div className="flex items-start gap-2 mb-3">
                    <div className="w-2 h-2 bg-red-500 rounded-full mt-1.5 flex-shrink-0"></div>
                    <div className="flex-1">
                      <h4 className="text-sm font-semibold text-red-800 dark:text-red-200 mb-1">
                        Merge Conflicts Detected
                      </h4>
                      <p className="text-xs text-red-700 dark:text-red-300 mb-3">
                        {conflictError || 'Please choose how to resolve the conflicts:'}
                      </p>
                    </div>
                  </div>
                  
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <button
                        onClick={handleForcePushLocal}
                        disabled={syncing}
                        className="flex-1 px-3 py-2 text-xs bg-orange-500 text-white rounded hover:bg-orange-600 disabled:opacity-50 transition-colors"
                      >
                        {syncing ? 'Pushing...' : 'Force Push Local'}
                      </button>
                      <button
                        onClick={handleForcePullRemote}
                        disabled={syncing}
                        className="flex-1 px-3 py-2 text-xs bg-purple-500 text-white rounded hover:bg-purple-600 disabled:opacity-50 transition-colors"
                      >
                        {syncing ? 'Pulling...' : 'Force Pull Remote'}
                      </button>
                    </div>
                    
                    <div className="text-xs text-red-600 dark:text-red-400 space-y-1">
                      <div><strong>Force Push Local:</strong> Overwrites GitHub with your local changes (discards remote changes)</div>
                      <div><strong>Force Pull Remote:</strong> Overwrites local with GitHub changes (discards local changes)</div>
                    </div>
                    
                    <button
                      onClick={() => {
                        setShowConflictResolution(false)
                        setConflictError(null)
                      }}
                      className="text-xs text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-200 underline"
                    >
                      Cancel
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
