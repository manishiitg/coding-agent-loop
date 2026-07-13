package testing

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var agentBrowseStressE2EFlags struct {
	serverURL        string
	provider         string
	model            string
	selectedFolder   string
	sessionPrefix    string
	cdpPort          int
	workers          int
	timeout          time.Duration
	skipCDPPreflight bool
	strictFinal      bool
}

var agentBrowseStressE2ECmd = &cobra.Command{
	Use:   "agent-browse-stress-e2e",
	Short: "Stress test parallel coding agents sharing agent_browser CDP mode",
	Long: `Runs a live Builder stress e2e for parallel coding agents sharing one CDP browser.

The test starts a local deterministic HTTP server, then launches N Builder chat
sessions concurrently. Each session must use direct agent_browser tool calls,
create a uniquely labeled CDP tab, and prove from tool output that it read its
own marker page. This exercises the shared-CDP tab lock and tab ownership path.

By default the test treats final-answer formatting as provider-obedience signal
and reports it as a warning. Use --strict-final to fail when the coding agent
does not echo the requested sentinel/marker in its final answer.

Example:
  mcp-agent test agent-browse-stress-e2e \
    --server-url http://127.0.0.1:18743 \
	--provider codex-cli \
    --model gemini-3.1-flash-lite \
    --cdp-port 9222 \
    --workers 5`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if agentBrowseStressE2EFlags.workers <= 0 {
			return fmt.Errorf("--workers must be > 0")
		}

		ctx, cancel := context.WithTimeout(cmd.Context(), agentBrowseStressE2EFlags.timeout)
		defer cancel()

		provider := strings.TrimSpace(agentBrowseStressE2EFlags.provider)
		if provider == "" {
			provider = "codex-cli"
		}
		model := strings.TrimSpace(agentBrowseStressE2EFlags.model)
		if model == "" {
			model = defaultCodingAgentE2EModel(provider)
		}
		sessionPrefix := strings.TrimSpace(agentBrowseStressE2EFlags.sessionPrefix)
		if sessionPrefix == "" {
			sessionPrefix = fmt.Sprintf("agent-browse-stress-%s-%d", strings.ReplaceAll(provider, "-", ""), time.Now().UnixNano())
		}

		client := &codingAgentChatE2EClient{
			baseURL: strings.TrimRight(agentBrowseStressE2EFlags.serverURL, "/"),
			token:   os.Getenv("MCP_API_TOKEN"),
			http:    &http.Client{Timeout: 30 * time.Second},
		}

		if !agentBrowseStressE2EFlags.skipCDPPreflight {
			if err := preflightAgentBrowserCDP(ctx, agentBrowseStressE2EFlags.cdpPort); err != nil {
				return err
			}
			fmt.Printf("PASS CDP preflight: Chrome DevTools and agent-browser are reachable on port %d\n", agentBrowseStressE2EFlags.cdpPort)
		}

		cases := makeAgentBrowseStressCases(agentBrowseStressE2EFlags.workers)
		testServer := newAgentBrowseStressServer(cases)
		defer testServer.Close()
		for i := range cases {
			cases[i].targetURL = testServer.URL + "/case/" + cases[i].marker
		}

		fmt.Printf("Running agent_browser stress e2e provider=%s model=%s workers=%d cdp_port=%d\n", provider, model, len(cases), agentBrowseStressE2EFlags.cdpPort)

		results := make([]agentBrowseStressResult, len(cases))
		var wg sync.WaitGroup
		for i := range cases {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				results[idx] = runAgentBrowseStressWorker(ctx, client, provider, model, sessionPrefix, cases[idx], cases)
			}(i)
		}
		wg.Wait()

		sort.Slice(results, func(i, j int) bool { return results[i].worker < results[j].worker })
		var failures []string
		for _, result := range results {
			if result.err != nil {
				failures = append(failures, fmt.Sprintf("worker %02d session=%s: %v", result.worker, result.sessionID, result.err))
				continue
			}
			if result.finalContractErr != nil {
				message := fmt.Sprintf("worker %02d session=%s: final-answer contract warning: %v", result.worker, result.sessionID, result.finalContractErr)
				if agentBrowseStressE2EFlags.strictFinal {
					failures = append(failures, message)
					continue
				}
				fmt.Printf("WARN %s\n", message)
			}
			fmt.Printf("PASS worker %02d session=%s query=%s marker=%s\n", result.worker, result.sessionID, result.queryID, result.marker)
		}
		if len(failures) > 0 {
			return fmt.Errorf("agent_browser stress e2e failed (%d/%d):\n%s", len(failures), len(results), strings.Join(failures, "\n"))
		}

		fmt.Println("PASS agent_browser stress e2e")
		return nil
	},
}

