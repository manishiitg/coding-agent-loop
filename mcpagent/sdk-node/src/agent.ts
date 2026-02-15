import { GrpcClient } from './grpc-client';
import { StreamHandler, AnyConversationEvent, ToolHandler } from './stream-handler';
import { ServerManager, ServerManagerOptions } from './server-manager';
import type {
  AgentConfig,
  Message,
  AskResponse,
  AskWithHistoryResponse,
  TokenUsageWithPricing,
  Capabilities,
  CreateAgentResponse,
} from './types';

/**
 * Custom error class for MCPAgent errors
 */
export class MCPAgentError extends Error {
  code: string;
  details?: unknown;

  constructor(code: string, message: string, details?: unknown) {
    super(`MCPAgentError: ${message}`);
    this.name = 'MCPAgentError';
    this.code = code;
    this.details = details;
  }
}

// Re-export event types
export type { AnyConversationEvent };

/**
 * Options for creating an MCPAgent
 */
export interface MCPAgentOptions {
  /** Options for the Go server (auto-started if not already running) */
  serverOptions?: ServerManagerOptions;
}

/**
 * MCPAgent client for interacting with AI agents via gRPC.
 * Automatically manages the Go server lifecycle.
 *
 * @example
 * ```typescript
 * // Simple usage - Go server auto-starts
 * const agent = new MCPAgent();
 *
 * await agent.initialize({
 *   provider: 'openai',
 *   modelId: 'gpt-4o',
 *   selectedServers: ['filesystem']
 * });
 *
 * const response = await agent.ask('What files are in the current directory?');
 * console.log(response.response);
 *
 * await agent.destroy();
 * ```
 */
export class MCPAgent {
  private grpcClient: GrpcClient | null = null;
  private streamHandler: StreamHandler | null = null;
  private serverManager: ServerManager;
  private agentId: string | null = null;
  private sessionId: string | null = null;
  private capabilities: Capabilities | null = null;
  private serverStartedByUs: boolean = false;
  private toolHandlers: Map<string, ToolHandler> = new Map();
  private registeredTools: Map<
    string,
    {
      name: string;
      description: string;
      parameters: Record<string, unknown>;
      timeoutMs?: number;
      category?: string;
    }
  > = new Map();

  /**
   * Create a new MCPAgent client.
   * The Go server will be auto-started on initialize() if not already running.
   *
   * @param options - Optional configuration
   */
  constructor(options: MCPAgentOptions = {}) {
    this.serverManager = new ServerManager(options.serverOptions);
  }

  /**
   * Register a custom tool with a JavaScript handler.
   * The tool will be available to the LLM during conversations.
   *
   * IMPORTANT: Call this BEFORE initialize() to include tools in agent creation.
   * Tools registered after initialize() will not be available until you create a new agent.
   *
   * @param name - Unique tool name
   * @param description - Description for the LLM (what the tool does)
   * @param parameters - JSON Schema for tool parameters
   * @param handler - Async function to execute when tool is called
   * @param options - Optional timeout and category settings
   *
   * @example
   * ```typescript
   * agent.registerTool(
   *   'get_weather',
   *   'Get current weather for a location',
   *   {
   *     type: 'object',
   *     properties: {
   *       location: { type: 'string', description: 'City name' }
   *     },
   *     required: ['location']
   *   },
   *   async (args) => {
   *     const weather = await fetchWeather(args.location as string);
   *     return JSON.stringify(weather);
   *   },
   *   { timeoutMs: 10000 }
   * );
   * ```
   */
  registerTool(
    name: string,
    description: string,
    parameters: Record<string, unknown>,
    handler: ToolHandler,
    options?: { timeoutMs?: number; category?: string }
  ): void {
    // Store handler for gRPC usage
    this.toolHandlers.set(name, handler);

    // Store metadata for agent creation
    this.registeredTools.set(name, {
      name,
      description,
      parameters,
      timeoutMs: options?.timeoutMs,
      category: options?.category,
    });
  }

  /**
   * Unregister a custom tool by name
   * @returns true if the tool was found and removed
   */
  unregisterTool(name: string): boolean {
    this.toolHandlers.delete(name);
    return this.registeredTools.delete(name);
  }

  /**
   * Initialize the agent with the given configuration.
   * This auto-starts the Go server if needed and creates a new agent instance.
   *
   * @param config - Agent configuration options
   * @throws MCPAgentError if initialization fails
   *
   * @example
   * ```typescript
   * await agent.initialize({
   *   provider: 'bedrock',
   *   modelId: 'anthropic.claude-sonnet-4-20250514-v1:0',
   *   selectedServers: ['filesystem', 'github'],
   *   enableContextSummarization: true
   * });
   * ```
   */
  async initialize(config: AgentConfig = {}): Promise<CreateAgentResponse> {
    // Auto-start Go server if not running
    const wasRunning = await this.serverManager.isServerHealthy();
    const socketPath = await this.serverManager.start();
    this.serverStartedByUs = !wasRunning;

    // Create gRPC client
    this.grpcClient = new GrpcClient(socketPath);
    this.streamHandler = new StreamHandler(this.grpcClient, this.toolHandlers);

    // Build custom tools for gRPC (no callback socket needed - handled via stream)
    const customTools = Array.from(this.registeredTools.values()).map((tool) => ({
      name: tool.name,
      description: tool.description,
      parameters: tool.parameters,
      callbackUrl: '', // Not used for gRPC
      timeoutMs: tool.timeoutMs,
      category: tool.category,
    }));

    const response = await this.grpcClient.createAgent(
      this.sessionId || '',
      config,
      customTools
    );

    this.agentId = response.agentId;
    this.sessionId = response.sessionId;
    this.capabilities = response.capabilities;

    return response;
  }

