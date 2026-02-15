/**
 * MCPAgent Node.js SDK
 *
 * A client library for interacting with MCPAgent - AI agents with MCP server support.
 * The Go server is automatically started when you call initialize().
 *
 * Features:
 * - gRPC bidirectional streaming for real-time responses
 * - Inline tool callbacks (no separate HTTP server needed)
 * - Automatic Go server lifecycle management
 * - Unix domain sockets for secure, fast IPC
 *
 * @example
 * ```typescript
 * import { MCPAgent } from '@mcpagent/node';
 *
 * // Simple usage - Go server auto-starts on initialize()
 * const agent = new MCPAgent();
 *
 * await agent.initialize({
 *   provider: 'openai',
 *   modelId: 'gpt-4o',
 *   selectedServers: ['filesystem']
 * });
 *
 * // Standard ask
 * const response = await agent.ask('What files are here?');
 * console.log(response.response);
 *
 * // Streaming (real-time tokens)
 * for await (const event of agent.askStream('Tell me a story')) {
 *   if (event.type === 'chunk') {
 *     process.stdout.write(event.text);
 *   }
 * }
 *
 * await agent.destroy();
 * ```
 *
 * @packageDocumentation
 */

// Main agent class
export { MCPAgent, MCPAgentError } from './agent';
export type { MCPAgentOptions, AnyConversationEvent } from './agent';

// Server manager (for advanced use cases)
export { ServerManager } from './server-manager';
export type { ServerManagerOptions } from './server-manager';

// gRPC client (for advanced use cases)
export { GrpcClient } from './grpc-client';

// Stream handler (for advanced use cases)
export { StreamHandler } from './stream-handler';
export type {
  ToolHandler,
  ConversationEvent,
  TextChunkConversationEvent,
  ToolCallConversationEvent,
  AgentEventConversationEvent,
  FinalConversationEvent,
  ErrorConversationEvent,
} from './stream-handler';

// Types
export type {
  Provider,
  AgentConfig,
  Message,
  TokenUsage,
  Costs,
  TokenUsageWithPricing,
  Capabilities,
  CreateAgentResponse,
  AskResponse,
  AskWithHistoryResponse,
  AgentSummary,
  ApiError,
  CustomToolDefinition,
  RegisterToolOptions,
} from './types';
