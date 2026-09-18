package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// Browser discovery is scoped to both the signed-in session owner and workflow.
// Never expose the host-wide dashboard, which includes unrelated workflows.
func (api *StreamingAPI) liveBrowserSessions(r *http.Request) []map[string]string {
	result := []map[string]string{}
	workspace := strings.TrimRight(strings.TrimSpace(r.URL.Query().Get("workspace_path")), "/")
	if workspace == "" {
		return result
	}
	result = api.playwrightSessions(GetUserIDFromContext(r.Context()), workspace)
	for _, item := range browser.GetSessionTracker().ActiveSessions() {
		if browser.IsUserBrowserSession(item["browser_session"]) {
			userID := GetUserIDFromContext(r.Context())
			expected := common.PrefixBrowserSessionID(common.WorkflowBrowserSessionNamespace(userID, "", workspace) + "--browser")
			userWorkspaceSession := common.PrefixBrowserSessionID(common.UserWorkspaceBrowserSessionNamespace(userID, "", workspace) + "--browser")
			productSession := common.PrefixBrowserSessionID(common.BrowserSessionNamespace(userID, "") + "--browser")
			if userID != "" && (item["browser_session"] == expected || item["browser_session"] == userWorkspaceSession || item["browser_session"] == productSession) {
				level, manifest := workflowAccessForWorkspacePath(r.Context(), GetUserFromContext(r.Context()), workspace)
				if manifest != nil {
					// A real Workflow/ folder: its own capabilities.browser_mode
					// setting can disable browser access even though a session
					// exists, so it gates visibility here too.
					item["label"] = "Workflow browser"
					if item["browser_session"] == expected && level != WorkflowAccessNone && (manifest.Capabilities.BrowserMode == "auto" || manifest.Capabilities.BrowserMode == "headless") {
						result = append(result, item)
					}
				} else if !IsMultiUserMode() || api.userOwnsActiveSessionAtWorkspace(userID, workspace) {
					// Product chats bind with BindSessionBrowserIsolation (user
					// scope), rather than the workflow-path namespace. Accept
					// that identity only here, never in a real workflow above.
					// No workflow manifest at this path: a fixed-workspace
					// product session (SparkQuill, Dominion, ...), not a
					// Workflow/ folder -- there is no browser_mode toggle to
					// check. WorkflowBrowserSessionNamespace hashes the
					// workspace path ONLY when it's non-empty (deliberately,
					// so a real Workflow's authorized users share one
					// browser) -- it does NOT fold in userID the way it does
					// for the empty-path case, so the identity match above is
					// NOT itself proof this user owns this workspace path.
					// Outside multi-user mode there is exactly one account on
					// the whole deployment (every fixed-workspace product
					// today), so that's moot; in multi-user mode, require the
					// same ownership check the non-shared branch below already
					// trusts before exposing anything.
					item["label"] = "Persistent browser"
					result = append(result, item)
				}
			}
			continue
		}
		id := item["workflow_session"]
		api.activeSessionsMux.RLock()
		owner := api.activeSessions[id]
		matches := owner != nil && strings.TrimRight(owner.WorkspacePath, "/") == workspace && owner.UserID == GetUserIDFromContext(r.Context())
		api.activeSessionsMux.RUnlock()
		if matches {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i]["browser_session"] < result[j]["browser_session"] })
	return result
}

// userOwnsActiveSessionAtWorkspace reports whether userID owns a currently
// tracked chat session whose workspace matches workspace, the same ownership
// signal the non-shared discovery branch above already relies on.
func (api *StreamingAPI) userOwnsActiveSessionAtWorkspace(userID, workspace string) bool {
	if userID == "" {
		return false
	}
	api.activeSessionsMux.RLock()
	defer api.activeSessionsMux.RUnlock()
	for _, owner := range api.activeSessions {
		if owner != nil && strings.TrimRight(owner.WorkspacePath, "/") == workspace && owner.UserID == userID {
			return true
		}
	}
	return false
}

// canControlLiveBrowser decides whether claims may take manual control of the
// live browser at workspace. Split out from handleLiveBrowserStream's closure
// so it's directly unit-testable without the websocket harness.
func (api *StreamingAPI) canControlLiveBrowser(ctx context.Context, claims *UserClaims, workspace string) bool {
	level, manifest := workflowAccessForWorkspacePath(ctx, claims, workspace)
	if manifest != nil {
		return level == WorkflowAccessOwner || level == WorkflowAccessWrite
	}
	// No workflow.json at this path: a fixed-workspace product session
	// (SparkQuill, Dominion, ...), not a Workflow/ folder. Mirror
	// liveBrowserSessions' same fallback -- without it, nobody could ever
	// take control of one of these live browsers, since manifest is always
	// nil for them.
	userID := ""
	if claims != nil {
		userID = claims.UserID
	}
	return !IsMultiUserMode() || api.userOwnsActiveSessionAtWorkspace(userID, workspace)
}

func (api *StreamingAPI) handleLiveBrowserSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"sessions": api.liveBrowserSessions(r)})
}

var liveBrowserTabRef = regexp.MustCompile(`^(t[0-9]+|[0-9]+)$`)

