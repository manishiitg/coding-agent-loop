import React, { useState, useMemo } from 'react'
import { X, Plus } from 'lucide-react'
import type { PlanStep, AgentConfigs, PrerequisiteRule } from '../../../utils/stepConfigMatching'

interface PrerequisiteConfigPanelProps {
  agentConfigs: AgentConfigs | undefined
  onUpdate: (configs: AgentConfigs) => void
  planSteps: PlanStep[] // All steps in the plan (for dependency selection)
  currentStepIndex: number // Current step index (0-based)
}

export const PrerequisiteConfigPanel: React.FC<PrerequisiteConfigPanelProps> = ({
  agentConfigs,
  onUpdate,
  planSteps,
  currentStepIndex,
}) => {
  const [enablePrerequisiteDetection, setEnablePrerequisiteDetection] = useState(
    agentConfigs?.enable_prerequisite_detection ?? false
  )
  const [rules, setRules] = useState<PrerequisiteRule[]>(
    agentConfigs?.prerequisite_rules ?? []
  )

  // Get available previous steps (steps before current step)
  const availableSteps = useMemo(() => {
    return planSteps.slice(0, currentStepIndex).map((step, idx) => ({
      id: step.id,
      title: step.title || `Step ${idx + 1}`,
      index: idx,
    }))
  }, [planSteps, currentStepIndex])

  // First step (index 0) cannot have prerequisites
  if (currentStepIndex === 0) {
    return null
  }

  // Handle enable/disable toggle
  const handleToggle = (enabled: boolean) => {
    setEnablePrerequisiteDetection(enabled)
    updateConfigs(enabled, rules)
  }

  // Handle adding a new rule
  const handleAddRule = () => {
    const newRule: PrerequisiteRule = {
      depends_on_step: '',
      description: '',
    }
    const newRules = [...rules, newRule]
    setRules(newRules)
    updateConfigs(enablePrerequisiteDetection, newRules)
  }

  // Handle removing a rule
  const handleRemoveRule = (index: number) => {
    const newRules = rules.filter((_, i) => i !== index)
    setRules(newRules)
    updateConfigs(enablePrerequisiteDetection, newRules)
  }

  // Handle updating a rule's step dependency
  const handleUpdateRuleStep = (index: number, stepId: string) => {
    const newRules = [...rules]
    newRules[index] = {
      ...newRules[index],
      depends_on_step: stepId,
    }
    setRules(newRules)
    updateConfigs(enablePrerequisiteDetection, newRules)
  }

  // Handle updating a rule's description
  const handleUpdateRuleDescription = (index: number, description: string) => {
    const newRules = [...rules]
    newRules[index] = {
      ...newRules[index],
      description,
    }
    setRules(newRules)
    updateConfigs(enablePrerequisiteDetection, newRules)
  }

  // Update agent configs
  const updateConfigs = (enabled: boolean, updatedRules: PrerequisiteRule[]) => {
    const updatedConfigs: AgentConfigs = {
      ...agentConfigs,
      enable_prerequisite_detection: enabled,
      prerequisite_rules: enabled && updatedRules.length > 0 ? updatedRules : undefined,
    }
    onUpdate(updatedConfigs)
  }


  // Get available steps for a rule (steps not already used in other rules, or the current rule's step)
  const getAvailableStepsForRule = (ruleIndex: number) => {
    const currentRuleStepId = rules[ruleIndex]?.depends_on_step
    return availableSteps.filter(step => {
      // Include if it's the current rule's step (so user can keep it selected)
      if (step.id === currentRuleStepId) return true
      // Exclude if it's used in another rule
      return !rules.some((r, i) => i !== ruleIndex && r.depends_on_step === step.id)
    })
  }

  return (
    <div className="border-t border-gray-200 dark:border-gray-700 pt-4 mt-4">
      <div className="flex items-center justify-between mb-3">
        <label className="flex items-center space-x-2 cursor-pointer">
          <input
            type="checkbox"
            checked={enablePrerequisiteDetection}
            onChange={(e) => handleToggle(e.target.checked)}
            className="w-4 h-4 text-blue-600 border-gray-300 dark:border-gray-600 rounded focus:ring-blue-500 dark:bg-gray-700 dark:checked:bg-blue-600"
          />
          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">
            Enable prerequisite detection
          </span>
        </label>
      </div>

      {enablePrerequisiteDetection && (
        <div className="space-y-4 mt-4">
          {/* Prerequisite Rules */}
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">
              Prerequisite Rules:
            </label>
            <div className="space-y-3">
              {rules.map((rule, ruleIndex) => (
                <div
                  key={ruleIndex}
                  className="p-3 bg-gray-50 dark:bg-gray-900/20 rounded-lg border border-gray-200 dark:border-gray-700 space-y-2"
                >
                  <div className="flex items-center justify-between mb-2">
                    <span className="text-xs font-semibold text-gray-700 dark:text-gray-300">
                      Rule {ruleIndex + 1}
                    </span>
                    <button
                      type="button"
                      onClick={() => handleRemoveRule(ruleIndex)}
                      className="text-gray-400 dark:text-gray-500 hover:text-red-600 dark:hover:text-red-400 transition-colors"
                      title="Remove rule"
                    >
                      <X className="w-4 h-4" />
                    </button>
                  </div>

                  {/* Step Selection */}
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                      Depends on step:
                    </label>
                    <select
                      value={rule.depends_on_step}
                      onChange={(e) => handleUpdateRuleStep(ruleIndex, e.target.value)}
                      className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    >
                      <option value="">Select a step...</option>
                      {getAvailableStepsForRule(ruleIndex).map((step) => (
                        <option key={step.id} value={step.id}>
                          Step {step.index + 1} - {step.title}
                        </option>
                      ))}
                    </select>
                  </div>

                  {/* Description */}
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                      Detection description:
                    </label>
                    <textarea
                      value={rule.description}
                      onChange={(e) => handleUpdateRuleDescription(ruleIndex, e.target.value)}
                      placeholder='e.g., "if login session is missing or expired, go back to step 0"'
                      className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 placeholder:text-gray-400 dark:placeholder:text-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent resize-y min-h-[60px]"
                      rows={2}
                    />
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-500">
                      Describe when to detect prerequisite failures for this specific step
                    </p>
                  </div>
                </div>
              ))}

              {/* Add Rule Button */}
              {availableSteps.length > 0 && (
                <button
                  type="button"
                  onClick={handleAddRule}
                  className="w-full flex items-center justify-center gap-2 px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-600 transition-colors"
                >
                  <Plus className="w-4 h-4" />
                  Add prerequisite rule
                </button>
              )}

              {availableSteps.length === 0 && rules.length === 0 && (
                <p className="text-xs text-gray-500 dark:text-gray-500 italic">
                  No previous steps available
                </p>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
