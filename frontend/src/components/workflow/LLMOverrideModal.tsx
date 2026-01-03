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
  const { availableLLMs, refreshAvailableLLMs } = useLLMStore()
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
  const saveValidationResponses = useWorkflowStore(state => state.saveValidationResponses)
  const setSaveValidationResponses = useWorkflowStore(state => state.setSaveValidationResponses)
  const disableShellExecAccess = useWorkflowStore(state => state.disableShellExecAccess)
  const setDisableShellExecAccess = useWorkflowStore(state => state.setDisableShellExecAccess)
  const disableReadImageAccess = useWorkflowStore(state => state.disableReadImageAccess)
  const setDisableReadImageAccess = useWorkflowStore(state => state.setDisableReadImageAccess)
  
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
  
  // Handle clear overrides
  const handleClear = () => {
    clearTempOverrideLLM()
    clearTempOverrideLLM2()
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
                  onRefresh={refreshAvailableLLMs}
                  disabled={!tempOverrideLLMEnabled}
                  inModal={true}
                  openDirection="down"
                  title="Select Temp LLM 1"
                />
              </div>
              {tempOverrideLLM && (
                <button
                  onClick={() => clearTempOverrideLLM()}
                  className="px-2 py-1 text-xs text-destructive hover:text-destructive/80"
                  title="Clear Temp LLM 1"
                >
                  Clear
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
                  onRefresh={refreshAvailableLLMs}
                  disabled={!tempOverrideLLMEnabled}
                  inModal={true}
                  openDirection="down"
                  title="Select Temp LLM 2"
                />
              </div>
              {tempOverrideLLM2 && (
                <button
                  onClick={() => clearTempOverrideLLM2()}
                  className="px-2 py-1 text-xs text-destructive hover:text-destructive/80"
                  title="Clear Temp LLM 2"
                >
                  Clear
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
          
          {/* Save execution logs checkbox */}
          <div className="space-y-2 pt-2 border-t border-border">
            <label className="flex items-start gap-2 cursor-pointer group">
              <input
                type="checkbox"
                checked={saveValidationResponses}
                onChange={(e) => setSaveValidationResponses(e.target.checked)}
                className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0"
              />
              <div className="flex-1">
                <div className="text-sm font-medium text-foreground">
                  Save execution logs to workspace
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  When enabled, execution logs are saved to the workspace: validation responses (logs/step-X/validation.json), execution results, and conversation history (logs/step-X/execution/).
                </div>
              </div>
            </label>
          </div>

          {/* Tool Access Control Section */}
          <div className="space-y-3 pt-2 border-t border-border">
            <div className="text-sm font-medium text-foreground">
              Tool Access Control
            </div>
            
            {/* Disable Shell Exec Access */}
            <label className="flex items-start gap-2 cursor-pointer group">
              <input
                type="checkbox"
                checked={disableShellExecAccess}
                onChange={(e) => setDisableShellExecAccess(e.target.checked)}
                className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0"
              />
              <div className="flex-1">
                <div className="text-sm font-medium text-foreground">
                  Disable Shell Exec Access
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  When enabled, the <code className="text-xs bg-muted px-1 py-0.5 rounded">execute_shell_command</code> tool will be disabled globally for all workflow execution agents. This prevents agents from executing shell commands.
                </div>
              </div>
            </label>

            {/* Disable Read Image Access */}
            <label className="flex items-start gap-2 cursor-pointer group">
              <input
                type="checkbox"
                checked={disableReadImageAccess}
                onChange={(e) => setDisableReadImageAccess(e.target.checked)}
                className="mt-0.5 w-4 h-4 rounded border-border text-primary focus:ring-primary focus:ring-2 focus:ring-offset-0"
              />
              <div className="flex-1">
                <div className="text-sm font-medium text-foreground">
                  Disable Read Image Access
                </div>
                <div className="text-xs text-muted-foreground mt-0.5">
                  When enabled, the <code className="text-xs bg-muted px-1 py-0.5 rounded">read_image</code> tool will be disabled globally for all workflow execution agents. This prevents agents from reading and analyzing image files.
                </div>
              </div>
            </label>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-4 border-t border-border bg-muted/30 flex-shrink-0">
          <div className="flex items-center gap-2">
            {tempOverrideLLM || tempOverrideLLM2 ? (
              <>
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
                <span className={`text-xs ml-2 ${tempOverrideLLMEnabled ? 'text-primary' : 'text-muted-foreground'}`}>
                  {tempOverrideLLMEnabled ? 'Active' : 'Disabled'}
                </span>
              </>
            ) : (
              <span className="text-xs text-muted-foreground">No overrides configured</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {(tempOverrideLLM || tempOverrideLLM2) && (
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

