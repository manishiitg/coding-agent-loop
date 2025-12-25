import React, { useState } from 'react'
import {
  X,
  Play,
  Settings,
  Edit2,
  Trash2,
  Save,
  ChevronDown,
  ChevronRight,
  GitBranch,
  RefreshCw,
  Link
} from 'lucide-react'
import type { WorkflowNode } from '../hooks/usePlanToFlow'
import type { PlanStep } from '../../../utils/stepConfigMatching'
import { isConditionalStep, isRegularStep } from '../../../utils/stepConfigMatching'

interface NodeDetailPanelProps {
  node: WorkflowNode | null
  onClose: () => void
  onRunStep: (stepId: string) => void
  onEditStep: (stepId: string, updates: Partial<PlanStep>) => void
  onDeleteStep: (stepId: string) => void
  onConfigureStep: (stepId: string) => void
  isRunning: boolean
}

export const NodeDetailPanel: React.FC<NodeDetailPanelProps> = ({
  node,
  onClose,
  onRunStep,
  onEditStep,
  onDeleteStep,
  onConfigureStep,
  isRunning
}) => {
  const [isEditing, setIsEditing] = useState(false)
  const [editedTitle, setEditedTitle] = useState('')
  const [editedDescription, setEditedDescription] = useState('')
  const [editedSuccessCriteria, setEditedSuccessCriteria] = useState('')
  const [showDependencies, setShowDependencies] = useState(false)

  if (!node || node.id === 'start' || node.id === 'end') {
    return null
  }

  const step = node.data.step as PlanStep
  const isConditional = isConditionalStep(step)
  const isLoop = isRegularStep(step) && step.has_loop

  const handleStartEdit = () => {
    setEditedTitle(step.title || '')
    setEditedDescription(step.description || '')
    setEditedSuccessCriteria(step.success_criteria || '')
    setIsEditing(true)
  }

  const handleSaveEdit = () => {
    onEditStep(node.id, {
      title: editedTitle,
      description: editedDescription,
      success_criteria: editedSuccessCriteria
    })
    setIsEditing(false)
  }

  const handleCancelEdit = () => {
    setIsEditing(false)
  }

  return (
    <div className="absolute bottom-4 left-4 right-4 max-w-md bg-white dark:bg-gray-800 rounded-lg shadow-lg border border-gray-200 dark:border-gray-700 z-10">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700">
        <div className="flex items-center gap-2">
          {isConditional && <GitBranch className="w-4 h-4 text-purple-500" />}
          {isLoop && <RefreshCw className="w-4 h-4 text-cyan-500" />}
          <span className="font-medium text-gray-900 dark:text-gray-100">
            {isEditing ? 'Edit Step' : `Step ${(node.data.stepIndex as number) + 1}`}
          </span>
        </div>
        <button
          onClick={onClose}
          className="p-1 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400"
        >
          <X className="w-4 h-4" />
        </button>
      </div>

      {/* Content */}
      <div className="p-4 space-y-4 max-h-[300px] overflow-y-auto">
        {isEditing ? (
          // Edit mode
          <div className="space-y-3">
            <div>
              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                Title
              </label>
              <input
                type="text"
                value={editedTitle}
                onChange={(e) => setEditedTitle(e.target.value)}
                className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-primary"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                Description
              </label>
              <textarea
                value={editedDescription}
                onChange={(e) => setEditedDescription(e.target.value)}
                rows={3}
                className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-primary resize-none"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                Success Criteria
              </label>
              <textarea
                value={editedSuccessCriteria}
                onChange={(e) => setEditedSuccessCriteria(e.target.value)}
                rows={2}
                className="w-full px-3 py-2 text-sm border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-primary resize-none"
              />
            </div>
          </div>
        ) : (
          // View mode
          <>
            <div>
              <h4 className="font-medium text-gray-900 dark:text-gray-100">
                {step.title}
              </h4>
              {step.description && (
                <p className="mt-1 text-sm text-gray-600 dark:text-gray-400">
                  {step.description}
                </p>
              )}
            </div>

            {step.success_criteria && (
              <div>
                <span className="text-xs font-medium text-green-600 dark:text-green-400">
                  Success Criteria:
                </span>
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  {step.success_criteria}
                </p>
              </div>
            )}

            {isConditional && step.condition_question && (
              <div className="p-2 bg-purple-50 dark:bg-purple-900/20 rounded">
                <span className="text-xs font-medium text-purple-600 dark:text-purple-400">
                  Condition:
                </span>
                <p className="text-sm text-gray-700 dark:text-gray-300">
                  {step.condition_question}
                </p>
              </div>
            )}

            {isLoop && (
              <div className="p-2 bg-cyan-50 dark:bg-cyan-900/20 rounded">
                <span className="text-xs font-medium text-cyan-600 dark:text-cyan-400">
                  Loop:
                </span>
                {step.loop_condition && (
                  <p className="text-sm text-gray-700 dark:text-gray-300">
                    Until: {step.loop_condition}
                  </p>
                )}
                {step.max_iterations && (
                  <p className="text-xs text-gray-500 dark:text-gray-500">
                    Max iterations: {step.max_iterations}
                  </p>
                )}
              </div>
            )}

            {/* Dependencies section */}
            {(step.context_dependencies?.length || step.context_output) && (
              <div>
                <button
                  onClick={() => setShowDependencies(!showDependencies)}
                  className="flex items-center gap-1 text-xs font-medium text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-gray-200"
                >
                  {showDependencies ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                  <Link className="w-3 h-3" />
                  Dependencies
                </button>
                {showDependencies && (
                  <div className="mt-2 space-y-1 text-xs">
                    {step.context_dependencies && step.context_dependencies.length > 0 && (
                      <div>
                        <span className="text-purple-600 dark:text-purple-400">Inputs: </span>
                        <span className="text-gray-600 dark:text-gray-400">
                          {step.context_dependencies.join(', ')}
                        </span>
                      </div>
                    )}
                    {step.context_output && (
                      <div>
                        <span className="text-orange-600 dark:text-orange-400">Output: </span>
                        <span className="text-gray-600 dark:text-gray-400">
                          {Array.isArray(step.context_output) ? step.context_output.join(', ') : step.context_output}
                        </span>
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}
          </>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between px-4 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50 rounded-b-lg">
        {isEditing ? (
          <div className="flex items-center gap-2 w-full justify-end">
            <button
              onClick={handleCancelEdit}
              className="px-3 py-1.5 text-sm text-gray-600 dark:text-gray-400 hover:bg-gray-200 dark:hover:bg-gray-700 rounded transition-colors"
            >
              Cancel
            </button>
            <button
              onClick={handleSaveEdit}
              className="flex items-center gap-1 px-3 py-1.5 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90 transition-colors"
            >
              <Save className="w-3.5 h-3.5" />
              Save
            </button>
          </div>
        ) : (
          <>
            <div className="flex items-center gap-1">
              <button
                onClick={() => onRunStep(node.id)}
                disabled={isRunning}
                className="flex items-center gap-1 px-3 py-1.5 text-sm bg-green-600 hover:bg-green-700 text-white rounded transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <Play className="w-3.5 h-3.5" />
                Run
              </button>
              <button
                onClick={handleStartEdit}
                className="p-1.5 rounded hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
                title="Edit step"
              >
                <Edit2 className="w-4 h-4" />
              </button>
              <button
                onClick={() => onConfigureStep(node.id)}
                className="p-1.5 rounded hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-600 dark:text-gray-400 transition-colors"
                title="Configure LLM settings"
              >
                <Settings className="w-4 h-4" />
              </button>
            </div>
            <button
              onClick={() => onDeleteStep(node.id)}
              className="p-1.5 rounded hover:bg-red-100 dark:hover:bg-red-900/30 text-red-500 dark:text-red-400 transition-colors"
              title="Delete step"
            >
              <Trash2 className="w-4 h-4" />
            </button>
          </>
        )}
      </div>
    </div>
  )
}

export default NodeDetailPanel

