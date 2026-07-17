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
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var codingAgentChatE2EFlags struct {
	serverURL           string
	provider            string
	model               string
	sessionID           string
	selectedFolder      string
	agentMode           string
	presetQueryID       string
	phaseID             string
	workshopMode        string
	enabledServers      string
	timeout             time.Duration
	skipLiveSteer       bool
	skipCompletionProbe bool
	runTmuxLossResume   bool
	vertexFinalJudge    bool
	vertexJudgeModel    string
}

var codingAgentChatE2ECmd = &cobra.Command{
	Use:   "coding-agent-chat-e2e",
	Short: "Run real e2e tests for tmux-backed coding agent chat sessions",
	Long: `Runs a real end-to-end test through the live coding-agent-loop HTTP API.

This intentionally does not call the provider adapter directly. It exercises:
1. /api/query turn startup
2. persisted session runtime capture
3. a second /api/query turn using the same chat session
4. optional /api/sessions/{session_id}/steer live input while a coding CLI is running
5. /api/sessions/{session_id}/events polling and unified completion extraction
6. terminal-backed completion detection after a real MCP bridge tool call
7. /api/terminals pane alias checks using tmux_session metadata
8. literal @ text handling so social handles are not expanded as @path

Example:
  mcp-agent test coding-agent-chat-e2e \
    --server-url http://localhost:18743 \
	--provider codex-cli \
    --model gemini-3.1-flash-lite \
    --selected-folder _users/default/Chats

  mcp-agent test coding-agent-chat-e2e \
    --server-url http://localhost:18743 \
    --provider cursor-cli \
    --model cursor-cli \
    --selected-folder _users/default/Chats

  mcp-agent test coding-agent-chat-e2e \
    --server-url http://localhost:18743 \
    --provider agy-cli \
    --model agy-cli \
    --selected-folder _users/default/Chats`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), codingAgentChatE2EFlags.timeout)
		defer cancel()

		client := &codingAgentChatE2EClient{
			baseURL:        strings.TrimRight(codingAgentChatE2EFlags.serverURL, "/"),
			token:          os.Getenv("AGENTWORKS_AUTH_TOKEN"),
			http:           &http.Client{Timeout: 30 * time.Second},
			agentMode:      codingAgentChatE2EFlags.agentMode,
			selectedFolder: codingAgentChatE2EFlags.selectedFolder,
			presetQueryID:  codingAgentChatE2EFlags.presetQueryID,
			phaseID:        codingAgentChatE2EFlags.phaseID,
			workshopMode:   codingAgentChatE2EFlags.workshopMode,
			enabledServers: codingAgentChatE2EFlags.enabledServers,
			timeout:        codingAgentChatE2EFlags.timeout,
		}
		if err := client.ensureUserAuth(ctx); err != nil {
			return err
		}

		provider := strings.TrimSpace(codingAgentChatE2EFlags.provider)
		if provider == "" {
			provider = "codex-cli"
		}
		model := strings.TrimSpace(codingAgentChatE2EFlags.model)
		if model == "" {
			model = defaultCodingAgentE2EModel(provider)
		}
		if codingAgentChatE2EFlags.vertexFinalJudge {
			if codingAgentVertexJudgeAPIKey() == "" {
				return fmt.Errorf("--vertex-final-judge requires GEMINI_API_KEY, VERTEX_API_KEY, or GOOGLE_API_KEY")
			}
			fmt.Printf("Vertex final-extraction judge enabled model=%s\n", codingAgentVertexJudgeModel())
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

		if codingAgentChatE2EFlags.runTmuxLossResume {
			if !providerSupportsTmuxLossResumeE2E(provider) {
				return fmt.Errorf("--run-tmux-loss-resume is only certified for claude-code, codex-cli, and agy-cli, got %q", provider)
			}
			killedTmux, err := client.killLatestTerminalTmux(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("failed to kill latest tmux terminal before resume check: %w", err)
			}
			fmt.Printf("Killed tmux session %s to force provider-native continuation recovery\n", killedTmux)
			resumeQuery := "The tmux pane for this coding-agent session was externally killed. Continue the same native provider session. What exact token did I ask you to take note of earlier? Do not use tools. Reply with exactly that token and nothing else."
			if err := client.runAndAssertContains(ctx, sessionID, provider, model, resumeQuery, []string{noteToken}); err != nil {
				return fmt.Errorf("tmux-loss native resume failed: %w", err)
			}
			fmt.Println("PASS tmux-loss resume: app-level chat recovered through provider-native continuation")
		}

		atToken := "AT_LITERAL_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		atHandle := "@fixyo.urflow"
		atQuery := fmt.Sprintf("Do not use tools. Treat %s as literal text, not a file path. Reply with exactly %s %s and nothing else.", atHandle, atToken, atHandle)
		if err := client.runAndAssertContains(ctx, sessionID, provider, model, atQuery, []string{atToken, atHandle}); err != nil {
			return fmt.Errorf("literal @ handle prompt failed: %w", err)
		}
		fmt.Println("PASS literal @ text: pasted handles are not expanded as @path")

		if !codingAgentChatE2EFlags.skipCompletionProbe {
			completionToken := "COMPLETION_E2E_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			completionQuery := fmt.Sprintf("Use execute_shell_command exactly once to run `printf %s`. After the tool returns, reply with exactly DONE_%s and nothing else.", completionToken, completionToken)
			if err := client.runAndAssertContains(ctx, sessionID, provider, model, completionQuery, []string{"DONE_" + completionToken}); err != nil {
				return fmt.Errorf("terminal completion probe failed: %w", err)
			}
			if err := client.assertNoDuplicateTerminalPanes(ctx, sessionID); err != nil {
				return fmt.Errorf("terminal pane alias check failed after completion probe: %w", err)
			}
			fmt.Println("PASS terminal completion: tool-backed turn reached unified completion and terminal panes are not duplicated")

			secondToolToken := "COMPLETION_E2E_SECOND_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			secondToolQuery := fmt.Sprintf("This is a later turn in the same native coding-agent session, after an earlier shell-tool turn. Use execute_shell_command exactly once to run `printf %s`. After the tool returns, reply with exactly DONE_%s and nothing else. Do not include any token from earlier turns, especially %s, %s, or DONE_%s.", secondToolToken, secondToolToken, noteToken, atToken, completionToken)
			earlierTurnFragments := []string{noteToken, "ACK_" + noteToken, atToken, completionToken, "DONE_" + completionToken}
			if err := client.runAndAssertFinal(ctx, sessionID, provider, model, secondToolQuery, []string{"DONE_" + secondToolToken}, earlierTurnFragments); err != nil {
				return fmt.Errorf("multi-turn terminal final extraction probe failed: %w", err)
			}
			if err := client.assertNoDuplicateTerminalPanes(ctx, sessionID); err != nil {
				return fmt.Errorf("terminal pane alias check failed after multi-turn completion probe: %w", err)
			}

			postToolToken := "POST_TOOL_FINAL_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			postToolQuery := fmt.Sprintf("Do not use tools. This is after multiple prior turns and multiple shell-tool turns in this same native session. Reply with exactly %s and nothing else. Do not include any previous note, @ handle, or completion token.", postToolToken)
			forbiddenPostToolFragments := append(append([]string{}, earlierTurnFragments...), secondToolToken, "DONE_"+secondToolToken)
			if err := client.runAndAssertFinal(ctx, sessionID, provider, model, postToolQuery, []string{postToolToken}, forbiddenPostToolFragments); err != nil {
				return fmt.Errorf("post-tool multi-turn final extraction probe failed: %w", err)
			}
			fmt.Println("PASS multi-turn tool extraction: later finals did not leak earlier tool or chat turns")

			longFinalToken := "LONG_FINAL_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			longFinalLines := make([]string, 0, 40)
			for i := 1; i <= 40; i++ {
				longFinalLines = append(longFinalLines, fmt.Sprintf("LINE_%02d LONG_FINAL_SENTINEL_%02d_%s", i, i, longFinalToken))
			}
			longFinalQuery := fmt.Sprintf("Do not use tools. This is a long final extraction regression after prior multi-turn shell-tool turns. Reply with exactly these %d lines, preserving every line and line order, and nothing else:\n%s", len(longFinalLines), strings.Join(longFinalLines, "\n"))
			forbiddenLongFinalFragments := append(append([]string{}, forbiddenPostToolFragments...), postToolToken)
			if err := client.runAndAssertFinal(ctx, sessionID, provider, model, longFinalQuery, longFinalLines, forbiddenLongFinalFragments); err != nil {
				return fmt.Errorf("long final extraction probe failed: %w", err)
			}
			fmt.Println("PASS long final extraction: all sentinel lines preserved after tool turns")
		}

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
			if err := client.waitForDerivedTerminalStatus(ctx, sessionID, 45*time.Second); err != nil {
				return fmt.Errorf("derived terminal status was not available during live turn: %w", err)
			}
			if err := client.assertNoDuplicateTerminalPanes(ctx, sessionID); err != nil {
				return fmt.Errorf("terminal pane alias check failed during live turn: %w", err)
			}
			fmt.Println("PASS live status: terminal stream produced derived status text")
			beforeSteer, _, err := client.getEvents(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("failed to capture event cursor before live steer: %w", err)
			}
			beforeSteerIndex := beforeSteer.LastProcessedIndex
			if err := client.sendSteer(ctx, sessionID, fmt.Sprintf("Live follow-up: include the exact token %s in your final answer.", liveToken)); err != nil {
				return fmt.Errorf("live steer POST failed: %w", err)
			}
			fmt.Println("Sent live steer message")

			final, raw, events, err := client.waitForCompletion(ctx, sessionID, beforeSteerIndex)
			if err != nil {
				return fmt.Errorf("live steer turn did not complete: %w", err)
			}
			if !eventsProveProvider(events, provider) {
				return fmt.Errorf("live steer event stream did not prove requested provider %q was used; evidence=%s", provider, summarizeProviderProofEvents(events))
			}
			if !strings.Contains(final, liveToken) {
				return fmt.Errorf("live steer token %q was not processed; final=%q", liveToken, final)
			}
			if err := client.assertAssistantCompletionAfter(ctx, sessionID, beforeSteerIndex, liveToken); err != nil {
				return fmt.Errorf("live steer token was not observed in assistant completion after steer: %w; final=%q raw=%s", err, final, truncateE2E(raw, 1200))
			}
			if err := client.assertVertexFinalJudge(ctx, sessionID, provider, liveQuery+"\n\nLive user message: include the exact token "+liveToken+" in the final answer.", final, raw, []string{liveToken}, nil); err != nil {
				return fmt.Errorf("live steer Vertex final-extraction judge failed: %w", err)
			}
			fmt.Println("PASS live steer: in-flight user message was processed by the coding agent")
		}

		fmt.Println("PASS coding agent chat e2e")
		return nil
	},
}

