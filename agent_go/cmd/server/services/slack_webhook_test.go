package services

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type slackWebhookRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn slackWebhookRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestValidateSlackIncomingWebhookURL(t *testing.T) {
	valid := []string{
		"https://hooks.slack.com/services/T123/B456/secret",
		"https://hooks.slack-gov.com/services/T123/B456/secret",
	}
	for _, raw := range valid {
		if err := ValidateSlackIncomingWebhookURL(raw); err != nil {
			t.Fatalf("expected valid URL %q: %v", raw, err)
		}
	}

	invalid := []string{
		"http://hooks.slack.com/services/T123/B456/secret",
		"https://example.com/services/T123/B456/secret",
		"https://hooks.slack.com.evil.test/services/T123/B456/secret",
		"https://hooks.slack.com:8443/services/T123/B456/secret",
		"https://hooks.slack.com/services/T123/B456",
		"https://hooks.slack.com/services/T123/B456/secret?redirect=https://example.com",
	}
	for _, raw := range invalid {
		if err := ValidateSlackIncomingWebhookURL(raw); err == nil {
			t.Fatalf("expected invalid URL %q", raw)
		}
	}
}

func TestSendSlackIncomingWebhook(t *testing.T) {
	original := slackWebhookHTTPClient
	t.Cleanup(func() { slackWebhookHTTPClient = original })

	var gotBody string
	slackWebhookHTTPClient = &http.Client{Transport: slackWebhookRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		if req.URL.String() != "https://hooks.slack.com/services/T123/B456/secret" {
			t.Fatalf("unexpected request URL")
		}
		if contentType := req.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("content type = %q", contentType)
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotBody = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     make(http.Header),
		}, nil
	})}

	msgID, err := SendSlackIncomingWebhook(context.Background(), "https://hooks.slack.com/services/T123/B456/secret", "**Done**")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if msgID != "webhook_ok" {
		t.Fatalf("message ID = %q", msgID)
	}
	if gotBody != `{"text":"*Done*"}` {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestSendSlackIncomingWebhookDoesNotLeakSecretURL(t *testing.T) {
	raw := "https://hooks.slack.com/services/T123/B456/super-secret-token"
	original := slackWebhookHTTPClient
	t.Cleanup(func() { slackWebhookHTTPClient = original })
	slackWebhookHTTPClient = &http.Client{Transport: slackWebhookRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader("invalid_token")),
			Header:     make(http.Header),
		}, nil
	})}

	_, err := SendSlackIncomingWebhook(context.Background(), raw, "test")
	if err == nil {
		t.Fatal("expected delivery error")
	}
	if strings.Contains(err.Error(), raw) || strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("error leaked webhook URL: %v", err)
	}
}
