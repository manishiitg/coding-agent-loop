export const getAvailableModels = (provider: string): string[] => {
  if (provider === "bedrock") {
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
  if (currentProvider === "bedrock") {
    return ["openai", "vertex", "anthropic"];
  } else if (currentProvider === "openai") {
    return ["bedrock", "vertex", "anthropic"];
  } else if (currentProvider === "vertex") {
    return ["openai", "bedrock", "anthropic"];
  } else if (currentProvider === "anthropic") {
    return ["openai", "bedrock", "vertex"];
  }
  return [];
};
