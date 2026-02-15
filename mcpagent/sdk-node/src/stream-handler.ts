import { ClientDuplexStream } from '@grpc/grpc-js';
import { EventEmitter } from 'events';
import {
  ConversationRequest,
  ConversationResponse,
  ToolCallEvent,
  Message as ProtoMessage,
} from './generated/agent';
import type { GrpcClient } from './grpc-client';
import type { Message, AskResponse, AskWithHistoryResponse, TokenUsage } from './types';
import { MCPAgentError } from './agent';

/**
 * Tool handler function type
 */
export type ToolHandler = (args: Record<string, unknown>) => Promise<string> | string;

/**
 * Conversation event types emitted by the stream handler
 */
export interface ConversationEvent {
  type: 'chunk' | 'tool_call' | 'agent_event' | 'final' | 'error';
}

export interface TextChunkConversationEvent extends ConversationEvent {
  type: 'chunk';
  text: string;
  isThinking: boolean;
}

export interface ToolCallConversationEvent extends ConversationEvent {
  type: 'tool_call';
  callId: string;
  toolName: string;
  arguments: Record<string, unknown>;
}

export interface AgentEventConversationEvent extends ConversationEvent {
  type: 'agent_event';
  eventType: string;
  traceId: string;
  spanId: string;
  parentId: string;
  correlationId: string;
  hierarchyLevel: number;
  sessionId: string;
  component: string;
  data?: Record<string, unknown>;
}

export interface FinalConversationEvent extends ConversationEvent {
  type: 'final';
  response: string;
  updatedMessages: Message[];
  tokenUsage: TokenUsage;
  durationMs: number;
}

export interface ErrorConversationEvent extends ConversationEvent {
  type: 'error';
  code: string;
  message: string;
  fatal: boolean;
}

export type AnyConversationEvent =
  | TextChunkConversationEvent
  | ToolCallConversationEvent
  | AgentEventConversationEvent
  | FinalConversationEvent
  | ErrorConversationEvent;

/**
 * Handles bidirectional streaming conversations with the MCPAgent server
 */
export class StreamHandler extends EventEmitter {
  private grpcClient: GrpcClient;
  private toolHandlers: Map<string, ToolHandler>;
  private activeStream: ClientDuplexStream<ConversationRequest, ConversationResponse> | null = null;

  constructor(grpcClient: GrpcClient, toolHandlers: Map<string, ToolHandler>) {
    super();
    this.grpcClient = grpcClient;
    this.toolHandlers = toolHandlers;
  }

