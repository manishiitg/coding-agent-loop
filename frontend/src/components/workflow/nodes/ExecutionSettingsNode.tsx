import { memo, useState, useRef, useEffect, useCallback } from 'react'
import { Handle, Position } from '@xyflow/react'
import { Settings, Play, Zap, SkipForward, ChevronDown, Check } from 'lucide-react'
import { useWorkflowStore, type ExecutionModeType } from '../../../stores/useWorkflowStore'

export type ExecutionSettingsNodeData = Record<string, unknown>

interface ExecutionSettingsNodeProps {
  data?: ExecutionSettingsNodeData
  selected?: boolean
}

// Execution Mode options - how to run (human feedback, learning, etc.)
const EXECUTION_MODE_OPTIONS: { id: ExecutionModeType; label: string; icon: typeof Play; description: string }[] = [
  { id: 'human_approval', label: 'With Human Approval', icon: Play, description: 'Pause for human approval before going to next step (learning enabled)' },
  { id: 'fast_execution', label: 'Fast Execution', icon: Zap, description: 'Execute all without pausing or learning' },
  { id: 'with_learning', label: 'With Learning (No Human)', icon: SkipForward, description: 'Proceed to next step without human approval (learning enabled)' },
]

export const ExecutionSettingsNode = memo(({ selected }: ExecutionSettingsNodeProps) => {
  const selectedExecutionMode = useWorkflowStore(state => state.selectedExecutionMode)
  const setExecutionMode = useWorkflowStore(state => state.setExecutionMode)
  
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)
  
  // Get current execution mode info
  const currentModeInfo = EXECUTION_MODE_OPTIONS.find(m => m.id === selectedExecutionMode) || EXECUTION_MODE_OPTIONS[0]
  
  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])
  
  // Handle selecting execution mode
  const handleSelectExecutionMode = useCallback((modeId: ExecutionModeType) => {
    setExecutionMode(modeId)
    setIsDropdownOpen(false)
  }, [setExecutionMode])
  
  const Icon = currentModeInfo.icon
  
  return (
    <div 
      className={`
        min-w-[240px] max-w-[300px] rounded-lg border-2 shadow-sm
        bg-purple-50 dark:bg-purple-900/20 border-purple-300 dark:border-purple-700
        hover:border-purple-400 dark:hover:border-purple-600 transition-colors
        ${selected ? 'ring-2 ring-purple-500/50' : ''}
      `}
    >
      {/* Input handle */}
      <Handle
        type="target"
        position={Position.Left}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
      
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 bg-purple-100 dark:bg-purple-900/40 rounded-t-lg border-b border-purple-200 dark:border-purple-800">
        <Settings className="w-4 h-4 text-purple-600 dark:text-purple-400" />
        <span className="text-sm font-medium text-purple-700 dark:text-purple-300">
          Execution Mode
        </span>
      </div>
      
      {/* Body */}
      <div className="px-3 py-3">
        <div className="relative" ref={dropdownRef}>
          <button
            onClick={() => setIsDropdownOpen(!isDropdownOpen)}
            className={`
              w-full flex items-center gap-2 px-3 py-2 rounded-md transition-all text-sm font-medium
              bg-white dark:bg-gray-800 text-gray-700 dark:text-gray-300 
              hover:bg-gray-50 dark:hover:bg-gray-700 border border-purple-200 dark:border-purple-800
            `}
            title="Select execution mode"
          >
            <Icon className="w-4 h-4 text-purple-600 dark:text-purple-400" />
            <span className="flex-1 text-left">{currentModeInfo.label}</span>
            <ChevronDown className={`w-4 h-4 transition-transform ${isDropdownOpen ? 'rotate-180' : ''}`} />
          </button>
          
          {/* Dropdown Menu */}
          {isDropdownOpen && (
            <div className="absolute top-full left-0 right-0 mt-1 bg-white dark:bg-gray-800 rounded-lg shadow-xl border border-gray-200 dark:border-gray-700 z-50">
              <div className="p-1">
                <div className="text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider px-3 py-2">
                  Execution Mode
                </div>
                {EXECUTION_MODE_OPTIONS.map((mode) => {
                  const ModeIcon = mode.icon
                  const isSelected = mode.id === selectedExecutionMode
                  return (
                    <button
                      key={mode.id}
                      onClick={() => handleSelectExecutionMode(mode.id)}
                      className={`
                        w-full text-left px-3 py-2.5 rounded-md transition-colors
                        ${isSelected 
                          ? 'bg-purple-100 dark:bg-purple-900/30' 
                          : 'hover:bg-gray-100 dark:hover:bg-gray-700'
                        }
                      `}
                    >
                      <div className="flex items-start gap-3">
                        <ModeIcon className={`w-4 h-4 mt-0.5 ${isSelected ? 'text-purple-600 dark:text-purple-400' : 'text-gray-500 dark:text-gray-400'}`} />
                        <div className="flex-1 min-w-0">
                          <div className={`font-medium text-sm ${isSelected ? 'text-purple-700 dark:text-purple-300' : 'text-gray-900 dark:text-gray-100'}`}>
                            {mode.label}
                          </div>
                          <div className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            {mode.description}
                          </div>
                        </div>
                        {isSelected && <Check className="w-4 h-4 text-purple-600 dark:text-purple-400 mt-0.5" />}
                      </div>
                    </button>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      </div>
      
      {/* Output handle */}
      <Handle
        type="source"
        position={Position.Right}
        className="!w-3 !h-3 !bg-purple-500 !border-2 !border-white dark:!border-gray-800"
      />
    </div>
  )
})

ExecutionSettingsNode.displayName = 'ExecutionSettingsNode'

