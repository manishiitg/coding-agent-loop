package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/workspace/security"
)

// agentBrowserTool wires the EXISTING agent_browser bridge tool — already
// declared in mcpagent's default bridgeTools (mcpagent/agent/coding_agents_bridge.go)
// alongside execute_shell_command — to a real executor. Same reasoning as
// shellTool()/diffPatchWorkspaceFileTool(): the name is already advertised to
// every coding-agent session regardless of whether family-server registers a
// handler, so without one the model sees the tool, calls it, and gets a
// confusing "not registered for this session" error instead of it either
// working or being silently absent.
//
// Runs the real `agent-browser` CLI with --auto-connect, which is exactly
// AgentWorks' "auto" browser_mode (docs/core/browser.md): reuse a real,
// already-running Chrome reachable over CDP if one exists — so Quill can open
// a page needing a login (a parent's ChatGPT/Claude share link, a school
// portal) using the parent's own signed-in browser — otherwise fall back to a
// private, isolated headless Chromium. There is no separate mode parameter:
// the CLI itself picks, matching what AgentWorks calls "auto".
//
// Parent-only (not offered in Child Mode): browsing the open internet is a
// parent capability, mirroring how web_search/generate_image are parent tools.
//
// Sandboxed with the same workspace/security.Isolator as shellTool(), so any
// file agent-browser writes (screenshot/download/pdf) lands inside the
// workspace, never the wider machine — and reuses the SAME Chrome/agent-browser
// CLI install already required for AgentWorks (docs/core/browser.md), no new
// infra for this app specifically.
func agentBrowserTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "agent_browser",
		Description: "Control a real web browser — use it to open and read a link the parent shared that you can't just " +
			"read as plain text (a ChatGPT or Claude share link, a news article, a school portal page). Before your first " +
			"browser action in a conversation, call agent_browser(command=\"skills\", args=[\"get\", \"core\"]) once to load " +
			"the CLI's own up-to-date command guide — don't guess commands from memory. The common flow is " +
			"open a URL, then read or snapshot the page, then act on what you find. It automatically reuses a real, " +
			"already-signed-in Chrome if the parent has one running, otherwise a private headless browser — you never " +
			"choose which.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "agent-browser subcommand, e.g. open, read, snapshot, click, get, skills",
				},
				"args": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "arguments/flags for the subcommand, e.g. [\"https://example.com\"]",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			command, _ := args["command"].(string)
			command = strings.TrimSpace(command)
			if command == "" {
				return "", fmt.Errorf("command is required")
			}
			var extra []string
			if raw, ok := args["args"].([]interface{}); ok {
				for _, v := range raw {
					if s, ok := v.(string); ok {
						extra = append(extra, s)
					}
				}
			}
			parts := []string{"agent-browser", "--auto-connect", shellQuoteArg(command)}
			for _, a := range extra {
				parts = append(parts, shellQuoteArg(a))
			}
			full := strings.Join(parts, " ")

			cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			iso := &security.Isolator{
				BaseDir:    workspaceRoot(),
				WorkDir:    workspaceRoot(),
				ReadPaths:  []string{"shared", "parent", "child"},
				WritePaths: []string{"shared", "parent"},
			}
			cmd, cleanup, err := iso.ExecuteIsolated(cctx, full, nil)
			if err != nil {
				return "", fmt.Errorf("sandbox setup failed: %w", err)
			}
			defer cleanup()
			return runShellOutput(cmd)
		},
	}
}

// shellQuoteArg wraps s in single quotes for safe inclusion in the shell
// command string ExecuteIsolated runs via `sh -c`, escaping any embedded
// single quotes. Needed because, unlike execute_shell_command (where the
// model already writes a complete, correctly-quoted shell command itself),
// agent_browser's args arrive as a plain JSON string array — a URL or typed
// text could contain spaces, quotes, or shell metacharacters that must reach
// the CLI unchanged.
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
