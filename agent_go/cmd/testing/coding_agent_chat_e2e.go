package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var codingAgentChatE2EFlags struct {
	serverURL      string
	provider       string
	model          string
	sessionID      string
	selectedFolder string
	agentMode      string
	presetQueryID  string
	phaseID        string
	workshopMode   string
	timeout        time.Duration
	skipLiveSteer  bool
}

var codingAgentChatE2ECmd = &cobra.Command{
	Use:   "coding-agent-chat-e2e",
	Short: "Run real e2e tests for tmux-backed coding agent chat sessions",
	Long: `Runs a real end-to-end test through the live mcp-agent-builder-go HTTP API.

This intentionally does not call the provider adapter directly. It exercises:
1. /api/query turn startup
2. persisted session runtime capture
3. a second /api/query turn using the same chat session
4. optional /api/sessions/{session_id}/steer live input while a coding CLI is running
5. /api/sessions/{session_id}/events polling and unified completion extraction

Example:
  mcp-agent test coding-agent-chat-e2e \
    --server-url http://localhost:8000 \
    --provider gemini-cli \
    --model gemini-3.1-flash-lite \
    --selected-folder _users/default/Chats

  mcp-agent test coding-agent-chat-e2e \
    --server-url http://localhost:8000 \
    --provider cursor-cli \
    --model cursor-cli \
    --selected-folder _users/default/Chats`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), codingAgentChatE2EFlags.timeout)
		defer cancel()

		client := &codingAgentChatE2EClient{
			baseURL: strings.TrimRight(codingAgentChatE2EFlags.serverURL, "/"),
			token:   os.Getenv("MCP_API_TOKEN"),
			http:    &http.Client{Timeout: 30 * time.Second},
		}

		provider := strings.TrimSpace(codingAgentChatE2EFlags.provider)
		if provider == "" {
			provider = "gemini-cli"
		}
		model := strings.TrimSpace(codingAgentChatE2EFlags.model)
		if model == "" {
			model = defaultCodingAgentE2EModel(provider)
		}
		sessionID := strings.TrimSpace(codingAgentChatE2EFlags.sessionID)
		if sessionID == "" {
			sessionID = fmt.Sprintf("coding-agent-e2e-%s-%d", strings.ReplaceAll(provider, "-", ""), time.Now().UnixNano())
		}
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			_ = client.stopSession(stopCtx, sessionID)
		}()

		noteToken := "E2E_NOTE_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		fmt.Printf("Running coding agent chat e2e provider=%s model=%s session=%s\n", provider, model, sessionID)
		fmt.Printf("Selected folder: %s\n", codingAgentChatE2EFlags.selectedFolder)

		firstQuery := fmt.Sprintf("Take note of the exact token %s. Do not write it to any file, memory, or external store. Do not use tools. Reply with exactly ACK_%s and nothing else.", noteToken, noteToken)
		if err := client.runAndAssertContains(ctx, sessionID, provider, model, firstQuery, []string{"ACK_" + noteToken}); err != nil {
			return fmt.Errorf("turn 1 note canary failed: %w", err)
		}
		fmt.Println("PASS turn 1: note canary acknowledged")

		secondQuery := "What exact token did I ask you to take note of in the previous turn? Do not use tools. Reply with exactly that token and nothing else."
		if err := client.runAndAssertContains(ctx, sessionID, provider, model, secondQuery, []string{noteToken}); err != nil {
			return fmt.Errorf("turn 2 native multi-turn recall failed: %w", err)
		}
		fmt.Println("PASS turn 2: same chat session retained native coding-agent context")

		if !codingAgentChatE2EFlags.skipLiveSteer {
			liveToken := "LIVE_ACK_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			liveQuery := fmt.Sprintf("Use execute_shell_command once to run `sleep 8; echo READY_FOR_LIVE_INPUT`. After the tool returns, answer briefly. If a live user message arrives during this turn, include its requested acknowledgement token in your final answer.")
			queryID, err := client.startQuery(ctx, sessionID, provider, model, liveQuery)
			if err != nil {
				return fmt.Errorf("live steer setup query failed to start: %w", err)
			}
			fmt.Printf("Started live steer turn query=%s\n", queryID)

			if err := client.waitUntilCanSteer(ctx, sessionID, 45*time.Second); err != nil {
				return fmt.Errorf("live steer session never became steerable: %w", err)
			}
			if err := client.sendSteer(ctx, sessionID, fmt.Sprintf("Live follow-up: include the exact token %s in your final answer.", liveToken)); err != nil {
				return fmt.Errorf("live steer POST failed: %w", err)
			}
			fmt.Println("Sent live steer message")

			final, raw, err := client.waitForCompletion(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("live steer turn did not complete: %w", err)
			}
			if !rawContainsProvider(raw, provider) {
				return fmt.Errorf("live steer event stream did not prove requested provider %q was used", provider)
			}
			if !strings.Contains(final, liveToken) && !strings.Contains(raw, liveToken) {
				return fmt.Errorf("live steer token %q was not processed; final=%q", liveToken, final)
			}
			fmt.Println("PASS live steer: in-flight user message was processed by the coding agent")
		}

		fmt.Println("PASS coding agent chat e2e")
		return nil
	},
}

