package grpcserver

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/manishiitg/mcpagent/grpcserver/pb"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// AgentService implements the gRPC AgentService
type AgentService struct {
	pb.UnimplementedAgentServiceServer
	manager *AgentManager
	logger  loggerv2.Logger
}

// NewAgentService creates a new AgentService
func NewAgentService(manager *AgentManager, logger loggerv2.Logger) *AgentService {
	return &AgentService{
		manager: manager,
		logger:  logger,
	}
}

// HealthCheck implements the health check RPC
func (s *AgentService) HealthCheck(ctx context.Context, req *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error) {
	return &pb.HealthCheckResponse{
		Status: "ok",
	}, nil
}

// CreateAgent creates a new agent instance
func (s *AgentService) CreateAgent(ctx context.Context, req *pb.CreateAgentRequest) (*pb.CreateAgentResponse, error) {
	// Convert protobuf config to AgentConfig
	config, err := s.convertAgentConfig(req.Config)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid config: %v", err)
	}

	// Create the agent using the manager
	createReq := CreateAgentRequest{
		SessionID: req.SessionId,
		Config:    config,
	}

	agent, err := s.manager.CreateAgent(ctx, createReq)
	if err != nil {
		s.logger.Error("Failed to create agent", err)
		return nil, status.Errorf(codes.Internal, "failed to create agent: %v", err)
	}

	// Get capabilities
	caps, _ := s.manager.GetCapabilities(agent.ID)

	return &pb.CreateAgentResponse{
		AgentId:   agent.ID,
		SessionId: agent.SessionID,
		Status:    "ready",
		CreatedAt: timestamppb.New(agent.CreatedAt),
		Capabilities: &pb.Capabilities{
			Tools:   caps.Tools,
			Servers: caps.Servers,
		},
	}, nil
}

// GetAgent retrieves information about an agent
func (s *AgentService) GetAgent(ctx context.Context, req *pb.GetAgentRequest) (*pb.GetAgentResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	agent, ok := s.manager.GetAgent(req.AgentId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "agent not found: %s", req.AgentId)
	}

	// Get token usage
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, _ := agent.Agent.GetTokenUsage()

	caps, _ := s.manager.GetCapabilities(agent.ID)

	return &pb.GetAgentResponse{
		AgentId:   agent.ID,
		SessionId: agent.SessionID,
		Status:    "ready",
		CreatedAt: timestamppb.New(agent.CreatedAt),
		Capabilities: &pb.Capabilities{
			Tools:   caps.Tools,
			Servers: caps.Servers,
		},
		TokenUsage: &pb.TokenUsage{
			PromptTokens:     safeIntToInt32(promptTokens),
			CompletionTokens: safeIntToInt32(completionTokens),
			TotalTokens:      safeIntToInt32(totalTokens),
			CacheTokens:      safeIntToInt32(cacheTokens),
			ReasoningTokens:  safeIntToInt32(reasoningTokens),
			LlmCallCount:     safeIntToInt32(llmCallCount),
		},
	}, nil
}

// ListAgents lists all active agents
func (s *AgentService) ListAgents(ctx context.Context, req *pb.ListAgentsRequest) (*pb.ListAgentsResponse, error) {
	agents := s.manager.ListAgents()

	pbAgents := make([]*pb.AgentSummary, len(agents))
	for i, agent := range agents {
		pbAgents[i] = &pb.AgentSummary{
			AgentId:   agent.AgentID,
			SessionId: agent.SessionID,
			Status:    agent.Status,
			CreatedAt: timestamppb.New(agent.CreatedAt),
		}
	}

	return &pb.ListAgentsResponse{
		Agents: pbAgents,
	}, nil
}

// DestroyAgent destroys an agent
func (s *AgentService) DestroyAgent(ctx context.Context, req *pb.DestroyAgentRequest) (*pb.DestroyAgentResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	if err := s.manager.DestroyAgent(req.AgentId); err != nil {
		return nil, status.Errorf(codes.NotFound, "failed to destroy agent: %v", err)
	}

	return &pb.DestroyAgentResponse{
		AgentId:   req.AgentId,
		Destroyed: true,
	}, nil
}

// GetTokenUsage retrieves token usage and costs for an agent
func (s *AgentService) GetTokenUsage(ctx context.Context, req *pb.GetTokenUsageRequest) (*pb.TokenUsageResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	agent, ok := s.manager.GetAgent(req.AgentId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "agent not found: %s", req.AgentId)
	}

	// Get token usage with pricing
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, _, inputCost, outputCost, reasoningCost, cacheCost, totalCost, _ := agent.Agent.GetTokenUsageWithPricing()

	return &pb.TokenUsageResponse{
		TokenUsage: &pb.TokenUsage{
			PromptTokens:     safeIntToInt32(promptTokens),
			CompletionTokens: safeIntToInt32(completionTokens),
			TotalTokens:      safeIntToInt32(totalTokens),
			CacheTokens:      safeIntToInt32(cacheTokens),
			ReasoningTokens:  safeIntToInt32(reasoningTokens),
			LlmCallCount:     safeIntToInt32(llmCallCount),
		},
		Costs: &pb.Costs{
			InputCost:     inputCost,
			OutputCost:    outputCost,
			ReasoningCost: reasoningCost,
			CacheCost:     cacheCost,
			TotalCost:     totalCost,
		},
	}, nil
}

