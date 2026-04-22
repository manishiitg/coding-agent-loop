package virtualtools

import (
	"context"
	"sync"
)

// ParentChatContext describes the builder/chat session that invoked a workflow.
// When present, human_input steps are routed back to that chat instead of the
// blocking popup UI: the question is injected as a message into the parent
// session and the builder decides whether to answer it itself or ask the user.
type ParentChatContext struct {
	SessionID    string // parent chat/builder session ID
	UserID       string // user associated with the parent session
	WorkflowPath string // for display in the injected message
	GroupName    string // for display in the injected message
	AgentID      string // background-agent ID of the workflow run (optional)
}

// ChatInjectFunc injects a message into an existing chat session as if the user
// had sent it. Implemented by the server (wraps sendFollowUpInternal).
type ChatInjectFunc func(ctx context.Context, sessionID, userID, message string) error

var (
	parentChatMu       sync.RWMutex
	parentChatRegistry = map[string]*ParentChatContext{} // key: workflow session ID

	chatInjectMu sync.RWMutex
	chatInject   ChatInjectFunc
)

// RegisterParentChat associates a workflow session with its invoking chat session.
func RegisterParentChat(workflowSessionID string, pc *ParentChatContext) {
	if workflowSessionID == "" || pc == nil || pc.SessionID == "" {
		return
	}
	parentChatMu.Lock()
	parentChatRegistry[workflowSessionID] = pc
	parentChatMu.Unlock()
}

// GetParentChat returns the parent chat context for a workflow session, or nil.
func GetParentChat(workflowSessionID string) *ParentChatContext {
	if workflowSessionID == "" {
		return nil
	}
	parentChatMu.RLock()
	defer parentChatMu.RUnlock()
	return parentChatRegistry[workflowSessionID]
}

// UnregisterParentChat removes the mapping (called when the workflow ends).
func UnregisterParentChat(workflowSessionID string) {
	if workflowSessionID == "" {
		return
	}
	parentChatMu.Lock()
	delete(parentChatRegistry, workflowSessionID)
	parentChatMu.Unlock()
}

// SetChatInjector installs the function used to inject messages into a chat
// session. Called once from server startup.
func SetChatInjector(fn ChatInjectFunc) {
	chatInjectMu.Lock()
	chatInject = fn
	chatInjectMu.Unlock()
}

// InjectChatMessage calls the installed injector (if any). Returns nil silently
// if no injector is installed — callers should treat that as "not available".
func InjectChatMessage(ctx context.Context, sessionID, userID, message string) error {
	chatInjectMu.RLock()
	fn := chatInject
	chatInjectMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, sessionID, userID, message)
}

// HasChatInjector reports whether the server has installed an injector.
func HasChatInjector() bool {
	chatInjectMu.RLock()
	defer chatInjectMu.RUnlock()
	return chatInject != nil
}
