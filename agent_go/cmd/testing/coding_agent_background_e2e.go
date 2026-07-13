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

const backgroundContractAgentName = "bg-contract-check"

var codingAgentBackgroundE2ECmd = &cobra.Command{
	Use:   "coding-agent-background-e2e",
	Short: "Run a real e2e test for coding-agent background delegation state",
	Long: `Runs a real end-to-end test through the live coding-agent-loop HTTP API.

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
			baseURL:        strings.TrimRight(codingAgentBackgroundE2EFlags.serverURL, "/"),
			token:          os.Getenv("MCP_API_TOKEN"),
			http:           &http.Client{Timeout: 30 * time.Second},
			agentMode:      "simple",
			selectedFolder: codingAgentBackgroundE2EFlags.selectedFolder,
			enabledServers: "api-bridge",
			timeout:        codingAgentBackgroundE2EFlags.timeout,
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
		defer func() {
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer stopCancel()
			_ = client.stopSession(stopCtx, sessionID)
		}()

		foregroundToken := "BG_CONTRACT_STARTED_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		finalToken := "BACKGROUND_CONTRACT_DONE_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		fmt.Printf("Running coding agent background e2e provider=%s model=%s session=%s\n", provider, model, sessionID)
		fmt.Printf("Selected folder: %s\n", codingAgentBackgroundE2EFlags.selectedFolder)

		query := fmt.Sprintf(`This is a background-agent transport contract test.

Use the delegate tool exactly once with:
- name: %s
- reasoning_level: low
- instruction:
  You are a background contract-test agent. Use execute_shell_command exactly once to run this command:
  sleep 15; printf %s
  After the command returns, finish with exactly this text and nothing else:
  %s
  Do not start any other agents. Do not ask questions.

After the delegate tool returns an agent_id, your own final response must include this exact token:
%s

Do not call any other tools after delegate returns.`, backgroundContractAgentName, finalToken, finalToken, foregroundToken)

		if _, err := client.startQuery(ctx, sessionID, provider, model, query); err != nil {
			return fmt.Errorf("background delegation query failed to start: %w", err)
		}

		if err := client.waitForUnifiedCompletionContains(ctx, sessionID, []string{foregroundToken}, 2*time.Minute); err != nil {
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

		if err := client.waitForBackgroundCompletionContains(ctx, sessionID, backgroundContractAgentName, finalToken, time.Minute); err != nil {
			return fmt.Errorf("background completion token was not present in events: %w", err)
		}
		fmt.Println("PASS events: background completion token observed")

		if err := client.waitForAutoNotificationContains(ctx, sessionID, backgroundContractAgentName, finalToken, 90*time.Second); err != nil {
			return fmt.Errorf("auto-notification did not carry background result: %w", err)
		}
		fmt.Println("PASS auto-notification: synthetic user message carried background result")

		if err := client.waitForNoRunningBackground(ctx, sessionID, 45*time.Second); err != nil {
			return fmt.Errorf("background running flag did not clear: %w", err)
		}
		fmt.Println("PASS active sessions: background running flag cleared")

		fmt.Println("PASS coding agent background e2e")
		return nil
	},
}

func init() {
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.serverURL, "server-url", "http://localhost:18743", "coding-agent-loop server URL")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.provider, "provider", "claude-code", "coding CLI provider")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.model, "model", "", "model ID; defaults to a low-cost model for the selected provider")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.sessionID, "session-id", "", "session ID to reuse; generated when omitted")
	codingAgentBackgroundE2ECmd.Flags().StringVar(&codingAgentBackgroundE2EFlags.selectedFolder, "selected-folder", "_users/default/Chats", "workspace-relative folder for the chat session")
	codingAgentBackgroundE2ECmd.Flags().DurationVar(&codingAgentBackgroundE2EFlags.timeout, "timeout", 6*time.Minute, "overall test timeout")
}

func (c *codingAgentChatE2EClient) waitForUnifiedCompletionContains(ctx context.Context, sessionID string, required []string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var lastFinal string
	var lastRaw string
	since := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, raw, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return err
		}
		lastRaw = raw
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
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
	return fmt.Errorf("timed out after %s; final=%q raw=%s", time.Since(start).Round(time.Millisecond), lastFinal, truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForBackgroundCompletionContains(ctx context.Context, sessionID, agentName, needle string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var lastRaw string
	since := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, raw, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return err
		}
		lastRaw = raw
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
		for _, event := range resp.Events {
			if fmt.Sprint(event["type"]) != "background_agent_completed" {
				continue
			}
			if eventPayloadString(event, "name") != agentName {
				continue
			}
			if eventPayloadString(event, "status") != "completed" {
				return fmt.Errorf("background agent %q completed with status %q; raw=%s", agentName, eventPayloadString(event, "status"), truncateE2E(raw, 1500))
			}
			if strings.Contains(eventPayloadString(event, "result"), needle) {
				return nil
			}
		}
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session ended with status %s before observing token; raw=%s", resp.SessionStatus, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; raw=%s", time.Since(start).Round(time.Millisecond), truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForAutoNotificationContains(ctx context.Context, sessionID, agentName, needle string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var lastRaw string
	since := 0
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		resp, raw, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return err
		}
		lastRaw = raw
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
		for _, event := range resp.Events {
			if fmt.Sprint(event["type"]) != "user_message" {
				continue
			}
			content := eventPayloadString(event, "content")
			if !strings.HasPrefix(strings.TrimSpace(content), "[AUTO-NOTIFICATION]") {
				continue
			}
			if !strings.Contains(content, agentName) {
				continue
			}
			if strings.Contains(content, needle) {
				return nil
			}
			return fmt.Errorf("auto-notification for %q did not include result token %q; content=%q", agentName, needle, truncateE2E(content, 1500))
		}
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return fmt.Errorf("session ended with status %s before observing auto-notification; raw=%s", resp.SessionStatus, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s; raw=%s", time.Since(start).Round(time.Millisecond), truncateE2E(lastRaw, 2000))
}

func (c *codingAgentChatE2EClient) waitForActiveBackgroundSummary(ctx context.Context, sessionID string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var last string
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
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
	return fmt.Errorf("timed out after %s; latest active session payload=%s", time.Since(start).Round(time.Millisecond), truncateE2E(last, 2000))
}

func (c *codingAgentChatE2EClient) waitForNoRunningBackground(ctx context.Context, sessionID string, timeout time.Duration) error {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var last string
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
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
	return fmt.Errorf("timed out after %s; latest active session payload=%s", time.Since(start).Round(time.Millisecond), truncateE2E(last, 2000))
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
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	var last string
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		tree, raw, err := c.getExecutionTree(ctx, sessionID)
		if err != nil {
			return err
		}
		last = raw
		nodes := findBackgroundContractNodes(tree.Root)
		if len(nodes) > 0 {
			hasNonTerminal := false
			for _, node := range nodes {
				if node.Status == wantStatus {
					return nil
				}
				if node.Status != "failed" && node.Status != "canceled" {
					hasNonTerminal = true
				}
			}
			if !hasNonTerminal {
				node := nodes[0]
				for _, candidate := range nodes {
					if candidate.Source == "background_agent_registry" {
						node = candidate
						break
					}
				}
				return fmt.Errorf("background node reached terminal state before %q: status=%s error=%q raw=%s", wantStatus, node.Status, node.Error, truncateE2E(raw, 2000))
			}
		}
		if wantStatus == "running" && tree.Summary.HasRunningBackgroundAgents {
			if node := findBackgroundRegistryNode(tree.Root); node != nil && node.Status == wantStatus {
				return nil
			}
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return err
		}
	}
	return fmt.Errorf("timed out after %s waiting for background status %q; latest tree=%s", time.Since(start).Round(time.Millisecond), wantStatus, truncateE2E(last, 3000))
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

func findBackgroundContractNodes(node *codingAgentExecutionTreeNode) []*codingAgentExecutionTreeNode {
	if node == nil {
		return nil
	}
	var matches []*codingAgentExecutionTreeNode
	if strings.Contains(strings.ToLower(node.Name), backgroundContractAgentName) {
		matches = append(matches, node)
	}
	for _, child := range node.Children {
		matches = append(matches, findBackgroundContractNodes(child)...)
	}
	return matches
}

func findBackgroundRegistryNode(node *codingAgentExecutionTreeNode) *codingAgentExecutionTreeNode {
	if node == nil {
		return nil
	}
	if node.Source == "background_agent_registry" && strings.Contains(strings.ToLower(node.Name), backgroundContractAgentName) {
		return node
	}
	for _, child := range node.Children {
		if found := findBackgroundRegistryNode(child); found != nil {
			return found
		}
	}
	return nil
}

func truncateE2E(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "...<truncated>"
}
