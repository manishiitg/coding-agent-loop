package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var workflowAutoNotificationE2EFlags struct {
	serverURL     string
	provider      string
	model         string
	sessionID     string
	workspaceDocs string
	timeout       time.Duration
	keepFixture   bool
}

var workflowAutoNotificationE2ECmd = &cobra.Command{
	Use:   "workflow-auto-notification-e2e",
	Short: "Run a real workflow AUTO-notification e2e against a live builder server",
	Long: `Runs a real end-to-end check through the live mcp-agent-builder-go HTTP API.

This test:
1. Creates a real temporary workflow under workspace-docs/Workflow.
2. Starts a real multi-agent chat turn and asks that agent to call run_workflow.
3. Polls /api/sessions/{session_id}/events until a [AUTO-NOTIFICATION]
   user_message for workflow start appears.
4. Stops the chat session with cancelAgents=true.

It intentionally does not call runWorkflowInternal directly and does not use
the private session-scoped custom-tool HTTP endpoint.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := strings.TrimSpace(os.Getenv("MCP_API_TOKEN"))

		provider := strings.TrimSpace(workflowAutoNotificationE2EFlags.provider)
		model := strings.TrimSpace(workflowAutoNotificationE2EFlags.model)
		if model == "" {
			model = defaultCodingAgentE2EModel(provider)
		}
		if provider == "" || model == "" {
			return fmt.Errorf("provider and model are required")
		}

		sessionID := strings.TrimSpace(workflowAutoNotificationE2EFlags.sessionID)
		if sessionID == "" {
			sessionID = fmt.Sprintf("workflow-auto-notification-e2e-%s", strings.ReplaceAll(uuid.NewString(), "-", ""))
		}

		timeout := workflowAutoNotificationE2EFlags.timeout
		if timeout <= 0 {
			timeout = 8 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		workspaceDocs, err := resolveWorkflowAutoNotificationWorkspaceDocs()
		if err != nil {
			return err
		}
		relWorkflow, cleanup, err := createWorkflowAutoNotificationFixture(workspaceDocs, workflowAutoNotificationE2EFlags.keepFixture, provider, model)
		if err != nil {
			return err
		}
		defer cleanup()

		client := &codingAgentChatE2EClient{
			baseURL:        strings.TrimRight(workflowAutoNotificationE2EFlags.serverURL, "/"),
			token:          token,
			http:           &http.Client{Timeout: 90 * time.Second},
			agentMode:      "multi-agent",
			selectedFolder: "_users/default/Chats",
			enabledServers: "api-bridge",
			timeout:        timeout,
		}

		fmt.Printf("Running workflow auto-notification e2e provider=%s model=%s session=%s workflow=%s\n", provider, model, sessionID, relWorkflow)

		query := fmt.Sprintf(`Use the MCP api-bridge execute_shell_command tool exactly once to run this exact curl command:

curl -sS -X POST "$MCP_API_URL/tools/custom/run_workflow" -H "Authorization: Bearer $MCP_API_TOKEN" -H "Content-Type: application/json" -d '{"workflow_path":%q,"group_name":"default","instructions":"E2E workflow start auto-notification check. Keep this run minimal."}'

After the command returns, reply exactly RUN_WORKFLOW_TOOL_STARTED. Do not call any other workflow tool. Do not ask a question.`, relWorkflow)
		queryID, err := client.startQuery(ctx, sessionID, provider, model, query)
		if err != nil {
			return fmt.Errorf("start main agent run_workflow turn: %w", err)
		}
		fmt.Printf("Started main agent query=%s; waiting for workflow AUTO notification\n", queryID)

		agentID, err := client.waitForWorkflowStartAutoNotification(ctx, sessionID, relWorkflow, 3*time.Minute)
		if err != nil {
			return err
		}
		fmt.Printf("Observed workflow start for agent_id=%s\n", agentID)

		_ = client.stopSession(ctx, sessionID)

		fmt.Println("PASS workflow auto-notification e2e")
		return nil
	},
}

func init() {
	workflowAutoNotificationE2ECmd.Flags().StringVar(&workflowAutoNotificationE2EFlags.serverURL, "server-url", "http://localhost:18743", "mcp-agent-builder-go server URL")
	workflowAutoNotificationE2ECmd.Flags().StringVar(&workflowAutoNotificationE2EFlags.provider, "provider", "codex-cli", "provider used for the main multi-agent chat turn")
	workflowAutoNotificationE2ECmd.Flags().StringVar(&workflowAutoNotificationE2EFlags.model, "model", "", "model ID; defaults to the provider-specific E2E model")
	workflowAutoNotificationE2ECmd.Flags().StringVar(&workflowAutoNotificationE2EFlags.sessionID, "session-id", "", "session ID to reuse; generated when omitted")
	workflowAutoNotificationE2ECmd.Flags().StringVar(&workflowAutoNotificationE2EFlags.workspaceDocs, "workspace-docs", "", "absolute path to workspace-docs; defaults to WORKSPACE_DOCS_PATH or ../workspace-docs")
	workflowAutoNotificationE2ECmd.Flags().DurationVar(&workflowAutoNotificationE2EFlags.timeout, "timeout", 8*time.Minute, "overall test timeout")
	workflowAutoNotificationE2ECmd.Flags().BoolVar(&workflowAutoNotificationE2EFlags.keepFixture, "keep-fixture", false, "keep the temporary workflow fixture for debugging")
}

func resolveWorkflowAutoNotificationWorkspaceDocs() (string, error) {
	if value := strings.TrimSpace(workflowAutoNotificationE2EFlags.workspaceDocs); value != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("--workspace-docs must be absolute, got %q", value)
		}
		return value, nil
	}
	if value := strings.TrimSpace(os.Getenv("WORKSPACE_DOCS_PATH")); value != "" {
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("WORKSPACE_DOCS_PATH must be absolute, got %q", value)
		}
		return value, nil
	}
	candidate := filepath.Clean("../workspace-docs")
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(abs); statErr == nil && info.IsDir() {
		return abs, nil
	}
	return "", fmt.Errorf("workspace-docs not found; pass --workspace-docs or set WORKSPACE_DOCS_PATH")
}

func createWorkflowAutoNotificationFixture(workspaceDocs string, keep bool, provider, model string) (string, func(), error) {
	shortID := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	relWorkflow := "Workflow/_e2e_auto_notification_" + shortID
	absWorkflow := filepath.Join(workspaceDocs, filepath.FromSlash(relWorkflow))
	cleanup := func() {}
	if !keep {
		cleanup = func() {
			_ = os.RemoveAll(absWorkflow)
		}
	}

	if err := os.MkdirAll(filepath.Join(absWorkflow, "planning"), 0o755); err != nil {
		return "", cleanup, err
	}
	if err := os.MkdirAll(filepath.Join(absWorkflow, "variables"), 0o755); err != nil {
		return "", cleanup, err
	}

	noGlobalSecrets := []string{}
	now := time.Now().UTC().Format(time.RFC3339)
	fixtureModel := map[string]interface{}{
		"provider": provider,
		"model_id": model,
	}
	manifest := map[string]interface{}{
		"schema_version": 1,
		"id":             "wf_auto_notification_" + shortID,
		"label":          "E2E Auto Notification Workflow",
		"objective":      "Exercise workflow start auto-notification wiring.",
		"capabilities": map[string]interface{}{
			"selected_servers":             []string{},
			"selected_tools":               []string{},
			"selected_skills":              []string{},
			"selected_secrets":             []string{},
			"selected_global_secret_names": noGlobalSecrets,
			"browser_mode":                 "none",
			"use_code_execution_mode":      false,
			"llm_config": map[string]interface{}{
				"schema_version":  2,
				"mode":            "explicit",
				"builder_llm":     fixtureModel,
				"maintenance_llm": fixtureModel,
				"pulse_llm":       fixtureModel,
				"tiered_config": map[string]interface{}{
					"tier_1": fixtureModel,
					"tier_2": fixtureModel,
					"tier_3": fixtureModel,
				},
			},
		},
		"execution_defaults": map[string]interface{}{
			"always_use_same_run": true,
			"disable_learning":    true,
			"workshop_mode":       "run",
		},
		"ownership":  map[string]interface{}{},
		"schedules":  []interface{}{},
		"created_at": now,
		"updated_at": now,
	}
	plan := map[string]interface{}{
		"steps": []map[string]interface{}{
			{
				"type":                 "regular",
				"id":                   "step-auto-notification",
				"title":                "Auto notification smoke step",
				"description":          "Reply exactly WORKFLOW_AUTO_NOTIFICATION_STEP_OK and stop.",
				"context_dependencies": []string{},
				"context_output":       "auto_notification.json",
			},
		},
	}
	variables := map[string]interface{}{
		"variables":       []interface{}{},
		"groups":          []map[string]interface{}{{"name": "default", "values": map[string]string{}, "enabled": true}},
		"extraction_date": now,
	}

	if err := writeJSONFile(filepath.Join(absWorkflow, "workflow.json"), manifest); err != nil {
		cleanup()
		return "", cleanup, err
	}
	if err := writeJSONFile(filepath.Join(absWorkflow, "planning", "plan.json"), plan); err != nil {
		cleanup()
		return "", cleanup, err
	}
	if err := writeJSONFile(filepath.Join(absWorkflow, "variables", "variables.json"), variables); err != nil {
		cleanup()
		return "", cleanup, err
	}
	return relWorkflow, cleanup, nil
}

func writeJSONFile(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func (c *codingAgentChatE2EClient) waitForWorkflowStartAutoNotification(ctx context.Context, sessionID, workflowPath string, timeout time.Duration) (string, error) {
	start := time.Now()
	deadline := e2eDeadline(ctx, timeout)
	since := 0
	var lastRaw string
	var startedAgentID string
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return startedAgentID, err
		}
		resp, raw, err := c.getEventsSince(ctx, sessionID, since)
		if err != nil {
			return startedAgentID, err
		}
		lastRaw = raw
		since = advanceE2ECursor(since, resp.LastProcessedIndex)
		for _, event := range resp.Events {
			eventType := fmt.Sprint(event["type"])
			switch eventType {
			case "background_agent_started":
				agentID := strings.TrimSpace(eventPayloadString(event, "agent_id"))
				if agentID != "" {
					startedAgentID = agentID
				}
				continue
			case "user_message":
			default:
				continue
			}

			content := eventPayloadString(event, "content")
			trimmed := strings.TrimSpace(content)
			if !strings.HasPrefix(trimmed, "[AUTO-NOTIFICATION]") {
				continue
			}
			if !strings.Contains(strings.ToLower(content), "started") {
				return startedAgentID, fmt.Errorf("workflow auto-notification did not say started: %s", truncateE2E(content, 1500))
			}
			workflowSpace := filepath.Base(filepath.Clean(workflowPath))
			if !strings.Contains(content, workflowPath) && !strings.Contains(content, workflowSpace) {
				return startedAgentID, fmt.Errorf("workflow auto-notification did not include workflow path %s: %s", workflowPath, truncateE2E(content, 1500))
			}
			notificationAgentID := extractWorkflowAutoNotificationAgentID(content)
			if startedAgentID != "" && notificationAgentID != "" && notificationAgentID != startedAgentID {
				return startedAgentID, fmt.Errorf("workflow start event agent_id %s did not match AUTO notification agent_id %s: %s", startedAgentID, notificationAgentID, truncateE2E(content, 1500))
			}
			if startedAgentID == "" {
				startedAgentID = notificationAgentID
			}
			if startedAgentID == "" {
				return "", fmt.Errorf("workflow AUTO notification did not include an agent ID: %s", truncateE2E(content, 1500))
			}
			fmt.Printf("Observed workflow AUTO notification: %s\n", truncateE2E(content, 800))
			return startedAgentID, nil
		}
		// Some CLI-backed turns can briefly report the HTTP session as completed
		// before the provider finishes dispatching tool calls. Keep the workflow
		// fixture alive and wait for the actual background_agent_started event
		// instead of treating that transient status as proof the workflow was not
		// launched.
		if resp.SessionStatus == "error" || resp.SessionStatus == "stopped" {
			return startedAgentID, fmt.Errorf("session ended with status %s before workflow start AUTO notification; raw=%s", resp.SessionStatus, truncateE2E(lastRaw, 2000))
		}
		if err := sleepContext(ctx, time.Second); err != nil {
			return startedAgentID, err
		}
	}
	return startedAgentID, fmt.Errorf("timed out after %s waiting for workflow start AUTO notification; started_agent_id=%s raw=%s", time.Since(start).Round(time.Millisecond), startedAgentID, truncateE2E(lastRaw, 2500))
}

func extractWorkflowAutoNotificationAgentID(content string) string {
	const marker = "(ID:"
	idx := strings.Index(content, marker)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(marker):]
	end := strings.Index(rest, ")")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}