func init() {
	agentBrowseStressE2ECmd.Flags().StringVar(&agentBrowseStressE2EFlags.serverURL, "server-url", "http://127.0.0.1:18743", "coding-agent-loop server URL")
	agentBrowseStressE2ECmd.Flags().StringVar(&agentBrowseStressE2EFlags.provider, "provider", "codex-cli", "coding CLI provider; defaults to codex-cli")
	agentBrowseStressE2ECmd.Flags().StringVar(&agentBrowseStressE2EFlags.model, "model", "", "model ID; defaults to the provider-specific Builder coding-agent E2E model")
	agentBrowseStressE2ECmd.Flags().StringVar(&agentBrowseStressE2EFlags.selectedFolder, "selected-folder", "_users/default/Chats", "workspace-relative folder for the chat sessions")
	agentBrowseStressE2ECmd.Flags().StringVar(&agentBrowseStressE2EFlags.sessionPrefix, "session-prefix", "", "session ID prefix; generated when omitted")
	agentBrowseStressE2ECmd.Flags().IntVar(&agentBrowseStressE2EFlags.cdpPort, "cdp-port", 9222, "Chrome DevTools Protocol port to test")
	agentBrowseStressE2ECmd.Flags().IntVar(&agentBrowseStressE2EFlags.workers, "workers", 3, "number of parallel Builder chat sessions")
	agentBrowseStressE2ECmd.Flags().DurationVar(&agentBrowseStressE2EFlags.timeout, "timeout", 8*time.Minute, "overall stress test timeout")
	agentBrowseStressE2ECmd.Flags().BoolVar(&agentBrowseStressE2EFlags.skipCDPPreflight, "skip-cdp-preflight", false, "skip local Chrome CDP and agent-browser smoke checks")
	agentBrowseStressE2ECmd.Flags().BoolVar(&agentBrowseStressE2EFlags.strictFinal, "strict-final", false, "fail if the coding agent does not echo the requested final sentinel/marker")
}

type agentBrowseStressCase struct {
	worker         int
	scenario       string
	marker         string
	sentinel       string
	tabLabel       string
	browserSession string
	targetURL      string
}

type agentBrowseStressResult struct {
	worker           int
	sessionID        string
	queryID          string
	marker           string
	finalContractErr error
	err              error
}

func makeAgentBrowseStressCases(workers int) []agentBrowseStressCase {
	cases := make([]agentBrowseStressCase, 0, workers)
	for i := 0; i < workers; i++ {
		token := strings.ReplaceAll(uuid.NewString(), "-", "")
		cases = append(cases, agentBrowseStressCase{
			worker:         i,
			scenario:       agentBrowseStressScenarioForWorker(i),
			marker:         fmt.Sprintf("AGENT_BROWSER_STRESS_MARKER_%02d_%s", i, token[:12]),
			sentinel:       "AGENT_BROWSER_STRESS_OK_" + token[12:28],
			tabLabel:       fmt.Sprintf("stress%d%s", i, token[24:32]),
			browserSession: "agent_browse_stress_" + token,
		})
	}
	return cases
}

