import { BrainCircuit, Settings } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { useLLMStore } from '../../stores'

interface LLMConfigurationSummaryProps {
  minimized?: boolean
}

export default function LLMConfigurationSummary({
  minimized = false,
}: LLMConfigurationSummaryProps) {
  const { setShowLLMModal, savedLLMs } = useLLMStore()
  const count = savedLLMs.length

  if (minimized) {
    return (
      <TooltipProvider>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowLLMModal(true)
              }}
              className="p-2 text-muted-foreground hover:text-foreground transition-colors"
            >
              <BrainCircuit className="w-5 h-5" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{count} model{count !== 1 ? 's' : ''} enabled</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <BrainCircuit className="w-4 h-4 text-gray-600 dark:text-gray-400" />
          <span className="text-sm font-medium text-gray-900 dark:text-gray-100">Models</span>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="px-2 py-0.5 text-xs bg-green-100 dark:bg-green-900 text-green-700 dark:text-green-300 rounded-full">
            {count}
          </span>
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation()
              setShowLLMModal(true)
            }}
            className="p-1 rounded-md text-gray-500 hover:text-gray-900 hover:bg-gray-100 dark:text-gray-400 dark:hover:text-gray-100 dark:hover:bg-gray-800 transition-colors"
            title="Configure models"
          >
            <Settings className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      <button
        onClick={(e) => {
          e.stopPropagation()
          setShowLLMModal(true)
        }}
        className="w-full p-2 bg-gray-50 dark:bg-gray-800 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-md transition-colors text-left"
      >
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className={`w-2 h-2 rounded-full ${count > 0 ? 'bg-green-500' : 'bg-gray-400'}`} />
            <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
              {count > 0 ? `${count} Model${count !== 1 ? 's' : ''} Enabled` : 'No Models Configured'}
            </span>
          </div>
          <span className="text-xs text-gray-500">▶</span>
        </div>
      </button>
    </div>
  )
}
