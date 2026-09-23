package browser

import (
	"context"
	"encoding/json"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWorkflowBrowserSurvivesWorkflowCleanupAndCaptureProtectsLifetime(t *testing.T) {
	session := common.WorkflowBrowserSessionNamespace("Workflow/one") + "--browser"
	tracker := &SessionTracker{sessions: map[string]*browserSessionInfo{}}
	tracker.Touch(session, "builder", "run")
	tracker.Touch(session, "step", "run")
	tracker.SetCapture(session, true, "run", "Workflow/one")
	tracker.CloseAllForWorkflow("run", nil)
	if tracker.Count() != 1 || !tracker.Recording(session) {
		t.Fatal("workflow completion discarded shared browser/capture")
	}
	if len(tracker.RemoveAllForAgent("step")) != 0 {
		t.Fatal("agent cleanup discarded user browser")
	}
	if got := tracker.GetOldestSession(); got != "" {
		t.Fatal("active recording eligible for eviction")
	}
	if tracker.CaptureConflict(session, "run", "Workflow/one", "click") != "" {
		t.Fatal("own run blocked")
	}
	for _, command := range []string{"close", "reset"} {
		if tracker.CaptureConflict(session, "run", "Workflow/one", command) == "" {
			t.Fatal("recording can be destroyed")
		}
	}
	if tracker.CaptureConflict(session, "other", "Workflow/one", "click") == "" {
		t.Fatal("unrelated run can contaminate footage")
	}
	tracker.SetCapture(session, false, "", "")
	if tracker.CaptureConflict(session, "other", "Workflow/two", "click") != "" {
		t.Fatal("finished recording blocks browser")
	}
}
func TestWorkflowBrowserCommandsShareGateAcrossUsersAndChatAliases(t *testing.T) {
	for _, id := range []string{"one", "two"} {
		common.BindSessionBrowserIsolationForWorkflow(id, "Workflow/research")
		defer common.ClearSessionShellConfig(id)
	}
	release, err := AcquireBrowserAutomation(context.Background(), common.ResolveBrowserSessionID("one", "main"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if other, err := AcquireBrowserAutomation(ctx, common.ResolveBrowserSessionID("two", "ai-news")); err == nil {
		other()
		t.Fatal("same workflow's commands ran concurrently")
	}
	other, err := AcquireBrowserAutomation(context.Background(), common.SessionBrowserSessionNamespace("other-chat")+"--browser")
	if err != nil {
		t.Fatal(err)
	}
	other()
}

func TestProductWorkspaceBrowserIsNotCollapsedIntoSharedBrowser(t *testing.T) {
	t.Setenv("AGENT_BROWSER_SHARED_PROFILE", "/data/browser-profile")
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")
	const owner = "crew-chat"
	common.BindSessionBrowserIsolationForProject(owner, "_users/alice/Chats/Work/projects/crew-a")
	defer common.ClearSessionShellConfig(owner)
	expected := common.ResolveBrowserSessionID(owner, "default")
	defer GetSessionTracker().Remove(expected)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ShellExecuteRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !strings.Contains(req.Command, "--session "+expected) || strings.Contains(req.Command, "--session "+SharedSessionName) {
			t.Errorf("Crew browser collapsed into shared runtime: %s", req.Command)
		}
		if !strings.Contains(req.Command, "/data/browser-profile-projects/"+expected) {
			t.Errorf("Crew browser did not receive an isolated persistent profile: %s", req.Command)
		}
		_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: `{"success":true}`, ExitCode: 0}})
	}))
	defer server.Close()

	ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, owner)
	if _, err := NewExecutor(NewClient(server.URL)).HandleAgentBrowser(ctx, map[string]interface{}{
		"session": "default", "command": "open", "args": []interface{}{"https://example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, item := range GetSessionTracker().ActiveSessions() {
		if item["browser_session"] == expected && item["last_action"] != "Opened https://example.com" {
			t.Fatalf("last action = %q", item["last_action"])
		}
	}
}

func TestDescribeBrowserActionNeverLeaksValues(t *testing.T) {
	for _, c := range []struct {
		command string
		args    []string
		want    string
	}{
		{"open", []string{"https://user:pw@example.com/login?token=abc#frag"}, "Opened https://example.com/login"},
		{"click", []string{"@e12"}, `Clicked "@e12"`},
		{"fill", []string{"#password", "hunter2"}, `Typed into "#password"`},
		{"type", []string{"@e3", "secret text"}, `Typed into "@e3"`},
		{"select", []string{"#plan", "enterprise"}, `Selected an option in "#plan"`},
		{"eval", []string{"document.cookie"}, "Ran a script on the page"},
		{"find", []string{"role", "button", "click", "--name", "Sign in"}, `Used "Sign in"`},
		{"press", []string{"Enter"}, "Pressed Enter"},
		{"snapshot", []string{"-i"}, ""},
		{"get", []string{"text", "@e1"}, ""},
	} {
		if got := describeBrowserAction(c.command, c.args); got != c.want {
			t.Errorf("%s %v = %q, want %q", c.command, c.args, got, c.want)
		}
	}
	tracker := &SessionTracker{sessions: map[string]*browserSessionInfo{}}
	tracker.Touch("s", "a", "w")
	tracker.RecordAction("s", "click", []string{"@e1"})
	tracker.RecordAction("s", "snapshot", nil)
	if item := tracker.ActiveSessions()[0]; item["last_action"] != `Clicked "@e1"` || item["last_action_at"] == "" {
		t.Fatalf("read-only command replaced the last action: %v", item)
	}
}

func TestWorkflowBrowserExecutorSharesBuilderWorkflowAndCapture(t *testing.T) {
	t.Setenv("AGENT_BROWSER_SHARED_PROFILE", "/data/browser-profile")
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")
	const parent, child = "routing-builder", "routing-step"
	common.BindSessionBrowserIsolationForWorkflow(parent, "Workflow/demo")
	common.SetSessionBrowserNamespace(child, common.GetSessionShellConfig(parent).BrowserSessionNamespace)
	common.SetSessionBrowserSessionID(child, "old-workflow-specific-browser")
	for _, id := range []string{parent, child} {
		common.SetSessionWorkingDir(id, "Workflow/demo")
		common.SetSessionWorkflowPath(id, "Workflow/demo")
		common.SetSessionFolderGuard(id, []string{"Workflow/demo"}, []string{"Workflow/demo"})
		defer common.ClearSessionShellConfig(id)
	}
	expected := common.ResolveBrowserSessionID(parent, "main")
	defer GetSessionTracker().Remove(expected)
	opens, captures := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/execute" {
			opens++
			var req ShellExecuteRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if !strings.Contains(req.Command, "--session "+expected) || !strings.Contains(req.Command, "/data/browser-profile-workflows/"+expected) {
				t.Errorf("wrong browser/profile: %s", req.Command)
			}
			_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: ShellExecuteResponse{Stdout: `{"success":true}`, ExitCode: 0}})
		} else {
			captures++
			if r.URL.Path != "/api/browser/live/"+expected+"/recording" {
				t.Errorf("capture diverged: %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"recording":false}`))
		}
	}))
	defer server.Close()
	executor := NewExecutor(NewClient(server.URL))
	for i, id := range []string{parent, child} {
		ctx := context.WithValue(context.Background(), common.ChatSessionIDKey, id)
		ctx = context.WithValue(ctx, common.WorkflowSessionIDKey, parent)
		label := []string{"main", "ai-news"}[i]
		for _, cmd := range []string{"open", "capture"} {
			args := []string{"https://example.com"}
			if cmd == "capture" {
				args = []string{"status"}
			}
			if _, err := executor.HandleAgentBrowser(ctx, map[string]interface{}{"command": cmd, "session": label, "args": args}); err != nil {
				t.Fatalf("%s/%s: %v", id, cmd, err)
			}
		}
	}
	if opens != 2 || captures != 2 {
		t.Fatalf("unexpected calls: %d/%d", opens, captures)
	}
}