func init() {
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.serverURL, "server-url", "http://localhost:18743", "coding-agent-loop server URL")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.provider, "provider", "codex-cli", "coding CLI provider: codex-cli, cursor-cli, agy-cli, pi-cli, or claude-code")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.model, "model", "", "model ID; defaults to the provider-specific E2E model")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.sessionID, "session-id", "", "session ID to reuse; generated when omitted")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.selectedFolder, "selected-folder", "_users/default/Chats", "workspace-relative folder for the chat session")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.agentMode, "agent-mode", "simple", "agent mode to send to /api/query")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.presetQueryID, "preset-query-id", "", "workflow preset ID for workflow_phase chat")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.phaseID, "phase-id", "", "workflow phase ID for workflow_phase chat")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.workshopMode, "workshop-mode", "", "optional workflow workshop mode, for example builder or run")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.enabledServers, "enabled-servers", "api-bridge", "comma-separated MCP servers to expose during the E2E")
	codingAgentChatE2ECmd.Flags().DurationVar(&codingAgentChatE2EFlags.timeout, "timeout", 6*time.Minute, "overall test timeout")
	codingAgentChatE2ECmd.Flags().BoolVar(&codingAgentChatE2EFlags.skipLiveSteer, "skip-live-steer", false, "skip the in-flight /steer regression test")
	codingAgentChatE2ECmd.Flags().BoolVar(&codingAgentChatE2EFlags.skipCompletionProbe, "skip-completion-probe", false, "skip the tool-backed completion detection regression test")
	codingAgentChatE2ECmd.Flags().BoolVar(&codingAgentChatE2EFlags.runTmuxLossResume, "run-tmux-loss-resume", false, "kill the latest tmux pane after turn 2 and require provider-native continuation recovery; certified for claude-code, codex-cli, and agy-cli")
	codingAgentChatE2ECmd.Flags().BoolVar(&codingAgentChatE2EFlags.vertexFinalJudge, "vertex-final-judge", false, "use a Gemini/Vertex LLM judge to validate each extracted unified_completion final answer")
	codingAgentChatE2ECmd.Flags().StringVar(&codingAgentChatE2EFlags.vertexJudgeModel, "vertex-final-judge-model", "", "Gemini/Vertex model for --vertex-final-judge; defaults to VERTEX_FINAL_EXTRACTION_JUDGE_MODEL or gemini-3.1-pro-preview")
}

