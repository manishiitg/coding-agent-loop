import React from 'react'
import type { ComprehensiveCacheEvent } from '../../../generated/events'

interface ComprehensiveCacheEventDisplayProps {
  event: ComprehensiveCacheEvent
  mode?: 'compact' | 'detailed'
}

export const ComprehensiveCacheEventDisplay: React.FC<ComprehensiveCacheEventDisplayProps> = ({
  event,
  mode = 'compact'
}) => {
  const getStatusColor = (status: string) => {
    switch (status) {
      case 'hit':
        return 'text-green-600 dark:text-green-400'
      case 'miss':
        return 'text-yellow-600 dark:text-yellow-400'
      case 'write':
        return 'text-blue-600 dark:text-blue-400'
      case 'error':
        return 'text-red-600 dark:text-red-400'
      default:
        return 'text-gray-600 dark:text-gray-400'
    }
  }

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'hit':
        return 'Hit'
      case 'miss':
        return 'Miss'
      case 'write':
        return 'Write'
      case 'error':
        return 'Error'
      default:
        return 'Unknown'
    }
  }

  if (mode === 'compact') {
    return (
      <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded p-2">
        <div className="flex items-center gap-2 text-xs">
          <span className="font-medium text-gray-700 dark:text-gray-300">Comprehensive Cache</span>
          <span className="text-gray-600 dark:text-gray-400">• {event.operation}</span>
          <span className="text-green-600 dark:text-green-400">• {event.cache_hits} hits</span>
          <span className="text-yellow-600 dark:text-yellow-400">• {event.cache_misses} misses</span>
          <span className="text-blue-600 dark:text-blue-400">• {event.cache_writes} writes</span>
          {(event.cache_errors || 0) > 0 && <span className="text-red-600 dark:text-red-400">• {event.cache_errors} errors</span>}
          <span className="text-gray-600 dark:text-gray-400">• {event.servers_count} servers</span>
          <span className="text-gray-600 dark:text-gray-400">• {event.total_tools} tools</span>
        </div>
        {event.config_path && (
          <div className="text-xs text-gray-600 dark:text-gray-400 mt-1">
            {event.config_path.length > 80 ? `${event.config_path.substring(0, 80)}...` : event.config_path}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded p-2">
      <div className="flex items-start gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between mb-3">
            <div className="text-sm font-semibold text-gray-800 dark:text-gray-200">
              Comprehensive Cache Event
            </div>
            <div className="text-xs text-gray-600 dark:text-gray-400">
              {event.timestamp ? new Date(event.timestamp).toLocaleTimeString() : 'N/A'}
            </div>
          </div>

          {/* Operation Details */}
          <div className="grid grid-cols-2 gap-4 mb-4">
            <div className="bg-white dark:bg-gray-800 rounded p-3">
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300">Operation</div>
              <div className="text-sm font-semibold text-blue-600 dark:text-blue-400 capitalize">
                {event.operation}
              </div>
            </div>
            <div className="bg-white dark:bg-gray-800 rounded p-3">
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300">Config Path</div>
              <div className="text-xs text-gray-600 dark:text-gray-400 font-mono">
                {event.config_path}
              </div>
            </div>
          </div>

          {/* Cache Status */}
          <div className="bg-white dark:bg-gray-800 rounded p-3 mb-4">
            <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">Cache Status</div>
            <div className="grid grid-cols-4 gap-2 text-center">
              <div className="bg-green-100 dark:bg-green-900/20 rounded p-2">
                <div className="text-sm font-bold text-green-600 dark:text-green-400">{event.cache_hits}</div>
                <div className="text-xs text-green-600 dark:text-green-400">Hits</div>
              </div>
              <div className="bg-yellow-100 dark:bg-yellow-900/20 rounded p-2">
                <div className="text-sm font-bold text-yellow-600 dark:text-yellow-400">{event.cache_misses}</div>
                <div className="text-xs text-yellow-600 dark:text-yellow-400">Misses</div>
              </div>
              <div className="bg-blue-100 dark:bg-blue-900/20 rounded p-2">
                <div className="text-sm font-bold text-blue-600 dark:text-blue-400">{event.cache_writes}</div>
                <div className="text-xs text-blue-600 dark:text-blue-400">Writes</div>
              </div>
              <div className="bg-red-100 dark:bg-red-900/20 rounded p-2">
                <div className="text-sm font-bold text-red-600 dark:text-red-400">{event.cache_errors}</div>
                <div className="text-xs text-red-600 dark:text-red-400">Errors</div>
              </div>
            </div>
          </div>

          {/* Server Summary */}
          <div className="bg-white dark:bg-gray-800 rounded p-3 mb-4">
            <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">Server Summary</div>
            <div className="grid grid-cols-2 gap-4 text-center">
              <div>
                <div className="text-sm font-bold text-blue-600 dark:text-blue-400">{event.servers_count}</div>
                <div className="text-xs text-gray-600 dark:text-gray-400">Servers</div>
              </div>
              <div>
                <div className="text-sm font-bold text-blue-600 dark:text-blue-400">{event.total_tools}</div>
                <div className="text-xs text-gray-600 dark:text-gray-400">Tools</div>
              </div>
            </div>
          </div>

          {/* Performance Metrics */}
          <div className="bg-white dark:bg-gray-800 rounded p-3 mb-4">
            <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">Performance</div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-xs text-gray-600 dark:text-gray-400">Connection Time</div>
                <div className="text-xs font-mono text-gray-800 dark:text-gray-200">{event.connection_time}</div>
              </div>
              <div>
                <div className="text-xs text-gray-600 dark:text-gray-400">Cache Time</div>
                <div className="text-xs font-mono text-gray-800 dark:text-gray-200">{event.cache_time}</div>
              </div>
            </div>
          </div>

          {/* Individual Server Status */}
          {event.server_status && Object.keys(event.server_status).length > 0 && (
            <div className="bg-white dark:bg-gray-800 rounded p-3 mb-4">
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-2">Server Status</div>
              <div className="space-y-2 max-h-40 overflow-y-auto">
                {Object.entries(event.server_status).map(([serverName, status]) => (
                  <div key={serverName} className="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-700 rounded">
                    <div className="flex items-center space-x-2">
                      <span className="text-xs">{getStatusIcon(status.status || 'unknown')}</span>
                      <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
                        {serverName}
                      </span>
                    </div>
                    <div className="flex items-center space-x-4 text-xs">
                      <span className={`font-medium ${getStatusColor(status.status || 'unknown')}`}>
                        {(status.status || 'unknown').toUpperCase()}
                      </span>
                      <span className="text-gray-500 dark:text-gray-400">
                        {status.tools_count || 0} tools
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Errors */}
          {event.errors && event.errors.length > 0 && (
            <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-3">
              <div className="text-xs font-medium text-red-700 dark:text-red-300 mb-2">Errors</div>
              <div className="space-y-1">
                {event.errors.map((error, index) => (
                  <div key={index} className="text-xs text-red-600 dark:text-red-400">
                    • {error}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