func (api *StreamingAPI) handleLiveBrowserStream(w http.ResponseWriter, r *http.Request) {
	session := mux.Vars(r)["session"]
	authorized := func() bool {
		for _, item := range api.liveBrowserSessions(r) {
			if item["browser_session"] == session {
				return true
			}
		}
		return false
	}
	if !authorized() {
		http.Error(w, "Browser session not found", http.StatusNotFound)
		return
	}
	if !api.checkLiveAttachOrigin(r) {
		http.Error(w, "Origin not allowed", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(session, "pw-") {
		api.handlePlaywrightViewer(w, r, session)
		return
	}
	workspaceURL := strings.TrimRight(os.Getenv("WORKSPACE_API_URL"), "/")
	if workspaceURL == "" {
		workspaceURL = "http://127.0.0.1:8081"
	}
	target, err := url.Parse(workspaceURL)
	if err != nil {
		http.Error(w, "Workspace unavailable", http.StatusBadGateway)
		return
	}
	target.Scheme = strings.Replace(target.Scheme, "http", "ws", 1)
	target.Path += "/api/browser/live/" + url.PathEscape(session) + "/stream"
	target.RawQuery = ""
	headers := http.Header{}
	headers.Set("X-Workspace-Token", os.Getenv("WORKSPACE_API_TOKEN"))
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	upstream, response, err := dialer.DialContext(r.Context(), target.String(), headers)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		http.Error(w, "Live browser unavailable; check the server agent-browser version and stream", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	_ = upstream.WriteJSON(map[string]interface{}{"type": "config", "maxFps": 10})
	upgrader := api.liveAttachUpgrader()
	viewer, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer viewer.Close()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Only this connection can hold control. Disconnect/heartbeat timeout always
	// releases waiting browser tool calls. Watchers cannot inject any raw commands.
	var releaseControl func()
	defer func() {
		if releaseControl != nil {
			releaseControl()
		}
	}()
	var writeMu sync.Mutex
	send := func(value interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		viewer.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return viewer.WriteJSON(value)
	}
	sendError := func(message string) { _ = send(map[string]interface{}{"type": "viewer_error", "message": message}) }
	upstream.SetReadLimit(8 << 20)
	viewer.SetReadLimit(16 << 10)
	_ = viewer.SetReadDeadline(time.Now().Add(45 * time.Second))
	go func() {
		defer cancel()
		defer viewer.Close()
		for {
			_, data, err := upstream.ReadMessage()
			if err != nil {
				return
			}
			// Only viewport/status/tab updates are needed. Don't leak console, storage,
			// network payloads or command outputs through this viewer.
			var message struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(data, &message) != nil {
				continue
			}
			switch message.Type {
			case "frame", "status", "tabs", "url":
			default:
				continue
			}
			writeMu.Lock()
			viewer.SetWriteDeadline(time.Now().Add(10 * time.Second))
			err = viewer.WriteMessage(websocket.TextMessage, data)
			writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	canControl := func() bool {
		if !currentUserCanWriteWorkflows(r) {
			return false
		}
		workspace := strings.TrimRight(strings.TrimSpace(r.URL.Query().Get("workspace_path")), "/")
		return api.canControlLiveBrowser(ctx, GetUserFromContext(r.Context()), workspace)
	}
	_ = send(map[string]interface{}{"type": "viewer_control", "controlling": false})
	for {
		_, data, err := viewer.ReadMessage()
		if err != nil {
			return
		}
		if !authorized() {
			return
		}
		var message struct {
			Type   string `json:"type"`
			Tab    string `json:"tab"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		}
		if json.Unmarshal(data, &message) != nil {
			continue
		}
		_ = viewer.SetReadDeadline(time.Now().Add(45 * time.Second))
		switch message.Type {
		case "ping":
			if releaseControl != nil {
				browser.GetSessionTracker().TouchExisting(session)
			}
			_ = send(map[string]string{"type": "pong"})
		case "take_control":
			if releaseControl == nil {
				if !canControl() {
					sendError("You do not have permission to control this workflow browser.")
					continue
				}
				var ok bool
				releaseControl, ok = browser.TryTakeBrowserControl(session)
				if !ok {
					sendError("The browser is busy. Wait for the current action to finish, then take control.")
					continue
				}
			}
			_ = send(map[string]interface{}{"type": "viewer_control", "controlling": true})
		case "release_control":
			if releaseControl != nil {
				releaseControl()
				releaseControl = nil
			}
			_ = send(map[string]interface{}{"type": "viewer_control", "controlling": false})
		case "resize_viewport":
			if releaseControl == nil || !canControl() {
				continue
			}
			// Bound rendering cost and accept only real viewport dimensions.
			if message.Width < 320 || message.Width > 1920 || message.Height < 320 || message.Height > 1920 {
				sendError("Page dimensions must be between 320 and 1920 pixels.")
				continue
			}
			_, err := browser.NewClient(workspaceURL).ExecuteCommand(ctx, append(browser.HeadlessLaunchArgsForSession(session), "--session", session, "set", "viewport", fmt.Sprint(message.Width), fmt.Sprint(message.Height), "--json"), &browser.ExecuteOptions{Timeout: 10 * time.Second})
			if err != nil {
				sendError("Unable to resize the browser page.")
			}
		case "switch_tab":
			if releaseControl == nil || !liveBrowserTabRef.MatchString(message.Tab) {
				continue
			}
			_, err := browser.NewClient(workspaceURL).ExecuteCommand(ctx, append(browser.HeadlessLaunchArgsForSession(session), "--session", session, "tab", message.Tab, "--json"), &browser.ExecuteOptions{Timeout: 10 * time.Second})
			if err != nil {
				sendError("Unable to switch browser tab.")
			}
		case "input_mouse", "input_keyboard", "input_touch":
			if releaseControl == nil {
				continue
			}
			upstream.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := upstream.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		}
	}
}