type codingAgentChatE2EClient struct {
	baseURL        string
	token          string
	http           *http.Client
	agentMode      string
	selectedFolder string
	presetQueryID  string
	phaseID        string
	workshopMode   string
	enabledServers string
	timeout        time.Duration
}

// ensureUserAuth obtains the JWT required by /api routes. MCP_API_TOKEN is a
// different credential used only by per-tool bridge endpoints and must never
// be sent as the application bearer token. Single-user servers expose the
// public login route without requiring credentials; multi-user test runs can
// supply AGENTWORKS_AUTH_TOKEN explicitly.
func (c *codingAgentChatE2EClient) ensureUserAuth(ctx context.Context) error {
	if strings.TrimSpace(c.token) != "" {
		return nil
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/api/auth/login", "", map[string]interface{}{}, &resp); err != nil {
		return fmt.Errorf("authenticate live E2E client: %w (set AGENTWORKS_AUTH_TOKEN for multi-user servers)", err)
	}
	if strings.TrimSpace(resp.Token) == "" {
		return fmt.Errorf("authenticate live E2E client: server returned an empty token")
	}
	c.token = strings.TrimSpace(resp.Token)
	return nil
}

func defaultCodingAgentE2EModel(provider string) string {
	switch provider {
	case "codex-cli":
		return "gpt-5.3-codex-spark"
	case "cursor-cli":
		return "cursor-cli"
	case "agy-cli":
		return "agy-cli"
	case "claude-code":
		return "claude-code"
	case "pi-cli":
		return "google/gemini-3.5-flash"
	default:
		return ""
	}
}

