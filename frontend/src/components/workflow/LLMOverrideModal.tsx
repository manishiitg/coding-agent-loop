import { useEffect } from 'react'
import { X, Settings, AlertCircle } from 'lucide-react'
import { Button } from '../ui/Button'
import { useLLMStore } from '../../stores'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import type { AgentLLMConfig } from '../../services/api-types'
import type { LLMOption } from '../../types/llm'
import LLMSelectionDropdown from '../LLMSelectionDropdown'

interface LLMOverrideModalProps {
  isOpen: boolean
  onClose: () => void
}

export default function LLMOverrideModal({ isOpen, onClose }: LLMOverrideModalProps) {
  const { availableLLMs, refreshAvailableLLMs } = useLLMStore()
  const tempOverrideLLM = useWorkflowStore(state => state.tempOverrideLLM)
  const setTempOverrideLLM = useWorkflowStore(state => state.setTempOverrideLLM)
  const clearTempOverrideLLM = useWorkflowStore(state => state.clearTempOverrideLLM)
  const fallbackToOriginalLLMOnFailure = useWorkflowStore(state => state.fallbackToOriginalLLMOnFailure)
  const setFallbackToOriginalLLMOnFailure = useWorkflowStore(state => state.setFallbackToOriginalLLMOnFailure)
  
  // Convert tempOverrideLLM to LLMOption format for the dropdown
  const selectedLLM: LLMOption | null = tempOverrideLLM
    ? availableLLMs.find(
        llm => llm.provider === tempOverrideLLM.provider && llm.model === tempOverrideLLM.model_id
      ) || null
    : null
  
  // Handle LLM selection from dropdown
  const handleLLMSelect = (llm: LLMOption) => {
    const config: AgentLLMConfig = {
      provider: llm.provider as AgentLLMConfig['provider'],
      model_id: llm.model
    }
    setTempOverrideLLM(config)
  }
  
  // Handle clear override
  const handleClear = () => {
    clearTempOverrideLLM()
    onClose()
  }
  
  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])
  
  if (!isOpen) return null
  
  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-2xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <Settings className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold text-foreground">Execution Agent LLM Override</h2>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="h-8 w-8 p-0 hover:bg-secondary"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Description */}
        <div className="px-4 py-3 border-b border-border bg-muted/30">
          <div className="flex items-start gap-2 text-sm text-muted-foreground">
            <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
            <p>
              Override the LLM for <strong>execution agents only</strong> during this execution.
              Validation and learning agents will continue using their step-level or preset-level configurations.
              This takes priority over step-level and preset-level configurations for execution agents.
              The override clears when you refresh the page.
            </p>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 p-6 space-y-4 min-h-[300px]">
          <div className="space-y-3">
            <label className="text-sm font-medium text-foreground block">
              Select LLM for Execution Agents
            </label>
            <div className="flex items-start gap-2 pb-4">
              <div className="relative z-10">
                <LLMSelectionDropdown
                  availableLLMs={availableLLMs}
                  selectedLLM={selectedLLM}
                  onLLMSelect={handleLLMSelect}
                  onRefresh={refreshAvailableLLMs}
                  disabled={false}
                  inModal={true}
                  openDirection="down"
                />
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              This LLM will be used for execution agents across all steps. Validation and learning agents are not affected.
            </p>
          </div>
          
          {/* Fallback to original LLM on failure checkbox */}
          {tempOverrideLLM && (
            <div className="space-y-2 pt-2 border-t border-border">
              <label className="flex items-start gap-2 cursor-pointer group">
                <input
                  type="checkbox"
                  checked={fallbackToOriginalLLMOnFailure}
                  onChange={(e) => setFallbackToOriginalLLMOnFailure(e.target.checked)}
                  className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0"
                />
                <div className="flex-1">
                  <div className="text-sm font-medium text-foreground">
                    Fallback to original LLM on validation failure
                  </div>
                  <div className="text-xs text-muted-foreground mt-0.5">
                    If validation fails, retry with the original LLM (step config → preset → orchestrator default) instead of the temp override
                  </div>
                </div>
              </label>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-4 border-t border-border bg-muted/30 flex-shrink-0">
          <div className="text-xs text-muted-foreground">
            {tempOverrideLLM 
              ? `Current override: ${tempOverrideLLM.provider}/${tempOverrideLLM.model_id}`
              : 'No override active (using step/preset configs)'
            }
          </div>
          <div className="flex items-center gap-2">
            {tempOverrideLLM && (
              <Button
                variant="outline"
                onClick={handleClear}
                className="text-destructive hover:text-destructive"
              >
                Clear Override
              </Button>
            )}
            <Button variant="outline" onClick={onClose}>
              Close
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

