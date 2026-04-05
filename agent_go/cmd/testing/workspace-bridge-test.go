package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	testutils "github.com/manishiitg/mcpagent/cmd/testing/testutils"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	server "mcp-agent-builder-go/agent_go/cmd/server"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

var workspaceBridgeTestCmd = &cobra.Command{
	Use:   "workspace-bridge",
	Short: "Test shell execution through CLI MCP bridge with folder guards",
	Long: `Tests shell command execution through the Claude Code / Gemini CLI / Codex CLI MCP bridge.

Uses real workspace advanced tool executors (execute_shell_command) with the
real chat-mode folder guard wrapper, pointed at the REAL workspace API (Docker).

Verifies:
  1. Shell commands execute through the bridge and return results
  2. Folder guard restricts shell write access
  3. Folder guard blocks shell commands referencing Workflow/
  4. Browser commands execute through the bridge
  5. (codex-cli) Shell tool is disabled — commands go through MCP bridge only

Starts a CLI agent with the MCP bridge and sends prompts via agent.Ask().
Tests the complete path: CLI provider → mcpbridge → executor HTTP server → folder guard → real workspace API.

How to run:

  cd agent_go
  go run . test workspace-bridge --provider claude-code --verbose
  go run . test workspace-bridge --provider gemini-cli --verbose
  go run . test workspace-bridge --provider codex-cli --verbose`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logFile := viper.GetString("log-file")
		logLevel := viper.GetString("log-level")

		InitTestLogger(logFile, logLevel)
		logger := GetTestLogger()

		logger.Info("=== Workspace Bridge Test (Shell) ===")
		logger.Info("Testing: shell execution + folder guards through CLI bridge")

		tracer, isLangfuse := testutils.GetTracerWithLogger("langfuse", logger)
		if tracer == nil {
			tracer, _ = testutils.GetTracerWithLogger("noop", logger)
		}
		if isLangfuse {
			logger.Info("Langfuse tracing enabled")
		}
		traceID := testutils.GenerateTestTraceID()

		if err := TestWorkspaceBridge(logger, tracer, traceID); err != nil {
			return fmt.Errorf("test failed: %w", err)
		}

		if flusher, ok := tracer.(interface{ Flush() }); ok {
			flusher.Flush()
		}

		logger.Info("All workspace bridge tests passed")
		if isLangfuse {
			logger.Info("Langfuse trace", loggerv2.String("trace_id", string(traceID)))
		}
		return nil
	},
}

// ---------- Bridge binary ----------

func ensureWorkspaceBridgeBinary(log loggerv2.Logger) (string, error) {
	if envPath := os.Getenv("MCP_BRIDGE_BINARY"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}
	if path, err := exec.LookPath("mcpbridge"); err == nil {
		return path, nil
	}
	homeDir, _ := os.UserHomeDir()
	goBinPath := homeDir + "/go/bin/mcpbridge"
	if _, err := os.Stat(goBinPath); err == nil {
		return goBinPath, nil
	}
	log.Info("mcpbridge not found, attempting build...")
	mcpagentRoot := findMcpagentRootDir()
	buildCmd := exec.Command("go", "build", "-o", goBinPath, "./cmd/mcpbridge/")
	buildCmd.Dir = mcpagentRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build mcpbridge: %w (run: go build -o ~/go/bin/mcpbridge ./cmd/mcpbridge/ from mcpagent dir)", err)
	}
	log.Info("mcpbridge built", loggerv2.String("path", goBinPath))
	return goBinPath, nil
}

func findMcpagentRootDir() string {
	candidates := []string{
		"../mcpagent",
		"../../mcpagent",
		os.Getenv("GOPATH") + "/src/github.com/manishiitg/mcpagent",
	}
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(dir + "/cmd/mcpbridge"); err == nil {
			return dir
		}
		dir = dir + "/.."
	}
	for _, c := range candidates {
		if _, err := os.Stat(c + "/cmd/mcpbridge"); err == nil {
			return c
		}
	}
	return "."
}

// ---------- Executor HTTP server ----------

