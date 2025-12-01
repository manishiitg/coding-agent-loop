import { useState } from "react";
import { Settings, Star, ChevronDown, ChevronUp } from "lucide-react";
import { useLLMStore, useSavedLLMConfigsStore } from "../../stores";

interface LLMConfigurationSectionProps {
  minimized?: boolean;
}

// Provider badge colors
const getProviderColor = (provider: string) => {
  switch (provider) {
    case 'openrouter': return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200';
    case 'bedrock': return 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200';
    case 'openai': return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200';
    case 'vertex': return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200';
    case 'anthropic': return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200';
    default: return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200';
  }
};

export default function LLMConfigurationSection({
  minimized = false,
}: LLMConfigurationSectionProps) {
  
  // Store subscriptions
  const { primaryConfig, setShowLLMModal } = useLLMStore()
  const { 
    configs, 
    primaryConfigId, 
    fallbackConfigIds,
    setPrimaryConfigId 
  } = useSavedLLMConfigsStore()
  
  const [isExpanded, setIsExpanded] = useState(false);

  // Get current primary saved config
  const primarySavedConfig = primaryConfigId 
    ? configs.find(c => c.id === primaryConfigId) 
    : null

  // Get fallback saved configs
  const fallbackSavedConfigs = fallbackConfigIds
    .map(id => configs.find(c => c.id === id))
    .filter((c): c is NonNullable<typeof c> => c !== null)

  // Handle primary config selection change
  const handlePrimaryChange = (configId: string) => {
    if (configId === '') {
      setPrimaryConfigId(null)
    } else {
      setPrimaryConfigId(configId)
    }
  }

  // Minimized view - just an icon button
  if (minimized) {
    return (
      <div className="relative">
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          title="LLM Configuration"
        >
          <svg
            className="w-5 h-5 text-gray-600 dark:text-gray-400"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
            />
          </svg>
        </button>
        {isExpanded && (
          <div className="absolute left-16 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-4 z-10 min-w-64">
            <div className="space-y-3">
              {/* Quick Selection */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Primary Config
                </label>
                {configs.length === 0 ? (
                  <p className="text-xs text-gray-500 dark:text-gray-400 italic">
                    No saved configs. Open settings to create one.
                  </p>
                ) : (
                  <select
                    value={primaryConfigId || ''}
                    onChange={(e) => handlePrimaryChange(e.target.value)}
                    className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm"
                  >
                    <option value="">Select config...</option>
                    {configs.map((config) => (
                      <option key={config.id} value={config.id}>
                        {config.name} ({config.provider})
                      </option>
                    ))}
                  </select>
                )}
              </div>
              
              {/* Open Modal Button */}
              <button
                onClick={() => setShowLLMModal(true)}
                className="w-full px-3 py-2 bg-black text-white text-sm rounded hover:bg-gray-800 transition-colors flex items-center justify-center gap-2"
              >
                <Settings className="w-4 h-4" />
                Configure LLM
              </button>
            </div>
          </div>
        )}
      </div>
    );
  }

  return (
    <div>
      {/* Header */}
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100 flex items-center gap-2">
          <svg
            className="w-4 h-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
            />
          </svg>
          LLM Configuration
        </h3>
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300 transition-colors"
        >
          {isExpanded ? (
            <ChevronUp className="w-4 h-4" />
          ) : (
            <ChevronDown className="w-4 h-4" />
          )}
        </button>
      </div>

      {/* Content */}
      {isExpanded && (
        <div className="space-y-3">
          {/* Primary Config Selection */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Primary Configuration
            </label>
            {configs.length === 0 ? (
              <div className="text-xs text-gray-500 dark:text-gray-400 italic p-2 bg-gray-50 dark:bg-gray-800 rounded">
                No saved configs yet. Click "Configure LLM" to create one.
              </div>
            ) : (
              <select
                value={primaryConfigId || ''}
                onChange={(e) => handlePrimaryChange(e.target.value)}
                className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              >
                <option value="">Select a saved config...</option>
                {configs.map((config) => (
                  <option key={config.id} value={config.id}>
                    {config.name} ({config.provider}: {config.model_id.slice(0, 30)}{config.model_id.length > 30 ? '...' : ''})
                  </option>
                ))}
              </select>
            )}
          </div>

          {/* Current Selection Display */}
          {primarySavedConfig && (
            <div className="p-3 bg-gray-50 dark:bg-gray-800 rounded-md space-y-2">
              <div className="flex items-center gap-2">
                <Star className="w-4 h-4 text-yellow-500" />
                <span className="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {primarySavedConfig.name}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className={`text-xs px-2 py-0.5 rounded ${getProviderColor(primarySavedConfig.provider)}`}>
                  {primarySavedConfig.provider}
                </span>
                <span className="text-xs text-gray-600 dark:text-gray-400 truncate">
                  {primarySavedConfig.model_id}
                </span>
              </div>
              {primarySavedConfig.options?.temperature !== undefined && (
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  temp: {primarySavedConfig.options.temperature}
                </div>
              )}
            </div>
          )}

          {/* Fallback Summary */}
          {fallbackSavedConfigs.length > 0 && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                Fallback Configs ({fallbackSavedConfigs.length})
              </label>
              <div className="space-y-1 max-h-32 overflow-y-auto">
                {fallbackSavedConfigs.map((config, index) => (
                  <div 
                    key={config.id}
                    className="flex items-center gap-2 p-2 bg-gray-50 dark:bg-gray-800 rounded text-xs"
                  >
                    <span className="text-gray-500 w-4">{index + 1}.</span>
                    <span className={`px-1.5 py-0.5 rounded ${getProviderColor(config.provider)}`}>
                      {config.provider}
                    </span>
                    <span className="text-gray-700 dark:text-gray-300 truncate">
                      {config.name}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Summary when using inline config (no saved config selected) */}
          {!primarySavedConfig && primaryConfig.model_id && (
            <div className="p-3 bg-yellow-50 dark:bg-yellow-900/20 rounded-md">
              <div className="text-xs text-yellow-800 dark:text-yellow-200 mb-2">
                ⚠️ Using inline config (not from saved configs)
              </div>
              <div className="text-xs text-gray-600 dark:text-gray-400">
                Provider: <span className="font-mono">{primaryConfig.provider}</span>
              </div>
              <div className="text-xs text-gray-600 dark:text-gray-400">
                Model: <span className="font-mono truncate">{primaryConfig.model_id}</span>
              </div>
              {primaryConfig.fallback_models.length > 0 && (
                <div className="text-xs text-gray-600 dark:text-gray-400">
                  Fallbacks: {primaryConfig.fallback_models.length}
                </div>
              )}
            </div>
          )}

          {/* Configure Button */}
          <button
            onClick={() => setShowLLMModal(true)}
            className="w-full px-3 py-2 bg-black text-white text-sm rounded hover:bg-gray-800 transition-colors flex items-center justify-center gap-2"
          >
            <Settings className="w-4 h-4" />
            Configure LLM
          </button>
        </div>
      )}
    </div>
  );
}
