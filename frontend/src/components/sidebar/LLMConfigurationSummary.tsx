import { useState, useMemo } from 'react'
import { Settings, ChevronDown, ChevronRight } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { useLLMStore, useAppStore } from '../../stores'
import { getModelDisplayName, getProviderDisplayInfo } from '../../utils/llmDisplay'

interface LLMConfigurationSummaryProps {
  minimized?: boolean
}

export default function LLMConfigurationSummary({
  minimized = false,
}: LLMConfigurationSummaryProps) {
  // Get current mode from app store
  const agentMode = useAppStore(state => state.agentMode)
  const currentMode: 'multi-agent' | 'workflow' = agentMode === 'workflow' ? 'workflow' : 'multi-agent'

  const { getConfigForMode, setShowLLMModal, savedLLMs, availableLLMs, modelMetadataCatalog } = useLLMStore()

  // Get mode-specific config
  const modeConfig = getConfigForMode(currentMode)
  const agentConfig = modeConfig.agentConfig
  const primaryConfig = modeConfig.primaryConfig

  const [isExpanded, setIsExpanded] = useState(false)

  const currentLLM = useMemo(() => {
    const primaryLLM = agentConfig?.primary || primaryConfig
    if (primaryLLM?.provider && primaryLLM?.model_id) {
      return { provider: primaryLLM.provider, model_id: primaryLLM.model_id }
    }

    const firstPublishedLLM = savedLLMs[0]
    if (firstPublishedLLM?.provider && firstPublishedLLM?.model_id) {
      return { provider: firstPublishedLLM.provider, model_id: firstPublishedLLM.model_id }
    }

    const firstAvailableLLM = availableLLMs[0]
    if (firstAvailableLLM?.provider && firstAvailableLLM?.model) {
      return { provider: firstAvailableLLM.provider, model_id: firstAvailableLLM.model }
    }

    return { provider: '', model_id: '' }
  }, [agentConfig?.primary, primaryConfig, savedLLMs, availableLLMs])

  const providerInfo = getProviderDisplayInfo(currentLLM.provider)
  const modelDisplayName = getModelDisplayName({
    provider: currentLLM.provider,
    modelId: currentLLM.model_id,
    metadata: modelMetadataCatalog,
    savedLLMs,
    availableLLMs,
  })

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
              title="LLM Configuration"
            >
              <Settings className="w-5 h-5" />
            </button>
          </TooltipTrigger>
          <TooltipContent>
            <p>LLM Configuration - {providerInfo.name}</p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    )
  }

  return (
    <TooltipProvider>
      <div>
        {/* Header */}
        <div className="flex items-center justify-between mb-2">
          <h3 className="text-sm font-semibold text-foreground flex items-center gap-2">
            <Settings className="w-4 h-4" />
            LLM Configuration
            <span className={`text-xs px-1.5 py-0.5 rounded-full ${
              currentMode === 'workflow'
                ? 'bg-purple-500/20 text-purple-400'
                : 'bg-blue-500/20 text-blue-400'
            }`}>
              {currentMode === 'workflow' ? 'Workflow' : 'Chat'}
            </span>
          </h3>
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className="text-muted-foreground hover:text-foreground transition-colors"
          >
            {isExpanded ? (
              <ChevronDown className="w-4 h-4" />
            ) : (
              <ChevronRight className="w-4 h-4" />
            )}
          </button>
        </div>

        {/* Content */}
        {isExpanded && (
          <div className="space-y-3">
            {/* Current Configuration Display */}
            <div className="bg-card rounded-md p-3 space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">Provider:</span>
                <span className={`text-sm font-medium ${providerInfo.colorClass}`}>
                  {providerInfo.name}
                </span>
              </div>
              
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">Model:</span>
                <span 
                  className="text-sm font-mono text-foreground truncate max-w-32"
                  title={currentLLM.model_id}
                >
                  {modelDisplayName}
                </span>
              </div>
            </div>

            {/* Configure Button */}
            <button
              onClick={(e) => {
                e.stopPropagation()
                setShowLLMModal(true)
              }}
              className="w-full px-3 py-2 bg-primary hover:bg-primary/90 text-primary-foreground text-sm font-medium rounded-md transition-colors focus:ring-2 focus:ring-primary focus:ring-offset-2 focus:ring-offset-background"
            >
              Configure LLM Settings
            </button>

            {/* Quick Info */}
            <div className="text-xs text-muted-foreground space-y-1">
              <div>• API keys stored securely in workspace</div>
              <div>• Changes apply to new conversations</div>
              <div>• Test keys before saving</div>
            </div>
          </div>
        )}

        {/* Collapsed Summary */}
        {!isExpanded && (
          <div 
            className="bg-card rounded-md p-3 cursor-pointer hover:bg-secondary transition-colors"
            onClick={(e) => {
              e.stopPropagation()
              setShowLLMModal(true)
            }}
          >
            <div className="flex items-center justify-between">
              <div>
                <div className={`text-sm font-medium ${providerInfo.colorClass}`}>
                  {providerInfo.name}
                </div>
                <div className="text-xs text-muted-foreground truncate max-w-24">
                  {modelDisplayName}
                </div>
              </div>
              <Settings className="w-4 h-4 text-muted-foreground" />
            </div>
          </div>
        )}
      </div>
    </TooltipProvider>
  )
}
