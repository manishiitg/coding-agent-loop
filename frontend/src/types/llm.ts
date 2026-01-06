// Shared LLM types for the application

export interface LLMOption {
  provider: string;
  model: string;
  label: string;
  description?: string;

  // Metadata from Published LLM / Model Registry
  temperature?: number;
  contextWindow?: number;       // Context window size in tokens
  inputCostPer1M?: number;      // Input cost per 1M tokens
  outputCostPer1M?: number;     // Output cost per 1M tokens

  // Model-specific options (reasoning, thinking, etc.)
  options?: Record<string, unknown>;
}
