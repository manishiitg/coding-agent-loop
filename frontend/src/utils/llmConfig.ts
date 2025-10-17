// Shared LLM configuration utilities

// Available models for each provider (shared with sidebar)
export const OPENROUTER_MODELS = [
  "x-ai/grok-code-fast-1",
  "openai/gpt-5-mini",
];

export const BEDROCK_MODELS = [
  "global.anthropic.claude-sonnet-4-5-20250929-v1:0",
  "us.anthropic.claude-sonnet-4-20250514-v1:0",
  "us.anthropic.claude-3-7-sonnet-20250219-v1:0",
];

export const OPENAI_MODELS = [
  "gpt-5-mini",
];

// Get available models for a provider
export const getAvailableModels = (provider: string): string[] => {
  switch (provider) {
    case "openrouter": {
      // Include custom models for OpenRouter (same as sidebar)
      const customModels = (() => {
        const saved = localStorage.getItem('openrouter_custom_models');
        return saved ? JSON.parse(saved) : [];
      })();
      return [...OPENROUTER_MODELS, ...customModels];
    }
    case "bedrock":
      return BEDROCK_MODELS;
    case "openai":
      return OPENAI_MODELS;
    default:
      return [];
  }
};

// Get all available LLM options for dropdown
export const getAllAvailableLLMs = (): LLMOption[] => {
  // Get custom models from localStorage (same as sidebar)
  const customModels = (() => {
    const saved = localStorage.getItem('openrouter_custom_models');
    return saved ? JSON.parse(saved) : [];
  })();

  return [
    // OpenRouter models (including custom ones)
    ...OPENROUTER_MODELS.map(model => ({
      provider: 'openrouter' as const,
      model,
      label: `OpenRouter - ${model}`,
      description: 'OpenRouter model'
    })),
    // Custom OpenRouter models
    ...customModels.map((model: string) => ({
      provider: 'openrouter' as const,
      model,
      label: `OpenRouter - ${model}`,
      description: 'Custom OpenRouter model'
    })),
    // Bedrock models
    ...BEDROCK_MODELS.map(model => ({
      provider: 'bedrock' as const,
      model,
      label: `Bedrock - ${model}`,
      description: 'AWS Bedrock model'
    })),
    // OpenAI models
    ...OPENAI_MODELS.map(model => ({
      provider: 'openai' as const,
      model,
      label: `OpenAI - ${model}`,
      description: 'OpenAI model'
    }))
  ];
};

// Get fallback providers for a given provider
export const getFallbackProviders = (currentProvider: string): string[] => {
  if (currentProvider === "openrouter") {
    return ["openai", "bedrock"];
  } else if (currentProvider === "bedrock") {
    return ["openrouter", "openai"];
  }
  return [];
};