func init() {
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.serverURL, "server-url", "http://localhost:8000", "mcp-agent-builder-go server URL")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.provider, "provider", "gemini-cli", "coding CLI provider: gemini-cli, codex-cli, cursor-cli, or claude-code")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.model, "model", "", "model ID; defaults to the provider-specific E2E model")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.sessionID, "session-id", "", "session ID to reuse; generated when omitted")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.selectedFolder, "selected-folder", "_users/default/Chats", "workspace-relative folder for the chat session")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.agentMode, "agent-mode", "simple", "agent mode to send to /api/query")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.presetQueryID, "preset-query-id", "", "workflow preset ID for workflow_phase chat")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.phaseID, "phase-id", "", "workflow phase ID for workflow_phase chat")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.workshopMode, "workshop-mode", "", "optional workflow workshop mode, for example builder or run")
	codingAgentChatE2ECmd.Flags().DurationVar(&codingAgentChatE2EFlags.timeout, "timeout", 6*time.Minute, "overall test timeout")
	codingAgentChatE2ECmd.Flags().BoolVar(&codingAgentChatE2EFlags.skipLiveSteer, "skip-live-steer", false, "skip the in-flight /steer regression test")
}

type codingAgentChatE2EClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func defaultCodingAgentE2EModel(provider string) string {
	switch provider {
	case "gemini-cli":
		return "gemini-3.1-flash-lite"
	case "codex-cli":
		return "gpt-5.3-codex-spark"
	case "cursor-cli":
		return "cursor-cli"
	case "claude-code":
		return "claude-haiku-4.5"
	default:
		return ""
	}
}

func (c *codingAgentChatE2EClient) runAndAssertContains(ctx context.Context, sessionID, provider, model, query string, required []string) error {
	if _, err := c.startQuery(ctx, sessionID, provider, model, query); err != nil {
		return err
	}
	final, raw, err := c.waitForCompletion(ctx, sessionID)
	if err != nil {
		return err
	}
	if !rawContainsProvider(raw, provider) {
		return fmt.Errorf("event stream did not prove requested provider %q was used", provider)
	}
	for _, needle := range required {
		if !strings.Contains(final, needle) {
			return fmt.Errorf("completion did not contain %q; final=%q", needle, final)
		}
	}
	return nil
}

