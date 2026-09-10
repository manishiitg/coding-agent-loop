package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
)

func (api *StreamingAPI) handleBrowserRecording(w http.ResponseWriter, r *http.Request) {
	session := mux.Vars(r)["session"]
	authorized := false
	for _, item := range api.liveBrowserSessions(r) {
		if item["browser_session"] == session {
			authorized = true
			break
		}
	}
	if !authorized {
		http.Error(w, "Browser session not found", 404)
		return
	}
	if strings.HasPrefix(session, "pw-") {
		http.Error(w, "Playwright recordings are managed by the test runner", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Action string `json:"action"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&request) != nil || (request.Action != "start" && request.Action != "stop" && request.Action != "status") {
		http.Error(w, "Invalid recording action", 400)
		return
	}
	workspace := r.URL.Query().Get("workspace_path")
	if request.Action != "status" {
		level, manifest := workflowAccessForWorkspacePath(r.Context(), GetUserFromContext(r.Context()), workspace)
		if !currentUserCanWriteWorkflows(r) || manifest == nil || (level != WorkflowAccessOwner && level != WorkflowAccessWrite) {
			http.Error(w, "Workflow write access required", 403)
			return
		}
	}
	endpoint := strings.TrimRight(os.Getenv("WORKSPACE_API_URL"), "/")
	if endpoint == "" {
		endpoint = "http://127.0.0.1:8081"
	}
	payload, _ := json.Marshal(map[string]string{"action": request.Action, "workspace_path": workspace})
	upstream, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint+"/api/browser/live/"+session+"/recording", bytes.NewReader(payload))
	if err != nil {
		http.Error(w, "Workspace unavailable", 502)
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	upstream.Header.Set("X-Workspace-Token", os.Getenv("WORKSPACE_API_TOKEN"))
	response, err := (&http.Client{Timeout: 100 * time.Second}).Do(upstream)
	if err != nil {
		http.Error(w, "Recording request could not be completed; check recording status before retrying", 502)
		return
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.StatusCode)
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	var state struct {
		Recording bool `json:"recording"`
	}
	if response.StatusCode == http.StatusOK && json.Unmarshal(body, &state) == nil && state.Recording {
		browser.GetSessionTracker().TouchExisting(session)
	}
	_, _ = w.Write(body)
}