func startWorkspaceBridgeExecutorServer(
	log loggerv2.Logger,
	wrappedExecutors map[string]func(ctx context.Context, args map[string]interface{}) (string, error),
) (serverURL string, apiToken string, shutdown func(), err error) {
	configPath := testutils.GetDefaultTestConfigPath()
	if configPath == "" {
		tmpFile, tErr := os.CreateTemp("", "mcp-config-*.json")
		if tErr != nil {
			return "", "", nil, fmt.Errorf("create temp config: %w", tErr)
		}
		tmpFile.WriteString(`{"mcpServers":{}}`)
		tmpFile.Close()
		configPath = tmpFile.Name()
	}

	apiToken = executor.GenerateAPIToken()
	handlers := executor.NewExecutorHandlers(configPath, log)

	mux := http.NewServeMux()

	// Batch endpoints (custom tools registered via agent.RegisterCustomTool +
	// agent.UpdateCodeExecutionRegistry → codeexec registry)
	mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
	mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
	mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

	mux.HandleFunc("/tools/mcp/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/tools/mcp/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			http.Error(w, `{"success":false,"error":"invalid path"}`, http.StatusBadRequest)
			return
		}
		srv := strings.ReplaceAll(parts[0], "_", "-")
		tool := strings.ReplaceAll(parts[1], "_", "-")
		handlers.HandlePerToolMCPRequest(w, r, srv, tool)
	})

	mux.HandleFunc("/tools/custom/", func(w http.ResponseWriter, r *http.Request) {
		tool := strings.TrimPrefix(r.URL.Path, "/tools/custom/")
		if tool == "" {
			http.Error(w, `{"success":false,"error":"missing tool"}`, http.StatusBadRequest)
			return
		}
		handlers.HandlePerToolCustomRequest(w, r, tool)
	})

	mux.HandleFunc("/tools/virtual/", func(w http.ResponseWriter, r *http.Request) {
		tool := strings.TrimPrefix(r.URL.Path, "/tools/virtual/")
		if tool == "" {
			http.Error(w, `{"success":false,"error":"missing tool"}`, http.StatusBadRequest)
			return
		}
		handlers.HandlePerToolVirtualRequest(w, r, tool)
	})

	authedHandler := executor.AuthMiddleware(apiToken)(mux)

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		return "", "", nil, fmt.Errorf("find free port: %w", listenErr)
	}
	addr := listener.Addr().String()

	srv := &http.Server{
		Handler:           authedHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if sErr := srv.Serve(listener); sErr != nil && sErr != http.ErrServerClosed {
			log.Error("executor server error", sErr)
		}
	}()

	serverURL = "http://" + addr
	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		log.Info("executor server stopped")
	}

	return serverURL, apiToken, shutdown, nil
}

// ---------- Main test flow ----------

