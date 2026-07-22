package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

var emailBoldRE = regexp.MustCompile(`\*\*(.+?)\*\*`)

// emailHTMLFromText renders a plain/markdown notification message into a compact,
// EMAIL-SAFE inline-styled HTML body (Gmail strips <style>/<head>/class CSS, so
// every element carries its own style — the same constraint AgentWorks documents
// for notify_user's email_html). Supports the little markdown Quill actually
// writes: a title, paragraphs, "- " bullets, and **bold**. Not a full markdown
// engine — just enough to make the Pulse/notify email read nicely instead of as
// a raw text blob.
func emailHTMLFromText(title, body string) string {
	inline := func(s string) string {
		s = html.EscapeString(s)
		return emailBoldRE.ReplaceAllString(s, `<strong>$1</strong>`)
	}
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.6;color:#2b3a55;max-width:560px;margin:0 auto">`)
	if strings.TrimSpace(title) != "" {
		b.WriteString(`<h2 style="font-size:18px;color:#1f2d45;margin:0 0 12px">` + html.EscapeString(title) + `</h2>`)
	}
	inList := false
	closeList := func() {
		if inList {
			b.WriteString(`</ul>`)
			inList = false
		}
	}
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" {
			closeList()
			continue
		}
		switch {
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* "):
			if !inList {
				b.WriteString(`<ul style="margin:8px 0;padding-left:20px">`)
				inList = true
			}
			b.WriteString(`<li style="margin:4px 0">` + inline(strings.TrimSpace(t[2:])) + `</li>`)
		case strings.HasPrefix(t, "## "):
			closeList()
			b.WriteString(`<h3 style="font-size:16px;color:#1f2d45;margin:14px 0 6px">` + inline(strings.TrimSpace(t[3:])) + `</h3>`)
		default:
			closeList()
			b.WriteString(`<p style="margin:8px 0">` + inline(t) + `</p>`)
		}
	}
	closeList()
	b.WriteString(`<p style="margin:18px 0 0;font-size:12px;color:#8a94a6">— SparkQuill</p>`)
	b.WriteString(`</div>`)
	return b.String()
}

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
	return deliverNotificationHTML(ctx, title, msg, "")
}

// deliverNotificationHTML is deliverNotification with an explicit HTML email
// body (inline-styled, as Gmail strips <style>/<head>/class CSS — the same
// contract AgentWorks' notify_user email_html uses). When htmlBody is empty the
// plain message is auto-wrapped into a simple inline-styled HTML email, so every
// notification email renders richly (not a bare plain-text blob) — this is what
// makes the Pulse digest email HTML. Desktop and WhatsApp always get plain text.
func deliverNotificationHTML(ctx context.Context, title, msg, htmlBody string) notifyResult {
	res := notifyResult{Failed: map[string]string{}}
	if strings.TrimSpace(htmlBody) == "" {
		htmlBody = emailHTMLFromText(title, msg)
	}

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
	if sent, err := gmailNotify(title, msg, htmlBody, extraTo); err != nil {
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
			"and a message. The email is always sent as formatted HTML; the plain message is used as-is for desktop/WhatsApp and " +
			"as the email's fallback. For a richer email you MAY pass email_html — but it MUST be EMAIL-SAFE: INLINE styles only " +
			"(a style attribute on each element), because Gmail strips <style>/<head> blocks and class CSS. Omit email_html to let " +
			"the message be auto-formatted. Returns a delivery status; report it honestly and do NOT claim it was sent if the " +
			"status is \"failed\" or \"no_channels\".",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":      map[string]interface{}{"type": "string", "description": "short notification title"},
				"message":    map[string]interface{}{"type": "string", "description": "the notification body (1–2 sentences), plain text — used for desktop/WhatsApp and as the email fallback"},
				"email_html": map[string]interface{}{"type": "string", "description": "OPTIONAL rich HTML email body, inline-styled only (Gmail strips <style>/<head>/class CSS). Omit to auto-format the message."},
			},
			"required": []string{"title", "message"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			title, _ := args["title"].(string)
			msg, _ := args["message"].(string)
			htmlBody, _ := args["email_html"].(string)
			title, msg, htmlBody = strings.TrimSpace(title), strings.TrimSpace(msg), strings.TrimSpace(htmlBody)
			if msg == "" {
				return "", fmt.Errorf("message is required")
			}
			if title == "" {
				title = "SparkQuill"
			}
			b, _ := json.Marshal(deliverNotificationHTML(ctx, title, msg, htmlBody))
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