  /**
   * Start a streaming conversation
   * Returns an async generator that yields conversation events
   */
  async *converse(
    agentId: string,
    question: string,
    history?: Message[]
  ): AsyncGenerator<AnyConversationEvent> {
    // Create the stream
    const stream = this.grpcClient.createConverseStream();
    this.activeStream = stream;

    // Create a queue to collect events
    const eventQueue: AnyConversationEvent[] = [];
    let resolveNext: ((value: IteratorResult<AnyConversationEvent>) => void) | null = null;
    let done = false;
    let streamError: Error | null = null;

    // Helper to push events
    const pushEvent = (event: AnyConversationEvent) => {
      if (resolveNext) {
        resolveNext({ value: event, done: false });
        resolveNext = null;
      } else {
        eventQueue.push(event);
      }
    };

    // Handle incoming messages from server
    stream.on('data', async (response: ConversationResponse) => {
      try {
        const event = await this.handleResponse(stream, agentId, response);
        if (event) {
          pushEvent(event);
          if (event.type === 'final' || (event.type === 'error' && event.fatal)) {
            done = true;
            if (resolveNext) {
              resolveNext({ value: undefined as unknown as AnyConversationEvent, done: true });
              resolveNext = null;
            }
          }
        }
      } catch (err) {
        streamError = err instanceof Error ? err : new Error(String(err));
        done = true;
        if (resolveNext) {
          resolveNext({ value: undefined as unknown as AnyConversationEvent, done: true });
          resolveNext = null;
        }
      }
    });

    stream.on('end', () => {
      done = true;
      if (resolveNext) {
        resolveNext({ value: undefined as unknown as AnyConversationEvent, done: true });
        resolveNext = null;
      }
    });

    stream.on('error', (err: Error) => {
      streamError = err;
      done = true;
      const errorEvent: ErrorConversationEvent = {
        type: 'error',
        code: 'STREAM_ERROR',
        message: err.message,
        fatal: true,
      };
      pushEvent(errorEvent);
    });

    // Send the initial question
    const historyProto: ProtoMessage[] = history
      ? history.map((m) => ({ role: m.role, content: m.content }))
      : [];

    stream.write({
      agentId,
      question: {
        text: question,
        history: historyProto,
      },
    });

    // Yield events as they come
    while (!done || eventQueue.length > 0) {
      if (eventQueue.length > 0) {
        yield eventQueue.shift()!;
      } else if (!done) {
        // Wait for the next event
        const result = await new Promise<IteratorResult<AnyConversationEvent>>((resolve) => {
          resolveNext = resolve;
        });
        if (result.done) {
          break;
        }
        yield result.value;
      }
    }

    // Clean up
    this.activeStream = null;
    stream.end();

    // Throw any stream error
    if (streamError) {
      throw new MCPAgentError('STREAM_ERROR', (streamError as Error).message);
    }
  }

  /**
   * Simple ask method that collects the final response from streaming
   */
  async ask(agentId: string, question: string): Promise<AskResponse> {
    let finalResponse: FinalConversationEvent | null = null;
    let responseText = '';

    for await (const event of this.converse(agentId, question)) {
      if (event.type === 'chunk') {
        responseText += event.text;
      } else if (event.type === 'final') {
        finalResponse = event;
      } else if (event.type === 'error' && event.fatal) {
        throw new MCPAgentError(event.code, event.message);
      }
    }

    if (!finalResponse) {
      throw new MCPAgentError('NO_RESPONSE', 'No final response received');
    }

    return {
      response: finalResponse.response || responseText,
      tokenUsage: finalResponse.tokenUsage,
      durationMs: finalResponse.durationMs,
    };
  }

  /**
   * Ask with history - collects the final response from streaming
   */
  async askWithHistory(agentId: string, messages: Message[]): Promise<AskWithHistoryResponse> {
    // Get the last user message as the question
    const lastUserMessage = [...messages].reverse().find((m) => m.role === 'user');
    if (!lastUserMessage) {
      throw new MCPAgentError('INVALID_INPUT', 'No user message found in history');
    }

    // Use all but the last user message as history
    const lastUserIndex = messages.lastIndexOf(lastUserMessage);
    const history = messages.slice(0, lastUserIndex);

    let finalResponse: FinalConversationEvent | null = null;
    let responseText = '';

    for await (const event of this.converse(agentId, lastUserMessage.content, history)) {
      if (event.type === 'chunk') {
        responseText += event.text;
      } else if (event.type === 'final') {
        finalResponse = event;
      } else if (event.type === 'error' && event.fatal) {
        throw new MCPAgentError(event.code, event.message);
      }
    }

    if (!finalResponse) {
      throw new MCPAgentError('NO_RESPONSE', 'No final response received');
    }

    return {
      response: finalResponse.response || responseText,
      tokenUsage: finalResponse.tokenUsage,
      durationMs: finalResponse.durationMs,
      updatedMessages: finalResponse.updatedMessages,
    };
  }

  /**
   * Cancel the current conversation
   */
  cancel(reason: string = 'User cancelled'): void {
    if (this.activeStream) {
      this.activeStream.write({
        agentId: '',
        cancel: { reason },
      });
      this.activeStream.end();
      this.activeStream = null;
    }
  }