func (c *codingAgentChatE2EClient) runAndAssertContains(ctx context.Context, sessionID, provider, model, query string, required []string) error {
	return c.runAndAssertFinal(ctx, sessionID, provider, model, query, required, nil)
}

func (c *codingAgentChatE2EClient) runAndAssertFinal(ctx context.Context, sessionID, provider, model, query string, required, forbidden []string) error {
	since := 0
	if resp, _, err := c.getEvents(ctx, sessionID); err == nil {
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
	}
	if _, err := c.startQuery(ctx, sessionID, provider, model, query); err != nil {
		return err
	}
	final, raw, events, err := c.waitForCompletion(ctx, sessionID, since)
	if err != nil {
		return err
	}
	if !eventsProveProvider(events, provider) {
		return fmt.Errorf("event stream did not prove requested provider %q was used; evidence=%s", provider, summarizeProviderProofEvents(events))
	}
	for _, needle := range required {
		if !strings.Contains(final, needle) {
			return fmt.Errorf("completion did not contain %q; final=%q", needle, final)
		}
	}
	for _, needle := range forbidden {
		if strings.TrimSpace(needle) == "" {
			continue
		}
		if strings.Contains(final, needle) {
			return fmt.Errorf("completion leaked forbidden prior-turn fragment %q; final=%q", needle, final)
		}
	}
	if err := c.assertVertexFinalJudge(ctx, sessionID, provider, query, final, raw, required, forbidden); err != nil {
		return err
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
		"agent_mode":      coalesceE2EString(c.agentMode, "simple"),
		"enabled_servers": splitCSVOrDefault(c.enabledServers, []string{"api-bridge"}),
		"selected_folder": coalesceE2EString(c.selectedFolder, "_users/default/Chats"),
		"max_turns":       -1,
	}
	if c.presetQueryID != "" {
		payload["preset_query_id"] = c.presetQueryID
	}
	if c.phaseID != "" {
		payload["phase_id"] = c.phaseID
	}
	if c.workshopMode != "" {
		payload["execution_options"] = map[string]interface{}{
			"workshop_mode": c.workshopMode,
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
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	since := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, _, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return err
		}
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
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
	return fmt.Errorf("timed out after %s", time.Since(start).Round(time.Millisecond))
}

func (c *codingAgentChatE2EClient) waitForCompletion(ctx context.Context, sessionID string, since int) (string, string, []map[string]interface{}, error) {
	var finalResult string
	var lastRaw string
	var rawHistory strings.Builder
	var eventHistory []map[string]interface{}
	start := time.Now()
	deadline := e2eDeadline(ctx, c.timeoutOrDefault())
	if since < 0 {
		since = 0
	}
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return "", lastRaw, eventHistory, err
		}
		resp, raw, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return "", lastRaw, eventHistory, err
		}
		lastRaw = raw
		if raw != "" {
			rawHistory.WriteString(raw)
			rawHistory.WriteString("\n")
		}
		eventHistory = append(eventHistory, resp.Events...)
		nextSince := advanceE2ECursor(since, resp.LastProcessedIndex)
		if extracted := extractUnifiedCompletionFinal(resp.Events); extracted != "" {
			finalResult = extracted
		}
		switch resp.SessionStatus {
		case "completed":
			if strings.TrimSpace(finalResult) == "" {
				if len(resp.Events) > 0 {
					since = nextSince
				}
				if err := sleepContext(ctx, 500*time.Millisecond); err != nil {
					return "", lastRaw, eventHistory, err
				}
				continue
			}
			if rawHistory.Len() > 0 {
				return finalResult, rawHistory.String(), eventHistory, nil
			}
			return finalResult, lastRaw, eventHistory, nil
		case "error", "stopped":
			return finalResult, lastRaw, eventHistory, fmt.Errorf("session ended with status %s; final=%q", resp.SessionStatus, finalResult)
		}
		since = nextSince
		if err := sleepContext(ctx, time.Second); err != nil {
			return "", lastRaw, eventHistory, err
		}
	}
	return finalResult, lastRaw, eventHistory, fmt.Errorf("timed out waiting for session completion after %s", time.Since(start).Round(time.Millisecond))
}

