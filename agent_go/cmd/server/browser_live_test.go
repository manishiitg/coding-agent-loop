package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	workspacehandlers "github.com/manishiitg/coding-agent-loop/workspace/handlers"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
)

func TestLiveBrowserSessionIsolation(t *testing.T) {
	tracker := browser.GetSessionTracker()
	tracker.Touch("live-own", "run-own", "run-own")
	tracker.Touch("live-other", "run-other", "run-other")
	defer tracker.Remove("live-own")
	defer tracker.Remove("live-other")
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{
		"run-own":   {UserID: "alice", WorkspacePath: "Workflow/one"},
		"run-other": {UserID: "bob", WorkspacePath: "Workflow/one"},
	}}
	request := httptest.NewRequest("GET", "/?workspace_path=Workflow/one", nil)
	request = request.WithContext(context.WithValue(request.Context(), UserContextKey, &UserClaims{UserID: "alice"}))
	items := api.liveBrowserSessions(request)
	if len(items) != 1 || items[0]["browser_session"] != "live-own" {
		t.Fatalf("wrong sessions: %v", items)
	}
	request.URL.RawQuery = "workspace_path=Workflow/two"
	if len(api.liveBrowserSessions(request)) != 0 {
		t.Fatal("cross-workflow leak")
	}
	request.URL.RawQuery = ""
	if len(api.liveBrowserSessions(request)) != 0 {
		t.Fatal("unscoped sessions exposed")
	}
}

func TestLiveBrowserStreamWatchControlAndDisconnect(t *testing.T) {
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")
	t.Setenv("MULTI_USER_MODE", "false")
	const session = "live-stream-test"
	browser.GetSessionTracker().Touch(session, "run", "run")
	defer browser.GetSessionTracker().Remove(session)
	received := make(chan map[string]interface{}, 8)
	workspace := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/documents/") {
			json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "data": map[string]string{"content": `{"version":"1","id":"test","label":"Test"}`}})
			return
		}
		if r.Header.Get("X-Workspace-Token") != "test-service-token" {
			t.Error("missing service auth")
			http.Error(w, "auth", 401)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.WriteJSON(map[string]interface{}{"type": "frame", "data": "test-frame"})
		for {
			var message map[string]interface{}
			if conn.ReadJSON(&message) != nil {
				return
			}
			if message["type"] != "config" {
				received <- message
			}
		}
	}))
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	t.Setenv("WORKSPACE_API_TOKEN", "test-service-token")
	api := &StreamingAPI{activeSessions: map[string]*ActiveSessionInfo{"run": {UserID: "alice", WorkspacePath: "Workflow/one"}}}
	router := mux.NewRouter()
	router.HandleFunc("/api/browser/live/{session}/stream", api.handleLiveBrowserStream)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		router.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserContextKey, &UserClaims{UserID: "alice"})))
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http", "ws", 1)+"/api/browser/live/"+session+"/stream?workspace_path=Workflow/one", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	readType := func(kind string) map[string]interface{} {
		t.Helper()
		for {
			var message map[string]interface{}
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message["type"] == "viewer_error" {
				t.Fatalf("viewer error: %v", message)
			}
			if message["type"] == kind {
				return message
			}
		}
	}
	readType("viewer_control")
	conn.WriteJSON(map[string]interface{}{"type": "input_mouse", "eventType": "mousePressed", "x": 10, "y": 10})
	conn.WriteJSON(map[string]string{"type": "ping"})
	readType("pong")
	select {
	case value := <-received:
		t.Fatalf("watch mode forwarded input %v", value)
	default:
	}
	conn.WriteJSON(map[string]string{"type": "take_control"})
	if readType("viewer_control")["controlling"] != true {
		t.Fatal("control not acquired")
	}
	if release, ok := browser.TryTakeBrowserControl(session); ok {
		release()
		t.Fatal("control gate not held")
	}
	conn.WriteJSON(map[string]interface{}{"type": "input_keyboard", "eventType": "char", "text": "hello"})
	select {
	case value := <-received:
		if value["text"] != "hello" {
			t.Fatal(value)
		}
	case <-time.After(time.Second):
		t.Fatal("control input not forwarded")
	}
	conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := browser.AcquireBrowserAutomation(ctx, session)
	if err != nil {
		t.Fatal("disconnect left browser blocked", err)
	}
	release()
}

func TestLiveBrowserCannotBypassWorkflowProxy(t *testing.T) {
	response := httptest.NewRecorder()
	workspaceProxyHandler().ServeHTTP(response, httptest.NewRequest("GET", "/api/wp/api/browser/live/other/stream", nil))
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code)
	}
}

// Opt-in integration check against the installed CLI and real headless Chrome.
func TestLiveBrowserRealHeadless(t *testing.T) {
	if os.Getenv("RUN_LIVE_BROWSER_E2E") != "1" {
		t.Skip("set RUN_LIVE_BROWSER_E2E=1")
	}
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")
	name := fmt.Sprintf("aw-live-e2e-%d", time.Now().UnixNano())
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, "agent-browser", append([]string{"--session", name}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("agent-browser failed: %v %s", err, output)
		}
		return string(output)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "agent-browser", "--session", name, "close").Run()
	}()
	run("open", "data:text/html,<html><body><h1>Live browser test</h1><input aria-label='Test input' style='position:absolute;left:20px;top:100px;width:200px;height:40px'></body></html>")
	router := gin.New()
	router.GET("/api/browser/live/:session/stream", workspacehandlers.BrowserLiveStream)
	workspace := httptest.NewServer(router)
	defer workspace.Close()
	streamURL := strings.Replace(workspace.URL, "http", "ws", 1) + "/api/browser/live/" + name + "/stream"

	conn, _, err := websocket.DefaultDialer.Dial(streamURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	gotFrame, gotTabs := false, false
	for !gotFrame || !gotTabs {
		var message map[string]interface{}
		if err := conn.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message["type"] == "frame" {
			gotFrame = len(message["data"].(string)) > 100
		}
		if message["type"] == "tabs" {
			gotTabs = len(message["tabs"].([]interface{})) > 0
		}
	}
	for _, eventType := range []string{"mousePressed", "mouseReleased"} {
		if err := conn.WriteJSON(map[string]interface{}{"type": "input_mouse", "eventType": eventType, "x": 30, "y": 110, "button": "left", "clickCount": 1}); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.WriteJSON(map[string]string{"type": "input_keyboard", "eventType": "keyDown", "key": "a", "code": "KeyA", "text": "a"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		result := run("eval", "document.querySelector('input').value", "--json")
		if strings.Contains(result, `"result":"a"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("input did not reach headless browser: %s", result)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
