import { ChevronDown } from 'lucide-react'
import { usePresetApplication } from '../../stores/useGlobalPresetStore'

interface ActivePresetDisplayProps {
  modeCategory: 'deep-research' | 'workflow'
  showSelector: boolean
  onToggle: () => void
}

export default function ActivePresetDisplay({
  modeCategory,
  showSelector,
  onToggle
}: ActivePresetDisplayProps) {
  const { getActivePreset } = usePresetApplication()
  const activePreset = getActivePreset(modeCategory)

  return (
    <div className="space-y-2">
      {/* Active Preset */}
      {activePreset ? (
        <div className="flex items-center justify-between p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
          <div className="flex items-center gap-2">
            <div className="w-2 h-2 bg-blue-500 rounded-full"></div>
            <span className="text-sm font-medium text-blue-900 dark:text-blue-100">
              {activePreset.label || 'Preset Selected'}
            </span>
          </div>
          <button
            onClick={onToggle}
            className="p-1 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-200 transition-colors"
          >
            <ChevronDown className={`w-3 h-3 transition-transform ${showSelector ? 'rotate-180' : ''}`} />
          </button>
        </div>
      ) : (
        <div className="flex items-center justify-between p-2 bg-gray-50 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg">
          <span className="text-sm text-gray-600 dark:text-gray-400">
            No preset selected
          </span>
          <button
            onClick={onToggle}
            className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
          >
            <ChevronDown className={`w-3 h-3 transition-transform ${showSelector ? 'rotate-180' : ''}`} />
          </button>
        </div>
      )}
    </div>
  )
}
