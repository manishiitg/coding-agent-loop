// Shared LLM configuration utilities
import type { LLMOption } from '../types/llm'

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