type codingAgentEventsResponse struct {
	Events             []map[string]interface{} `json:"events"`
	SessionStatus      string                   `json:"session_status"`
	LastProcessedIndex int                      `json:"last_processed_index"`
	CanSteer           bool                     `json:"can_steer"`
}

type codingAgentTerminalsResponse struct {
	Terminals []codingAgentTerminalSnapshot `json:"terminals"`
	Total     int                           `json:"total"`
}

type codingAgentTerminalSnapshot struct {
	TerminalID  string                    `json:"terminal_id"`
	SessionID   string                    `json:"session_id"`
	TmuxSession string                    `json:"tmux_session"`
	Active      bool                      `json:"active"`
	State       string                    `json:"state"`
	Content     string                    `json:"content"`
	Status      codingAgentTerminalStatus `json:"status"`
	UpdatedAt   time.Time                 `json:"updated_at"`
}

type codingAgentTerminalStatus struct {
	StatusText       string `json:"status_text"`
	AssistantPreview string `json:"assistant_preview"`
	ToolSummary      string `json:"tool_summary"`
	ProviderLabel    string `json:"provider_label"`
}

func (c *codingAgentChatE2EClient) getEvents(ctx context.Context, sessionID string) (*codingAgentEventsResponse, string, error) {
	return c.getEventsSince(ctx, sessionID, 0)
}

func (c *codingAgentChatE2EClient) getEventsSince(ctx context.Context, sessionID string, since int) (*codingAgentEventsResponse, string, error) {
	if since < 0 {
		since = 0
	}
	endpoint := fmt.Sprintf("/api/sessions/%s/events?since=%d", url.PathEscape(sessionID), since)
	var resp codingAgentEventsResponse
	raw, err := c.doJSONRaw(ctx, http.MethodGet, endpoint, sessionID, nil, &resp)
	if err != nil {
		return nil, raw, err
	}
	return &resp, raw, nil
}

