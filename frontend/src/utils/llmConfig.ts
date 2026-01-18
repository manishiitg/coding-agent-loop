// Shared LLM configuration utilities

export const OPENROUTER_MODELS = [
  "openai/gpt-4o-mini",
  "openai/gpt-4o",
  "anthropic/claude-3.5-sonnet",
  "anthropic/claude-3-haiku",
  "google/gemini-flash-1.5",
  "meta-llama/llama-3.1-70b-instruct",
  "mistral/mistral-large",
];

export const getAvailableModels = (provider: string): string[] => {
  if (provider === "openrouter") {
    return OPENROUTER_MODELS;
  } else if (provider === "bedrock") {
    return [
      "anthropic.claude-3-sonnet-20240229-v1:0",
      "anthropic.claude-3-haiku-20240307-v1:0",
      "anthropic.claude-3-5-sonnet-20240620-v1:0"
    ];
  } else if (provider === "openai") {
    return ["gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo"];
  } else if (provider === "vertex") {
    return ["gemini-1.5-flash-001", "gemini-1.5-pro-001"];
  } else if (provider === "anthropic") {
    return ["claude-3-5-sonnet-20240620", "claude-3-opus-20240229", "claude-3-sonnet-20240229", "claude-3-haiku-20240307"];
  }
  return [];
};

// Get fallback providers for a given provider
export const getFallbackProviders = (currentProvider: string): string[] => {
  if (currentProvider === "openrouter") {
    return ["openai", "bedrock", "vertex", "anthropic"];
  } else if (currentProvider === "bedrock") {
    return ["openrouter", "openai", "vertex", "anthropic"];
  } else if (currentProvider === "openai") {
    return ["openrouter", "bedrock", "vertex", "anthropic"];
  } else if (currentProvider === "vertex") {
    return ["openrouter", "openai", "bedrock", "anthropic"];
  } else if (currentProvider === "anthropic") {
    return ["openrouter", "openai", "bedrock", "vertex"];
  }
  return [];
};