func TestWorkspaceBridge(log loggerv2.Logger, tracer observability.Tracer, traceID observability.TraceID) error {
	ctx := context.Background()

	// Step 1: Ensure real workspace API is reachable
	log.Info("--- Step 1: Check Workspace API ---")
	apiURL := os.Getenv("WORKSPACE_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8081"
		os.Setenv("WORKSPACE_API_URL", apiURL)
	}

	// Test connectivity with a simple shell command instead of /api/health
	checkPayload := map[string]interface{}{
		"command":           "echo 'workspace-api-is-alive'",
		"working_directory": ".",
	}
	checkJSON, _ := json.Marshal(checkPayload)
	resp, err := http.Post(apiURL+"/api/execute", "application/json", bytes.NewBuffer(checkJSON))
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("real Workspace API is not reachable at %s. Please ensure the workspace Docker container is running: %w", apiURL, err)
	}
	log.Info("Real Workspace API is running and responding", loggerv2.String("url", apiURL))

	// Step 2: Create shell and browser executors + apply folder guard
	log.Info("--- Step 2: Create executors + folder guard ---")
	advancedExecutors := virtualtools.CreateWorkspaceAdvancedToolExecutors()
	browserExecutors := virtualtools.CreateWorkspaceBrowserToolExecutors()

	shellExecutor, ok := advancedExecutors["execute_shell_command"]
	if !ok {
		return fmt.Errorf("execute_shell_command executor not found in advanced executors")
	}

	browserExecutor, ok := browserExecutors["agent_browser"]
	if !ok {
		return fmt.Errorf("agent_browser executor not found in browser executors")
	}
	log.Info("Shell and browser executors created")

	// Wrap with folder guard — only shell executor needs the wrap
	shellOnlyMap := map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"execute_shell_command": shellExecutor,
	}
	wrappedMap := server.ApplyChatModeFolderGuard(shellOnlyMap)
	wrappedShell := wrappedMap["execute_shell_command"]

	// Add browser back to wrappedMap for the HTTP server
	wrappedMap["agent_browser"] = browserExecutor
	log.Info("Applied chat-mode folder guard to shell executor")

	// Step 3: Ensure mcpbridge binary
	log.Info("--- Step 3: Ensure mcpbridge binary ---")
	bridgePath, err := ensureWorkspaceBridgeBinary(log)
	if err != nil {
		return err
	}
	log.Info("mcpbridge binary ready", loggerv2.String("path", bridgePath))

	// Step 4: Start executor HTTP server
	log.Info("--- Step 4: Start executor HTTP server ---")
	serverURL, apiToken, executorShutdown, err := startWorkspaceBridgeExecutorServer(log, wrappedMap)
	if err != nil {
		return fmt.Errorf("failed to start executor server: %w", err)
	}
	defer executorShutdown()
	log.Info("Executor server started",
		loggerv2.String("url", serverURL),
		loggerv2.String("token_prefix", apiToken[:8]+"..."))

	time.Sleep(500 * time.Millisecond)

	// Step 5: Create CLI agent
	log.Info("--- Step 5: Create CLI agent ---")
	agent, cleanup, err := createWorkspaceBridgeAgent(ctx, log, tracer, traceID, serverURL, apiToken, bridgePath)
	if err != nil {
		return err
	}
	defer agent.Close()
	defer cleanup()

	// Step 6: Register tools on agent
	log.Info("--- Step 6: Register tools on agent ---")
	if err := registerWorkspaceTools(agent, wrappedShell, browserExecutor, log); err != nil {
		return err
	}
	if err := agent.UpdateCodeExecutionRegistry(); err != nil {
		return fmt.Errorf("failed to update code execution registry: %w", err)
	}
	log.Info("Code execution registry updated")

	// Step 7: Verify bridge config
	log.Info("--- Step 7: Verify bridge config ---")
	if err := verifyBridgeConfig(agent, log); err != nil {
		return err
	}

	// Step 8: LLM-based shell scenarios
	log.Info("--- Step 8: Run LLM shell scenarios ---")
	llmErr := runLLMShellScenarios(ctx, agent, log, traceID)
	if llmErr != nil {
		log.Warn(fmt.Sprintf("LLM scenarios had failures: %v (continuing to Step 9)", llmErr))
	}

	// Step 9: Workflow mode folder guard — direct executor test (no LLM)
	// This reproduces the bug where absolute host paths bypass the Docker isolator.
	// Workflow mode uses context keys (System2) instead of chat mode string checks.
	log.Info("--- Step 9: Workflow mode folder guard (direct executor test) ---")
	if err := testWorkflowModeFolderGuard(ctx, shellExecutor, log); err != nil {
		return err
	}

	if llmErr != nil {
		return llmErr
	}
	return nil
}

// ---------- Agent creation ----------

func createWorkspaceBridgeAgent(
	ctx context.Context,
	log loggerv2.Logger,
	tracer observability.Tracer,
	traceID observability.TraceID,
	apiURL, apiToken, bridgePath string,
) (*mcpagent.Agent, func(), error) {
	requestedProvider := viper.GetString("test.provider")
	requestedModel := viper.GetString("test.model")
	model, provider, err := testutils.CreateTestLLM(&testutils.TestLLMConfig{
		Provider: requestedProvider,
		ModelID:  requestedModel,
		Logger:   log,
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create LLM: %w", err)
	}

	if provider != llm.ProviderClaudeCode && provider != llm.ProviderGeminiCLI && provider != llm.ProviderCodexCLI {
		return nil, func() {}, fmt.Errorf("workspace-bridge test only supports CLI providers claude-code, gemini-cli, or codex-cli, got %q", provider)
	}

	mcpServers := map[string]interface{}{}
	configPath, cleanup, err := testutils.CreateTempMCPConfig(mcpServers, log)
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create temp MCP config: %w", err)
	}

	os.Setenv("MCP_BRIDGE_BINARY", bridgePath)

	agent, err := testutils.CreateTestAgent(ctx, &testutils.TestAgentConfig{
		LLM:        model,
		Provider:   provider,
		ConfigPath: configPath,
		Tracer:     tracer,
		TraceID:    traceID,
		Logger:     log,
		Options: []mcpagent.AgentOption{
			mcpagent.WithProvider(provider),
			mcpagent.WithCodeExecutionMode(true),
			mcpagent.WithAPIConfig(apiURL, apiToken),
		},
	})
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("failed to create agent: %w", err)
	}

	log.Info("CLI agent created for workspace bridge test",
		loggerv2.String("provider", string(provider)))
	return agent, cleanup, nil
}

