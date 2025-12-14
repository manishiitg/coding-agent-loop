import { memo, useCallback, type MouseEvent } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Variable, Layers, CheckCircle, Circle, Play } from 'lucide-react'
import type { VariablesManifest, VariableGroup } from '../../../services/api-types'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

export interface VariablesNodeData extends Record<string, unknown> {
  manifest: VariablesManifest | null
  onOpenSidebar?: () => void
  isLoading?: boolean
}

interface VariablesNodeProps {
  data: VariablesNodeData
  selected?: boolean
}

// Get enabled groups from manifest
const getEnabledGroups = (manifest: VariablesManifest | null): VariableGroup[] => {
  if (!manifest) return []
  if (!manifest.groups || manifest.groups.length === 0) {
    // Single group mode - create virtual group from variables
    if (!manifest.variables || !Array.isArray(manifest.variables)) return []
    const values: Record<string, string> = {}
    manifest.variables.forEach(v => {
      values[v.name] = v.value || ''
    })
    return [{
      group_id: 'group-1',
      values,
      enabled: true
    }]
  }
  return manifest.groups.filter(g => g.enabled)
}

// Check if manifest has multiple groups
const hasMultipleGroups = (manifest: VariablesManifest | null): boolean => {
  return !!manifest?.groups && manifest.groups.length > 1
}