func (c *codingAgentChatE2EClient) startQuery(ctx context.Context, sessionID, provider, model, query string) (string, error) {
	payload := map[string]interface{}{
		"query":    query,
		"provider": provider,
		"model_id": model,
		"llm_config": map[string]interface{}{
			"primary": map[string]interface{}{
				"provider": provider,
				"model_id": model,
			},
			"fallbacks": []interface{}{},
		},
		"agent_mode":      codingAgentChatE2EFlags.agentMode,
		"enabled_servers": []string{"NO_SERVERS"},
		"selected_folder": codingAgentChatE2EFlags.selectedFolder,
		"max_turns":       -1,
	}
	if codingAgentChatE2EFlags.presetQueryID != "" {
		payload["preset_query_id"] = codingAgentChatE2EFlags.presetQueryID
	}
	if codingAgentChatE2EFlags.phaseID != "" {
		payload["phase_id"] = codingAgentChatE2EFlags.phaseID
	}
	if codingAgentChatE2EFlags.workshopMode != "" {
		payload["execution_options"] = map[string]interface{}{
			"workshop_mode": codingAgentChatE2EFlags.workshopMode,
		}
	}

	var resp struct {
		QueryID   string `json:"query_id"`
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		Message   string `json:"message"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/query", sessionID, payload, &resp); err != nil {
		return "", err
	}
	if resp.Status != "started" && resp.Status != "workflow_started" {
		return "", fmt.Errorf("unexpected query status %q message=%q", resp.Status, resp.Message)
	}
	if resp.QueryID == "" {
		return "", fmt.Errorf("server returned empty query_id")
	}
	return resp.QueryID, nil
}

func (c *codingAgentChatE2EClient) waitUntilCanSteer(ctx context.Context, sessionID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, _, err := c.getEvents(ctx, sessionID)
		if err != nil {
			return err
		}
		if resp.CanSteer {
			return nil
		}
		if resp.SessionStatus == "completed" || resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session reached %s before it became steerable", resp.SessionStatus)
		}
		if err := sleepContext(ctx, 750*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s", timeout)
}

func (c *codingAgentChatE2EClient) waitForCompletion(ctx context.Context, sessionID string) (string, string, error) {
	var rawBuilder strings.Builder
	var finalResult string
	deadline := time.Now().Add(codingAgentChatE2EFlags.timeout)
	for time.Now().Before(deadline) {
		resp, raw, err := c.getEvents(ctx, sessionID)
		if err != nil {
			return "", rawBuilder.String(), err
		}
		rawBuilder.WriteString(raw)
		if extracted := extractUnifiedCompletionFinal(resp.Events); extracted != "" {
			finalResult = extracted
		}
		switch resp.SessionStatus {
		case "completed":
			return finalResult, rawBuilder.String(), nil
		case "error", "stopped":
			return finalResult, rawBuilder.String(), fmt.Errorf("session ended with status %s; final=%q", resp.SessionStatus, finalResult)
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return "", rawBuilder.String(), err
		}
	}
	return finalResult, rawBuilder.String(), fmt.Errorf("timed out waiting for session completion")
}

type codingAgentEventsResponse struct {
	Events             []map[string]interface{} `json:"events"`
	SessionStatus      string                   `json:"session_status"`
	LastProcessedIndex int                      `json:"last_processed_index"`
	CanSteer           bool                     `json:"can_steer"`
}

func (c *codingAgentChatE2EClient) getEvents(ctx context.Context, sessionID string) (*codingAgentEventsResponse, string, error) {
	endpoint := fmt.Sprintf("/api/sessions/%s/events?since=0", url.PathEscape(sessionID))
	var resp codingAgentEventsResponse
	raw, err := c.doJSONRaw(ctx, http.MethodGet, endpoint, sessionID, nil, &resp)
	if err != nil {
		return nil, raw, err
	}
	return &resp, raw, nil
}

func (c *codingAgentChatE2EClient) sendSteer(ctx context.Context, sessionID, message string) error {
	payload := map[string]interface{}{"message": message}
	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/api/sessions/%s/steer", url.PathEscape(sessionID)), sessionID, payload, nil)
}

func (c *codingAgentChatE2EClient) stopSession(ctx context.Context, sessionID string) error {
	return c.doJSON(ctx, http.MethodPost, "/api/session/stop?cancelAgents=true", sessionID, map[string]interface{}{}, nil)
}

func (c *codingAgentChatE2EClient) doJSON(ctx context.Context, method, endpoint, sessionID string, payload interface{}, into interface{}) error {
	_, err := c.doJSONRaw(ctx, method, endpoint, sessionID, payload, into)
	return err
}

func (c *codingAgentChatE2EClient) doJSONRaw(ctx context.Context, method, endpoint, sessionID string, payload interface{}, into interface{}) (string, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(data)
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	if strings.Contains(endpoint, "?") {
		parts := strings.SplitN(endpoint, "?", 2)
		u.Path = path.Join(u.Path, parts[0])
		u.RawQuery = parts[1]
	} else {
		u.Path = path.Join(u.Path, endpoint)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session-ID", sessionID)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	httpResp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()
	rawBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", err
	}
	raw := string(rawBytes)
	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return raw, fmt.Errorf("%s %s returned %s: %s", method, endpoint, httpResp.Status, raw)
	}
	if into != nil && len(rawBytes) > 0 {
		if err := json.Unmarshal(rawBytes, into); err != nil {
			return raw, fmt.Errorf("failed to decode %s response: %w; raw=%s", endpoint, err, raw)
		}
	}
	return raw, nil
}

func extractUnifiedCompletionFinal(events []map[string]interface{}) string {
	for i := len(events) - 1; i >= 0; i-- {
		if fmt.Sprint(events[i]["type"]) != "unified_completion" {
			continue
		}
		if value := findStringField(events[i], "final_result"); value != "" {
			return value
		}
	}
	return ""
}

func findStringField(value interface{}, key string) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		if direct, ok := typed[key].(string); ok && direct != "" {
			return direct
		}
		for _, child := range typed {
			if found := findStringField(child, key); found != "" {
				return found
			}
		}
	case []interface{}:
		for _, child := range typed {
			if found := findStringField(child, key); found != "" {
				return found
			}
		}
	}
	return ""
}

func rawContainsProvider(raw, provider string) bool {
	if provider == "" {
		return true
	}
	compactNeedle := fmt.Sprintf(`"provider":"%s"`, provider)
	spacedNeedle := fmt.Sprintf(`"provider": "%s"`, provider)
	return strings.Contains(raw, compactNeedle) || strings.Contains(raw, spacedNeedle)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
