package testing

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var codingAgentBackgroundE2EFlags struct {
	serverURL      string
	provider       string
	model          string
	sessionID      string
	selectedFolder string
	timeout        time.Duration
}

var codingAgentBackgroundE2ECmd = &cobra.Command{
	Use:   "coding-agent-background-e2e",
	Short: "Run a real e2e test for coding-agent background delegation state",
	Long: `Runs a real end-to-end test through the live mcp-agent-builder-go HTTP API.

This test exercises the same path the UI uses:
1. /api/query with a tmux-backed coding agent
2. delegate tool starting a real background agent
3. foreground turn completing while the background agent is still running
4. /api/sessions/active and /api/sessions/{session_id}/execution-tree state
5. background agent completion notification/result

Example:
  mcp-agent test coding-agent-background-e2e \
    --server-url http://localhost:18743 \
    --provider claude-code`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(cmd.Context(), codingAgentBackgroundE2EFlags.timeout)
		defer cancel()

		client := &codingAgentChatE2EClient{
			baseURL: strings.TrimRight(codingAgentBackgroundE2EFlags.serverURL, "/"),
			token:   os.Getenv("MCP_API_TOKEN"),
			http:    &http.Client{Timeout: 30 * time.Second},
		}

		provider := strings.TrimSpace(codingAgentBackgroundE2EFlags.provider)
		if provider == "" {
			provider = "claude-code"
		}
		model := strings.TrimSpace(codingAgentBackgroundE2EFlags.model)
		if model == "" {
			model = defaultCodingAgentE2EModel(provider)
		}
		sessionID := strings.TrimSpace(codingAgentBackgroundE2EFlags.sessionID)
		if sessionID == "" {
			sessionID = fmt.Sprintf("coding-agent-bg-e2e-%s-%d", strings.ReplaceAll(provider, "-", ""), time.Now().UnixNano())
		}
		oldChatFlags := codingAgentChatE2EFlags
		codingAgentChatE2EFlags.serverURL = codingAgentBackgroundE2EFlags.serverURL
		codingAgentChatE2EFlags.provider = provider
		codingAgentChatE2EFlags.model = model
		codingAgentChatE2EFlags.sessionID = sessionID
		codingAgentChatE2EFlags.selectedFolder = codingAgentBackgroundE2EFlags.selectedFolder
		codingAgentChatE2EFlags.agentMode = "simple"
		codingAgentChatE2EFlags.timeout = codingAgentBackgroundE2EFlags.timeout
		defer func() {
			codingAgentChatE2EFlags = oldChatFlags
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			_ = client.stopSession(stopCtx, sessionID)
		}()

		finalToken := "BACKGROUND_CONTRACT_DONE_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		fmt.Printf("Running coding agent background e2e provider=%s model=%s session=%s\n", provider, model, sessionID)
		fmt.Printf("Selected folder: %s\n", codingAgentBackgroundE2EFlags.selectedFolder)

		query := fmt.Sprintf(`This is a background-agent transport contract test.

Use the delegate tool exactly once with:
- name: bg-contract-check
- reasoning_level: low
- instruction:
  You are a background contract-test agent. Use execute_shell_command exactly once to run this command:
  sleep 15; printf %s
  After the command returns, finish with exactly this text and nothing else:
  %s
  Do not start any other agents. Do not ask questions.

After the delegate tool returns an agent_id, your own final response must be exactly:
STARTED_BG_CONTRACT_CHECK

Do not call any other tools after delegate returns.`, finalToken, finalToken)

		if _, err := client.startQuery(ctx, sessionID, provider, model, query); err != nil {
			return fmt.Errorf("background delegation query failed to start: %w", err)
		}

		if err := client.waitForUnifiedCompletionContains(ctx, sessionID, []string{"STARTED_BG_CONTRACT_CHECK"}, 2*time.Minute); err != nil {
			return fmt.Errorf("foreground turn did not return start acknowledgement: %w", err)
		}
		fmt.Println("PASS foreground: main coding agent returned after starting background agent")

		if err := client.waitForActiveBackgroundSummary(ctx, sessionID, 45*time.Second); err != nil {
			return fmt.Errorf("active session did not show completed foreground + running background: %w", err)
		}
		fmt.Println("PASS active sessions: foreground is completed while background remains running")

		if err := client.waitForBackgroundNodeStatus(ctx, sessionID, "running", 45*time.Second); err != nil {
			return fmt.Errorf("execution tree did not show running background node: %w", err)
		}
		fmt.Println("PASS execution tree: background node is visible and running")

		if err := client.waitForBackgroundNodeStatus(ctx, sessionID, "completed", codingAgentBackgroundE2EFlags.timeout); err != nil {
			return fmt.Errorf("background node did not complete: %w", err)
		}
		fmt.Println("PASS execution tree: background node completed")

		if err := client.waitForNonUserEventContains(ctx, sessionID, []string{finalToken}, time.Minute); err != nil {
			return fmt.Errorf("background completion token was not present in events: %w", err)
		}
		fmt.Println("PASS events: background completion token observed")

		if err := client.waitForNoRunningBackground(ctx, sessionID, 45*time.Second); err != nil {
			return fmt.Errorf("background running flag did not clear: %w", err)
		}
		fmt.Println("PASS active sessions: background running flag cleared")

		fmt.Println("PASS coding agent background e2e")
		return nil
	},
}

