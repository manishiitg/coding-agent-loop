import { useState } from 'react'
import { Settings, ChevronDown, ChevronRight } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../ui/tooltip'
import { useLLMStore } from '../../stores'

interface LLMConfigurationSummaryProps {
  minimized?: boolean
}

export default function LLMConfigurationSummary({
  minimized = false,
}: LLMConfigurationSummaryProps) {
  const { primaryConfig: llmConfig, setShowLLMModal } = useLLMStore()
  const [isExpanded, setIsExpanded] = useState(false)

  // Get provider display info
  const getProviderInfo = (provider: string) => {
    switch (provider) {
      case 'openrouter':
        return { name: 'OpenRouter', color: 'text-blue-600 dark:text-blue-400' }
      case 'bedrock':
        return { name: 'AWS Bedrock', color: 'text-orange-600 dark:text-orange-400' }
      case 'openai':
        return { name: 'OpenAI', color: 'text-green-600 dark:text-green-400' }
      default:
        return { name: provider, color: 'text-gray-600 dark:text-gray-400' }
    }
  }

  const providerInfo = getProviderInfo(llmConfig.provider)

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
                <span className={`text-sm font-medium ${providerInfo.color}`}>
                  {providerInfo.name}
                </span>
              </div>
              
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">Model:</span>
                <span 
                  className="text-sm font-mono text-foreground truncate max-w-32"
                  title={llmConfig.model_id}
                >
                  {llmConfig.model_id}
                </span>
              </div>
              
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium text-muted-foreground">Fallbacks:</span>
                <span className="text-sm font-mono text-foreground">
                  {llmConfig.fallback_models.length}
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
              <div>• API keys stored securely in browser</div>
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
                <div className={`text-sm font-medium ${providerInfo.color}`}>
                  {providerInfo.name}
                </div>
                <div className="text-xs text-muted-foreground truncate max-w-24">
                  {llmConfig.model_id}
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
