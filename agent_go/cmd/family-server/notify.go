package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// notifyResult is the honest, multi-channel delivery outcome — modeled on
// AgentWorks' notify_user contract so the caller (a chat turn or Pulse) can
// report truthfully what actually reached the parent instead of assuming
// success. Status: delivered | partial | failed | no_channels.
type notifyResult struct {
	Status    string            `json:"status"`
	Delivered []string          `json:"delivered,omitempty"`
	Failed    map[string]string `json:"failed,omitempty"`
}

// deliverNotification fans a notification out to every channel available right
// now — the local desktop, WhatsApp (the paired "message yourself" chat), and
// Gmail (an email to the connected account itself). This mirrors AgentWorks'
// notify_user multi-channel fan-out, simplified for a single family: every
// channel targets the parent's own device/account, so there's no per-user
// routing. A channel that isn't set up is skipped (not a failure); a channel
// that's set up but errors is recorded as failed.
func deliverNotification(ctx context.Context, title, msg string) notifyResult {
	res := notifyResult{Failed: map[string]string{}}

	if err := sendDesktopNotification(ctx, title, msg); err != nil {
		res.Failed["desktop"] = err.Error()
	} else {
		res.Delivered = append(res.Delivered, "desktop")
	}

	if whatsAppBot.IsConnected() {
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		if err := whatsAppBot.SendToSelf(sendCtx, title+"\n\n"+msg); err != nil {
			res.Failed["whatsapp"] = err.Error()
		} else {
			res.Delivered = append(res.Delivered, "whatsapp")
		}
		cancel()
	}

	stateMu.Lock()
	extraTo := loadState().Pulse.NotifyEmails
	stateMu.Unlock()
	if sent, err := gmailNotify(title, msg, extraTo); err != nil {
		res.Failed["gmail"] = err.Error()
	} else if sent {
		res.Delivered = append(res.Delivered, "gmail")
	}

	switch {
	case len(res.Delivered) == 0 && len(res.Failed) == 0:
		res.Status = "no_channels"
	case len(res.Delivered) == 0:
		res.Status = "failed"
	case len(res.Failed) > 0:
		res.Status = "partial"
	default:
		res.Status = "delivered"
	}
	if len(res.Failed) == 0 {
		res.Failed = nil
	}
	return res
}

// notifyTool sends a notification to the parent across every channel that's
// set up — desktop, WhatsApp, and Gmail (see deliverNotification). It returns
// an honest delivery status; the agent must report it truthfully and not
// claim a send that didn't happen.
func notifyTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "notify_user",
		Description: "Send a short notification to the parent. It goes to every channel they've set up — a desktop " +
			"notification, WhatsApp (if linked), and email (if Gmail is connected) — you do not choose which. Provide a title " +
			"and a message. It returns a delivery status; report it honestly and do NOT claim it was sent if the status is " +
			"\"failed\" or \"no_channels\".",
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
			b, _ := json.Marshal(deliverNotification(ctx, title, msg))
			return string(b), nil
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
