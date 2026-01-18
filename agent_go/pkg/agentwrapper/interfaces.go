// Package agent provides interfaces for agent abstractions that wrap complex MCP agents
// into simple LLM-like interfaces with optional streaming, capabilities, lifecycle, and metrics support.

package agent

import (
	"context"
	"errors"
)

// LLMAgent is the core synchronous interface for agent invocation.
// It provides a simple prompt-in, response-out contract, hiding all underlying complexity.
type LLMAgent interface {
	// Invoke sends a prompt to the agent and returns the complete response.
	// The context can be used for cancellation and timeouts.
	Invoke(ctx context.Context, prompt string) (string, error)
}

// AgentStreamer provides optional streaming support for real-time response generation.
// It returns a read-only channel of chunks; the channel closes when streaming completes.
type AgentStreamer interface {
	// Stream sends a prompt and returns a channel of response chunks.
	// Chunks contain either content or an error. The channel closes on completion.
	// Cancel the context to stop streaming early.
	Stream(ctx context.Context, prompt string) (<-chan StreamChunk, error)
}

// StreamChunk represents a single chunk in the streaming response.
// It can contain either content or an error (but not both).
type StreamChunk struct {
	Content string // Response content chunk
	Err     error  // Error, if any (e.g., network failure)
}

// AgentCapabilities provides methods for discovering agent features.
type AgentCapabilities interface {
	// GetCapabilities returns a human-readable summary of available tools/capabilities.
	GetCapabilities() string
	// GetName returns a unique identifier for the agent (e.g., "filesystem-agent-bedrock").
	GetName() string
}

// AgentLifecycle provides methods for managing the agent's lifecycle.
type AgentLifecycle interface {
	// Initialize prepares the agent for use (e.g., connects to MCP servers).
	// It should be idempotent (safe to call multiple times).
	Initialize(ctx context.Context) error
	// Close releases resources (e.g., closes connections).
	// It should be safe to call on an uninitialized agent.
	Close() error
	// IsReady checks if the agent is initialized and healthy.
	IsReady() bool
}

// AgentMetrics provides runtime statistics about agent usage.
type AgentMetrics interface {
	// GetMetrics returns a map of metrics (e.g., {"invocations": 5, "total_tokens": 1234}).
	GetMetrics() map[string]any
}

// Agent is the composed interface that embeds all optional interfaces.
// Concrete implementations should satisfy this for full functionality.
// Parent systems can type-assert for specific features as needed.
type Agent interface {
	LLMAgent
	AgentStreamer     // Optional streaming
	AgentCapabilities // Optional discovery
	AgentLifecycle    // Optional management
	AgentMetrics      // Optional monitoring
}

// ErrNotInitialized is returned when Invoke/Stream is called on an uninitialized agent.
var ErrNotInitialized = errors.New("agent not initialized")

// ErrStreamCanceled is sent as a chunk error when the context is canceled during streaming.
var ErrStreamCanceled = errors.New("stream canceled")
