package grpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/grpcserver/pb"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/google/uuid"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// StreamHandler manages a bidirectional streaming conversation
type StreamHandler struct {
	manager *AgentManager
	logger  loggerv2.Logger
	stream  pb.AgentService_ConverseServer

	// Current conversation state
	agentID         string
	agent           *ManagedAgent
	toolResultsChan chan *pb.ToolResultMessage
	cancelFunc      context.CancelFunc

	// Channels for coordinating between receive and question handling
	questionChan chan *questionRequest
	errChan      chan error

	mu sync.Mutex
}

// questionRequest holds a question to be processed
type questionRequest struct {
	agentID  string
	question *pb.QuestionMessage
}

// NewStreamHandler creates a new stream handler
func NewStreamHandler(
	manager *AgentManager,
	logger loggerv2.Logger,
	stream pb.AgentService_ConverseServer,
) *StreamHandler {
	return &StreamHandler{
		manager:         manager,
		logger:          logger,
		stream:          stream,
		toolResultsChan: make(chan *pb.ToolResultMessage, 10),
		questionChan:    make(chan *questionRequest, 1),
		errChan:         make(chan error, 1),
	}
}

// Handle processes the bidirectional stream
func (h *StreamHandler) Handle() error {
	ctx, cancel := context.WithCancel(h.stream.Context())
	defer cancel()

	var wg sync.WaitGroup

	// Start receive loop in a goroutine so it can continue receiving
	// tool results while question processing is in progress
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.receiveLoop(ctx, cancel)
	}()

	// Process questions as they come in
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()

		case err := <-h.errChan:
			cancel()
			wg.Wait()
			return err

		case req := <-h.questionChan:
			if err := h.handleQuestion(ctx, req.agentID, req.question); err != nil {
				h.sendError(err, true)
				cancel()
				wg.Wait()
				return err
			}
		}
	}
}

// receiveLoop continuously receives messages from the client
func (h *StreamHandler) receiveLoop(ctx context.Context, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Receive message from client
		req, err := h.stream.Recv()
		if errors.Is(err, io.EOF) {
			h.logger.Debug("Stream closed by client")
			cancel()
			return
		}
		if err != nil {
			h.logger.Error("Failed to receive message", err)
			select {
			case h.errChan <- err:
			default:
			}
			return
		}

		// Handle the message based on type
		switch payload := req.Payload.(type) {
		case *pb.ConversationRequest_Question:
			// Send to question channel for processing
			select {
			case h.questionChan <- &questionRequest{agentID: req.AgentId, question: payload.Question}:
			case <-ctx.Done():
				return
			}

		case *pb.ConversationRequest_ToolResult:
			// Forward tool result to the waiting execution function
			h.logger.Debug("Received tool result",
				loggerv2.String("call_id", payload.ToolResult.CallId))
			select {
			case h.toolResultsChan <- payload.ToolResult:
			case <-ctx.Done():
				return
			}

		case *pb.ConversationRequest_Cancel:
			h.logger.Info("Received cancel request", loggerv2.String("reason", payload.Cancel.Reason))
			if h.cancelFunc != nil {
				h.cancelFunc()
			}
			cancel()
			return

		default:
			h.sendError(fmt.Errorf("unknown message type"), false)
		}
	}
}

// handleQuestion processes a question and sends responses via the stream
func (h *StreamHandler) handleQuestion(ctx context.Context, agentID string, question *pb.QuestionMessage) error {
	h.mu.Lock()

	// Validate agent
	if agentID == "" {
		h.mu.Unlock()
		return status.Error(codes.InvalidArgument, "agent_id is required")
	}

	agent, ok := h.manager.GetAgent(agentID)
	if !ok {
		h.mu.Unlock()
		return status.Errorf(codes.NotFound, "agent not found: %s", agentID)
	}

	h.agentID = agentID
	h.agent = agent

	// Create cancellable context
	convCtx, cancel := context.WithCancel(ctx)
	h.cancelFunc = cancel
	h.mu.Unlock()

	defer cancel()

	startTime := time.Now()

	// Register custom tools with stream-based execution
	if len(agent.CustomTools) > 0 {
		h.registerCustomTools(convCtx, agent)
	}

	// Subscribe to agent events for streaming
	eventChan, unsubscribe, ok := h.subscribeToEvents(convCtx, agent)
	if ok {
		defer unsubscribe()

		// Forward events to stream in background
		go h.forwardEvents(convCtx, eventChan)
	}

	// Prepare messages for conversation
	var response string
	var updatedMessages []llmtypes.MessageContent
	var err error

	if len(question.History) > 0 {
		// Multi-turn conversation
		messages := h.convertMessagesToLLM(question.History)

		// Add the new question
		messages = append(messages, llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: question.Text}},
		})

		response, updatedMessages, err = agent.Agent.AskWithHistory(convCtx, messages)
	} else {
		// Single turn
		response, err = agent.Agent.Ask(convCtx, question.Text)
	}

	if err != nil {
		h.logger.Error("Conversation failed", err, loggerv2.String("agent_id", agentID))
		return status.Errorf(codes.Internal, "conversation failed: %v", err)
	}

	duration := time.Since(startTime)

	// Get token usage
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, _ := agent.Agent.GetTokenUsage()

	// Send final response
	finalResp := &pb.ConversationResponse{
		Payload: &pb.ConversationResponse_FinalResponse{
			FinalResponse: &pb.FinalResponse{
				Response:        response,
				UpdatedMessages: h.convertMessagesToProto(updatedMessages),
				TokenUsage: &pb.TokenUsage{
					PromptTokens:     safeIntToInt32(promptTokens),
					CompletionTokens: safeIntToInt32(completionTokens),
					TotalTokens:      safeIntToInt32(totalTokens),
					CacheTokens:      safeIntToInt32(cacheTokens),
					ReasoningTokens:  safeIntToInt32(reasoningTokens),
					LlmCallCount:     safeIntToInt32(llmCallCount),
				},
				DurationMs: duration.Milliseconds(),
			},
		},
	}

	if err := h.stream.Send(finalResp); err != nil {
		h.logger.Error("Failed to send final response", err)
		return err
	}

	return nil
}

