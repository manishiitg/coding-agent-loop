package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

// parentConversationID is the SINGLE canonical parent↔Quill conversation for
// this family. Web chat, WhatsApp, and Pulse all read/write/resume THIS one —
// one file (parent/conversations/parent.json), one warm tmux session — so
// Quill has unified context no matter how the parent reaches it. The app is one
// family with one ongoing conversation; there is no multi-conversation list.
const parentConversationID = "parent"

type storedConversation struct {
	ID        string                     `json:"id"`
	Scope     string                     `json:"scope"`
	Title     string                     `json:"title"`
	UpdatedAt string                     `json:"updated_at"`
	Messages  []enginedetect.ChatMessage `json:"messages"`
}

// persistConversation writes a chat transcript to <scope>/conversations/<id>.json.
// This is a runtime side-effect of a chat turn (mirroring the engine's own
// chat-history files) — not a CRUD API. Best-effort; failures are ignored so a
// persistence hiccup never breaks the reply.
func persistConversation(scope, id string, messages []enginedetect.ChatMessage) {
	id = strings.TrimSpace(id)
	if id == "" || (scope != "parent" && scope != "child") || len(messages) == 0 {
		return
	}
	id = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
	dir := filepath.Join(workspaceRoot(), scope, "conversations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	title := ""
	for _, m := range messages {
		if strings.EqualFold(m.Role, "user") && strings.TrimSpace(m.Text) != "" {
			title = strings.TrimSpace(m.Text)
			break
		}
	}
	if len([]rune(title)) > 60 {
		title = string([]rune(title)[:60]) + "…"
	}
	conv := storedConversation{
		ID:        id,
		Scope:     scope,
		Title:     title,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Messages:  messages,
	}
	b, err := json.MarshalIndent(conv, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, id+".json"), b, 0o600)
}

// loadStoredConversation reads one persisted conversation by id (same id
// sanitization as persistConversation). Used by Pulse to load a specific,
// known conversation directly rather than scanning the whole directory.
func loadStoredConversation(scope, id string) (storedConversation, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return storedConversation{}, false
	}
	id = strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(id)
	b, err := os.ReadFile(filepath.Join(workspaceRoot(), scope, "conversations", id+".json"))
	if err != nil {
		return storedConversation{}, false
	}
	var conv storedConversation
	if json.Unmarshal(b, &conv) != nil || conv.ID == "" {
		return storedConversation{}, false
	}
	return conv, true
}

// handleWorkspaceFile serves GET /api/workspace/file?path=... — read one
// workspace text file. This is the generic file-system READ primitive the
// file-based UI needs (drawer preview, the academic map, a saved conversation).
// It is not domain CRUD: one primitive, any workspace-relative path, read-only.
func handleWorkspaceFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rel := r.URL.Query().Get("path")
	abs, ok := resolveWorkspacePath(rel)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if info.Size() > 1024*1024 {
		writeJSON(w, http.StatusOK, map[string]interface{}{"path": rel, "is_text": false, "size": info.Size()})
		return
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"path": rel, "is_text": true, "content": string(b)})
}

// handleWorkspaceRaw serves GET /api/workspace/raw?path=... — the raw bytes of a
// workspace file (for images the viewer renders with <img>). http.ServeFile sets
// the content type and handles range requests.
func handleWorkspaceRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	abs, ok := resolveWorkspacePath(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, abs)
}
