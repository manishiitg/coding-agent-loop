package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var stateKeyRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// safeStateKey turns an arbitrary key from interactive HTML into a safe single
// filename (no traversal, no separators).
func safeStateKey(k string) string {
	k = strings.NewReplacer("/", "-", "\\", "-", "..", "-", " ", "-").Replace(strings.TrimSpace(k))
	k = stateKeyRe.ReplaceAllString(k, "")
	if len(k) > 120 {
		k = k[:120]
	}
	return k
}

// handleWorkspaceState persists interactive-HTML state (a child's typed answers,
// quiz progress, etc.) to a workspace file under child/attempts/, so the state
// survives reloads AND the agent can read the child's work later to give
// feedback. POST {key,data} to save; GET ?key= to load. The write happens in the
// app (parent frame), reached from the sandboxed HTML via postMessage.
func handleWorkspaceState(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Join(workspaceRoot(), "child", "attempts")
	switch r.Method {
	case http.MethodGet:
		key := safeStateKey(r.URL.Query().Get("key"))
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key required"})
			return
		}
		b, err := os.ReadFile(filepath.Join(dir, key+".json"))
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"key": key, "data": nil})
			return
		}
		var stored map[string]interface{}
		if json.Unmarshal(b, &stored) != nil {
			stored = map[string]interface{}{"key": key, "data": nil}
		}
		writeJSON(w, http.StatusOK, stored)
	case http.MethodPost:
		var req struct {
			Key  string      `json:"key"`
			Data interface{} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		key := safeStateKey(req.Key)
		if key == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key required"})
			return
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out, _ := json.MarshalIndent(map[string]interface{}{
			"key": req.Key, "data": req.Data, "updated_at": time.Now().UTC().Format(time.RFC3339),
		}, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, key+".json"), out, 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