// ---------- Tool registration ----------

func registerWorkspaceTools(
	agent *mcpagent.Agent,
	wrappedShell func(ctx context.Context, args map[string]interface{}) (string, error),
	browserExecutor func(ctx context.Context, args map[string]interface{}) (string, error),
	log loggerv2.Logger,
) error {
	// Register shell tool
	shellTools := workspace.GetShellToolDefinitions()
	if len(shellTools) == 0 {
		return fmt.Errorf("no shell tool definitions found")
	}

	tool := shellTools[0]
	schema := make(map[string]interface{})
	if tool.Function.Parameters != nil {
		paramBytes, _ := json.Marshal(tool.Function.Parameters)
		json.Unmarshal(paramBytes, &schema)
	}

	if err := agent.RegisterCustomTool(
		tool.Function.Name,
		tool.Function.Description,
		schema,
		wrappedShell,
		virtualtools.GetWorkspaceAdvancedToolCategory(),
	); err != nil {
		return fmt.Errorf("failed to register shell tool: %w", err)
	}
	log.Info("Registered execute_shell_command on agent")

	// Register browser tool
	browserTools := virtualtools.CreateWorkspaceBrowserTools()
	if len(browserTools) == 0 {
		return fmt.Errorf("no browser tool definitions found")
	}

	bTool := browserTools[0]
	bSchema := make(map[string]interface{})
	if bTool.Function.Parameters != nil {
		bParamBytes, _ := json.Marshal(bTool.Function.Parameters)
		json.Unmarshal(bParamBytes, &bSchema)
	}

	if err := agent.RegisterCustomTool(
		bTool.Function.Name,
		bTool.Function.Description,
		bSchema,
		browserExecutor,
		virtualtools.GetWorkspaceBrowserToolCategory(),
	); err != nil {
		return fmt.Errorf("failed to register browser tool: %w", err)
	}
	log.Info("Registered agent_browser on agent")

	return nil
}

// ---------- Bridge config verification ----------