// subscribeToEvents subscribes to the agent's streaming events
func (h *StreamHandler) subscribeToEvents(ctx context.Context, agent *ManagedAgent) (<-chan *events.AgentEvent, func(), bool) {
	// Try to get the streaming tracer if available
	eventChan, unsubscribe, ok := agent.Agent.SubscribeToEvents(ctx)
	return eventChan, unsubscribe, ok
}

// forwardEvents forwards agent events to the gRPC stream
func (h *StreamHandler) forwardEvents(ctx context.Context, eventChan <-chan *events.AgentEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			if event != nil {
				h.sendAgentEvent(*event)
			}
		}
	}
}

// sendAgentEvent sends an agent event via the stream
func (h *StreamHandler) sendAgentEvent(event events.AgentEvent) {
	// Convert event data to protobuf Struct
	eventData := event.Data
	if eventData == nil {
		return
	}

	// Check for streaming chunk events
	if eventData.GetEventType() == events.StreamingChunk {
		if chunkEvent, ok := eventData.(*events.StreamingChunkEvent); ok {
			h.sendTextChunk(chunkEvent.Content, false)
			return
		}
	}

	// Check for tool call events - these need special handling for bidirectional flow
	if eventData.GetEventType() == events.ToolCallStart {
		if toolEvent, ok := eventData.(*events.ToolCallStartEvent); ok {
			h.sendToolCallStart(toolEvent)
			return
		}
	}

	// For other events, send as AgentEvent
	pbEvent := &pb.AgentEvent{
		Type:           string(eventData.GetEventType()),
		Timestamp:      timestamppb.New(event.Timestamp),
		TraceId:        event.TraceID,
		SpanId:         event.SpanID,
		ParentId:       event.ParentID,
		CorrelationId:  event.CorrelationID,
		HierarchyLevel: safeIntToInt32(event.HierarchyLevel),
		SessionId:      event.SessionID,
		Component:      event.Component,
	}

	resp := &pb.ConversationResponse{
		Payload: &pb.ConversationResponse_AgentEvent{
			AgentEvent: pbEvent,
		},
	}

	if err := h.stream.Send(resp); err != nil {
		h.logger.Debug("Failed to send agent event", loggerv2.String("error", err.Error()))
	}
}

// sendTextChunk sends a streaming text chunk
func (h *StreamHandler) sendTextChunk(text string, isThinking bool) {
	resp := &pb.ConversationResponse{
		Payload: &pb.ConversationResponse_TextChunk{
			TextChunk: &pb.TextChunkEvent{
				Text:       text,
				IsThinking: isThinking,
			},
		},
	}

	if err := h.stream.Send(resp); err != nil {
		h.logger.Debug("Failed to send text chunk", loggerv2.String("error", err.Error()))
	}
}

// sendToolCallStart sends a tool call request to the client
func (h *StreamHandler) sendToolCallStart(toolEvent *events.ToolCallStartEvent) {
	callID := uuid.New().String()[:8]

	// Parse arguments JSON string to map
	var argsMap map[string]interface{}
	if toolEvent.ToolParams.Arguments != "" {
		if err := json.Unmarshal([]byte(toolEvent.ToolParams.Arguments), &argsMap); err != nil {
			h.logger.Debug("Failed to parse tool arguments JSON", loggerv2.String("error", err.Error()))
			argsMap = make(map[string]interface{})
		}
	} else {
		argsMap = make(map[string]interface{})
	}

	// Convert to protobuf Struct
	argsStruct, err := structpb.NewStruct(argsMap)
	if err != nil {
		h.logger.Error("Failed to convert tool arguments", err)
		argsStruct = &structpb.Struct{}
	}

	resp := &pb.ConversationResponse{
		Payload: &pb.ConversationResponse_ToolCall{
			ToolCall: &pb.ToolCallEvent{
				CallId:    callID,
				ToolName:  toolEvent.ToolName,
				Arguments: argsStruct,
				TimeoutMs: 30000, // Default 30s timeout
			},
		},
	}

	if err := h.stream.Send(resp); err != nil {
		h.logger.Error("Failed to send tool call", err)
	}
}

