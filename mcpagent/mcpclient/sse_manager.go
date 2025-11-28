package mcpclient

import (
	"context"
	"fmt"

	"mcpagent/logger"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
)

// SSEManager provides simple SSE connection management
type SSEManager struct {
	url     string
	headers map[string]string
	logger  logger.ExtendedLogger
}

// NewSSEManager creates a new SSE manager
func NewSSEManager(url string, headers map[string]string, logger logger.ExtendedLogger) *SSEManager {
	return &SSEManager{
		url:     url,
		headers: headers,
		logger:  logger,
	}
}

// CreateClient creates a new SSE client with direct connection
func (s *SSEManager) CreateClient() (*client.Client, error) {
	// Create transport options
	var options []transport.ClientOption

	// Add headers if provided
	if len(s.headers) > 0 {
		options = append(options, transport.WithHeaders(s.headers))
	}

	// Add custom logger for better debugging
	options = append(options, transport.WithSSELogger(s.logger))

	// Create SSE transport
	sseTransport, err := transport.NewSSE(s.url, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE transport: %w", err)
	}

	// Create client with transport
	return client.NewClient(sseTransport), nil
}

// Connect creates and starts an SSE client
func (s *SSEManager) Connect(ctx context.Context) (*client.Client, error) {
	client, err := s.CreateClient()
	if err != nil {
		return nil, err
	}

	// For SSE connections, use background context for Start() to prevent stream cancellation
	// The provided context will be used for actual MCP calls (ListTools, etc.)
	// This prevents the SSE stream from being canceled when the caller's context is done
	startCtx := context.Background()
	s.logger.Infof("🔍 Using background context for SSE Start() to prevent stream cancellation")

	// Start the client with background context
	if err := client.Start(startCtx); err != nil {
		return nil, fmt.Errorf("failed to start SSE client: %w", err)
	}

	return client, nil
}