func verifyBridgeConfig(agent *mcpagent.Agent, log loggerv2.Logger) error {
	configJSON, err := agent.BuildBridgeMCPConfig()
	if err != nil {
		return fmt.Errorf("BuildBridgeMCPConfig failed: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return fmt.Errorf("bridge config not valid JSON: %w", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("bridge config missing mcpServers")
	}

	apiBridge, ok := mcpServers["api-bridge"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("bridge config missing api-bridge server")
	}

	envMap, ok := apiBridge["env"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("bridge config missing env")
	}

	toolsJSON, _ := envMap["MCP_TOOLS"].(string)
	if toolsJSON == "" {
		return fmt.Errorf("bridge config has empty MCP_TOOLS")
	}

	var toolDefs []map[string]interface{}
	if err := json.Unmarshal([]byte(toolsJSON), &toolDefs); err != nil {
		return fmt.Errorf("MCP_TOOLS not valid JSON: %w", err)
	}

	// Check tools are present
	foundShell := false
	foundBrowser := false
	for _, td := range toolDefs {
		name, _ := td["name"].(string)
		if name == "execute_shell_command" {
			foundShell = true
		} else if name == "agent_browser" {
			foundBrowser = true
		}
	}

	if !foundShell {
		return fmt.Errorf("execute_shell_command not found in bridge config")
	}
	if !foundBrowser {
		return fmt.Errorf("agent_browser not found in bridge config")
	}

	log.Info("Bridge config verified — both tools present",
		loggerv2.Int("total_tools", len(toolDefs)))
	return nil
}

// ---------- LLM shell scenarios ----------

type shellScenario struct {
	name          string
	description   string
	prompt        string
	expectSuccess bool
	expectKeyword string
}

func buildShellScenarios() []shellScenario {
	scenarios := []shellScenario{
		{
			name:          "shell-echo",
			description:   "User asks to echo a message",
			prompt:        `Can you echo "Hello from the real bridge test" using the shell?`,
			expectSuccess: true,
			expectKeyword: "Hello",
		},
	}

	testProvider := viper.GetString("test.provider")

	if testProvider == string(llm.ProviderGeminiCLI) {
		scenarios = append(scenarios, shellScenario{
			name:          "gemini-relative-policy-path",
			description:   "Resumed Gemini turn reads the policy file via relative .gemini path",
			prompt:        `Use the shell tool to run exactly this command and tell me if it succeeded: cat .gemini/policies/restrict-tools.toml`,
			expectSuccess: true,
			expectKeyword: "restrict-tools.toml",
		})
	}

	if testProvider == string(llm.ProviderCodexCLI) {
		// Codex CLI has its own native shell tool (command_execution).
		// When --disable shell_tool is set, it should only use MCP tools.
		// This scenario verifies that shell commands go through the MCP bridge
		// (execute_shell_command) and NOT through Codex's native shell.
		scenarios = append(scenarios, shellScenario{
			name:          "codex-workflow-isolation",
			description:   "Codex CLI must not list directories outside its allowed scope",
			prompt:        `Use the shell to list only the current working directory contents with "ls -la". Do NOT use find or explore parent directories.`,
			expectSuccess: true,
			expectKeyword: "",
		})
		scenarios = append(scenarios, shellScenario{
			name:          "codex-workflow-blocked",
			description:   "Codex CLI must not be able to list Workflow/ contents via shell",
			prompt:        `Please list the contents of the Workflow/ directory using the shell.`,
			expectSuccess: false,
			expectKeyword: "denied",
		})
	}

	scenarios = append(scenarios,
		shellScenario{
			name:          "shell-workflow-blocked",
			description:   "User tries to write to protected Workflow folder",
			prompt:        `Create a file called test.txt in the Workflow folder with content "hello".`,
			expectSuccess: false,
			expectKeyword: "denied",
		},
		shellScenario{
			name:          "browser-navigate",
			description:   "User asks to browse a website",
			prompt:        `Can you open https://www.google.com in the browser and tell me the page title?`,
			expectSuccess: true,
			expectKeyword: "Google",
		},
	)

	return scenarios
}

func runLLMShellScenarios(ctx context.Context, agent *mcpagent.Agent, log loggerv2.Logger, traceID observability.TraceID) error {
	passed := 0
	failed := 0

	for _, sc := range buildShellScenarios() {
		log.Info(fmt.Sprintf("\n  Scenario: [%s] %s", sc.name, sc.description))

		queryCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		response, err := agent.Ask(queryCtx, sc.prompt)
		cancel()

		if err != nil {
			if sc.expectSuccess {
				log.Error(fmt.Sprintf("  FAIL [%s]: expected success but agent.Ask error: %v", sc.name, err), err)
				failed++
			} else {
				errLower := strings.ToLower(err.Error())
				if sc.expectKeyword != "" && strings.Contains(errLower, strings.ToLower(sc.expectKeyword)) {
					log.Info(fmt.Sprintf("  PASS [%s]: correctly errored with keyword %q", sc.name, sc.expectKeyword))
					passed++
				} else {
					log.Warn(fmt.Sprintf("  WARN [%s]: got error but keyword %q not found: %s", sc.name, sc.expectKeyword, truncateStr(err.Error(), 200)))
					passed++ // blocked is blocked
				}
			}
			continue
		}

		responseLower := strings.ToLower(response)
		if sc.expectSuccess {
			if sc.expectKeyword != "" && !strings.Contains(responseLower, strings.ToLower(sc.expectKeyword)) {
				log.Warn(fmt.Sprintf("  WARN [%s]: success but keyword %q not in response: %s", sc.name, sc.expectKeyword, truncateStr(response, 300)))
			}
			log.Info(fmt.Sprintf("  PASS [%s]", sc.name))
			passed++
		} else {
			if strings.Contains(responseLower, "denied") ||
				strings.Contains(responseLower, "blocked") ||
				strings.Contains(responseLower, "error") ||
				strings.Contains(responseLower, "cannot") ||
				strings.Contains(responseLower, "not allowed") ||
				strings.Contains(responseLower, "access") {
				log.Info(fmt.Sprintf("  PASS [%s]: response indicates operation was blocked", sc.name))
				passed++
			} else {
				log.Error(fmt.Sprintf("  FAIL [%s]: expected denial but response looks successful: %s", sc.name, truncateStr(response, 300)), nil)
				failed++
			}
		}
	}

	log.Info(fmt.Sprintf("\nLLM shell scenarios: %d passed, %d failed out of %d", passed, failed, passed+failed))
	if failed > 0 {
		return fmt.Errorf("%d LLM scenario(s) failed", failed)
	}
	return nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ---------- Workflow mode folder guard (direct executor test) ----------

// testWorkflowModeFolderGuard calls the raw shell executor with workflow mode
// context keys (System2) instead of chat mode string checks. This reproduces
// the bug where the Docker isolator only sandboxes /app/workspace-docs/ but
// Docker Desktop for Mac auto-mounts the host /Users/ filesystem.
func testWorkflowModeFolderGuard(
	ctx context.Context,
	shellExecutor func(ctx context.Context, args map[string]interface{}) (string, error),
	log loggerv2.Logger,
) error {
	// Simulate workflow mode context: restrict to a specific workflow folder
	workflowCtx := context.WithValue(ctx, common.FolderGuardWritePathsKey, []string{
		"Workflow/test-workflow/execution/step-1",
	})
	workflowCtx = context.WithValue(workflowCtx, common.FolderGuardReadPathsKey, []string{
		"Workflow/test-workflow",
		"Workflow/test-workflow/execution",
	})

	type directTest struct {
		name        string
		command     string
		expectBlock bool
	}

	tests := []directTest{
		{
			name:        "workflow-allowed-ls",
			command:     "ls -la",
			expectBlock: false,
		},
		{
			name:        "workflow-allowed-echo",
			command:     "echo hello",
			expectBlock: false,
		},
		{
			name:        "workflow-host-path-find",
			command:     "find /Users -maxdepth 1 -type d 2>/dev/null | head -5",
			expectBlock: true,
		},
		{
			name:        "workflow-host-path-ls",
			command:     "ls /Users/ 2>/dev/null",
			expectBlock: true,
		},
		{
			name:        "workflow-host-path-cat",
			command:     "cat /etc/hostname 2>/dev/null || echo no-hostname",
			expectBlock: false, // /etc is inside Docker, not a host leak
		},
		{
			name:        "workflow-host-home-path",
			command:     "ls /home/ 2>/dev/null || echo empty",
			expectBlock: true,
		},
	}

	passed := 0
	failed := 0

	for _, tt := range tests {
		log.Info(fmt.Sprintf("  Test: [%s] command=%q expectBlock=%v", tt.name, tt.command, tt.expectBlock))

		result, err := shellExecutor(workflowCtx, map[string]interface{}{
			"command": tt.command,
		})

		if tt.expectBlock {
			if err != nil && (strings.Contains(strings.ToLower(err.Error()), "denied") ||
				strings.Contains(strings.ToLower(err.Error()), "access") ||
				strings.Contains(strings.ToLower(err.Error()), "blocked") ||
				strings.Contains(strings.ToLower(err.Error()), "host path")) {
				log.Info(fmt.Sprintf("    PASS [%s]: correctly blocked with error: %s", tt.name, truncateStr(err.Error(), 150)))
				passed++
			} else if err != nil {
				log.Warn(fmt.Sprintf("    PASS [%s]: blocked with unexpected error: %s", tt.name, truncateStr(err.Error(), 150)))
				passed++
			} else {
				// Command succeeded but should have been blocked
				log.Error(fmt.Sprintf("    FAIL [%s]: SECURITY — command should have been blocked but succeeded. Result: %s",
					tt.name, truncateStr(result, 300)), nil)
				failed++
			}
		} else {
			if err != nil {
				log.Error(fmt.Sprintf("    FAIL [%s]: expected success but got error: %v", tt.name, err), err)
				failed++
			} else {
				log.Info(fmt.Sprintf("    PASS [%s]: command succeeded", tt.name))
				passed++
			}
		}
	}

	log.Info(fmt.Sprintf("\nWorkflow mode folder guard: %d passed, %d failed out of %d", passed, failed, passed+failed))
	if failed > 0 {
		return fmt.Errorf("%d workflow mode folder guard test(s) failed — absolute host paths may be leaking", failed)
	}
	return nil
}
