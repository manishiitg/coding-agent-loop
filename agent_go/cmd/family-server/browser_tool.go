package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
)

// agentBrowserTool REUSES AgentWorks' pkg/browser.Executor — the entire CDP
// tab manager (cdp_tabs.go/cdp_registry.go), artifact broker, live status and
// error rewriting — instead of reimplementing any of it. The Executor talks to
// a workspace-api server; family-server runs a tiny loopback one (see
// browser_backend.go) backed by its own security.Isolator. All this file does
// is: build that Executor once, inject family-server's FolderGuard/session
// into the context on each call, and expose it as an agentsession.Tool.
//
// CDP-only, port 9222 (WithCdpPort below): the Executor connects only to the
// parent's dedicated "Chrome CDP" browser (set up via the Browser connector),
// never a headless fallback. Parent-only tool.
const browserCDPPort = 9222

var (
	browserExecutor     *browser.Executor
	browserExecutorErr  error
	browserExecutorOnce sync.Once
)

func getBrowserExecutor() (*browser.Executor, error) {
	browserExecutorOnce.Do(func() {
		url, err := startBrowserBackend()
		if err != nil {
			browserExecutorErr = err
			return
		}
		client := browser.NewClient(url)
		browserExecutor = browser.NewExecutor(client, browser.WithCdpPort(browserCDPPort))
	})
	return browserExecutor, browserExecutorErr
}

// browserFolderGuardContext injects the same sandbox intent execute_shell_command
// uses (shared/parent/child writable, plus the host Downloads folder readable)
// so the reused Executor + loopback backend sandbox agent-browser identically
// and can stage downloads/screenshots into the workspace.
func browserFolderGuardContext(ctx context.Context) context.Context {
	writePaths := []string{"shared", "parent", "child"}
	readPaths := append([]string{"skills"}, writePaths...)
	if dl := hostDownloadsPath(); dl != "" {
		readPaths = append(readPaths, dl)
	}
	ctx = context.WithValue(ctx, common.FolderGuardWritePathsKey, writePaths)
	ctx = context.WithValue(ctx, common.FolderGuardReadPathsKey, readPaths)
	// Stable session id for this single-family browser; in CDP mode the Executor
	// remaps it to a shared per-port name anyway, so it just needs to be non-empty.
	ctx = context.WithValue(ctx, common.ChatSessionIDKey, "family-browser")
	ctx = context.WithValue(ctx, common.WorkflowSessionIDKey, "family-browser")
	return ctx
}

func agentBrowserTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "agent_browser",
		Description: "Control the parent's own signed-in web browser (their dedicated Chrome CDP browser, set up via the Browser " +
			"connector). Use it to read a page the parent can't get as plain text — a school portal, a class site, a shared link. " +
			"PROTOCOL: (1) call agent_browser(command=\"status\", session=\"family\") FIRST — it returns the authorized CDP endpoint to use. " +
			"(2) Call agent_browser(command=\"skills\", args=[\"--cdp\", \"<endpoint>\", \"get\", \"core\"], session=\"family\") once to load the " +
			"CLI's command guide. (3) Every later call must start its args with [\"--cdp\", \"<endpoint>\", ...]. " +
			"The parent's browser usually has MANY tabs and the active one is often unrelated — run command=\"tab\", args=[\"--cdp\", \"<endpoint>\"] " +
			"to list them, pick the one matching what you want, switch to it, then read/snapshot. Never act on an unrelated tab. " +
			"DOWNLOADS land in the parent's Downloads folder, not the workspace — after downloading, copy the file into shared/ with " +
			"execute_shell_command before reading it. If status reports CDP isn't reachable, tell the parent to open their signed-in browser " +
			"from Connectors → Browser; don't keep retrying.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "agent-browser subcommand: status (first, no args), skills, tab, open, read, snapshot, click, fill, type, download, screenshot, etc.",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "arguments; for CDP mode every call after status must begin with [\"--cdp\", \"<endpoint>\"]",
				},
				"session": map[string]interface{}{
					"type":        "string",
					"description": "session name (use \"family\")",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			exec, err := getBrowserExecutor()
			if err != nil {
				return "", fmt.Errorf("browser backend unavailable: %w", err)
			}
			if s, _ := args["session"].(string); strings.TrimSpace(s) == "" {
				args["session"] = "family" // Executor requires a non-empty session
			}
			return exec.HandleAgentBrowser(browserFolderGuardContext(ctx), args)
		},
	}
}
