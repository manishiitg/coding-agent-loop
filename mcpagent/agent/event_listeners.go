package mcpagent

import (
	"context"
	"mcpagent/events"
	"sync"

	"github.com/mark3labs/mcp-go/util"
)

// AgentEventDispatcher manages multiple agent event listeners
type AgentEventDispatcher struct {
	listeners []AgentEventListener
	mu        sync.RWMutex
	logger    util.Logger
}

// NewAgentEventDispatcher creates a new agent event dispatcher
func NewAgentEventDispatcher(logger util.Logger) *AgentEventDispatcher {
	return &AgentEventDispatcher{
		listeners: make([]AgentEventListener, 0),
		logger:    logger,
	}
}

// AddListener adds an event listener to the dispatcher
func (d *AgentEventDispatcher) AddListener(listener AgentEventListener) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.listeners = append(d.listeners, listener)
}

// DispatchEvent sends an event to all registered listeners
func (d *AgentEventDispatcher) DispatchEvent(ctx context.Context, event *events.AgentEvent) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, listener := range d.listeners {
		func() {
			defer func() {
				if r := recover(); r != nil {
					d.logger.Errorf("Panic in event listener %s: %v", listener.Name(), r)
				}
			}()

			if err := listener.HandleEvent(ctx, event); err != nil {
				d.logger.Errorf("Error in event listener %s: %v", listener.Name(), err)
			}
		}()
	}
}

// ConsoleAgentEventListener outputs events to console for debugging
type ConsoleAgentEventListener struct {
	logger util.Logger
}

func NewConsoleAgentEventListener(logger util.Logger) *ConsoleAgentEventListener {
	return &ConsoleAgentEventListener{
		logger: logger,
	}
}

// Name returns the listener name
func (c *ConsoleAgentEventListener) Name() string {
	return "console_agent"
}

// HandleEvent outputs events to console for debugging
func (c *ConsoleAgentEventListener) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	logger := c.logger

	// Simple event logging without detailed switch statement
	logger.Infof("📊 Event: %s", event.Type)

	return nil
}

// SSEAgentEventListener handles Server-Sent Events for real-time updates
type SSEAgentEventListener struct {
	clients map[string]chan *events.AgentEvent
	mu      sync.RWMutex
	logger  util.Logger
}

// NewSSEAgentEventListener creates a new SSE agent event listener
func NewSSEAgentEventListener(logger util.Logger) *SSEAgentEventListener {
	return &SSEAgentEventListener{
		clients: make(map[string]chan *events.AgentEvent),
		logger:  logger,
	}
}

// Name returns the listener name
func (s *SSEAgentEventListener) Name() string {
	return "sse_agent"
}

// AddClient adds a client to receive SSE events
func (s *SSEAgentEventListener) AddClient(clientID string, ch chan *events.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[clientID] = ch
}

// RemoveClient removes a client from SSE events
func (s *SSEAgentEventListener) RemoveClient(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, exists := s.clients[clientID]; exists {
		close(ch)
		delete(s.clients, clientID)
	}
}

// HandleEvent sends events to all connected SSE clients
func (s *SSEAgentEventListener) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for clientID, ch := range s.clients {
		select {
		case ch <- event:
			// Event sent successfully
		default:
			// Channel is full or closed, remove client
			s.logger.Infof("SSE client %s channel is full or closed, removing", clientID)
			go s.RemoveClient(clientID)
		}
	}

	return nil
}
