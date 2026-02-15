import { credentials, ChannelCredentials, ClientDuplexStream } from '@grpc/grpc-js';
import {
  AgentServiceClient,
  CreateAgentRequest,
  CreateAgentResponse,
  GetAgentRequest,
  GetAgentResponse,
  ListAgentsRequest,
  ListAgentsResponse,
  DestroyAgentRequest,
  DestroyAgentResponse,
  GetTokenUsageRequest,
  TokenUsageResponse,
  AskRequest,
  AskResponse,
  AskWithHistoryRequest,
  AskWithHistoryResponse,
  HealthCheckRequest,
  HealthCheckResponse,
  ConversationRequest,
  ConversationResponse,
  AgentConfig as ProtoAgentConfig,
  CustomToolDefinition as ProtoCustomToolDefinition,
  Message as ProtoMessage,
} from './generated/agent';
import type {
  AgentConfig,
  CreateAgentResponse as SdkCreateAgentResponse,
  AskResponse as SdkAskResponse,
  AskWithHistoryResponse as SdkAskWithHistoryResponse,
  TokenUsageWithPricing,
  AgentSummary,
  Message,
  CustomToolDefinition,
} from './types';
import { MCPAgentError } from './agent';

/**
 * gRPC client for communicating with the MCPAgent server via Unix socket
 */
export class GrpcClient {
  private client: AgentServiceClient;
  private _connected: boolean = false;

  /**
   * Create a new gRPC client
   * @param socketPath - Unix socket path (e.g., '/tmp/mcpagent-grpc-1234.sock')
   */
  constructor(socketPath: string) {
    // gRPC uses 'unix://' prefix for Unix sockets
    const target = `unix://${socketPath}`;
    this.client = new AgentServiceClient(target, credentials.createInsecure());
  }

  /**
   * Check if the client is connected
   */
  get connected(): boolean {
    return this._connected;
  }

  /**
   * Close the gRPC connection
   */
  close(): void {
    this.client.close();
    this._connected = false;
  }