  /**
   * Handle a response from the server
   */
  private async handleResponse(
    stream: ClientDuplexStream<ConversationRequest, ConversationResponse>,
    agentId: string,
    response: ConversationResponse
  ): Promise<AnyConversationEvent | null> {
    // Check each field of the response (ts-proto generates optional fields, not oneof)
    if (response.textChunk) {
      return {
        type: 'chunk',
        text: response.textChunk.text,
        isThinking: response.textChunk.isThinking,
      };
    }

    if (response.toolCall) {
      return this.handleToolCall(stream, agentId, response.toolCall);
    }

    if (response.agentEvent) {
      return {
        type: 'agent_event',
        eventType: response.agentEvent.type,
        traceId: response.agentEvent.traceId,
        spanId: response.agentEvent.spanId,
        parentId: response.agentEvent.parentId,
        correlationId: response.agentEvent.correlationId,
        hierarchyLevel: response.agentEvent.hierarchyLevel,
        sessionId: response.agentEvent.sessionId,
        component: response.agentEvent.component,
        data: response.agentEvent.data || undefined,
      };
    }

    if (response.finalResponse) {
      return {
        type: 'final',
        response: response.finalResponse.response,
        updatedMessages: response.finalResponse.updatedMessages.map((m: ProtoMessage) => ({
          role: m.role as 'user' | 'assistant' | 'system',
          content: m.content,
        })),
        tokenUsage: {
          promptTokens: response.finalResponse.tokenUsage?.promptTokens || 0,
          completionTokens: response.finalResponse.tokenUsage?.completionTokens || 0,
          totalTokens: response.finalResponse.tokenUsage?.totalTokens || 0,
          cacheTokens: response.finalResponse.tokenUsage?.cacheTokens || 0,
          reasoningTokens: response.finalResponse.tokenUsage?.reasoningTokens || 0,
          llmCallCount: response.finalResponse.tokenUsage?.llmCallCount || 0,
        },
        durationMs: Number(response.finalResponse.durationMs),
      };
    }

    if (response.error) {
      return {
        type: 'error',
        code: response.error.code,
        message: response.error.message,
        fatal: response.error.fatal,
      };
    }

    return null;
  }

  /**
   * Handle a tool call from the server
   */
  private async handleToolCall(
    stream: ClientDuplexStream<ConversationRequest, ConversationResponse>,
    agentId: string,
    toolCall: ToolCallEvent
  ): Promise<ToolCallConversationEvent> {
    const handler = this.toolHandlers.get(toolCall.toolName);

    // Execute the tool handler if available
    const startTime = Date.now();
    if (handler) {
      try {
        const args = toolCall.arguments || {};
        const result = await Promise.resolve(handler(args as Record<string, unknown>));
        const durationMs = Date.now() - startTime;

        // Send the result back to the server
        stream.write({
          agentId,
          toolResult: {
            callId: toolCall.callId,
            success: true,
            result,
            durationMs,
          },
        });
      } catch (err) {
        // Send the error back to the server
        const errorMessage = err instanceof Error ? err.message : String(err);
        const durationMs = Date.now() - startTime;
        stream.write({
          agentId,
          toolResult: {
            callId: toolCall.callId,
            success: false,
            result: '',
            error: {
              message: errorMessage,
              code: 'TOOL_ERROR',
            },
            durationMs,
          },
        });
      }
    } else {
      // No handler registered - send error
      stream.write({
        agentId,
        toolResult: {
          callId: toolCall.callId,
          success: false,
          result: '',
          error: {
            message: `No handler registered for tool: ${toolCall.toolName}`,
            code: 'HANDLER_NOT_FOUND',
          },
          durationMs: 0,
        },
      });
    }

    return {
      type: 'tool_call',
      callId: toolCall.callId,
      toolName: toolCall.toolName,
      arguments: (toolCall.arguments || {}) as Record<string, unknown>,
    };
  }
}
