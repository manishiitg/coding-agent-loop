package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// handleHandoff is the "Give to <child>" button. In the activity-folder model a
// handoff is one state change: point the child session at an activity DIR
// (current-activity.json, see activity.go). The frontend then switches to child
// mode; Quill (the child tutor) reads that activity's activity.json, opens its
// files, and greets the child. The child is sandboxed to exactly that one
// folder (childShellTool / childCanSee/childCanWrite).
func handleHandoff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		// Dir is the activity folder (workspace-relative) being handed off. The
		// old {path,manifest} shape is gone — everything is an activity now.
		Dir string `json:"dir"`
		// Resume, when the frontend sends it, is the parent's explicit answer to
		// "continue the child's existing chat for this activity, or start fresh?"
		// and overrides the same-activity heuristic below. Nil = use the heuristic.
		Resume *bool `json:"resume,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dir is required"})
		return
	}
	dir := strings.Trim(strings.TrimSpace(req.Dir), "/")
	act, ok := loadActivity(dir)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "activity not found"})
		return
	}

	// Re-opening the SAME activity resumes its own conversation; a different
	// activity is a new session (its own conversation). The parent's explicit
	// Resume answer overrides.
	prevDir := currentActivityDir()
	var newSession bool
	if req.Resume != nil {
		newSession = !*req.Resume
	} else {
		newSession = dir != prevDir
	}
	saveCurrentActivity(dir)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"dir":         act.Dir,
		"title":       act.Title,
		"guide_note":  act.GuideNote,
		"new_session": newSession,
	})
}
