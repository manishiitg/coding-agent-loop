package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// sseEvent is one message on a conversation's live stream — either a cosmetic
// "what Quill is doing right now" status label (Type "status") or a real
// content fragment of the reply as the model generates it (Type "delta").
// Both share one connection/subscription per conversation since they're both
// small, ordered, ephemeral signals for the SAME in-flight turn.
type sseEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// statusHub is a tiny in-memory pub-sub for live per-turn events, keyed by
// conversation id. It exists because mcpagent's own tool-call/streaming
// events never fire for our persistent/tmux sessions the way a direct API
// integration would (the CLI runs its own agentic loop inside the pane and
// calls our bridge directly) — so status labels rely on our own custom-tool
// Handler functions, which DO run directly in this process; streaming deltas
// rely on mcpagent's WithStreamingCallback (see agentsession.Config.StreamCallback),
// which DOES work for tmux/persistent sessions once the provider's own
// streaming env var is set (see main.go). The SSE endpoint below fans both
// out to the browser over one connection.
type statusHub struct {
	mu   sync.Mutex
	subs map[string]map[chan sseEvent]struct{}
}

var statusHubs = &statusHub{subs: map[string]map[chan sseEvent]struct{}{}}

func (h *statusHub) publish(conversationID, label string) {
	h.publishEvent(conversationID, sseEvent{Type: "status", Text: label})
}

func (h *statusHub) publishDelta(conversationID, text string) {
	h.publishEvent(conversationID, sseEvent{Type: "delta", Text: text})
}

func (h *statusHub) publishEvent(conversationID string, ev sseEvent) {
	conversationID = normalizeConversationID(conversationID)
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[conversationID] {
		select {
		case ch <- ev:
		default: // slow/absent subscriber — drop rather than block the tool call
		}
	}
}

func (h *statusHub) subscribe(conversationID string) (chan sseEvent, func()) {
	conversationID = normalizeConversationID(conversationID)
	ch := make(chan sseEvent, 64) // deltas arrive far more often than status labels
	h.mu.Lock()
	if h.subs[conversationID] == nil {
		h.subs[conversationID] = map[chan sseEvent]struct{}{}
	}
	h.subs[conversationID][ch] = struct{}{}
	h.mu.Unlock()
	unsubscribe := func() {
		h.mu.Lock()
		delete(h.subs[conversationID], ch)
		if len(h.subs[conversationID]) == 0 {
			delete(h.subs, conversationID)
		}
		h.mu.Unlock()
	}
	return ch, unsubscribe
}

// normalizeConversationID gives every uncorrelated caller (empty conversation
// id) the same bucket, matching how the rest of the server treats "no id".
func normalizeConversationID(id string) string {
	if id == "" {
		return "default"
	}
	return id
}

// toolStatusLabels are friendly, present-progressive phase labels shown while
// the agent works. The parent sees an honest phase during the wait — never
// raw commands, file paths, or tool/terminal output (§2A: no terminal, ever).
// A tool not listed here (e.g. suggest_actions, bridge chatter) stays silent.
var toolStatusLabels = map[string]string{
	"read_image":              "Reading the image",
	"generate_image":          "Drawing an illustration",
	"web_search":              "Looking up best practices",
	"set_child_profile":       "Saving the profile",
	"open_file":               "Opening the file",
	"approve_for_child":       "Sending it to your child",
	"create_learning_package": "Putting the package together",
	"set_teaching_style":      "Saving your teaching preference",
	"notify_user":             "Sending a notification",
	"execute_shell_command":   "Working through it",
	"agent_browser":           "Checking that link",
}

// withLiveStatus wraps each tool whose name has a friendly label so its
// Handler publishes that label to conversationID's status stream the moment it
// starts, then runs unchanged. Tools with no mapped label pass through as-is.
func withLiveStatus(conversationID string, tools []agentsession.Tool) []agentsession.Tool {
	out := make([]agentsession.Tool, len(tools))
	for i, t := range tools {
		label, ok := toolStatusLabels[t.Name]
		if !ok {
			out[i] = t
			continue
		}
		orig := t.Handler
		t.Handler = func(ctx context.Context, args map[string]interface{}) (string, error) {
			statusHubs.publish(conversationID, label)
			return orig(ctx, args)
		}
		out[i] = t
	}
	return out
}

// GET /api/parent/status?conversation_id=... and /api/child/status?conversation_id=...
// — SSE stream of live "Quill is: …" status lines for one conversation, while a
// turn is in flight. Purely additive UX: if nothing subscribes, publish() is a
// no-op fan-out to zero listeners.
func statusStreamHandler(scope string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conversationID := r.URL.Query().Get("conversation_id")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		setSSEHeaders(w)
		flusher.Flush() // push headers immediately -- otherwise they sit buffered until the first event

		ch, unsubscribe := statusHubs.subscribe(scope + ":" + conversationID)
		defer unsubscribe()

		for {
			select {
			case <-r.Context().Done():
				return
			case ev := <-ch:
				b, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
			}
		}
	}
}

var handleParentStatusStream = statusStreamHandler("parent")
var handleChildStatusStream = statusStreamHandler("child")

// setSSEHeaders prepares the response for a long-lived event stream.
func setSSEHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // don't let a proxy buffer the stream
	w.WriteHeader(http.StatusOK)
}
