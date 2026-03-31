import { useEffect } from 'react'
import { X, Settings, AlertCircle, Brain } from 'lucide-react'
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
  const { availableLLMs, loadDefaultsFromBackend } = useLLMStore()
  const tempOverrideLLM = useWorkflowStore(state => state.tempOverrideLLM)
  const setTempOverrideLLM = useWorkflowStore(state => state.setTempOverrideLLM)
  const clearTempOverrideLLM = useWorkflowStore(state => state.clearTempOverrideLLM)
  const tempOverrideLLM2 = useWorkflowStore(state => state.tempOverrideLLM2)
  const setTempOverrideLLM2 = useWorkflowStore(state => state.setTempOverrideLLM2)
  const clearTempOverrideLLM2 = useWorkflowStore(state => state.clearTempOverrideLLM2)
  const tempOverrideLLMEnabled = useWorkflowStore(state => state.tempOverrideLLMEnabled)
  const setTempOverrideLLMEnabled = useWorkflowStore(state => state.setTempOverrideLLMEnabled)
  const fallbackToOriginalLLMOnFailure = useWorkflowStore(state => state.fallbackToOriginalLLMOnFailure)
  const setFallbackToOriginalLLMOnFailure = useWorkflowStore(state => state.setFallbackToOriginalLLMOnFailure)
  const skipLearningWhenTempLLM1 = useWorkflowStore(state => state.skipLearningWhenTempLLM1)
  const setSkipLearningWhenTempLLM1 = useWorkflowStore(state => state.setSkipLearningWhenTempLLM1)
  const skipLearningWhenTempLLM2 = useWorkflowStore(state => state.skipLearningWhenTempLLM2)
  const setSkipLearningWhenTempLLM2 = useWorkflowStore(state => state.setSkipLearningWhenTempLLM2)
  const tempLearningLLM = useWorkflowStore(state => state.tempLearningLLM)
  const setTempLearningLLM = useWorkflowStore(state => state.setTempLearningLLM)
  const clearTempLearningLLM = useWorkflowStore(state => state.clearTempLearningLLM)
  
  // Convert tempOverrideLLM to LLMOption format for the dropdown
  const selectedLLM1: LLMOption | null = tempOverrideLLM
    ? availableLLMs.find(
        llm => llm.provider === tempOverrideLLM.provider && llm.model === tempOverrideLLM.model_id
      ) || null
    : null
  
  const selectedLLM2: LLMOption | null = tempOverrideLLM2
    ? availableLLMs.find(
        llm => llm.provider === tempOverrideLLM2.provider && llm.model === tempOverrideLLM2.model_id
      ) || null
    : null
  
  const selectedLearningLLM: LLMOption | null = tempLearningLLM
    ? availableLLMs.find(
        llm => llm.provider === tempLearningLLM.provider && llm.model === tempLearningLLM.model_id
      ) || null
    : null
  
  // Handle LLM selection from dropdown
  const handleLLM1Select = (llm: LLMOption) => {
    const config: AgentLLMConfig = {
      provider: llm.provider as AgentLLMConfig['provider'],
      model_id: llm.model
    }
    setTempOverrideLLM(config)
  }
  
  const handleLLM2Select = (llm: LLMOption) => {
    const config: AgentLLMConfig = {
      provider: llm.provider as AgentLLMConfig['provider'],
      model_id: llm.model
    }
    setTempOverrideLLM2(config)
  }
  
  const handleLearningLLMSelect = (llm: LLMOption) => {
    const config: AgentLLMConfig = {
      provider: llm.provider as AgentLLMConfig['provider'],
      model_id: llm.model
    }
    setTempLearningLLM(config)
  }
  
  // Handle clear overrides
  const handleClear = () => {
    clearTempOverrideLLM()
    clearTempOverrideLLM2()
    clearTempLearningLLM()
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
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" style={{ zIndex: 50 }}>
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
            <div>
              <p>
                Override LLM for <strong>execution agents only</strong>. Falls back: Temp LLM 1 (cheaper) → Temp LLM 2 (more expensive) → Step LLM.
              </p>
            </div>
          </div>
        </div>

        {/* Enable/Disable Toggle */}
        <div className="px-6 py-4 border-b border-border bg-background">
          <label className="flex items-center justify-between cursor-pointer group">
            <div className="flex-1">
              <div className="text-sm font-medium text-foreground">
                Enable Temp LLM Override
              </div>
              <div className="text-xs text-muted-foreground mt-0.5">
                {tempOverrideLLMEnabled 
                  ? 'Temp LLM overrides are active and will be used during execution'
                  : 'Temp LLM overrides are disabled. Configurations are preserved but not used.'}
              </div>
            </div>
            <div className="ml-4">
              <div className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${
                tempOverrideLLMEnabled ? 'bg-primary' : 'bg-muted'
              }`}>
                <input
                  type="checkbox"
                  checked={tempOverrideLLMEnabled}
                  onChange={(e) => setTempOverrideLLMEnabled(e.target.checked)}
                  className="sr-only"
                />
                <span
                  className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${
                    tempOverrideLLMEnabled ? 'translate-x-6' : 'translate-x-1'
                  }`}
                />
              </div>
            </div>
          </label>
        </div>

        {/* Content */}
        <div className={`flex-1 p-6 space-y-6 min-h-[400px] overflow-y-auto ${!tempOverrideLLMEnabled ? 'opacity-60' : ''}`}>
          {/* Temp LLM 1 */}
          <div className="space-y-3">
            <label className="text-sm font-medium text-foreground block">
              Temp LLM 1 (Cheaper LLM - First Attempt)
            </label>
            <div className="flex items-start gap-2 pb-2">
              <div className="relative flex-1">
                <LLMSelectionDropdown
                  availableLLMs={availableLLMs}
                  selectedLLM={selectedLLM1}
                  onLLMSelect={handleLLM1Select}
                  onRefresh={loadDefaultsFromBackend}
                  disabled={!tempOverrideLLMEnabled}
                  inModal={true}
                  openDirection="down"
                  title="Select Temp LLM 1"
                />
              </div>
              {tempOverrideLLM && (
                <button
                  onClick={() => clearTempOverrideLLM()}
                  className="flex items-center justify-center w-7 h-7 rounded-md border border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive transition-colors"
                  title="Remove Temp LLM 1 override"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              Cheaper LLM for first attempt. Falls back to Temp LLM 2 if validation fails.
            </p>
            {/* Skip learning checkbox for Temp LLM 1 */}
            <label className={`flex items-start gap-2 cursor-pointer group ${!tempOverrideLLMEnabled ? 'opacity-50' : ''}`}>
              <input
                type="checkbox"
                checked={skipLearningWhenTempLLM1}
                onChange={(e) => setSkipLearningWhenTempLLM1(e.target.checked)}
                disabled={!tempOverrideLLMEnabled}
                className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0 disabled:opacity-50"
              />
              <div className="flex-1">
                <div className="text-sm font-medium text-foreground">
                  Skip learning when Temp LLM 1 is used
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  When enabled, learning phases will be skipped when Temp LLM 1 is used. By default, learning runs to capture patterns.
                </div>
              </div>
            </label>
          </div>
          
          {/* Temp LLM 2 */}
          <div className="space-y-3">
            <label className="text-sm font-medium text-foreground block">
              Temp LLM 2 (More Expensive LLM - Second Attempt)
            </label>
            <div className="flex items-start gap-2 pb-2">
              <div className="relative flex-1">
                <LLMSelectionDropdown
                  availableLLMs={availableLLMs}
                  selectedLLM={selectedLLM2}
                  onLLMSelect={handleLLM2Select}
                  onRefresh={loadDefaultsFromBackend}
                  disabled={!tempOverrideLLMEnabled}
                  inModal={true}
                  openDirection="down"
                  title="Select Temp LLM 2"
                />
              </div>
              {tempOverrideLLM2 && (
                <button
                  onClick={() => clearTempOverrideLLM2()}
                  className="flex items-center justify-center w-7 h-7 rounded-md border border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive transition-colors"
                  title="Remove Temp LLM 2 override"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              More expensive, more capable LLM for second attempt. Falls back to step LLM if validation fails.
            </p>
            {/* Skip learning checkbox for Temp LLM 2 */}
            <label className={`flex items-start gap-2 cursor-pointer group ${!tempOverrideLLMEnabled ? 'opacity-50' : ''}`}>
              <input
                type="checkbox"
                checked={skipLearningWhenTempLLM2}
                onChange={(e) => setSkipLearningWhenTempLLM2(e.target.checked)}
                disabled={!tempOverrideLLMEnabled}
                className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0 disabled:opacity-50"
              />
              <div className="flex-1">
                <div className="text-sm font-medium text-foreground">
                  Skip learning when Temp LLM 2 is used
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  When enabled, learning phases will be skipped when Temp LLM 2 is used. By default, learning runs to capture patterns.
                </div>
              </div>
            </label>
          </div>
          
          {/* Temp Learning LLM */}
          <div className="space-y-3 pt-4 border-t border-border">
            <label className="text-sm font-medium text-foreground block">
              Temp Learning LLM (For Existing Learnings)
            </label>
            <div className="flex items-start gap-2 pb-2">
              <div className="relative flex-1">
                <LLMSelectionDropdown
                  availableLLMs={availableLLMs}
                  selectedLLM={selectedLearningLLM}
                  onLLMSelect={handleLearningLLMSelect}
                  onRefresh={loadDefaultsFromBackend}
                  disabled={false}
                  inModal={true}
                  openDirection="down"
                  title="Select Temp Learning LLM"
                />
              </div>
              {tempLearningLLM && (
                <button
                  onClick={() => clearTempLearningLLM()}
                  className="flex items-center justify-center w-7 h-7 rounded-md border border-destructive/30 text-destructive hover:bg-destructive/10 hover:text-destructive transition-colors"
                  title="Remove Temp Learning LLM override"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              )}
            </div>
            <p className="text-xs text-muted-foreground">
              Use this LLM when learnings already exist for a step. For new learning (no existing learnings), the default LLM will be used.
            </p>
          </div>
          
          {/* Fallback to original LLM on failure checkbox */}
          {(tempOverrideLLM || tempOverrideLLM2) && (
            <div className="space-y-2 pt-2 border-t border-border">
              <label className={`flex items-start gap-2 cursor-pointer group ${!tempOverrideLLMEnabled ? 'opacity-50' : ''}`}>
                <input
                  type="checkbox"
                  checked={fallbackToOriginalLLMOnFailure}
                  onChange={(e) => setFallbackToOriginalLLMOnFailure(e.target.checked)}
                  disabled={!tempOverrideLLMEnabled}
                  className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0 disabled:opacity-50"
                />
                <div className="flex-1">
                  <div className="text-sm font-medium text-foreground">
                    Fallback to original LLM on validation failure
                  </div>
                  <div className="text-xs text-muted-foreground mt-0.5">
                    Skip temp overrides and use original LLM when validation fails.
                  </div>
                </div>
              </label>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-4 border-t border-border bg-muted/30 flex-shrink-0">
          <div className="flex items-center gap-2">
            {tempOverrideLLM || tempOverrideLLM2 || tempLearningLLM ? (
              <>
                {(tempOverrideLLM || tempOverrideLLM2) && (
                  <div className="flex items-center gap-1.5">
                    <div title={tempOverrideLLM ? `Temp LLM 1: ${tempOverrideLLM.provider}/${tempOverrideLLM.model_id}` : 'Temp LLM 1: not set'}>
                      <Brain 
                        className={`w-4 h-4 ${tempOverrideLLMEnabled && tempOverrideLLM ? 'text-primary fill-primary/20' : 'text-muted-foreground'}`} 
                      />
                    </div>
                    <span className="text-xs text-muted-foreground">→</span>
                    <div title={tempOverrideLLM2 ? `Temp LLM 2: ${tempOverrideLLM2.provider}/${tempOverrideLLM2.model_id}` : 'Temp LLM 2: not set'}>
                      <Brain 
                        className={`w-4 h-4 ${tempOverrideLLMEnabled && tempOverrideLLM2 ? 'text-primary fill-primary/20' : 'text-muted-foreground'}`} 
                      />
                    </div>
                    <span className="text-xs text-muted-foreground">→</span>
                    <div title="Step LLM (fallback)">
                      <Brain className="w-4 h-4 text-muted-foreground" />
                    </div>
                  </div>
                )}
                {tempOverrideLLMEnabled && (tempOverrideLLM || tempOverrideLLM2) && (
                  <span className={`text-xs ml-2 ${tempOverrideLLMEnabled ? 'text-primary' : 'text-muted-foreground'}`}>
                    {tempOverrideLLMEnabled ? 'Active' : 'Disabled'}
                  </span>
                )}
                {tempLearningLLM && (
                  <div className="flex items-center gap-1.5 ml-2">
                    <div title={`Temp Learning LLM: ${tempLearningLLM.provider}/${tempLearningLLM.model_id}`}>
                      <Brain className="w-4 h-4 text-primary fill-primary/20" />
                    </div>
                    <span className="text-xs text-muted-foreground">Learning</span>
                  </div>
                )}
              </>
            ) : (
              <span className="text-xs text-muted-foreground">No overrides configured</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {(tempOverrideLLM || tempOverrideLLM2 || tempLearningLLM) && (
              <Button
                variant="outline"
                onClick={handleClear}
                className="text-destructive hover:text-destructive"
              >
                Clear All
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
