package services

import (
	"fmt"
	"regexp"
	"strings"
)

// withBotSender puts who is asking at the top of a Slack turn's message: in
// a channel many people talk to one conversation, and the agent must know
// which of them it is answering (and can reach them by email). It runs once
// per message, where the turn is built — never before command parsing,
// which reads the raw text. WhatsApp chats are the paired user's own.
func withBotSender(msg *BotIncomingMessage) {
	if msg == nil || msg.senderNoted || msg.Platform != "slack" {
		return
	}
	msg.senderNoted = true
	if line := botSenderLine(*msg); line != "" {
		msg.Text = line + "\n\n" + msg.Text
	}
}

// botDMUserID is the account a 1:1 Slack DM runs as, or "" for any other
// message (channels run as their route).
func botDMUserID(msg BotIncomingMessage, workspaceUserID string) string {
	if msg.Platform != "slack" || !msg.DirectMessage {
		return ""
	}
	return strings.TrimSpace(workspaceUserID)
}

func botSenderLine(msg BotIncomingMessage) string {
	name := strings.TrimSpace(msg.UserName)
	if name == strings.TrimSpace(msg.UserID) {
		// The connector had no display name, only the platform id.
		name = ""
	}
	email := strings.TrimSpace(msg.UserEmail)
	switch {
	case name != "" && email != "":
		return fmt.Sprintf("From: %s <%s> (Slack)", name, email)
	case email != "":
		return fmt.Sprintf("From: %s (Slack)", email)
	case name != "":
		return fmt.Sprintf("From: %s (Slack)", name)
	case strings.TrimSpace(msg.UserID) != "":
		return fmt.Sprintf("From: Slack user %s", strings.TrimSpace(msg.UserID))
	}
	return ""
}

var slackMarkupPattern = regexp.MustCompile(`<[@#!][^>]*>`)

// botSessionTitle names a Slack thread's session for the tab and the
// activity list: who started it, what they asked and where, e.g.
// "Bob: check the ACME invoice · #finance". Several threads of one bot
// otherwise all show as "Slack". Empty for other platforms.
func botSessionTitle(msg BotIncomingMessage, channelName string) string {
	if msg.Platform != "slack" {
		return ""
	}
	text := strings.Join(strings.Fields(slackMarkupPattern.ReplaceAllString(msg.Text, "")), " ")
	if runes := []rune(text); len(runes) > 48 {
		text = strings.TrimSpace(string(runes[:47])) + "…"
	}
	name := strings.TrimSpace(msg.UserName)
	if name == strings.TrimSpace(msg.UserID) {
		name = ""
	}
	title := text
	if name != "" {
		title = strings.TrimSpace(name + ": " + text)
	}
	if channel := strings.TrimPrefix(strings.TrimSpace(channelName), "#"); channel != "" {
		if title == "" {
			return "Slack · #" + channel
		}
		title += " · #" + channel
	}
	if title == "" {
		return "Slack thread"
	}
	return title
}