// sendError sends an error event via the stream
func (h *StreamHandler) sendError(err error, fatal bool) {
	code := "INTERNAL_ERROR"
	message := err.Error()

	// Extract code from gRPC status if available
	if st, ok := status.FromError(err); ok {
		switch st.Code() {
		case codes.NotFound:
			code = "NOT_FOUND"
		case codes.InvalidArgument:
			code = "INVALID_ARGUMENT"
		case codes.DeadlineExceeded:
			code = "TIMEOUT"
		case codes.Canceled:
			code = "CANCELLED"
		}
		message = st.Message()
	}

	resp := &pb.ConversationResponse{
		Payload: &pb.ConversationResponse_Error{
			Error: &pb.ErrorEvent{
				Code:    code,
				Message: message,
				Fatal:   fatal,
			},
		},
	}

	if sendErr := h.stream.Send(resp); sendErr != nil {
		h.logger.Debug("Failed to send error", loggerv2.String("error", sendErr.Error()))
	}
}

// convertMessagesToLLM converts protobuf messages to LLM format
func (h *StreamHandler) convertMessagesToLLM(messages []*pb.Message) []llmtypes.MessageContent {
	result := make([]llmtypes.MessageContent, len(messages))
	for i, msg := range messages {
		var role llmtypes.ChatMessageType
		switch msg.Role {
		case "user":
			role = llmtypes.ChatMessageTypeHuman
		case "assistant":
			role = llmtypes.ChatMessageTypeAI
		case "system":
			role = llmtypes.ChatMessageTypeSystem
		default:
			role = llmtypes.ChatMessageTypeHuman
		}

		result[i] = llmtypes.MessageContent{
			Role:  role,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: msg.Content}},
		}
	}
	return result
}

// convertMessagesToProto converts LLM messages to protobuf format
func (h *StreamHandler) convertMessagesToProto(messages []llmtypes.MessageContent) []*pb.Message {
	if messages == nil {
		return nil
	}

	result := make([]*pb.Message, len(messages))
	for i, msg := range messages {
		role := "user"
		switch msg.Role {
		case llmtypes.ChatMessageTypeHuman:
			role = "user"
		case llmtypes.ChatMessageTypeAI:
			role = "assistant"
		case llmtypes.ChatMessageTypeSystem:
			role = "system"
		}

		content := ""
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				content += textPart.Text
			}
		}

		result[i] = &pb.Message{
			Role:    role,
			Content: content,
		}
	}
	return result
}

// registerCustomTools registers custom tools with stream-based execution
func (h *StreamHandler) registerCustomTools(ctx context.Context, agent *ManagedAgent) {
	for _, toolDef := range agent.CustomTools {
		toolName := toolDef.Name
		h.logger.Info("Registering custom tool for stream execution",
			loggerv2.String("tool", toolName))

		// Create execution function that uses gRPC stream for tool callbacks
		executionFunc := func(execCtx context.Context, args map[string]interface{}) (string, error) {
			callID := uuid.New().String()[:8]

			// Convert args to protobuf Struct
			argsStruct, err := structpb.NewStruct(args)
			if err != nil {
				return "", fmt.Errorf("failed to convert tool arguments: %w", err)
			}

			// Send tool call request to client
			resp := &pb.ConversationResponse{
				Payload: &pb.ConversationResponse_ToolCall{
					ToolCall: &pb.ToolCallEvent{
						CallId:    callID,
						ToolName:  toolName,
						Arguments: argsStruct,
						TimeoutMs: safeIntToInt32(toolDef.TimeoutMs),
					},
				},
			}

			if err := h.stream.Send(resp); err != nil {
				return "", fmt.Errorf("failed to send tool call: %w", err)
			}

			h.logger.Debug("Sent tool call, waiting for result",
				loggerv2.String("tool", toolName),
				loggerv2.String("call_id", callID))

			// Wait for tool result from client
			select {
			case <-execCtx.Done():
				return "", execCtx.Err()
			case result := <-h.toolResultsChan:
				if result.CallId != callID {
					h.logger.Warn("Received tool result for different call",
						loggerv2.String("expected", callID),
						loggerv2.String("received", result.CallId))
				}
				if !result.Success {
					errMsg := "tool execution failed"
					if result.Error != nil {
						errMsg = result.Error.Message
					}
					return "", fmt.Errorf(errMsg)
				}
				return result.Result, nil
			}
		}

		// Determine category
		category := toolDef.Category
		if category == "" {
			category = "custom"
		}

		// Register with the agent
		err := agent.Agent.RegisterCustomTool(
			toolDef.Name,
			toolDef.Description,
			toolDef.Parameters,
			executionFunc,
			category,
		)
		if err != nil {
			h.logger.Error("Failed to register custom tool",
				err,
				loggerv2.String("tool", toolDef.Name))
		}
	}
}