func (c *codingAgentChatE2EClient) waitForDerivedTerminalStatus(ctx context.Context, sessionID string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, _, err := c.getTerminals(ctx, sessionID)
		if err != nil {
			return err
		}
		for _, terminal := range resp.Terminals {
			if !terminal.Active {
				continue
			}
			if strings.TrimSpace(terminal.Content) == "" {
				continue
			}
			if strings.TrimSpace(terminal.Status.StatusText) != "" || strings.TrimSpace(terminal.Status.ToolSummary) != "" {
				return nil
			}
		}
		if err := sleepContext(ctx, 750*time.Millisecond); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s", time.Since(start).Round(time.Millisecond))
}

func (c *codingAgentChatE2EClient) getTerminals(ctx context.Context, sessionID string) (*codingAgentTerminalsResponse, string, error) {
	endpoint := fmt.Sprintf("/api/terminals?session_id=%s", url.QueryEscape(sessionID))
	var resp codingAgentTerminalsResponse
	raw, err := c.doJSONRaw(ctx, http.MethodGet, endpoint, sessionID, nil, &resp)
	if err != nil {
		return nil, raw, err
	}
	return &resp, raw, nil
}

func providerSupportsTmuxLossResumeE2E(provider string) bool {
	switch strings.TrimSpace(provider) {
	case "claude-code", "codex-cli", "agy-cli":
		return true
	default:
		return false
	}
}

func (c *codingAgentChatE2EClient) killLatestTerminalTmux(ctx context.Context, sessionID string) (string, error) {
	resp, raw, err := c.getTerminals(ctx, sessionID)
	if err != nil {
		return "", err
	}
	var selected codingAgentTerminalSnapshot
	for _, terminal := range resp.Terminals {
		if strings.TrimSpace(terminal.TmuxSession) == "" {
			continue
		}
		if selected.TmuxSession == "" || (terminal.Active && !selected.Active) || (terminal.Active == selected.Active && terminal.UpdatedAt.After(selected.UpdatedAt)) {
			selected = terminal
		}
	}
	if strings.TrimSpace(selected.TmuxSession) == "" {
		return "", fmt.Errorf("no tmux-backed terminal found; raw=%s", truncateE2E(raw, 2000))
	}

	killCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(killCtx, "tmux", "kill-session", "-t", selected.TmuxSession).CombinedOutput()
	if err != nil {
		return selected.TmuxSession, fmt.Errorf("tmux kill-session -t %s: %w output=%s", selected.TmuxSession, err, string(output))
	}
	return selected.TmuxSession, nil
}

func (c *codingAgentChatE2EClient) assertNoDuplicateTerminalPanes(ctx context.Context, sessionID string) error {
	const samples = 3
	for i := 0; i < samples; i++ {
		if err := c.assertNoDuplicateTerminalPanesOnce(ctx, sessionID); err != nil {
			return fmt.Errorf("sample %d/%d: %w", i+1, samples, err)
		}
		if i < samples-1 {
			if err := sleepContext(ctx, 250*time.Millisecond); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *codingAgentChatE2EClient) assertNoDuplicateTerminalPanesOnce(ctx context.Context, sessionID string) error {
	resp, raw, err := c.getTerminals(ctx, sessionID)
	if err != nil {
		return err
	}
	byTmux := map[string]string{}
	for _, terminal := range resp.Terminals {
		tmuxSession := strings.TrimSpace(terminal.TmuxSession)
		if tmuxSession == "" {
			continue
		}
		if previousID := byTmux[tmuxSession]; previousID != "" && previousID != terminal.TerminalID {
			return fmt.Errorf("tmux session %q appears in multiple terminal snapshots: %s and %s; raw=%s", tmuxSession, previousID, terminal.TerminalID, raw)
		}
		byTmux[tmuxSession] = terminal.TerminalID
	}
	return nil
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
		if value := eventPayloadString(events[i], "final_result"); value != "" {
			return value
		}
	}
	return ""
}

func eventsProveProvider(events []map[string]interface{}, provider string) bool {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return true
	}
	for _, event := range events {
		switch fmt.Sprint(event["type"]) {
		case "agent_start", "llm_generation_start", "llm_generation_end", "model_change", "token_usage", "unified_completion":
			if eventPayloadString(event, "provider") == provider {
				return true
			}
			if metadataMap := eventPayloadMap(event, "metadata"); metadataMap != nil {
				if fmt.Sprint(metadataMap["provider"]) == provider {
					return true
				}
				if handle, ok := metadataMap["coding_provider_session_handle"].(map[string]interface{}); ok && fmt.Sprint(handle["provider"]) == provider {
					return true
				}
			}
		}
	}
	return false
}