  /**
   * Health check
   */
  async healthCheck(): Promise<{ status: string }> {
    return new Promise((resolve, reject) => {
      this.client.healthCheck({}, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        this._connected = true;
        resolve({ status: response?.status || 'ok' });
      });
    });
  }

  /**
   * Create a new agent
   */
  async createAgent(
    sessionId: string,
    config: AgentConfig,
    customTools?: CustomToolDefinition[]
  ): Promise<SdkCreateAgentResponse> {
    const request: CreateAgentRequest = {
      sessionId,
      config: this.convertAgentConfig(config, customTools),
    };

    return new Promise((resolve, reject) => {
      this.client.createAgent(request, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        this._connected = true;
        resolve(this.convertCreateAgentResponse(response!));
      });
    });
  }

  /**
   * Get agent information
   */
  async getAgent(agentId: string): Promise<{
    agentId: string;
    sessionId: string;
    status: string;
    createdAt: string;
    capabilities: { tools: string[]; servers: string[] };
    tokenUsage?: {
      promptTokens: number;
      completionTokens: number;
      totalTokens: number;
      cacheTokens: number;
      reasoningTokens: number;
      llmCallCount: number;
    };
  }> {
    return new Promise((resolve, reject) => {
      this.client.getAgent({ agentId }, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve({
          agentId: response!.agentId,
          sessionId: response!.sessionId,
          status: response!.status,
          createdAt: response!.createdAt?.toISOString() || new Date().toISOString(),
          capabilities: {
            tools: response!.capabilities?.tools || [],
            servers: response!.capabilities?.servers || [],
          },
          tokenUsage: response!.tokenUsage ? {
            promptTokens: response!.tokenUsage.promptTokens,
            completionTokens: response!.tokenUsage.completionTokens,
            totalTokens: response!.tokenUsage.totalTokens,
            cacheTokens: response!.tokenUsage.cacheTokens,
            reasoningTokens: response!.tokenUsage.reasoningTokens,
            llmCallCount: response!.tokenUsage.llmCallCount,
          } : undefined,
        });
      });
    });
  }

  /**
   * List all agents
   */
  async listAgents(): Promise<AgentSummary[]> {
    return new Promise((resolve, reject) => {
      this.client.listAgents({}, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve(
          response!.agents.map((agent) => ({
            agentId: agent.agentId,
            sessionId: agent.sessionId,
            status: agent.status,
            createdAt: agent.createdAt?.toISOString() || new Date().toISOString(),
          }))
        );
      });
    });
  }

  /**
   * Destroy an agent
   */
  async destroyAgent(agentId: string): Promise<{ agentId: string; destroyed: boolean }> {
    return new Promise((resolve, reject) => {
      this.client.destroyAgent({ agentId }, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve({
          agentId: response!.agentId,
          destroyed: response!.destroyed,
        });
      });
    });
  }

  /**
   * Get token usage with pricing
   */
  async getTokenUsage(agentId: string): Promise<TokenUsageWithPricing> {
    return new Promise((resolve, reject) => {
      this.client.getTokenUsage({ agentId }, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve({
          promptTokens: response!.tokenUsage?.promptTokens || 0,
          completionTokens: response!.tokenUsage?.completionTokens || 0,
          totalTokens: response!.tokenUsage?.totalTokens || 0,
          cacheTokens: response!.tokenUsage?.cacheTokens || 0,
          reasoningTokens: response!.tokenUsage?.reasoningTokens || 0,
          llmCallCount: response!.tokenUsage?.llmCallCount || 0,
          costs: {
            inputCost: response!.costs?.inputCost || 0,
            outputCost: response!.costs?.outputCost || 0,
            reasoningCost: response!.costs?.reasoningCost || 0,
            cacheCost: response!.costs?.cacheCost || 0,
            totalCost: response!.costs?.totalCost || 0,
          },
        });
      });
    });
  }

  /**
   * Ask a question (unary RPC - no streaming)
   */
  async ask(agentId: string, question: string): Promise<SdkAskResponse> {
    return new Promise((resolve, reject) => {
      this.client.ask({ agentId, question }, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve({
          response: response!.response,
          tokenUsage: {
            promptTokens: response!.tokenUsage?.promptTokens || 0,
            completionTokens: response!.tokenUsage?.completionTokens || 0,
            totalTokens: response!.tokenUsage?.totalTokens || 0,
            cacheTokens: response!.tokenUsage?.cacheTokens || 0,
            reasoningTokens: response!.tokenUsage?.reasoningTokens || 0,
            llmCallCount: response!.tokenUsage?.llmCallCount || 0,
          },
          durationMs: Number(response!.durationMs),
        });
      });
    });
  }

  /**
   * Ask with history (unary RPC - no streaming)
   */
  async askWithHistory(
    agentId: string,
    messages: Message[]
  ): Promise<SdkAskWithHistoryResponse> {
    const protoMessages: ProtoMessage[] = messages.map((msg) => ({
      role: msg.role,
      content: msg.content,
    }));

    return new Promise((resolve, reject) => {
      this.client.askWithHistory({ agentId, messages: protoMessages }, (err, response) => {
        if (err) {
          reject(this.wrapError(err));
          return;
        }
        resolve({
          response: response!.response,
          tokenUsage: {
            promptTokens: response!.tokenUsage?.promptTokens || 0,
            completionTokens: response!.tokenUsage?.completionTokens || 0,
            totalTokens: response!.tokenUsage?.totalTokens || 0,
            cacheTokens: response!.tokenUsage?.cacheTokens || 0,
            reasoningTokens: response!.tokenUsage?.reasoningTokens || 0,
            llmCallCount: response!.tokenUsage?.llmCallCount || 0,
          },
          durationMs: Number(response!.durationMs),
          updatedMessages: response!.updatedMessages.map((msg) => ({
            role: msg.role as 'user' | 'assistant' | 'system',
            content: msg.content,
          })),
        });
      });
    });
  }

  /**
   * Start a bidirectional streaming conversation
   * Returns the raw duplex stream for use by StreamHandler
   */
  createConverseStream(): ClientDuplexStream<ConversationRequest, ConversationResponse> {
    return this.client.converse();
  }

  /**
   * Convert SDK AgentConfig to proto AgentConfig
   */
  private convertAgentConfig(
    config: AgentConfig,
    customTools?: CustomToolDefinition[]
  ): ProtoAgentConfig {
    const protoTools: ProtoCustomToolDefinition[] = (customTools || []).map((tool) => ({
      name: tool.name,
      description: tool.description,
      parameters: tool.parameters,
      timeoutMs: tool.timeoutMs || 30000,
      category: tool.category || 'custom',
    }));

    return {
      provider: config.provider || '',
      modelId: config.modelId || '',
      temperature: config.temperature || 0,
      maxTurns: config.maxTurns || 0,
      mcpConfigPath: config.mcpConfigPath || '',
      selectedServers: config.selectedServers || [],
      selectedTools: config.selectedTools || [],
      systemPrompt: config.systemPrompt || '',
      enableContextSummarization: config.enableContextSummarization || false,
      enableContextOffloading: config.enableContextOffloading || false,
      enableStreaming: config.enableStreaming || false,
      customTools: protoTools,
    };
  }

  /**
   * Convert proto CreateAgentResponse to SDK type
   */
  private convertCreateAgentResponse(response: CreateAgentResponse): SdkCreateAgentResponse {
    return {
      agentId: response.agentId,
      sessionId: response.sessionId,
      status: response.status,
      createdAt: response.createdAt?.toISOString() || new Date().toISOString(),
      capabilities: {
        tools: response.capabilities?.tools || [],
        servers: response.capabilities?.servers || [],
      },
    };
  }

  /**
   * Wrap gRPC error as MCPAgentError
   */
  private wrapError(err: unknown): MCPAgentError {
    if (err instanceof Error) {
      // Extract gRPC error details
      const grpcErr = err as Error & { code?: number; details?: string };
      const code = this.grpcCodeToString(grpcErr.code);
      return new MCPAgentError(code, grpcErr.message || grpcErr.details || 'Unknown error');
    }
    return new MCPAgentError('UNKNOWN_ERROR', String(err));
  }

  /**
   * Convert gRPC status code to string
   */
  private grpcCodeToString(code?: number): string {
    switch (code) {
      case 0:
        return 'OK';
      case 1:
        return 'CANCELLED';
      case 2:
        return 'UNKNOWN';
      case 3:
        return 'INVALID_ARGUMENT';
      case 4:
        return 'DEADLINE_EXCEEDED';
      case 5:
        return 'NOT_FOUND';
      case 6:
        return 'ALREADY_EXISTS';
      case 7:
        return 'PERMISSION_DENIED';
      case 8:
        return 'RESOURCE_EXHAUSTED';
      case 9:
        return 'FAILED_PRECONDITION';
      case 10:
        return 'ABORTED';
      case 11:
        return 'OUT_OF_RANGE';
      case 12:
        return 'UNIMPLEMENTED';
      case 13:
        return 'INTERNAL';
      case 14:
        return 'UNAVAILABLE';
      case 15:
        return 'DATA_LOSS';
      case 16:
        return 'UNAUTHENTICATED';
      default:
        return 'UNKNOWN_ERROR';
    }
  }
}
