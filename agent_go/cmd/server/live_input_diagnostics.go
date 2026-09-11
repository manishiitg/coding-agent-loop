package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/terminals"
)

const (
	liveInputDiagnosticCaptureTimeout = 2 * time.Second
	liveInputDiagnosticMaxLines       = 80
	liveInputDiagnosticMaxRunes       = 12_000
)

type liveInputTerminalDiagnostic struct {
	CapturedAt  string `json:"captured_at"`
	Provider    string `json:"provider,omitempty"`
	TmuxSession string `json:"tmux_session"`
	Content     string `json:"content"`
}

type liveInputUnavailableResponse struct {
	Error            string                       `json:"error"`
	Message          string                       `json:"message"`
	Provider         string                       `json:"provider,omitempty"`
	SessionID        string                       `json:"session_id"`
	TechnicalDetails string                       `json:"technical_details"`
	TerminalSnapshot *liveInputTerminalDiagnostic `json:"terminal_snapshot,omitempty"`
}

// writeLiveInputUnavailable preserves the normal user-facing 409 while adding
// a support-ready snapshot of the exact retained pane that rejected the input.
// The caller has already authorized access to sessionID; no cross-session pane
// lookup is performed here.
func (api *StreamingAPI) writeLiveInputUnavailable(w http.ResponseWriter, sessionID, provider, reason string) {
	reason = terminals.RedactSensitiveTerminalText(strings.TrimSpace(reason))
	if reason == "" {
		reason = "Live input was not accepted by the coding CLI"
	}
	message := reason
	if !strings.HasPrefix(strings.ToLower(message), "live input") {
		message = "Live input unavailable: " + message
	}

	response := liveInputUnavailableResponse{
		Error:     "live_input_unavailable",
		Message:   message,
		Provider:  strings.TrimSpace(provider),
		SessionID: strings.TrimSpace(sessionID),
	}
	detailLines := []string{
		"Request failed with status code 409",
		message,
	}

	if snapshot, ok := api.liveMainCodingTmuxSnapshot(sessionID); ok {
		if response.Provider == "" {
			response.Provider = retainedCodingAgentProvider(snapshot)
		}
		capturedAt := time.Now().UTC()
		captureCtx, cancel := context.WithTimeout(context.Background(), liveInputDiagnosticCaptureTimeout)
		content, err := runTerminalTmuxOutputCommand(captureCtx, "capture-pane", "-p", "-J", "-t", snapshot.TmuxSession)
		cancel()
		if err != nil {
			detailLines = append(detailLines,
				fmt.Sprintf("Provider: %s", liveInputDiagnosticProviderLabel(response.Provider)),
				fmt.Sprintf("Session: %s", response.SessionID),
				fmt.Sprintf("tmux: %s", snapshot.TmuxSession),
				"Terminal snapshot unavailable: "+terminals.RedactSensitiveTerminalText(err.Error()),
			)
		} else if content = boundedRedactedLiveInputSnapshot(content); content != "" {
			response.TerminalSnapshot = &liveInputTerminalDiagnostic{
				CapturedAt:  capturedAt.Format(time.RFC3339),
				Provider:    response.Provider,
				TmuxSession: snapshot.TmuxSession,
				Content:     content,
			}
			detailLines = append(detailLines,
				fmt.Sprintf("Provider: %s", liveInputDiagnosticProviderLabel(response.Provider)),
				fmt.Sprintf("Session: %s", response.SessionID),
				fmt.Sprintf("Captured: %s", capturedAt.Format(time.RFC3339)),
				fmt.Sprintf("tmux: %s", snapshot.TmuxSession),
				"",
				"Terminal snapshot (visible pane, sanitized):",
				content,
			)
		} else {
			detailLines = append(detailLines,
				fmt.Sprintf("Provider: %s", liveInputDiagnosticProviderLabel(response.Provider)),
				fmt.Sprintf("Session: %s", response.SessionID),
				fmt.Sprintf("tmux: %s", snapshot.TmuxSession),
				"Terminal snapshot unavailable: the visible pane was empty.",
			)
		}
	} else {
		detailLines = append(detailLines,
			fmt.Sprintf("Provider: %s", liveInputDiagnosticProviderLabel(response.Provider)),
			fmt.Sprintf("Session: %s", response.SessionID),
			"Terminal snapshot unavailable: no live main coding-agent pane was found.",
		)
	}

	response.TechnicalDetails = terminals.RedactSensitiveTerminalText(strings.Join(detailLines, "\n"))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(response)
}

func liveInputDiagnosticProviderLabel(provider string) string {
	if provider = strings.TrimSpace(provider); provider != "" {
		return provider
	}
	return "unknown"
}

func boundedRedactedLiveInputSnapshot(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, content)
	content = terminals.RedactSensitiveTerminalText(strings.TrimSpace(content))
	if content == "" {
		return ""
	}

	truncated := false
	lines := strings.Split(content, "\n")
	if len(lines) > liveInputDiagnosticMaxLines {
		lines = lines[len(lines)-liveInputDiagnosticMaxLines:]
		truncated = true
	}
	content = strings.Join(lines, "\n")
	runes := []rune(content)
	if len(runes) > liveInputDiagnosticMaxRunes {
		content = string(runes[len(runes)-liveInputDiagnosticMaxRunes:])
		truncated = true
	}
	if truncated {
		content = "[snapshot truncated to the final visible content]\n" + content
	}
	return content
}