  /**
   * Ask the agent a single question.
   * The agent will use available tools to answer the question.
   *
   * @param question - The question to ask
   * @returns The agent's response with token usage
   * @throws MCPAgentError if the agent is not initialized or the request fails
   *
   * @example
   * ```typescript
   * const response = await agent.ask('List all files in the src directory');
   * console.log(response.response);
   * console.log(`Used ${response.tokenUsage.totalTokens} tokens`);
   * ```
   */
  async ask(question: string): Promise<AskResponse> {
    this.ensureInitialized();
    return this.streamHandler!.ask(this.agentId!, question);
  }

  /**
   * Ask a question with streaming responses.
   * Returns an async generator that yields events as they arrive.
   *
   * @param question - The question to ask
   * @yields Conversation events (chunks, tool calls, final response)
   * @throws MCPAgentError if agent is not initialized
   *
   * @example
   * ```typescript
   * for await (const event of agent.askStream('What is 2+2?')) {
   *   if (event.type === 'chunk') {
   *     process.stdout.write(event.text);
   *   } else if (event.type === 'final') {
   *     console.log('\nDone:', event.response);
   *   }
   * }
   * ```
   */
  async *askStream(question: string): AsyncGenerator<AnyConversationEvent> {
    this.ensureInitialized();
    yield* this.streamHandler!.converse(this.agentId!, question);
  }

  /**
   * Continue a multi-turn conversation with the agent.
   * Pass the full conversation history to maintain context.
   *
   * @param messages - Array of conversation messages
   * @returns The agent's response with updated message history
   * @throws MCPAgentError if the agent is not initialized or the request fails
   *
   * @example
   * ```typescript
   * const messages: Message[] = [
   *   { role: 'user', content: 'List files in src/' },
   *   { role: 'assistant', content: 'Found: index.ts, utils.ts' },
   *   { role: 'user', content: 'Show me index.ts' }
   * ];
   *
   * const response = await agent.askWithHistory(messages);
   * console.log(response.response);
   * // Use response.updatedMessages for the next turn
   * ```
   */
  async askWithHistory(messages: Message[]): Promise<AskWithHistoryResponse> {
    this.ensureInitialized();
    return this.streamHandler!.askWithHistory(this.agentId!, messages);
  }

  /**
   * Ask with history and streaming responses.
   * Returns an async generator that yields events as they arrive.
   *
   * @param messages - Array of conversation messages
   * @yields Conversation events (chunks, tool calls, final response)
   * @throws MCPAgentError if agent is not initialized
   */
  async *askWithHistoryStream(messages: Message[]): AsyncGenerator<AnyConversationEvent> {
    this.ensureInitialized();

    // Get the last user message as the question
    const lastUserMessage = [...messages].reverse().find((m) => m.role === 'user');
    if (!lastUserMessage) {
      throw new MCPAgentError('INVALID_INPUT', 'No user message found in history');
    }

    // Use all but the last user message as history
    const lastUserIndex = messages.lastIndexOf(lastUserMessage);
    const history = messages.slice(0, lastUserIndex);

    yield* this.streamHandler!.converse(this.agentId!, lastUserMessage.content, history);
  }

  /**
   * Get the current token usage statistics with pricing.
   *
   * @returns Token usage and cost breakdown
   * @throws MCPAgentError if the agent is not initialized
   *
   * @example
   * ```typescript
   * const usage = await agent.getTokenUsage();
   * console.log(`Total tokens: ${usage.totalTokens}`);
   * console.log(`Total cost: $${usage.costs.totalCost.toFixed(4)}`);
   * ```
   */
  async getTokenUsage(): Promise<TokenUsageWithPricing> {
    this.ensureInitialized();
    return this.grpcClient!.getTokenUsage(this.agentId!);
  }

  /**
   * Destroy the agent and clean up resources.
   * Always call this when done with the agent.
   *
   * @example
   * ```typescript
   * try {
   *   await agent.initialize({ ... });
   *   await agent.ask('...');
   * } finally {
   *   await agent.destroy();
   * }
   * ```
   */
  async destroy(): Promise<void> {
    // Destroy agent via gRPC
    if (this.agentId && this.grpcClient) {
      await this.grpcClient.destroyAgent(this.agentId);
      this.agentId = null;
      this.sessionId = null;
      this.capabilities = null;
    }

    // Close gRPC client
    if (this.grpcClient) {
      this.grpcClient.close();
      this.grpcClient = null;
      this.streamHandler = null;
    }

    // Stop Go server if we started it
    if (this.serverStartedByUs) {
      await this.serverManager.stop();
      this.serverStartedByUs = false;
    }

    this.registeredTools.clear();
    this.toolHandlers.clear();
  }

  /**
   * Get the agent ID (null if not initialized)
   */
  getAgentId(): string | null {
    return this.agentId;
  }

  /**
   * Get the session ID (null if not initialized)
   */
  getSessionId(): string | null {
    return this.sessionId;
  }

  /**
   * Get the agent's capabilities (null if not initialized)
   */
  getCapabilities(): Capabilities | null {
    return this.capabilities;
  }

  /**
   * Check if the agent is initialized
   */
  isInitialized(): boolean {
    return this.agentId !== null && this.grpcClient !== null;
  }

  private ensureInitialized(): void {
    if (!this.grpcClient || !this.agentId) {
      throw new MCPAgentError(
        'NOT_INITIALIZED',
        'Agent not initialized. Call initialize() first.'
      );
    }
  }
}
