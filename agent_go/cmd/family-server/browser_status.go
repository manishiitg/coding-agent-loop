package main

import (
	"net/http"
	"os/exec"
)

// GET /api/browser/status — a lightweight setup check for the Connectors
// UI's "Browser" section: is the agent-browser CLI installed at all? There's
// no simple external way to ask agent-browser "is a CDP Chrome currently
// reachable" (it decides that internally per-call via --auto-connect), so
// this deliberately only reports the one-time setup step, not live
// connectivity — the panel's own copy guides the parent through the rest
// (launching a CDP-enabled Chrome and signing into the school portal there).
func handleBrowserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	_, err := exec.LookPath("agent-browser")
	writeJSON(w, http.StatusOK, map[string]bool{"cli_installed": err == nil})
}
