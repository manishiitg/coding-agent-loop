// Shared LLM types for the application

export interface LLMOption {
  provider: string;
  model: string;
  label: string;
  description?: string;
}
