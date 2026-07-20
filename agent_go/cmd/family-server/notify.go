package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// notifyTool sends a notification to the parent. Zero-config default = a desktop
// notification (macOS osascript / Linux notify-send). Email or WhatsApp channels
// can be layered on later via notification config, without changing the tool the
// agent calls.
func notifyTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "notify_user",
		Description: "Send a short notification to the parent (e.g. the child finished a session, a test or report is ready, a backup completed, or a decision is needed). Shows as a desktop notification. Provide a title and a message.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":   map[string]interface{}{"type": "string", "description": "short notification title"},
				"message": map[string]interface{}{"type": "string", "description": "the notification body (1–2 sentences)"},
			},
			"required": []string{"title", "message"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			title, _ := args["title"].(string)
			msg, _ := args["message"].(string)
			title, msg = strings.TrimSpace(title), strings.TrimSpace(msg)
			if msg == "" {
				return "", fmt.Errorf("message is required")
			}
			if title == "" {
				title = "SparkQuill"
			}
			if err := sendDesktopNotification(ctx, title, msg); err != nil {
				return fmt.Sprintf(`{"status":"logged","channel":"none","note":%q}`, err.Error()), nil
			}
			return `{"status":"ok","channel":"desktop"}`, nil
		},
	}
}

func sendDesktopNotification(ctx context.Context, title, msg string) error {
	switch runtime.GOOS {
	case "darwin":
		esc := func(s string) string { return strings.ReplaceAll(s, `"`, `\"`) }
		script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(msg), esc(title))
		return exec.CommandContext(ctx, "osascript", "-e", script).Run()
	case "linux":
		return exec.CommandContext(ctx, "notify-send", title, msg).Run()
	default:
		return fmt.Errorf("no desktop notifier on %s", runtime.GOOS)
	}
}
