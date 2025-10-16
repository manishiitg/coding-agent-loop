import React, { useState } from 'react'
import { ChevronDown, ChevronUp, HelpCircle } from 'lucide-react'
import { useModeStore } from '../stores/useModeStore'
import { getModeIcon } from '../utils/modeHelpers'
import { getModeInfoForPanel } from '../constants/modeInfo'

interface ModeInfoPanelProps {
  minimized?: boolean
}

export const ModeInfoPanel: React.FC<ModeInfoPanelProps> = ({ minimized = false }) => {
  const { selectedModeCategory } = useModeStore()
  const [expanded, setExpanded] = useState(false)

  if (!selectedModeCategory) {
    return null
  }

  const modeInfo = getModeInfoForPanel(selectedModeCategory)

  if (minimized) {
    return (
      <div className="p-2">
        <button
          onClick={() => setExpanded(!expanded)}
          className="w-full flex items-center justify-center p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
          title="Mode Information"
        >
          <HelpCircle className="w-4 h-4" />
        </button>
        
        {/* Expanded content in minimized mode */}
        {expanded && (
          <div className="mt-2 bg-gray-50 dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg p-4">
            {/* Header */}
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                {getModeIcon(selectedModeCategory, "w-5 h-5 text-blue-600")}
                <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
                  {modeInfo.title}
                </h3>
              </div>
              <button
                onClick={() => setExpanded(false)}
                className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
              >
                <ChevronUp className="w-4 h-4" />
              </button>
            </div>

            {/* Description */}
            <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
              {modeInfo.description}
            </p>

            {/* Expanded Content */}
            <div className="space-y-4">
              {/* Features */}
              {modeInfo.features.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                    Key Features:
                  </h4>
                  <ul className="space-y-1">
                    {modeInfo.features.map((feature: string, index: number) => (
                      <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                        <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                        {feature}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Example Queries */}
              {modeInfo.examples.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                    Example Queries:
                  </h4>
                  <div className="space-y-1">
                    {modeInfo.examples.map((example: string, index: number) => (
                      <div key={index} className="text-xs text-gray-500 dark:text-gray-400 italic bg-white dark:bg-slate-700 p-2 rounded border">
                        "{example}"
                      </div>
                    ))}
                  </div>
                </div>
              )}

              {/* Tips */}
              {modeInfo.tips.length > 0 && (
                <div>
                  <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                    Tips:
                  </h4>
                  <ul className="space-y-1">
                    {modeInfo.tips.map((tip: string, index: number) => (
                      <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                        <div className="w-1.5 h-1.5 bg-green-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                        {tip}
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {/* Keyboard Shortcuts */}
              <div>
                <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                  Keyboard Shortcuts:
                </h4>
                <div className="text-xs text-gray-500 dark:text-gray-400 space-y-1">
                  <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+1</kbd> Simple Mode</div>
                  <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+2</kbd> ReAct Mode</div>
                  <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+3</kbd> Deep Research</div>
                  <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+4</kbd> Workflow Mode</div>
                  <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+N</kbd> New Chat</div>
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="bg-gray-50 dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg p-4">
      {/* Header */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          {getModeIcon(selectedModeCategory, "w-5 h-5 text-blue-600")}
          <h3 className="text-sm font-semibold text-gray-900 dark:text-white">
            {modeInfo.title}
          </h3>
        </div>
        <button
          onClick={() => setExpanded(!expanded)}
          className="p-1 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
        >
          {expanded ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
        </button>
      </div>

      {/* Description */}
      <p className="text-sm text-gray-600 dark:text-gray-400 mb-3">
        {modeInfo.description}
      </p>

      {/* Expanded Content */}
      {expanded && (
        <div className="space-y-4">
          {/* Features */}
          {modeInfo.features.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Key Features:
              </h4>
              <ul className="space-y-1">
                {modeInfo.features.map((feature: string, index: number) => (
                  <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                    <div className="w-1.5 h-1.5 bg-blue-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                    {feature}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Example Queries */}
          {modeInfo.examples.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Example Queries:
              </h4>
              <div className="space-y-1">
                {modeInfo.examples.map((example: string, index: number) => (
                  <div key={index} className="text-xs text-gray-500 dark:text-gray-400 italic bg-white dark:bg-slate-700 p-2 rounded border">
                    "{example}"
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Tips */}
          {modeInfo.tips.length > 0 && (
            <div>
              <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
                Tips:
              </h4>
              <ul className="space-y-1">
                {modeInfo.tips.map((tip: string, index: number) => (
                  <li key={index} className="flex items-start text-xs text-gray-600 dark:text-gray-400">
                    <div className="w-1.5 h-1.5 bg-green-500 rounded-full mr-2 mt-1.5 flex-shrink-0" />
                    {tip}
                  </li>
                ))}
              </ul>
            </div>
          )}

          {/* Keyboard Shortcuts */}
          <div>
            <h4 className="text-xs font-semibold text-gray-700 dark:text-gray-300 mb-2">
              Keyboard Shortcuts:
            </h4>
            <div className="text-xs text-gray-500 dark:text-gray-400 space-y-1">
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+1</kbd> Simple Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+2</kbd> ReAct Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+3</kbd> Deep Research</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+4</kbd> Workflow Mode</div>
              <div>• <kbd className="px-1 py-0.5 bg-gray-200 dark:bg-gray-600 rounded text-xs">Ctrl+N</kbd> New Chat</div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
