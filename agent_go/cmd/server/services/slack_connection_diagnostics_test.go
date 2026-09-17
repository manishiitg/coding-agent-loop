package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type slackDiagnosticTransport func(*http.Request) (*http.Response, error)

func (f slackDiagnosticTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSlackConnectionDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, scopes, botError, appError string
		header                           bool
		success                          bool
		missing                          string
	}{
		{name: "granted", scopes: "app_mentions:read, chat:write", header: true, success: true},
		{name: "missing mentions", scopes: "chat:write", header: true, missing: "app_mentions:read"},
		{name: "missing replies", scopes: "app_mentions:read", header: true, missing: "chat:write"},
		{name: "no scope header", success: true},
		{name: "empty granted scopes", header: true, missing: "app_mentions:read"},
		{name: "invalid bot", botError: "invalid_auth"},
		{name: "invalid app", scopes: "app_mentions:read,chat:write", header: true, appError: "invalid_auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := http.DefaultClient
			defer func() { http.DefaultClient = old }()
			calls := 0
			http.DefaultClient = &http.Client{Transport: slackDiagnosticTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost {
					t.Fatalf("unexpected method %s", r.Method)
				}
				h := make(http.Header)
				payload := map[string]interface{}{"ok": true}
				switch r.URL.Path {
				case "/api/auth.test":
					if r.Header.Get("Authorization") != "Bearer xoxb-test-secret" {
						t.Fatal("bot token was not used for auth check")
					}
					if tc.header {
						h.Set("X-OAuth-Scopes", tc.scopes)
					}
					if tc.botError != "" {
						payload = map[string]interface{}{"ok": false, "error": tc.botError}
					}
				case "/api/apps.connections.open":
					if r.Header.Get("Authorization") != "Bearer xapp-test-secret" {
						t.Fatal("app token was not used for socket check")
					}
					if tc.appError != "" {
						payload = map[string]interface{}{"ok": false, "error": tc.appError}
					} else {
						payload["url"] = "wss://sensitive-ticket"
					}
				default:
					t.Fatalf("diagnostic must not post channel messages: %s", r.URL.Path)
				}
				raw, _ := json.Marshal(payload)
				return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
			})}
			result := (&SlackService{}).DiagnoseConnectionWithConfig(context.Background(), &SlackConfig{Enabled: true, BotToken: "xoxb-test-secret", AppToken: "xapp-test-secret"})
			if result.Success != tc.success {
				t.Fatalf("success=%v want %v: %+v", result.Success, tc.success, result)
			}
			if calls != 2 {
				t.Fatalf("calls=%d want 2, without retries", calls)
			}
			checks := map[string]SlackConnectionCheck{}
			for _, check := range result.Checks {
				checks[check.Name] = check
			}
			if tc.missing != "" && (!strings.Contains(result.Message, tc.missing) || !strings.Contains(result.Message, "reinstall")) {
				t.Fatal("summary omitted the missing permission or corrective action")
			}
			if tc.missing != "" && checks[tc.missing].Status != "missing" {
				t.Fatalf("did not identify missing scope: %+v", checks)
			}
			if !tc.header && tc.botError == "" && checks["Bot permissions"].Status != "manual" {
				t.Fatal("unknown scopes were reported as verified")
			}
			if checks["Event subscriptions"].Status != "manual" || checks["Mention delivery"].Status != "manual" {
				t.Fatal("token check incorrectly verified incoming events")
			}
			for _, instruction := range []string{"Enable Socket Mode", "Request URL empty", "Save Changes", "same Slack app"} {
				if !strings.Contains(checks["Event subscriptions"].Message, instruction) {
					t.Fatalf("event setup omitted %q", instruction)
				}
			}
			raw, _ := json.Marshal(result)
			for _, secret := range []string{"xoxb-test-secret", "xapp-test-secret", "sensitive-ticket"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("diagnostic exposed credential or socket URL")
				}
			}
		})
	}
}
