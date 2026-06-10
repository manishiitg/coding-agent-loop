package guidance

import (
	"context"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
)

// DocReadTracker records which reference docs each chat session has loaded
// via get_reference_doc. The precondition middleware (see WithDocPrecondition)
// reads this state to decide whether a gated tool call is allowed to proceed.
//
// Lifetime: in-memory only. A session's loads survive for the lifetime of the
// server process. When the server restarts, the agent must re-load any docs
// before gated tools succeed again. This is intentional — the agent's chat
// context resets on restart anyway, so the doc content would have to be
// re-fetched to be useful.
//
// Thread-safety: RWMutex-protected. MarkLoaded uses a write lock; HasLoaded
// uses a read lock. Both are O(1) hash lookups.
type DocReadTracker struct {
	mu    sync.RWMutex
	loads map[string]map[string]time.Time // sessionID → kind → loadedAt
}

// NewDocReadTracker returns a fresh tracker with no loads recorded.
func NewDocReadTracker() *DocReadTracker {
	return &DocReadTracker{
		loads: make(map[string]map[string]time.Time),
	}
}

// MarkLoaded records that the given session has successfully loaded the
// named reference doc. Subsequent HasLoaded(sessionID, kind) returns true
// until the process restarts.
//
// No-op if sessionID is empty (defensive — should never happen in practice
// because RegisterReferenceDocTool extracts sessionID from context before
// calling).
func (t *DocReadTracker) MarkLoaded(sessionID, kind string) {
	if sessionID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	bySession, ok := t.loads[sessionID]
	if !ok {
		bySession = make(map[string]time.Time)
		t.loads[sessionID] = bySession
	}
	bySession[kind] = time.Now()
}

// HasLoaded reports whether the given session has previously called
// get_reference_doc(kind=...) successfully.
//
// Returns false if sessionID is empty, the session has never loaded any doc,
// or the specific kind hasn't been loaded by this session.
func (t *DocReadTracker) HasLoaded(sessionID, kind string) bool {
	if sessionID == "" {
		return false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	bySession, ok := t.loads[sessionID]
	if !ok {
		return false
	}
	_, loaded := bySession[kind]
	return loaded
}

// MissingFor returns the subset of `required` kinds that the session has
// NOT loaded yet. Used by precondition middleware to build a teaching error
// listing exactly which docs the agent must load before retrying.
func (t *DocReadTracker) MissingFor(sessionID string, required []string) []string {
	if len(required) == 0 {
		return nil
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	bySession := t.loads[sessionID]
	missing := make([]string, 0, len(required))
	for _, k := range required {
		if _, ok := bySession[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

// SessionIDFromContext extracts the chat session ID from the context using
// the shared common.ChatSessionIDKey. Returns "" if not set. Centralized
// here so the tracker and middleware never reach into pkg/common from their
// own call sites.
func SessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(common.ChatSessionIDKey).(string); ok {
		return v
	}
	return ""
}

// defaultTracker is the process-wide singleton used by RegisterReferenceDocTool
// and the precondition middleware. Tests should construct their own tracker
// via NewDocReadTracker rather than relying on this global.
var defaultTracker = NewDocReadTracker()

// DefaultTracker returns the process-wide singleton tracker. Production
// callers (server tool registration, gated tool wrappers) use this.
func DefaultTracker() *DocReadTracker {
	return defaultTracker
}

// ToolHandler is the standard signature every workflow tool's handler uses.
// Mirrored here so WithDocPrecondition can wrap handlers without importing
// the mcpagent package.
type ToolHandler func(ctx context.Context, args map[string]interface{}) (string, error)

// WithDocPrecondition wraps a tool handler with a doc-read precondition gate.
// Before delegating to `next`, the wrapper checks that the current session
// has loaded every kind in `requiredKinds` via get_reference_doc. If any are
// missing, the call returns a teaching-error string telling the agent exactly
// which docs to load before retrying — instead of running the gated handler.
//
// The wrapper is no-op when there is no session ID on the context (e.g. in
// tests that don't bother stamping one). This avoids breaking unrelated
// integration tests; production paths always stamp ChatSessionIDKey.
//
// Use the process-wide DefaultTracker for production wiring:
//
//	wrapped := guidance.WithDocPrecondition(
//	    []string{"optimize-playbook", "code-authoring"},
//	    guidance.DefaultTracker(),
//	    originalHandler,
//	)
//
// Tests can pass their own tracker via NewDocReadTracker so each test starts
// from a clean state.
func WithDocPrecondition(requiredKinds []string, tracker *DocReadTracker, next ToolHandler) ToolHandler {
	if len(requiredKinds) == 0 {
		return next
	}
	if tracker == nil {
		tracker = DefaultTracker()
	}
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		sid := SessionIDFromContext(ctx)
		if sid == "" {
			// No session ID — likely a test path or an internal caller that
			// doesn't go through the chat surface. Don't block.
			return next(ctx, args)
		}
		missing := tracker.MissingFor(sid, requiredKinds)
		if len(missing) == 0 {
			return next(ctx, args)
		}
		return preconditionErrorMessage(missing), nil
	}
}

// preconditionErrorMessage builds the structured teaching-error returned to
// the agent when a gated tool fires before its prerequisite docs are loaded.
// Format: a single JSON object the model can recognize, with a `next_action`
// field naming the exact tool calls to make. The format intentionally mirrors
// successful tool results (string body, parseable JSON) so the model treats
// the error consistently with other tool returns.
func preconditionErrorMessage(missing []string) string {
	// Build a list of suggested calls so the agent has an exact next step,
	// not just a list of names.
	calls := make([]string, 0, len(missing))
	for _, k := range missing {
		calls = append(calls, `get_reference_doc(kind="`+k+`")`)
	}
	return `{"error": "precondition_not_met", "message": "This tool requires reference docs that have not been loaded in this session. Call ` +
		joinAnd(calls) +
		` first, then retry. The reference doc explains the rules this tool's downstream agent will apply — calling without it risks producing changes that violate those rules. Only the get_reference_doc call satisfies this gate — reading the same content from a skill's references/ folder or from disk does not register, so make the tool call even if you have already read that material.", "required_kinds": ` +
		jsonStringList(missing) + `}`
}

// joinAnd renders a list of strings as "a, b and c" — natural-language
// concatenation used in agent-facing error messages.
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	out := ""
	for i, s := range items {
		switch {
		case i == 0:
			out = s
		case i == len(items)-1:
			out += " and " + s
		default:
			out += ", " + s
		}
	}
	return out
}

// jsonStringList renders a Go []string as a JSON array literal. Used inside
// the teaching-error string so the agent can parse `required_kinds` if it
// wants to programmatically retry. Local helper to avoid pulling encoding/json
// just for this.
func jsonStringList(items []string) string {
	out := "["
	for i, s := range items {
		if i > 0 {
			out += ", "
		}
		out += `"` + s + `"`
	}
	out += "]"
	return out
}
