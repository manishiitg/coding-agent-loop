import React from 'react'
import { AlertTriangle, ArrowRight } from 'lucide-react'
import { type ModeCategory } from '../../stores/useModeStore'
import { getModeIcon, getModeName, getModeDescription } from '../../utils/modeHelpers'

interface ModeSwitchDialogProps {
  isOpen: boolean
  onConfirm: () => void
  onCancel: () => void
  newModeCategory: ModeCategory
  currentModeCategory: ModeCategory
}

export const ModeSwitchDialog: React.FC<ModeSwitchDialogProps> = ({
  isOpen,
  onConfirm,
  onCancel,
  newModeCategory,
  currentModeCategory
}) => {
  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="relative bg-white dark:bg-slate-800 rounded-xl border border-gray-200 dark:border-slate-700 p-8 max-w-md mx-4 shadow-xl">
        {/* Header */}
        <div className="flex items-center gap-3 mb-6">
          <div className="flex items-center justify-center w-12 h-12 bg-amber-50 dark:bg-amber-900/20 rounded-xl">
            <AlertTriangle className="w-6 h-6 text-amber-600" />
          </div>
          <div>
            <h2 className="text-xl font-bold text-gray-900 dark:text-white">
              Switch Mode
            </h2>
            <p className="text-sm text-gray-600 dark:text-gray-400">
              This will start a new chat session
            </p>
          </div>
        </div>

        {/* Mode Transition */}
        <div className="mb-6">
          <div className="flex items-center justify-between">
            {/* From Mode */}
            <div className="flex items-center gap-3">
              {getModeIcon(currentModeCategory, "w-6 h-6 text-blue-600")}
              <div>
                <div className="font-medium text-gray-900 dark:text-white">
                  {getModeName(currentModeCategory)}
                </div>
                <div className="text-sm text-gray-600 dark:text-gray-400">
                  {getModeDescription(currentModeCategory)}
                </div>
              </div>
            </div>

            {/* Arrow */}
            <ArrowRight className="w-5 h-5 text-gray-400 mx-4" />

            {/* To Mode */}
            <div className="flex items-center gap-3">
              {getModeIcon(newModeCategory, "w-6 h-6 text-blue-600")}
              <div>
                <div className="font-medium text-gray-900 dark:text-white">
                  {getModeName(newModeCategory)}
                </div>
                <div className="text-sm text-gray-600 dark:text-gray-400">
                  {getModeDescription(newModeCategory)}
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Warning Message */}
        <div className="mb-6 p-4 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg">
          <p className="text-sm text-amber-800 dark:text-amber-200">
            <strong>Warning:</strong> Switching modes will clear your current chat and start a new session. 
            Your current conversation will be lost.
          </p>
        </div>

        {/* Action Buttons */}
        <div className="flex gap-3 justify-end">
          <button
            onClick={onCancel}
            className="px-4 py-2 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700 rounded-lg transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={onConfirm}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors flex items-center gap-2"
          >
            Switch Mode
            <ArrowRight className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  )
}