// Ask handles a single question (unary RPC for backward compatibility)
func (s *AgentService) Ask(ctx context.Context, req *pb.AskRequest) (*pb.AskResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if req.Question == "" {
		return nil, status.Error(codes.InvalidArgument, "question is required")
	}

	agent, ok := s.manager.GetAgent(req.AgentId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "agent not found: %s", req.AgentId)
	}

	startTime := time.Now()

	// Call the agent
	response, err := agent.Agent.Ask(ctx, req.Question)
	if err != nil {
		s.logger.Error("Ask failed", err, loggerv2.String("agent_id", req.AgentId))
		return nil, status.Errorf(codes.Internal, "ask failed: %v", err)
	}

	duration := time.Since(startTime)

	// Get token usage
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, _ := agent.Agent.GetTokenUsage()

	return &pb.AskResponse{
		Response: response,
		TokenUsage: &pb.TokenUsage{
			PromptTokens:     safeIntToInt32(promptTokens),
			CompletionTokens: safeIntToInt32(completionTokens),
			TotalTokens:      safeIntToInt32(totalTokens),
			CacheTokens:      safeIntToInt32(cacheTokens),
			ReasoningTokens:  safeIntToInt32(reasoningTokens),
			LlmCallCount:     safeIntToInt32(llmCallCount),
		},
		DurationMs: duration.Milliseconds(),
	}, nil
}

// AskWithHistory handles a multi-turn conversation (unary RPC for backward compatibility)
func (s *AgentService) AskWithHistory(ctx context.Context, req *pb.AskWithHistoryRequest) (*pb.AskWithHistoryResponse, error) {
	if req.AgentId == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}
	if len(req.Messages) == 0 {
		return nil, status.Error(codes.InvalidArgument, "messages array is required")
	}

	agent, ok := s.manager.GetAgent(req.AgentId)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "agent not found: %s", req.AgentId)
	}

	startTime := time.Now()

	// Convert messages to LLM format
	messages := make([]llmtypes.MessageContent, len(req.Messages))
	for i, msg := range req.Messages {
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

		messages[i] = llmtypes.MessageContent{
			Role:  role,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: msg.Content}},
		}
	}

	// Call the agent
	response, updatedMessages, err := agent.Agent.AskWithHistory(ctx, messages)
	if err != nil {
		s.logger.Error("AskWithHistory failed", err, loggerv2.String("agent_id", req.AgentId))
		return nil, status.Errorf(codes.Internal, "ask with history failed: %v", err)
	}

	duration := time.Since(startTime)

	// Convert updated messages back to protobuf
	pbMessages := make([]*pb.Message, len(updatedMessages))
	for i, msg := range updatedMessages {
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

		pbMessages[i] = &pb.Message{
			Role:    role,
			Content: content,
		}
	}

	// Get token usage
	promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCallCount, _ := agent.Agent.GetTokenUsage()

	return &pb.AskWithHistoryResponse{
		Response:        response,
		UpdatedMessages: pbMessages,
		TokenUsage: &pb.TokenUsage{
			PromptTokens:     safeIntToInt32(promptTokens),
			CompletionTokens: safeIntToInt32(completionTokens),
			TotalTokens:      safeIntToInt32(totalTokens),
			CacheTokens:      safeIntToInt32(cacheTokens),
			ReasoningTokens:  safeIntToInt32(reasoningTokens),
			LlmCallCount:     safeIntToInt32(llmCallCount),
		},
		DurationMs: duration.Milliseconds(),
	}, nil
}

// Converse implements bidirectional streaming conversation
// This is the key method that enables real-time streaming and inline tool callbacks
func (s *AgentService) Converse(stream pb.AgentService_ConverseServer) error {
	// Create a stream handler for this conversation
	handler := NewStreamHandler(s.manager, s.logger, stream)
	return handler.Handle()
}

// Helper function to convert protobuf AgentConfig to AgentConfig
func (s *AgentService) convertAgentConfig(pbConfig *pb.AgentConfig) (AgentConfig, error) {
	if pbConfig == nil {
		return AgentConfig{}, nil
	}

	var temp *float64
	if pbConfig.Temperature != 0 {
		t := pbConfig.Temperature
		temp = &t
	}

	// Convert custom tools
	var customTools []CustomToolDefinition
	for _, tool := range pbConfig.CustomTools {
		params := make(map[string]interface{})
		if tool.Parameters != nil {
			params = tool.Parameters.AsMap()
		}

		customTools = append(customTools, CustomToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  params,
			TimeoutMs:   int(tool.TimeoutMs),
			Category:    tool.Category,
		})
	}

	return AgentConfig{
		Provider:                   pbConfig.Provider,
		ModelID:                    pbConfig.ModelId,
		Temperature:                temp,
		MaxTurns:                   int(pbConfig.MaxTurns),
		MCPConfigPath:              pbConfig.McpConfigPath,
		SelectedServers:            pbConfig.SelectedServers,
		SelectedTools:              pbConfig.SelectedTools,
		SystemPrompt:               pbConfig.SystemPrompt,
		EnableContextSummarization: pbConfig.EnableContextSummarization,
		EnableContextOffloading:    pbConfig.EnableContextOffloading,
		EnableStreaming:            pbConfig.EnableStreaming,
		CustomTools:                customTools,
	}, nil
}

// safeIntToInt32 safely converts int to int32 with bounds checking
func safeIntToInt32(n int) int32 {
	const maxInt32 = 1<<31 - 1
	const minInt32 = -1 << 31
	if n > maxInt32 {
		return maxInt32
	}
	if n < minInt32 {
		return minInt32
	}
	return int32(n)
}