func agentBrowseStressScenarioForWorker(worker int) string {
	switch worker % 4 {
	case 0:
		return "snapshot"
	case 1:
		return "get-text"
	case 2:
		return "eval"
	default:
		return "tab-list-snapshot"
	}
}

func newAgentBrowseStressServer(cases []agentBrowseStressCase) *httptest.Server {
	markers := make(map[string]agentBrowseStressCase, len(cases))
	for _, tc := range cases {
		markers[tc.marker] = tc
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		marker := strings.TrimPrefix(r.URL.Path, "/case/")
		tc, ok := markers[marker]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html>
<html>
<head><title>%s</title></head>
<body>
<main>
<h1 id="headline">Stress headline %02d</h1>
<button id="marker" type="button">%s</button>
<p>This page belongs only to worker %02d.</p>
</main>
</body>
</html>`, html.EscapeString(tc.marker), tc.worker, html.EscapeString(tc.marker), tc.worker)
	}))
}

func runAgentBrowseStressWorker(ctx context.Context, client *codingAgentChatE2EClient, provider, model, sessionPrefix string, tc agentBrowseStressCase, allCases []agentBrowseStressCase) agentBrowseStressResult {
	result := agentBrowseStressResult{
		worker:    tc.worker,
		sessionID: fmt.Sprintf("%s-%02d", sessionPrefix, tc.worker),
		marker:    tc.marker,
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.stopSession(stopCtx, result.sessionID)
	}()

	readStep, expectedCommand := agentBrowseStressReadStep(tc)
	query := fmt.Sprintf(`This is Builder agent_browser shared-CDP stress worker %02d.

Use only the Builder agent_browser MCP tool directly. Do not call execute_shell_command. Do not run curl. Do not use raw CDP or Playwright directly.

Scenario: %s

Task:
1. Create a new CDP tab with label %q by calling command "tab" with args exactly ["new", "--label", "%s", "%s"]. Do not reorder these args.
2. %s
3. Find the exact visible marker text %q.

When done, reply with exactly three lines:
%s
marker: %s
headline: <the visible h1 text>

Use agent_browser session: %q`, tc.worker, tc.scenario, tc.tabLabel, tc.tabLabel, tc.targetURL, readStep, tc.marker, tc.sentinel, tc.marker, tc.browserSession)

	queryID, err := client.startAgentBrowseQueryWithOptions(ctx, result.sessionID, agentBrowseQueryOptions{
		provider:       provider,
		model:          model,
		query:          query,
		selectedFolder: agentBrowseStressE2EFlags.selectedFolder,
		cdpPort:        agentBrowseStressE2EFlags.cdpPort,
	})
	if err != nil {
		result.err = fmt.Errorf("query failed to start: %w", err)
		return result
	}
	result.queryID = queryID

	final, raw, events, err := client.waitForAgentBrowseCompletion(ctx, result.sessionID, agentBrowseStressE2EFlags.timeout)
	if err != nil {
		result.err = fmt.Errorf("turn did not complete: %w", err)
		return result
	}
	if !eventsProveProvider(events, provider) {
		result.err = fmt.Errorf("event stream did not prove provider %q was used", provider)
		return result
	}
	evidence := collectAgentBrowserEvidence(events, tc.targetURL, tc.tabLabel)
	readEvidence := collectAgentBrowserStressReadEvidence(events, tc, allCases)
	if evidence.shellCalls > 0 {
		result.err = fmt.Errorf("used execute_shell_command instead of direct agent_browser tool calls")
		return result
	}
	if evidence.agentBrowserCalls == 0 {
		result.err = fmt.Errorf("did not include an agent_browser tool call; final=%q", final)
		return result
	}
	if evidence.navigationCalls == 0 {
		result.err = fmt.Errorf("did not include agent_browser navigation to %s; commands=%v", tc.targetURL, evidence.commands)
		return result
	}
	if evidence.readCalls == 0 {
		result.err = fmt.Errorf("did not include agent_browser page read call; commands=%v", evidence.commands)
		return result
	}
	if expectedCommand != "" && !evidence.hasCommand(expectedCommand) {
		result.err = fmt.Errorf("did not include expected %q read command for scenario %s; commands=%v", expectedCommand, tc.scenario, evidence.commands)
		return result
	}
	if containsAgentBrowserTimeout(raw) {
		result.err = fmt.Errorf("event stream contains agent_browser timeout/failure evidence")
		return result
	}
	if !readEvidence.markerFound {
		result.err = fmt.Errorf("agent_browser tool output did not contain this worker marker %q; commands=%v", tc.marker, evidence.commands)
		return result
	}
	if readEvidence.otherMarker != "" {
		result.err = fmt.Errorf("agent_browser tool output leaked another worker marker %q", readEvidence.otherMarker)
		return result
	}
	if !strings.Contains(final, tc.sentinel) || !strings.Contains(final, tc.marker) {
		result.finalContractErr = fmt.Errorf("completion missing sentinel/marker; final=%q", final)
	}
	for _, other := range allCases {
		if other.marker != tc.marker && strings.Contains(final, other.marker) {
			result.finalContractErr = fmt.Errorf("completion leaked another worker marker %q; final=%q", other.marker, final)
		}
	}
	return result
}

type agentBrowseStressReadEvidence struct {
	markerFound bool
	otherMarker string
}

func collectAgentBrowserStressReadEvidence(events []map[string]interface{}, tc agentBrowseStressCase, allCases []agentBrowseStressCase) agentBrowseStressReadEvidence {
	var evidence agentBrowseStressReadEvidence
	readCallIDs := make(map[string]bool)
	for _, event := range events {
		if fmt.Sprint(event["type"]) != "tool_call_start" {
			continue
		}
		data := nestedEventData(event)
		if fmt.Sprint(data["tool_name"]) != "agent_browser" {
			continue
		}
		toolArgs, ok := parseAgentBrowserToolArgs(data)
		if !ok || !isAgentBrowserReadCall(strings.ToLower(strings.TrimSpace(toolArgs.Command))) {
			continue
		}
		if toolCallID := eventString(data, "tool_call_id"); toolCallID != "" {
			readCallIDs[toolCallID] = true
		}
	}

	for _, event := range events {
		if fmt.Sprint(event["type"]) != "tool_call_end" {
			continue
		}
		data := nestedEventData(event)
		if fmt.Sprint(data["tool_name"]) != "agent_browser" {
			continue
		}
		toolCallID := eventString(data, "tool_call_id")
		if !readCallIDs[toolCallID] {
			continue
		}
		result := fmt.Sprint(data["result"])
		if strings.Contains(result, tc.marker) {
			evidence.markerFound = true
		}
		for _, other := range allCases {
			if other.marker != tc.marker && strings.Contains(result, other.marker) {
				evidence.otherMarker = other.marker
				return evidence
			}
		}
	}
	return evidence
}

func eventString(data map[string]interface{}, key string) string {
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func agentBrowseStressReadStep(tc agentBrowseStressCase) (string, string) {
	switch tc.scenario {
	case "get-text":
		return fmt.Sprintf(`Read that exact labeled tab by calling command "get" with args exactly ["tab", "%s", "text", "body"].`, tc.tabLabel), "get"
	case "eval":
		return fmt.Sprintf(`Read that exact labeled tab by calling command "eval" with args exactly ["tab", "%s", "document.querySelector('#marker').textContent + '\n' + document.querySelector('#headline').textContent"].`, tc.tabLabel), "eval"
	case "tab-list-snapshot":
		return fmt.Sprintf(`First list tabs by calling command "tab" with args exactly ["list"], then read the exact labeled tab by calling command "snapshot" with args exactly ["tab", "%s", "-i"].`, tc.tabLabel), "snapshot"
	default:
		return fmt.Sprintf(`Read that exact labeled tab by calling command "snapshot" with args exactly ["tab", "%s", "-i"].`, tc.tabLabel), "snapshot"
	}
}
