import { useState, useEffect, useCallback, useRef } from 'react'
import { RefreshCw, Search, Pause, Play, Database, Brain, FileText } from 'lucide-react'
import { agentApi } from '../../services/api'
import type { SemanticSearchStatus, SemanticJobStatus } from '../../services/api-types'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'

interface SemanticSearchSyncProps {
  onResync?: () => void
  isVisible?: boolean // Whether the workspace is visible (not minimized)
}

export default function SemanticSearchSync({ onResync, isVisible = true }: SemanticSearchSyncProps) {
  const [status, setStatus] = useState<SemanticSearchStatus | null>(null)
  const [jobStatus, setJobStatus] = useState<SemanticJobStatus | null>(null)
  const [loading, setLoading] = useState<boolean>(true)
  const [syncing, setSyncing] = useState<boolean>(false)
  const [error, setError] = useState<string | null>(null)
  const [showDetails, setShowDetails] = useState<boolean>(false)
  const [dryRun, setDryRun] = useState<boolean>(false)
  const [force, setForce] = useState<boolean>(false)
  
  // Search functionality
  const [searchQuery, setSearchQuery] = useState<string>('')
  const [searchResults, setSearchResults] = useState<Array<{file_path?: string; name?: string; score?: number}>>([])
  const [searchLoading, setSearchLoading] = useState<boolean>(false)
  const [searchError, setSearchError] = useState<string | null>(null)
  const [showSearchResults, setShowSearchResults] = useState<boolean>(false)
  
  // Polling state
  const [isPolling, setIsPolling] = useState<boolean>(true)
  const [lastPollTime, setLastPollTime] = useState<Date | null>(null)
  const [pollingEnabled, setPollingEnabled] = useState<boolean>(true)
  const [isSemanticSearchDisabled, setIsSemanticSearchDisabled] = useState<boolean>(false)
  const pollingIntervalRef = useRef<NodeJS.Timeout | null>(null)
  const lastStatusRef = useRef<SemanticSearchStatus | null>(null)

  // Fetch semantic search status with change detection
  const fetchStatus = useCallback(async (isPolling = false) => {
    try {
      if (!isPolling) setLoading(true)
      setError(null)
      
      // First, fetch status to check if semantic search is disabled
      const statusResponse = await agentApi.getSemanticSearchStatus()
      
      if (statusResponse.success && statusResponse.data) {
        const newStatus = statusResponse.data
        
        // Handle disabled semantic search response
        if (newStatus.enabled === false) {
          // Create a mock status for disabled semantic search
          const disabledStatus: SemanticSearchStatus = {
            services: {
              qdrant: { available: false },
              embedding: { 
                available: false,
                model: {
                  available: false,
                  enabled: false,
                  model: 'disabled',
                  provider: 'disabled'
                }
              }
            },
            jobs: {
              job_stats: {
                completed: 0,
                pending: 0,
                processing: 0,
                failed: 0
              },
              running: false,
              worker_count: 0
            },
            timestamp: Date.now()
          }
          
          // Create mock job status for disabled state
          const disabledJobStatus: SemanticJobStatus = {
            job_stats: {
              completed: 0,
              pending: 0,
              processing: 0,
              failed: 0
            },
            running: false,
            worker_count: 0
          }
          
          setStatus(disabledStatus)
          setJobStatus(disabledJobStatus)
          lastStatusRef.current = disabledStatus
          setLastPollTime(new Date())
          setIsSemanticSearchDisabled(true) // Stop polling when disabled
          return // Skip jobs API call when disabled
        }
        
        // If we get here, semantic search is enabled - fetch jobs API
        setIsSemanticSearchDisabled(false)
        
        // Fetch job status only when semantic search is enabled
        const jobResponse = await agentApi.getSemanticJobStatus()
        
        if (jobResponse.success && jobResponse.data) {
          // Handle disabled semantic search response for job status (shouldn't happen if status is enabled, but handle it anyway)
          if (jobResponse.data.enabled === false) {
            const disabledJobStatus: SemanticJobStatus = {
              job_stats: {
                completed: 0,
                pending: 0,
                processing: 0,
                failed: 0
              },
              running: false,
              worker_count: 0
            }
            setJobStatus(disabledJobStatus)
          } else {
            setJobStatus(jobResponse.data)
          }
        }
        
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
        setError(statusResponse.message || 'Failed to fetch semantic search status')
      }
    } catch (err) {
      console.error('Failed to fetch semantic search status:', err)
      setError(err instanceof Error ? err.message : 'Failed to fetch semantic search status')
    } finally {
      if (!isPolling) setLoading(false)
    }
  }, [])

  // Manual resync with polling pause
  const handleResync = async () => {
    try {
      setIsPolling(false) // Pause polling during resync
      setSyncing(true)
      setError(null)
      
      const response = await agentApi.triggerSemanticResync(dryRun, force)
      if (response.success) {
        // Refresh status after resync
        await fetchStatus()
        onResync?.()
      } else {
        setError(response.message || 'Resync failed')
      }
    } catch (err: unknown) {
      console.error('Resync failed:', err)
      setError(err instanceof Error ? err.message : 'Resync failed')
    } finally {
      setSyncing(false)
      setIsPolling(true) // Resume polling after resync
    }
  }

  // Perform semantic search
  const handleSearch = async () => {
    if (!searchQuery.trim()) return
    
    try {
      setSearchLoading(true)
      setSearchError(null)
      
      const response = await agentApi.searchSemanticDocuments({
        query: searchQuery,
        folder: '',
        limit: 10,
        include_regex: false,
        regex_limit: 0
      })
      
      if (response.success && response.data) {
        // Handle disabled semantic search response
        if (response.data.search_method === 'disabled') {
          setSearchResults([])
          setShowSearchResults(true)
          setSearchError('Semantic search is disabled')
          return
        }
        
        // Handle both semantic and regex results from hybrid search
        const results = response.data.semantic_results || response.data.results || []
        setSearchResults(results)
        setShowSearchResults(true)
      } else {
        setSearchError(response.message || 'Semantic search failed')
      }
    } catch (err: unknown) {
      console.error('Semantic search failed:', err)
      setSearchError(err instanceof Error ? err.message : 'Semantic search failed')
    } finally {
      setSearchLoading(false)
    }
  }

  // Load status on mount
  useEffect(() => {
    fetchStatus()
  }, [fetchStatus])

  // Polling effect - only poll when visible, enabled, and semantic search is not disabled
  useEffect(() => {
    if (isVisible && pollingEnabled && isPolling && !isSemanticSearchDisabled) {
      const interval = setInterval(() => {
        fetchStatus(true) // Pass true to indicate this is a polling call
      }, 10000) // 10 seconds for more frequent updates
      
      pollingIntervalRef.current = interval
      
      return () => {
        if (interval) {
          clearInterval(interval)
          pollingIntervalRef.current = null
        }
      }
    } else {
      // Clear interval if not visible, polling disabled, or semantic search is disabled
      if (pollingIntervalRef.current) {
        clearInterval(pollingIntervalRef.current)
        pollingIntervalRef.current = null
      }
    }
  }, [isVisible, pollingEnabled, isPolling, isSemanticSearchDisabled, fetchStatus])

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
    if (!status?.services?.qdrant?.available || !status?.services?.embedding?.available) return 'text-gray-500'
    if ((jobStatus?.job_stats?.pending || 0) > 0 || (jobStatus?.job_stats?.processing || 0) > 0) return 'text-orange-500'
    return 'text-green-500'
  }

  const getPollingStatus = () => {
    if (!isVisible) return 'Paused (hidden)'
    if (!pollingEnabled) return 'Disabled'
    if (syncing) return 'Paused (syncing)'
    return 'Active'
  }

  const getOverallStatus = () => {
    if (!status) return 'Unknown'
    
    // Check if semantic search is disabled (handle both response structures)
    if (status.enabled === false || !status.services) return 'Disabled'
    
    // Add null checks for services
    if (!status.services.qdrant || !status.services.embedding) return 'Disabled'
    
    if (!status.services.qdrant.available && !status.services.embedding.available) return 'Disabled'
    if (!status.services.qdrant.available) return 'Qdrant Unavailable'
    if (!status.services.embedding.available) return 'Embedding Service Unavailable'
    if ((jobStatus?.job_stats?.pending || 0) > 0 || (jobStatus?.job_stats?.processing || 0) > 0) return 'Processing'
    return 'Ready'
  }

  return (
    <TooltipProvider>
      <div className="flex items-center gap-2">
        {/* Search Sync Status - Search Icon with Status Color */}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => setShowDetails(!showDetails)}
              className={`p-1 hover:bg-gray-100 dark:hover:bg-gray-800 rounded transition-colors ${getStatusColor()} relative border border-gray-300 dark:border-gray-600`}
              title="Search Sync Status"
            >
              {loading || syncing ? (
                <RefreshCw className="w-4 h-4 animate-spin" />
              ) : (
                <Search className="w-4 h-4" />
              )}
              {/* Polling indicator */}
              {isVisible && pollingEnabled && !syncing && !loading && (
                <div className="absolute -top-1 -right-1 w-2 h-2 bg-green-500 rounded-full animate-pulse" />
              )}
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <div className="text-center">
              <p className="font-medium">Search Sync Status</p>
              {status && jobStatus && (
                <div className="text-xs mt-1 space-y-1">
                  <p>Status: {getOverallStatus()}</p>
                  {getOverallStatus() === 'Disabled' ? (
                    <p className="text-gray-500">Semantic search is disabled</p>
                  ) : (
                    <>
                      <p>Qdrant: {status.services?.qdrant?.available ? 'Available' : 'Unavailable'}</p>
                      <p>Embedding: {status.services?.embedding?.available ? 'Available' : 'Unavailable'}</p>
                      {status.services?.embedding?.model && (
                        <p>Model: {status.services.embedding.model.model}</p>
                      )}
                      <p>Jobs: {jobStatus.job_stats.completed} completed, {jobStatus.job_stats.pending} pending</p>
                      {jobStatus.job_stats.processing > 0 && (
                        <p>Processing: {jobStatus.job_stats.processing}</p>
                      )}
                    </>
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
        {showDetails && status && jobStatus && (
          <div className="absolute top-full right-0 mt-2 w-80 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-50 p-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">
                  Search Sync Details
                </h3>
                <button
                  onClick={() => setShowDetails(false)}
                  className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                >
                  ×
                </button>
              </div>

              <div className="space-y-2 text-xs">
                {/* Service Status */}
                <div className="space-y-2">
                  <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300">
                    Service Status
                  </h4>
                  
                  {getOverallStatus() === 'Disabled' ? (
                    <div className="text-center py-2">
                      <p className="text-gray-500 dark:text-gray-400">Semantic search is disabled</p>
                      <p className="text-xs text-gray-400 mt-1">Enable semantic search in configuration to use this feature</p>
                    </div>
                  ) : (
                    <>
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1">
                          <Database className="w-3 h-3" />
                          <span className="text-gray-600 dark:text-gray-400">Qdrant:</span>
                        </div>
                        <span className={`font-medium ${status.services?.qdrant?.available ? 'text-green-600' : 'text-red-600'}`}>
                          {status.services?.qdrant?.available ? 'Available' : 'Unavailable'}
                        </span>
                      </div>
                      
                      <div className="flex items-center justify-between">
                        <div className="flex items-center gap-1">
                          <Brain className="w-3 h-3" />
                          <span className="text-gray-600 dark:text-gray-400">Embedding:</span>
                        </div>
                        <span className={`font-medium ${status.services?.embedding?.available ? 'text-green-600' : 'text-red-600'}`}>
                          {status.services?.embedding?.available ? 'Available' : 'Unavailable'}
                        </span>
                      </div>
                      
                      {status.services?.embedding?.model && (
                        <div className="flex justify-between">
                          <span className="text-gray-600 dark:text-gray-400">Model:</span>
                          <span className="text-gray-900 dark:text-gray-100 font-mono">
                            {status.services.embedding.model.model}
                          </span>
                        </div>
                      )}
                    </>
                  )}
                </div>

                {/* Job Status */}
                <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                  <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                    Processing Jobs
                  </h4>
                  
                  {getOverallStatus() === 'Disabled' ? (
                    <div className="text-center py-2">
                      <p className="text-gray-500 dark:text-gray-400">No jobs running</p>
                      <p className="text-xs text-gray-400 mt-1">Semantic search is disabled</p>
                    </div>
                  ) : (
                    <>
                      <div className="grid grid-cols-2 gap-2 text-xs">
                        <div className="flex justify-between">
                          <span className="text-gray-600 dark:text-gray-400">Completed:</span>
                          <span className="text-green-600 font-medium">{jobStatus.job_stats.completed}</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-600 dark:text-gray-400">Pending:</span>
                          <span className="text-yellow-600 font-medium">{jobStatus.job_stats.pending}</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-600 dark:text-gray-400">Processing:</span>
                          <span className="text-blue-600 font-medium">{jobStatus.job_stats.processing}</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-gray-600 dark:text-gray-400">Workers:</span>
                          <span className="text-gray-900 dark:text-gray-100">{jobStatus.worker_count}</span>
                        </div>
                      </div>
                      
                      <div className="flex justify-between mt-2">
                        <span className="text-gray-600 dark:text-gray-400">Status:</span>
                        <span className={`font-medium ${jobStatus.running ? 'text-green-600' : 'text-red-600'}`}>
                          {jobStatus.running ? 'Running' : 'Stopped'}
                        </span>
                      </div>
                    </>
                  )}
                </div>

                {/* Overall Status */}
                <div className="flex justify-between">
                  <span className="text-gray-600 dark:text-gray-400">Overall:</span>
                  <span className={`font-medium ${getStatusColor()}`}>
                    {getOverallStatus()}
                  </span>
                </div>
              </div>

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

              {/* Resync Options */}
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <label className="block text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                  Resync Options
                </label>
                
                {getOverallStatus() === 'Disabled' ? (
                  <div className="text-center py-2">
                    <p className="text-gray-500 dark:text-gray-400">Resync not available</p>
                    <p className="text-xs text-gray-400 mt-1">Semantic search is disabled</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <label className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={dryRun}
                        onChange={(e) => setDryRun(e.target.checked)}
                        className="w-3 h-3"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">Dry Run (preview only)</span>
                    </label>
                    
                    <label className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={force}
                        onChange={(e) => setForce(e.target.checked)}
                        className="w-3 h-3"
                      />
                      <span className="text-xs text-gray-600 dark:text-gray-400">Force (skip checks)</span>
                    </label>
                  </div>
                )}
              </div>

              <div className="flex gap-2 pt-2">
                <button
                  onClick={handleResync}
                  disabled={syncing || getOverallStatus() === 'Disabled'}
                  className="flex-1 px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50 transition-colors"
                >
                  {syncing ? 'Resyncing...' : 'Resync Now'}
                </button>
                <button
                  onClick={() => fetchStatus()}
                  disabled={loading}
                  className="px-3 py-1 text-xs bg-gray-500 text-white rounded hover:bg-gray-600 disabled:opacity-50 transition-colors"
                >
                  Refresh
                </button>
              </div>

              {/* Search Test Section */}
              <div className="pt-2 border-t border-gray-200 dark:border-gray-700">
                <label className="block text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                  Test Search
                </label>
                
                {getOverallStatus() === 'Disabled' ? (
                  <div className="text-center py-2">
                    <p className="text-gray-500 dark:text-gray-400">Search not available</p>
                    <p className="text-xs text-gray-400 mt-1">Semantic search is disabled</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        placeholder="Semantic search..."
                        className="flex-1 px-2 py-1 text-xs border border-gray-300 dark:border-gray-600 rounded bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                        onKeyPress={(e) => e.key === 'Enter' && handleSearch()}
                      />
                      <button
                        onClick={handleSearch}
                        disabled={searchLoading || !searchQuery.trim()}
                        className="px-3 py-1 text-xs bg-blue-500 text-white rounded hover:bg-blue-600 disabled:opacity-50 transition-colors flex items-center gap-1"
                      >
                        {searchLoading ? (
                          <RefreshCw className="w-3 h-3 animate-spin" />
                        ) : (
                          <Search className="w-3 h-3" />
                        )}
                      </button>
                    </div>
                    
                    {/* Search Results */}
                    {showSearchResults && (
                      <div className="mt-2 max-h-32 overflow-y-auto">
                        <div className="text-xs text-gray-600 dark:text-gray-400 mb-1">
                          Semantic Results ({searchResults.length}):
                        </div>
                        {searchResults.length > 0 ? (
                          <div className="space-y-1">
                            {searchResults.slice(0, 5).map((result, index) => (
                              <div key={index} className="flex items-center gap-2 p-1 bg-gray-50 dark:bg-gray-700 rounded text-xs">
                                <FileText className="w-3 h-3 text-gray-500" />
                                <span className="flex-1 truncate text-gray-700 dark:text-gray-300">
                                  {result.file_path || result.name}
                                </span>
                                {result.score && (
                                  <span className="text-blue-600 font-medium">
                                    {(result.score * 100).toFixed(0)}%
                                  </span>
                                )}
                              </div>
                            ))}
                            {searchResults.length > 5 && (
                              <div className="text-xs text-gray-500 text-center">
                                ... and {searchResults.length - 5} more
                              </div>
                            )}
                          </div>
                        ) : (
                          <div className="text-xs text-gray-500 text-center py-2">
                            No semantic results found
                          </div>
                        )}
                      </div>
                    )}
                    
                    {/* Search Error */}
                    {searchError && (
                      <div className="text-xs text-red-600 dark:text-red-400">
                        <strong>Search Error:</strong> {searchError}
                      </div>
                    )}
                  </div>
                )}
              </div>

              {/* Error Display */}
              {error && (
                <div className="mt-3 p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded">
                  <div className="text-xs text-red-600 dark:text-red-400">
                    <strong>Error:</strong> {error}
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