export const VariablesNode = memo(({ data, selected }: VariablesNodeProps) => {
  const { manifest, onOpenSidebar, isLoading } = data
  const currentRunningGroupId = useWorkflowStore(state => state.currentRunningGroupId)
  
  const variableCount = manifest?.variables?.length || 0
  const totalGroups = manifest?.groups?.length || (variableCount > 0 ? 1 : 0)
  const enabledGroups = getEnabledGroups(manifest)
  const enabledCount = enabledGroups.length
  const isMultiGroup = hasMultipleGroups(manifest)
  
  // Handle node click to open sidebar
  const handleClick = useCallback((e: MouseEvent) => {
    e.stopPropagation()
    if (onOpenSidebar) {
      onOpenSidebar()
    }
  }, [onOpenSidebar])
  
  // No variables yet
  if (!manifest || variableCount === 0) {
    return (
      <div 
        className={`
          min-w-[200px] max-w-[280px] rounded-lg border-2 shadow-sm cursor-pointer
          bg-gray-50 dark:bg-gray-800/50 border-gray-300 dark:border-gray-600
          hover:border-purple-400 dark:hover:border-purple-500 transition-colors
          ${selected ? 'ring-2 ring-purple-500/50' : ''}
        `}
        onClick={handleClick}
      >
        {/* Input handle */}
        <Handle
          type="target"
          position={Position.Left}
          className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
        />
        
        {/* Header */}
        <div className="flex items-center gap-2 px-3 py-2 bg-gray-100 dark:bg-gray-700/50 rounded-t-lg border-b border-gray-200 dark:border-gray-600">
          <Variable className="w-4 h-4 text-gray-500 dark:text-gray-400" />
          <span className="text-sm font-medium text-gray-600 dark:text-gray-300">
            Variables
          </span>
          {isLoading && (
            <span className="ml-auto text-xs text-gray-400">Loading...</span>
          )}
        </div>
        
        {/* Body */}
        <div className="px-3 py-2">
          <p className="text-xs text-gray-500 dark:text-gray-400 italic">
            No variables extracted yet
          </p>
          <p className="text-xs text-gray-400 dark:text-gray-500 mt-1">
            Click to add variables
          </p>
        </div>
        
        {/* Output handle */}
        <Handle
          type="source"
          position={Position.Right}
          className="!w-3 !h-3 !bg-gray-400 dark:!bg-gray-500 !border-2 !border-white dark:!border-gray-800"
        />
      </div>
    )
  }
  
  return (
    <div 
      className={`
        min-w-[220px] max-w-[300px] rounded-lg border-2 shadow-sm cursor-pointer
        bg-purple-50 dark:bg-purple-900/20 border-purple-300 dark:border-purple-600
        hover:border-purple-400 dark:hover:border-purple-500 transition-colors
        ${selected ? 'ring-2 ring-purple-500/50' : ''}
      `}
      onClick={handleClick}
    >
      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-3 !h-3 !bg-purple-400 dark:!bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
      
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-purple-100 dark:bg-purple-800/30 rounded-t-lg border-b border-purple-200 dark:border-purple-700">
        <Variable className="w-4 h-4 text-purple-600 dark:text-purple-400" />
        <span className="text-sm font-medium text-purple-700 dark:text-purple-300">
          Variables ({variableCount})
        </span>
        {isMultiGroup && (
          <span className="ml-auto flex items-center gap-1 text-xs text-purple-600 dark:text-purple-400">
            <Layers className="w-3 h-3" />
            {enabledCount}/{totalGroups} Groups
          </span>
        )}
      </div>
      
      {/* All groups with their values */}
      <div className="px-3 py-2 space-y-3">
        {(() => {
          // Get all groups (including virtual group for single-group mode)
          const allGroups = isMultiGroup && manifest.groups 
            ? manifest.groups 
            : enabledGroups
          return allGroups.map((group) => {
            const isRunning = currentRunningGroupId === group.group_id
            return (
            <div 
              key={group.group_id} 
              className={`space-y-1.5 ${isRunning ? 'bg-blue-50 dark:bg-blue-900/30 rounded p-1.5 border border-blue-200 dark:border-blue-700' : ''}`}
            >
              {/* Group header */}
              <div className="flex items-center gap-1.5 text-xs">
                {isRunning ? (
                  <Play className="w-3 h-3 text-blue-600 dark:text-blue-400 animate-pulse" />
                ) : group.enabled ? (
                  <CheckCircle className="w-3 h-3 text-green-600 dark:text-green-400" />
                ) : (
                  <Circle className="w-3 h-3 text-gray-400 dark:text-gray-500" />
                )}
                <span className={`font-mono font-semibold ${isRunning ? 'text-blue-700 dark:text-blue-300' : 'text-purple-600 dark:text-purple-400'}`}>
                  {group.group_id.toUpperCase()}
                </span>
                {isRunning && (
                  <span className="ml-auto text-[10px] text-blue-600 dark:text-blue-400 font-medium">
                    Running...
                  </span>
                )}
              </div>
            
            {/* Variables for this group */}
            <div className="ml-4 space-y-1">
              {manifest.variables && manifest.variables.slice(0, 3).map((variable, idx) => {
                const value = group.values[variable.name] || ''
                return (
                  <div key={idx} className="flex flex-col gap-0.5 text-xs">
                    <div className="flex items-center gap-1">
                      <span className="text-gray-500 dark:text-gray-400">•</span>
                      <span className="text-gray-700 dark:text-gray-300 font-mono">
                        {variable.name}
                      </span>
                    </div>
                    <div className="ml-3 text-gray-600 dark:text-gray-400 font-mono text-[10px] truncate">
                      {value ? value : <span className="italic text-gray-400 dark:text-gray-500">(empty)</span>}
                    </div>
                  </div>
                )
              })}
              {manifest.variables && manifest.variables.length > 3 && (
                <div className="text-[10px] text-gray-400 dark:text-gray-500 italic ml-3">
                  +{manifest.variables.length - 3} more...
                </div>
              )}
            </div>
          </div>
            )
          })
        })()}
      </div>
      
      {/* Groups section (only if multiple groups) */}
      {isMultiGroup && manifest.groups && (
        <div className="px-3 py-2 border-t border-purple-200 dark:border-purple-700">
          <div className="flex flex-wrap gap-1.5">
            {manifest.groups.slice(0, 6).map((group) => {
              const isRunning = currentRunningGroupId === group.group_id
              return (
              <div 
                key={group.group_id}
                className={`
                  flex items-center gap-1 px-1.5 py-0.5 rounded text-xs
                  ${isRunning
                    ? 'bg-blue-200 dark:bg-blue-700/50 text-blue-700 dark:text-blue-300 border border-blue-300 dark:border-blue-600'
                    : group.enabled 
                    ? 'bg-purple-200 dark:bg-purple-700/50 text-purple-700 dark:text-purple-300' 
                    : 'bg-gray-200 dark:bg-gray-700 text-gray-500 dark:text-gray-400 opacity-60'}
                `}
                title={isRunning ? 'Currently Running' : group.enabled ? 'Enabled' : 'Disabled'}
              >
                {isRunning ? (
                  <Play className="w-3 h-3 animate-pulse" />
                ) : group.enabled ? (
                  <CheckCircle className="w-3 h-3" />
                ) : (
                  <Circle className="w-3 h-3" />
                )}
                <span className="font-mono">{group.group_id}</span>
              </div>
            )
            })}
            {manifest.groups.length > 6 && (
              <span className="text-xs text-gray-500 dark:text-gray-400">
                +{manifest.groups.length - 6}
              </span>
            )}
          </div>
        </div>
      )}
      
      {/* Output handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-3 !h-3 !bg-purple-400 dark:!bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

VariablesNode.displayName = 'VariablesNode'