func init() {
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.serverURL, "server-url", "http://localhost:18743", "mcp-agent-builder-go server URL")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.provider, "provider", "claude-code", "coding CLI provider")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.model, "model", "", "model ID; defaults to a low-cost model for the selected provider")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.sessionID, "session-id", "", "session ID to reuse; generated when omitted")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.selectedFolder, "selected-folder", "_users/default/Chats", "workspace-relative folder for the chat session")
	codingAgentBackgroundE2ECmd.Flags().DurationVar(&codingAgentBackgroundE2EFlags.timeout, "timeout", 6*time.Minute, "overall test timeout")
}

func (c *codingAgentChatE2EClient) waitForFinalOrRawContains(ctx context.Context, sessionID string, required []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastFinal string
	var lastRaw string
	for time.Now().Before(deadline) {
		resp, raw, err := c.getEvents(ctx, sessionID)
		if err != nil {
			return err
		}
		lastRaw = raw
		if extracted := extractUnifiedCompletionFinal(resp.Events); extracted != "" {
			lastFinal = extracted
		}
		allPresent := true
		for _, needle := range required {
			if !strings.Contains(lastFinal, needle) && !strings.Contains(raw, needle) {
				allPresent = false
				break
			}
		}
		if allPresent {
			return nil
		}
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session ended with status %s; final=%q raw=%s", resp.SessionStatus, lastFinal, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; final=%q raw=%s", timeout, lastFinal, truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForUnifiedCompletionContains(ctx context.Context, sessionID string, required []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastFinal string
	var lastRaw string
	for time.Now().Before(deadline) {
		resp, raw, err := c.getEvents(ctx, sessionID)
		if err != nil {
			return err
		}
		lastRaw = raw
		if extracted := extractUnifiedCompletionFinal(resp.Events); extracted != "" {
			lastFinal = extracted
		}
		allPresent := true
		for _, needle := range required {
			if !strings.Contains(lastFinal, needle) {
				allPresent = false
				break
			}
		}
		if allPresent {
			return nil
		}
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session ended with status %s; final=%q raw=%s", resp.SessionStatus, lastFinal, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; final=%q raw=%s", timeout, lastFinal, truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForNonUserEventContains(ctx context.Context, sessionID string, required []string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastRaw string
	for time.Now().Before(deadline) {
		resp, raw, err := c.getEvents(ctx, sessionID)
		if err != nil {
			return err
		}
		lastRaw = raw
		allPresent := true
		for _, needle := range required {
			if !eventsContainNonUserString(resp.Events, needle) {
				allPresent = false
				break
			}
		}
		if allPresent {
			return nil
		}
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session ended with status %s before observing token; raw=%s", resp.SessionStatus, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; raw=%s", timeout, truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForActiveBackgroundSummary(ctx context.Context, sessionID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		sess, raw, err := c.getActiveSessionSummary(ctx, sessionID)
		if err != nil {
			return err
		}
		last = raw
		if sess != nil && sess.Status == "completed" && sess.HasRunningBackgroundAgents && sess.RunningBackgroundAgentCount > 0 {
			return nil
		}
		if sess != nil && sess.Status == "error" {
			return fmt.Errorf("session entered error status: %s", raw)
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; latest active session payload=%s", timeout, truncateE2E(last, 2000))
}

func (c *codingAgentChatE2EClient) waitForNoRunningBackground(ctx context.Context, sessionID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		sess, raw, err := c.getActiveSessionSummary(ctx, sessionID)
		if err != nil {
			return err
		}
		last = raw
		if sess != nil && !sess.HasRunningBackgroundAgents && sess.RunningBackgroundAgentCount == 0 {
			return nil
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; latest active session payload=%s", timeout, truncateE2E(last, 2000))
}

func (c *codingAgentChatE2EClient) getActiveSessionSummary(ctx context.Context, sessionID string) (*codingAgentActiveSessionSummary, string, error) {
	var resp codingAgentActiveSessionsResponse
	raw, err := c.doJSONRaw(ctx, http.MethodGet, "/api/sessions/active", sessionID, nil, &resp)
	if err != nil {
		return nil, raw, err
	}
	for i := range resp.ActiveSessions {
		if resp.ActiveSessions[i].SessionID == sessionID {
			return &resp.ActiveSessions[i], raw, nil
		}
	}
	return nil, raw, nil
}

type codingAgentActiveSessionsResponse struct {
	ActiveSessions []codingAgentActiveSessionSummary `json:"active_sessions"`
	Total          int                               `json:"total"`
}

type codingAgentActiveSessionSummary struct {
	SessionID                   string `json:"session_id"`
	Status                      string `json:"status"`
	HasRunningBackgroundAgents  bool   `json:"has_running_background_agents"`
	RunningBackgroundAgentCount int    `json:"running_background_agent_count"`
	CurrentExecutionName        string `json:"current_execution_name"`
}

func (c *codingAgentChatE2EClient) waitForBackgroundNodeStatus(ctx context.Context, sessionID, wantStatus string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		tree, raw, err := c.getExecutionTree(ctx, sessionID)
		if err != nil {
			return err
		}
		last = raw
		if node := findBackgroundContractNode(tree.Root); node != nil {
			if node.Status == wantStatus {
				return nil
			}
			if node.Status == "failed" || node.Status == "canceled" {
				return fmt.Errorf("background node reached %s: error=%q raw=%s", node.Status, node.Error, truncateE2E(raw, 2000))
			}
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s waiting for background status %q; latest tree=%s", timeout, wantStatus, truncateE2E(last, 3000))
}

func (c *codingAgentChatE2EClient) getExecutionTree(ctx context.Context, sessionID string) (*codingAgentExecutionTreeResponse, string, error) {
	endpoint := fmt.Sprintf("/api/sessions/%s/execution-tree", url.PathEscape(sessionID))
	var resp codingAgentExecutionTreeResponse
	raw, err := c.doJSONRaw(ctx, http.MethodGet, endpoint, sessionID, nil, &resp)
	if err != nil {
		return nil, raw, err
	}
	return &resp, raw, nil
}

type codingAgentExecutionTreeResponse struct {
	SessionID string                          `json:"session_id"`
	Root      *codingAgentExecutionTreeNode   `json:"root"`
	Summary   codingAgentExecutionTreeSummary `json:"summary"`
}

type codingAgentExecutionTreeSummary struct {
	SessionID                  string `json:"session_id"`
	SessionStatus              string `json:"session_status"`
	DisplayStatus              string `json:"display_status"`
	HasRunningBackgroundAgents bool   `json:"has_running_background_agents"`
}

type codingAgentExecutionTreeNode struct {
	ExecutionID string                          `json:"execution_id"`
	Source      string                          `json:"source"`
	Kind        string                          `json:"kind"`
	Name        string                          `json:"name"`
	Status      string                          `json:"status"`
	Error       string                          `json:"error"`
	Children    []*codingAgentExecutionTreeNode `json:"children"`
}

func findBackgroundContractNode(node *codingAgentExecutionTreeNode) *codingAgentExecutionTreeNode {
	if node == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(node.Name), "bg-contract-check") {
		return node
	}
	for _, child := range node.Children {
		if found := findBackgroundContractNode(child); found != nil {
			return found
		}
	}
	return nil
}

func eventsContainNonUserString(events []map[string]interface{}, needle string) bool {
	for _, event := range events {
		if fmt.Sprint(event["type"]) == "user_message" {
			continue
		}
		if valueContainsString(event, needle) {
			return true
		}
	}
	return false
}

func valueContainsString(value interface{}, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case map[string]interface{}:
		for _, child := range typed {
			if valueContainsString(child, needle) {
				return true
			}
		}
	case []interface{}:
		for _, child := range typed {
			if valueContainsString(child, needle) {
				return true
			}
		}
	}
	return false
}

func truncateE2E(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...<truncated>"
}