func summarizeProviderProofEvents(events []map[string]interface{}) string {
	if len(events) == 0 {
		return "no events"
	}
	parts := make([]string, 0, len(events))
	for _, event := range events {
		eventType := fmt.Sprint(event["type"])
		var providers []string
		if provider := eventPayloadString(event, "provider"); provider != "" {
			providers = append(providers, "provider="+provider)
		}
		if metadataMap := eventPayloadMap(event, "metadata"); metadataMap != nil {
			if provider := strings.TrimSpace(fmt.Sprint(metadataMap["provider"])); provider != "" && provider != "<nil>" {
				providers = append(providers, "metadata.provider="+provider)
			}
			if handle, ok := metadataMap["coding_provider_session_handle"].(map[string]interface{}); ok {
				if provider := strings.TrimSpace(fmt.Sprint(handle["provider"])); provider != "" && provider != "<nil>" {
					providers = append(providers, "handle.provider="+provider)
				}
			}
		}
		if len(providers) == 0 {
			parts = append(parts, eventType)
		} else {
			parts = append(parts, eventType+"["+strings.Join(providers, ",")+"]")
		}
	}
	if len(parts) > 16 {
		parts = append([]string{fmt.Sprintf("...%d earlier events", len(parts)-16)}, parts[len(parts)-16:]...)
	}
	return strings.Join(parts, " -> ")
}

func (c *codingAgentChatE2EClient) assertAssistantCompletionAfter(ctx context.Context, sessionID string, since int, needle string) error {
	resp, raw, err := c.getEventsSince(ctx, sessionID, since)
	if err != nil {
		return err
	}
	for _, event := range resp.Events {
		if fmt.Sprint(event["type"]) != "unified_completion" {
			continue
		}
		if strings.Contains(eventPayloadString(event, "final_result"), needle) {
			return nil
		}
	}
	return fmt.Errorf("no unified_completion after event index %d contained %q; raw=%s", since, needle, truncateE2E(raw, 1500))
}

func eventPayloadString(event map[string]interface{}, key string) string {
	if event == nil || key == "" {
		return ""
	}
	for _, payload := range eventPayloadCandidates(event) {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func eventPayloadMap(event map[string]interface{}, key string) map[string]interface{} {
	if event == nil || key == "" {
		return nil
	}
	for _, payload := range eventPayloadCandidates(event) {
		if value, ok := payload[key].(map[string]interface{}); ok {
			return value
		}
	}
	return nil
}

func eventPayloadCandidates(event map[string]interface{}) []map[string]interface{} {
	var candidates []map[string]interface{}
	if event == nil {
		return candidates
	}
	candidates = append(candidates, event)
	if data, ok := event["data"].(map[string]interface{}); ok {
		candidates = append(candidates, data)
		if nested, ok := data["data"].(map[string]interface{}); ok {
			candidates = append(candidates, nested)
		}
	}
	return candidates
}

func splitCSVOrDefault(value string, fallback []string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func coalesceE2EString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func (c *codingAgentChatE2EClient) timeoutOrDefault() time.Duration {
	if c != nil && c.timeout > 0 {
		return c.timeout
	}
	if codingAgentChatE2EFlags.timeout > 0 {
		return codingAgentChatE2EFlags.timeout
	}
	return 6 * time.Minute
}

func e2eDeadline(ctx context.Context, timeout time.Duration) time.Time {
	if timeout <= 0 {
		timeout = 6 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		return ctxDeadline
	}
	return deadline
}

func advanceE2ECursor(current, next int) int {
	if next > current {
		return next
	}
	return current
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
