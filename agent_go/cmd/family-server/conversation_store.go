package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

// convFileMu guards read-modify-write access to a conversation's transcript
// file. Needed now that a conversation can be written from two independent
// places for the same turn: the steer endpoint (appendUserMessageToConversation,
// the instant a message is delivered live) and the original turn's own
// completion (handleParentMessage, which reloads-then-appends the reply — see
// chat.go). Without this, a steer's write racing the original turn's
// read-then-write could see a torn/partial file and silently lose history.
var convFileMu sync.Mutex

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
	writeFileAtomic(filepath.Join(dir, id+".json"), b)
}

// writeFileAtomic writes to a temp file in the same directory then renames it
// into place, so a concurrent reader (loadStoredConversation, or anything else
// reading this file) never observes a truncated/partial write — a plain
// os.WriteFile truncates before writing, which a reader can catch mid-flight.
// Mirrors the same pattern mcpagent's own file-backed session store already
// uses for exactly this reason.
func writeFileAtomic(path string, b []byte) {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// appendUserMessageToConversation durably records a message the instant it's
// steered into an already-running turn (see steer.go) — so it's saved even if
// that turn later errors, or the process restarts before the turn completes.
// The whole reload-append-write happens under convFileMu as one step, so it
// can never race with any other reload-append-write for the SAME conversation.
func appendUserMessageToConversation(scope, id, text string) {
	convFileMu.Lock()
	defer convFileMu.Unlock()
	existing, _ := loadStoredConversation(scope, id)
	full := append([]enginedetect.ChatMessage(nil), existing.Messages...)
	full = append(full, enginedetect.ChatMessage{Role: "user", Text: text})
	persistConversation(scope, id, full)
}

// persistNewMessages durably records the message that kicks off a turn the
// INSTANT it starts — not just at completion — so the on-disk transcript is
// a genuinely append-only log from the moment a turn begins, before any
// tool calls run and before a concurrent steer (see steer.go) could possibly
// land. full is the caller's own full history including its new message,
// which by construction (see sendParentText) is always full's last element.
//
// This exists because two divergent things can each want to append to the
// SAME on-disk base once a turn is running: the turn's own kickoff message
// (known from the start) and a steered-in message (known only mid-turn,
// added via appendUserMessageToConversation). Persisting the kickoff here,
// immediately, means the disk copy is always the single, complete,
// continuously-growing source of truth by the time the turn completes —
// persistConversationReply then only ever needs to reload it and append the
// reply, never merge two divergent snapshots.
//
// Deliberately does NOT require existing on-disk history to be an exact
// prefix of full: the caller's own snapshot of "everything so far" can go
// stale relative to disk (another channel — Pulse, WhatsApp, a steer, or
// simply this same handler racing itself — may have appended something disk
// already has that the caller's snapshot doesn't), and disk can equally be
// stale relative to what the caller already knows. An earlier version of
// this function required byte-for-byte prefix agreement and silently did
// nothing when that failed, which — confirmed live — silently dropped the
// user's own new message, keeping only the eventual reply. Instead: always
// append full's last message onto whatever is CURRENTLY on disk (not onto
// full's own possibly-stale prefix), unless it's already the last thing on
// disk (e.g. this function running twice for the same turn).
func persistNewMessages(scope, id string, full []enginedetect.ChatMessage) {
	convFileMu.Lock()
	defer convFileMu.Unlock()
	if len(full) == 0 {
		return
	}
	newest := full[len(full)-1]
	existing, ok := loadStoredConversation(scope, id)
	if !ok {
		persistConversation(scope, id, []enginedetect.ChatMessage{newest})
		return
	}
	if n := len(existing.Messages); n > 0 {
		last := existing.Messages[n-1]
		if last.Role == newest.Role && last.Text == newest.Text {
			return // already there
		}
	}
	merged := append([]enginedetect.ChatMessage(nil), existing.Messages...)
	merged = append(merged, newest)
	persistConversation(scope, id, merged)
}

// persistConversationReply reloads the on-disk history for a conversation —
// which now always includes both the message that kicked off this turn
// (persisted immediately by persistNewMessages when the turn started) and
// any message steered into it mid-turn (persisted immediately by
// appendUserMessageToConversation) — and appends the reply. fallback is used
// only if nothing has been persisted for this id yet at all.
//
// The whole operation is one atomic critical section (not reload-then-
// separately-persist) so a concurrent steer's append can never be lost to a
// race with this turn's own completion write.
func persistConversationReply(scope, id string, fallback []enginedetect.ChatMessage, reply string) {
	convFileMu.Lock()
	defer convFileMu.Unlock()
	base := fallback
	if existing, ok := loadStoredConversation(scope, id); ok {
		base = existing.Messages
	}
	persistConversation(scope, id, withReply(base, reply))
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
// the content type and handles range requests. With ?download=1 it forces a
// download (Content-Disposition: attachment) rather than inline render — used by
// the viewer's Download button for files the browser can't preview (Word,
// PowerPoint, spreadsheets, archives, …). The download attribute on an <a> is
// ignored cross-origin, so the server has to set the disposition itself.
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
	if r.URL.Query().Get("download") != "" {
		name := strings.ReplaceAll(filepath.Base(abs), `"`, "")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	}
	// ?print=1 on an HTML file serves it in a full page (opened in a new tab) that
	// auto-opens the browser's print dialog. This is the robust way to print a
	// test/report: it doesn't depend on the generated HTML embedding a print
	// handler (a skill can forget to), and it isn't blocked by the in-app viewer's
	// iframe sandbox. Relative assets still resolve since it's the real file URL.
	if r.URL.Query().Get("print") != "" {
		ext := strings.ToLower(filepath.Ext(abs))
		if ext == ".html" || ext == ".htm" {
			b, err := os.ReadFile(abs)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n<script>window.addEventListener('load',function(){setTimeout(function(){window.print()},250)});</script>\n"))
			return
		}
	}
	http.ServeFile(w, r, abs)
}
