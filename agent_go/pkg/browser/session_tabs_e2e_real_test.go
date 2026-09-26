package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	workspacehandlers "github.com/manishiitg/coding-agent-loop/workspace/handlers"
	"github.com/spf13/viper"
)

// TestManagedSessionPerConversationTabsRealE2E drives a real managed headless
// agent-browser session shared by two conversations (a Crew's main chat and
// an `ask` conversation) through Executor -> Client -> workspace /api/execute.
// Each conversation must keep its own page while the other navigates, and one
// conversation's close must leave the other's tab working.
//
// Run with:
//
//	RUN_BROWSER_REAL_E2E=1 go test ./pkg/browser -run TestManagedSessionPerConversationTabsRealE2E -count=1 -v
func TestManagedSessionPerConversationTabsRealE2E(t *testing.T) {
	if os.Getenv("RUN_BROWSER_REAL_E2E") != "1" {
		t.Skip("set RUN_BROWSER_REAL_E2E=1 to run the live agent-browser contract")
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Fatalf("live browser E2E requires agent-browser in PATH: %v", err)
	}
	t.Setenv("NATIVE_WORKSPACE", "true")
	t.Setenv("AGENT_BROWSER_SHARED_PROFILE", "")
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")

	pages := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.Trim(r.URL.Path, "/")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, "<!doctype html><title>page-%s</title><h1>%s</h1>", name, name)
	}))
	defer pages.Close()

	previousDocsDir := viper.GetString("docs-dir")
	viper.Set("docs-dir", t.TempDir())
	t.Cleanup(func() { viper.Set("docs-dir", previousDocsDir) })
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.POST("/api/execute", workspacehandlers.ExecuteShellCommand)
	workspaceServer := httptest.NewServer(router)
	defer workspaceServer.Close()

	executor := NewExecutor(NewClient(workspaceServer.URL))
	session := fmt.Sprintf("e2e-crew-tabs-%d", time.Now().UnixNano())
	crewChat, askCall := "work:project:e2e-crew", fmt.Sprintf("product-e2e-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		killSessionRuntimeFully(session)
		GetSessionTracker().Remove(session)
	})

	run := func(owner, command string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), common.ChatSessionIDKey, owner), 90*time.Second)
		defer cancel()
		out, err := executor.HandleAgentBrowser(ctx, map[string]interface{}{"command": command, "session": session, "args": args})
		if err != nil {
			t.Fatalf("%s %s %v: %v", owner, command, args, err)
		}
		return out
	}
	title := func(owner string) string {
		out := run(owner, "get", "title")
		switch {
		case strings.Contains(out, "page-alpha"):
			return "alpha"
		case strings.Contains(out, "page-beta"):
			return "beta"
		case strings.Contains(out, "page-gamma"):
			return "gamma"
		}
		return out
	}

	run(crewChat, "open", pages.URL+"/alpha")
	run(askCall, "open", pages.URL+"/beta")
	if got := title(crewChat); got != "alpha" {
		t.Fatalf("crew chat's page moved: title=%q, want alpha", got)
	}
	if got := title(askCall); got != "beta" {
		t.Fatalf("ask conversation's page moved: title=%q, want beta", got)
	}
	// Interleave navigations: neither conversation sees the other's page.
	run(crewChat, "open", pages.URL+"/gamma")
	if got := title(askCall); got != "beta" {
		t.Fatalf("crew chat's navigation moved the ask conversation: title=%q", got)
	}
	if got := title(crewChat); got != "gamma" {
		t.Fatalf("crew chat lost its own navigation: title=%q", got)
	}

	// The ask conversation ends its browsing: only its tab closes.
	if out := run(askCall, "close"); !strings.Contains(out, "stays open") {
		t.Fatalf("ask conversation's close was not scoped to its tab: %s", out)
	}
	if got := title(crewChat); got != "gamma" {
		t.Fatalf("crew chat's tab broke after the other conversation closed: title=%q", got)
	}
	listed := run(crewChat, "tab")
	t.Logf("tabs after scoped close:\n%s", listed)

	// A second conversation opens a tab, then the crew chat's tab disappears
	// underneath it (closed outside AgentWorks): the next command gets a fresh
	// tab with a note, never the other conversation's page.
	run(askCall, "open", pages.URL+"/beta")
	crewTab := getScopedTabSelection(sessionTabScope(session), sessionTabOwnerKey(crewChat))
	closeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := executor.Client.ExecuteCommand(closeCtx, headlessTabTarget(session).command("tab", "close", crewTab), nil); err != nil {
		t.Fatalf("close crew tab directly: %v", err)
	}
	out := run(crewChat, "open", pages.URL+"/alpha")
	if !strings.Contains(out, "AGENTWORKS_BROWSER_TAB") {
		t.Fatalf("vanished tab was not reported: %s", out)
	}
	if got := title(askCall); got != "beta" {
		t.Fatalf("replacement tab borrowed the other conversation's page: title=%q", got)
	}
	if got := title(crewChat); got != "alpha" {
		t.Fatalf("crew chat's replacement tab: title=%q, want alpha", got)
	}
}
