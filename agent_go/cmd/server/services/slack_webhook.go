package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const slackWebhookResponseLimit = 1024

var slackWebhookHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// ValidateSlackIncomingWebhookURL accepts only Slack's official Incoming
// Webhook endpoints. This strict allow-list prevents a workflow secret from
// turning notify_user into a general-purpose server-side HTTP request.
func ValidateSlackIncomingWebhookURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid Slack Incoming Webhook URL")
	}
	if parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("invalid Slack Incoming Webhook URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "hooks.slack.com" && host != "hooks.slack-gov.com" {
		return fmt.Errorf("Slack Incoming Webhook must use an official Slack webhook host")
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) < 4 || segments[0] != "services" {
		return fmt.Errorf("invalid Slack Incoming Webhook path")
	}
	for _, segment := range segments[1:] {
		if segment == "" || segment == "." || segment == ".." || strings.Contains(strings.ToLower(segment), "%2f") {
			return fmt.Errorf("invalid Slack Incoming Webhook path")
		}
	}
	return nil
}

// SendSlackIncomingWebhook sends a one-way workflow notification. It returns a
// synthetic non-empty message ID because Incoming Webhooks return only "ok",
// not a Slack timestamp. Errors intentionally never contain the secret URL.
func SendSlackIncomingWebhook(ctx context.Context, webhookURL, message string) (string, error) {
	if err := ValidateSlackIncomingWebhookURL(webhookURL); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]string{"text": convertMarkdownToSlackMrkdwn(message)})
	if err != nil {
		return "", fmt.Errorf("could not encode Slack webhook message")
	}

	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(sendCtx, http.MethodPost, strings.TrimSpace(webhookURL), bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("could not create Slack webhook request")
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := slackWebhookHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("Slack webhook delivery failed")
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, slackWebhookResponseLimit))
	if readErr != nil {
		return "", fmt.Errorf("Slack webhook response could not be read")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || strings.TrimSpace(string(body)) != "ok" {
		return "", fmt.Errorf("Slack webhook rejected the notification (HTTP %d)", resp.StatusCode)
	}
	return "webhook_ok", nil
}
