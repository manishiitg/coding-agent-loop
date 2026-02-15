/**
 * LLM Provider types
 */
export type Provider = 'bedrock' | 'openai' | 'anthropic' | 'openrouter' | 'vertex';

/**
 * Configuration for creating an agent
 */
export interface AgentConfig {
  /** LLM provider to use */
  provider?: Provider;
  /** Model ID (e.g., 'gpt-4o', 'anthropic.claude-sonnet-4-20250514-v1:0') */
  modelId?: string;
  /** Temperature for LLM sampling (0.0 - 1.0) */
  temperature?: number;
  /** Maximum conversation turns */
  maxTurns?: number;
  /** Path to MCP servers configuration file */
  mcpConfigPath?: string;
  /** Filter to specific MCP servers */
  selectedServers?: string[];
  /** Filter to specific tools (format: "server:tool") */
  selectedTools?: string[];
  /** Custom system prompt */
  systemPrompt?: string;
  /** Enable automatic context summarization */
  enableContextSummarization?: boolean;
  /** Enable context offloading for large outputs */
  enableContextOffloading?: boolean;
  /** Enable streaming responses */
  enableStreaming?: boolean;
}

/**
 * Conversation message
 */
export interface Message {
  /** Role of the message sender */
  role: 'user' | 'assistant' | 'system';
  /** Message content */
  content: string;
}

/**
 * Token usage statistics
 */
export interface TokenUsage {
  /** Number of prompt tokens */
  promptTokens: number;
  /** Number of completion tokens */
  completionTokens: number;
  /** Total tokens (prompt + completion) */
  totalTokens: number;
  /** Number of cached tokens (if applicable) */
  cacheTokens?: number;
  /** Number of reasoning tokens (for reasoning models) */
  reasoningTokens?: number;
  /** Number of LLM API calls made */
  llmCallCount: number;
}

/**
 * Cost breakdown in USD
 */
export interface Costs {
  /** Cost for input tokens */
  inputCost: number;
  /** Cost for output tokens */
  outputCost: number;
  /** Cost for reasoning tokens (if applicable) */
  reasoningCost?: number;
  /** Savings from cache hits (if applicable) */
  cacheCost?: number;
  /** Total cost */
  totalCost: number;
}

/**
 * Token usage with pricing information
 */
export interface TokenUsageWithPricing extends TokenUsage {
  /** Cost breakdown */
  costs: Costs;
}

/**
 * Agent capabilities
 */
export interface Capabilities {
  /** Available tools (format: "server:tool") */
  tools: string[];
  /** Connected MCP servers */
  servers: string[];
}

/**
 * Response from creating an agent
 */
export interface CreateAgentResponse {
  /** Unique agent identifier */
  agentId: string;
  /** Session identifier for connection reuse */
  sessionId: string;
  /** Agent status */
  status: string;
  /** Creation timestamp */
  createdAt: string;
  /** Agent capabilities */
  capabilities: Capabilities;
}

/**
 * Response from asking a question
 */
export interface AskResponse {
  /** The agent's response */
  response: string;
  /** Token usage for this request */
  tokenUsage: TokenUsage;
  /** Duration of the request in milliseconds */
  durationMs: number;
}

/**
 * Response from multi-turn conversation
 */
export interface AskWithHistoryResponse extends AskResponse {
  /** Updated conversation history including the new response */
  updatedMessages: Message[];
}

/**
 * Agent summary for listing
 */
export interface AgentSummary {
  /** Agent identifier */
  agentId: string;
  /** Session identifier */
  sessionId: string;
  /** Agent status */
  status: string;
  /** Creation timestamp */
  createdAt: string;
}

/**
 * API error response
 */
export interface ApiError {
  /** Error code */
  code: string;
  /** Error message */
  message: string;
  /** Additional error details */
  details?: Record<string, unknown>;
}

/**
 * Custom tool definition with HTTP callback
 */
export interface CustomToolDefinition {
  /** Unique tool name */
  name: string;
  /** Description for the LLM */
  description: string;
  /** JSON Schema for tool parameters */
  parameters: Record<string, unknown>;
  /** HTTP callback URL where Go will POST to execute the tool */
  callbackUrl: string;
  /** Timeout in milliseconds (default: 30000) */
  timeoutMs?: number;
  /** Tool category (default: "custom") */
  category?: string;
}

/**
 * Options for registering a custom tool
 */
export interface RegisterToolOptions {
  /** Timeout in milliseconds for tool execution (default: 30000) */
  timeoutMs?: number;
  /** Tool category (default: "custom") */
  category?: string;
}
