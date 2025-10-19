import { useState } from "react";
import { OPENROUTER_MODELS, getAvailableModels, getFallbackProviders } from "../../utils/llmConfig";
import { useLLMStore } from "../../stores";

interface LLMConfigurationSectionProps {
  minimized?: boolean;
}


export default function LLMConfigurationSection({
  minimized = false,
}: LLMConfigurationSectionProps) {
  
  // Store subscriptions
  const { primaryConfig: llmConfig, setPrimaryConfig: setLlmConfig } = useLLMStore()
  const [isExpanded, setIsExpanded] = useState(false);
  const [showCrossProvider, setShowCrossProvider] = useState(false);
  const [customModelInput, setCustomModelInput] = useState("");
  const [customModels, setCustomModels] = useState<string[]>(() => {
    const saved = localStorage.getItem('openrouter_custom_models');
    return saved ? JSON.parse(saved) : [];
  });


  // Handle provider change
  const handleProviderChange = (provider: "openrouter" | "bedrock") => {
    const baseModels = getAvailableModels(provider);
    const availableModels = provider === "openrouter" ? [...baseModels, ...customModels] : baseModels;
    let fallbackModels: string[] = [];

    // Set appropriate fallback models based on provider
    if (provider === "openrouter") {
      fallbackModels = ["z-ai/glm-4.5", "openai/gpt-4o-mini"];
    } else if (provider === "bedrock") {
      fallbackModels = ["us.anthropic.claude-sonnet-4-20250514-v1:0","us.anthropic.claude-3-7-sonnet-20250219-v1:0"];
    }

    const newConfig = {
      ...llmConfig,
      provider,
      model_id: availableModels[0] || "", // Select first available model
      fallback_models: fallbackModels,
       cross_provider_fallback:
         provider === "openrouter"
           ? {
               provider: "openai" as const,
               models: ["gpt-5-mini"],
             }
           : {
               provider: "openrouter" as const,
               models: ["x-ai/grok-code-fast-1", "openai/gpt-5-mini"],
             },
    };
    setLlmConfig(newConfig);
    setShowCrossProvider(false);
    
    // Refresh available LLMs to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  };

  // Handle model change
  const handleModelChange = (model_id: string) => {
    setLlmConfig({
      ...llmConfig,
      model_id,
    });
    
    // Refresh available LLMs to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  };

  // Handle fallback models change
  const handleFallbackChange = (model: string, checked: boolean) => {
    const currentFallbacks = llmConfig.fallback_models || [];
    const newFallbacks = checked
      ? [...currentFallbacks, model]
      : currentFallbacks.filter((m) => m !== model);

    setLlmConfig({
      ...llmConfig,
      fallback_models: newFallbacks,
    });
  };

  // Handle cross-provider fallback change
  const handleCrossProviderChange = (provider: "openai" | "bedrock") => {
    const availableModels = getAvailableModels(provider);
    setLlmConfig({
      ...llmConfig,
      cross_provider_fallback: {
        provider,
        models: [availableModels[0] || ""], // Select first available model
      },
    });
  };

  // Handle cross-provider models change
  const handleCrossProviderModelsChange = (model: string, checked: boolean) => {
    if (!llmConfig.cross_provider_fallback) return;

    const currentModels = llmConfig.cross_provider_fallback.models;
    const newModels = checked
      ? [...currentModels, model]
      : currentModels.filter((m) => m !== model);

    setLlmConfig({
      ...llmConfig,
      cross_provider_fallback: {
        ...llmConfig.cross_provider_fallback,
        models: newModels,
      },
    });
  };

  // Handle adding custom model
  const handleAddCustomModel = () => {
    const model = customModelInput.trim();
    if (!model) return;

    // Check if model already exists
    const allModels = [...OPENROUTER_MODELS, ...customModels];
    if (allModels.includes(model)) {
      alert("Model already exists!");
      return;
    }

    // Basic validation - should contain a slash (provider/model format)
    if (!model.includes('/')) {
      alert("Model should be in format 'provider/model-name'");
      return;
    }

    const newCustomModels = [...customModels, model];
    setCustomModels(newCustomModels);
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels));
    setCustomModelInput("");
    
    // Refresh available LLMs in the store to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  };

  // Handle removing custom model
  const handleRemoveCustomModel = (model: string) => {
    const newCustomModels = customModels.filter(m => m !== model);
    setCustomModels(newCustomModels);
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels));
    
    // Refresh available LLMs in the store to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
    
    // If the removed model was selected, reset to first available model
    if (llmConfig.model_id === model) {
      const availableModels = [...OPENROUTER_MODELS, ...newCustomModels];
      setLlmConfig({
        ...llmConfig,
        model_id: availableModels[0] || "",
      });
    }
  };


  // Get current provider's available models
  const availableModels = getAvailableModels(llmConfig.provider);
  const fallbackProviders = getFallbackProviders(llmConfig.provider);

  if (minimized) {
    return (
      <div className="flex flex-col items-center py-2">
        <button
          onClick={() => setIsExpanded(!isExpanded)}
          className="p-2 text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200 transition-colors"
          title="LLM Configuration"
        >
          <svg
            className="w-5 h-5"
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
              {/* Provider Selection */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Provider
                </label>
                <div className="space-y-2">
                  <label className="flex items-center">
                    <input
                      type="radio"
                      name="provider"
                      value="openrouter"
                      checked={llmConfig.provider === "openrouter"}
                      onChange={() => handleProviderChange("openrouter")}
                      className="mr-2"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">
                      OpenRouter
                    </span>
                  </label>
                  <label className="flex items-center">
                    <input
                      type="radio"
                      name="provider"
                      value="bedrock"
                      checked={llmConfig.provider === "bedrock"}
                      onChange={() => handleProviderChange("bedrock")}
                      className="mr-2"
                    />
                    <span className="text-sm text-gray-700 dark:text-gray-300">
                      Bedrock
                    </span>
                  </label>
                </div>
              </div>

              {/* Model Selection */}
              <div>
                <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
                  Model
                </label>
                <select
                  value={llmConfig.model_id}
                  onChange={(e) => handleModelChange(e.target.value)}
                  className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm"
                >
                  {availableModels.map((model) => (
                    <option key={model} value={model}>
                      {model}
                    </option>
                  ))}
                </select>
              </div>
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
          <svg
            className={`w-4 h-4 transition-transform ${isExpanded ? "rotate-180" : ""}`}
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M19 9l-7 7-7-7"
            />
          </svg>
        </button>
      </div>

      {/* Content */}
      {isExpanded && (
        <div className="space-y-3">
          {/* Provider Selection */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Primary Provider
            </label>
            <div className="space-y-2">
              <label className="flex items-center">
                <input
                  type="radio"
                  name="provider"
                  value="openrouter"
                  checked={llmConfig.provider === "openrouter"}
                  onChange={() => handleProviderChange("openrouter")}
                  className="mr-2 text-blue-600"
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">
                  OpenRouter
                </span>
              </label>
              <label className="flex items-center">
                <input
                  type="radio"
                  name="provider"
                  value="bedrock"
                  checked={llmConfig.provider === "bedrock"}
                  onChange={() => handleProviderChange("bedrock")}
                  className="mr-2 text-blue-600"
                />
                <span className="text-sm text-gray-700 dark:text-gray-300">
                  Bedrock
                </span>
              </label>
            </div>
          </div>

          {/* Model Selection */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Primary Model
            </label>
            <div className="relative">
              <select
                value={llmConfig.model_id}
                onChange={(e) => handleModelChange(e.target.value)}
                className="w-full px-3 py-2 pr-8 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
              >
                {availableModels.map((model) => (
                  <option key={model} value={model}>
                    {model}
                  </option>
                ))}
              </select>
              
              {/* Remove button for custom models */}
              {llmConfig.provider === "openrouter" && customModels.includes(llmConfig.model_id) && (
                <button
                  onClick={() => handleRemoveCustomModel(llmConfig.model_id)}
                  className="absolute right-2 top-1/2 transform -translate-y-1/2 text-red-500 hover:text-red-700 text-sm p-1 hover:bg-red-50 dark:hover:bg-red-900/20 rounded"
                  title="Remove this custom model"
                >
                  ×
                </button>
              )}
            </div>
            
            {/* Custom Model Input - Only for OpenRouter */}
            {llmConfig.provider === "openrouter" && (
              <div className="mt-3">
                <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                  Add Custom Model
                </label>
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={customModelInput}
                    onChange={(e) => setCustomModelInput(e.target.value)}
                    placeholder="provider/model-name"
                    className="flex-1 px-2 py-1 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:ring-1 focus:ring-blue-500 focus:border-blue-500"
                    onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()}
                  />
                  <button
                    onClick={handleAddCustomModel}
                    className="px-3 py-1 bg-black text-white text-xs rounded hover:bg-gray-800 transition-colors focus:ring-1 focus:ring-gray-500 focus:ring-offset-1"
                  >
                    Add
                  </button>
                </div>
                
                {/* Custom Models List */}
                {customModels.length > 0 && (
                  <div className="mt-2">
                    <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Custom Models:</div>
                    <div className="space-y-1 max-h-32 overflow-y-auto">
                      {customModels.map((model) => (
                        <div key={model} className="flex items-center justify-between bg-gray-50 dark:bg-gray-800 rounded-md px-3 py-2">
                          <span className="text-sm text-gray-700 dark:text-gray-300 truncate flex-1">{model}</span>
                          <button
                            onClick={() => handleRemoveCustomModel(model)}
                            className="text-red-500 hover:text-red-700 text-sm ml-2 p-1 hover:bg-red-50 dark:hover:bg-red-900/20 rounded"
                            title="Remove model"
                          >
                            ×
                          </button>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Fallback Models */}
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">
              Fallback Models
            </label>
            <div className="max-h-32 overflow-y-auto space-y-1" key={`fallback-${llmConfig.model_id}`}>
              {availableModels
                .filter(model => model !== llmConfig.model_id) // Exclude currently selected model
                .map((model) => (
                <label key={model} className="flex items-center">
                  <input
                    type="checkbox"
                    checked={llmConfig.fallback_models.includes(model)}
                    onChange={(e) =>
                      handleFallbackChange(model, e.target.checked)
                    }
                    className="mr-2 text-blue-600"
                  />
                  <span className="text-xs text-gray-600 dark:text-gray-400 truncate">
                    {model}
                  </span>
                </label>
              ))}
            </div>
            {llmConfig.provider === "bedrock" && (
              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                For Bedrock, fallback is automatically set to Claude 3.7 Sonnet. Cross-provider fallback to OpenRouter is configured below.
              </p>
            )}
          </div>

          {/* Cross-Provider Fallback - For OpenRouter and Bedrock */}
          {(llmConfig.provider === "openrouter" || llmConfig.provider === "bedrock") && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                  Cross-Provider Fallback
                </label>
                <button
                  onClick={() => setShowCrossProvider(!showCrossProvider)}
                  className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  {showCrossProvider ? "Hide" : "Show"}
                </button>
              </div>

              {showCrossProvider && (
                <div className="space-y-3 pl-4 border-l-2 border-gray-200 dark:border-gray-600">
                  <div>
                    <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                      Fallback Provider
                    </label>
                    <select
                      value={llmConfig.cross_provider_fallback?.provider || ""}
                      onChange={(e) =>
                        handleCrossProviderChange(
                          e.target.value as "openai" | "bedrock"
                        )
                      }
                      className="w-full px-2 py-1 border border-gray-300 dark:border-gray-600 rounded text-xs bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
                    >
                      <option value="">Select provider...</option>
                      {fallbackProviders.map((provider) => (
                        <option key={provider} value={provider}>
                          {provider.charAt(0).toUpperCase() + provider.slice(1)}
                        </option>
                      ))}
                    </select>
                  </div>

                  {llmConfig.cross_provider_fallback?.provider && (
                    <div>
                      <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
                        Fallback Models
                      </label>
                      <div className="max-h-24 overflow-y-auto space-y-1">
                        {getAvailableModels(
                          llmConfig.cross_provider_fallback.provider
                        ).map((model) => (
                          <label key={model} className="flex items-center">
                            <input
                              type="checkbox"
                              checked={
                                llmConfig.cross_provider_fallback?.models.includes(
                                  model
                                ) || false
                              }
                              onChange={(e) =>
                                handleCrossProviderModelsChange(
                                  model,
                                  e.target.checked
                                )
                              }
                              className="mr-2 text-blue-600"
                            />
                            <span className="text-xs text-gray-600 dark:text-gray-400 truncate">
                              {model}
                            </span>
                          </label>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Summary */}
          <div className="pt-2 border-t border-gray-200 dark:border-gray-600">
            <div className="text-xs text-gray-500 dark:text-gray-400">
              <div className="flex items-center justify-between">
                <span>Provider:</span>
                <span className="font-mono">{llmConfig.provider}</span>
              </div>
              <div className="flex items-center justify-between">
                <span>Model:</span>
                <span
                  className="font-mono truncate max-w-32"
                  title={llmConfig.model_id}
                >
                  {llmConfig.model_id}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span>Fallbacks:</span>
                <span className="font-mono">
                  {llmConfig.fallback_models.length}
                </span>
              </div>
              {llmConfig.cross_provider_fallback && (
                <div className="flex items-center justify-between">
                  <span>Cross-Provider:</span>
                  <span className="font-mono">
                    {llmConfig.cross_provider_fallback.provider}
                  </span>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
