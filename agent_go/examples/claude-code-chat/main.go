// claude-code-chat is a standalone interactive CLI chat using the Claude Code MCP bridge.
//
// It sets up the full pipeline (workspace API, executors, folder guard, mcpbridge,
// executor HTTP server, agent, tools) and runs a multi-turn REPL loop.
//
// Usage:
//
//	go run ./examples/claude-code-chat/ [--verbose] [--no-guard] [--workspace-url URL]
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	server "mcp-agent-builder-go/agent_go/cmd/server"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/workspace"
)

const defaultLogFile = "/tmp/claude-code-chat.log"

// CLI flags
var (
	flagVerbose      = flag.Bool("verbose", false, "enable info-level logging (default: "+defaultLogFile+")")
	flagWorkspaceURL = flag.String("workspace-url", "", "workspace API URL (default: $WORKSPACE_API_URL or http://localhost:8081)")
	flagBridgeBinary = flag.String("bridge-binary", "", "path to mcpbridge binary (default: auto-detect)")
	flagNoGuard      = flag.Bool("no-guard", false, "disable folder guard on shell commands")
	flagLogFile      = flag.String("log-file", "", "log file path (default: "+defaultLogFile+", use 'stderr' for stderr)")
	flagMCPConfig    = flag.String("mcp-config", "", "path to MCP servers config JSON (adds servers like gmail, google-sheets, etc.)")
	flagMaxTurns     = flag.Int("max-turns", 0, "limit the number of agentic turns per message (0 = no limit)")
)

func main() {
	flag.Parse()

	logger := setupLogger()
	defer logger.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT/SIGTERM.
	// First signal: cancel context + close stdin to unblock scanner.
	// Second signal: force exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\nShutting down...\n")
		cancel()
		os.Stdin.Close() // unblock scanner.Scan()
		<-sigCh
		fmt.Fprintf(os.Stderr, "Force exit.\n")
		os.Exit(1)
	}()

	if err := run(ctx, logger); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func setupLogger() loggerv2.Logger {
	if !*flagVerbose {
		return loggerv2.NewNoop()
	}
	output := defaultLogFile
	if *flagLogFile != "" {
		output = *flagLogFile
	}
	logger, err := loggerv2.New(loggerv2.Config{
		Level:  "info",
		Format: "text",
		Output: output,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	if output != "stderr" && output != "stdout" {
		fmt.Fprintf(os.Stderr, "Logging to %s\n", output)
	}
	return logger
}

func run(ctx context.Context, log loggerv2.Logger) error {
	// Step 1: Check workspace API
	wsURL := *flagWorkspaceURL
	if wsURL == "" {
		wsURL = os.Getenv("WORKSPACE_API_URL")
	}
	if wsURL == "" {
		wsURL = "http://localhost:8081"
	}
	os.Setenv("WORKSPACE_API_URL", wsURL)

	if err := checkWorkspaceAPI(wsURL); err != nil {
		return err
	}
	log.Info("Workspace API reachable", loggerv2.String("url", wsURL))

	// Step 2: Ensure mcpbridge binary
	bridgePath, err := ensureBridgeBinary(log)
	if err != nil {
		return err
	}
	os.Setenv("MCP_BRIDGE_BINARY", bridgePath)
	log.Info("mcpbridge binary ready", loggerv2.String("path", bridgePath))

	// Step 3: Resolve MCP config (needed by both executor server and agent)
	mcpConfigPath := resolveMCPConfig()
	if mcpConfigPath == "" {
		return fmt.Errorf("MCP config not found. Create examples/claude-code-chat/mcp_servers.json or pass --mcp-config")
	}
	fmt.Fprintf(os.Stderr, "MCP config: %s\n", mcpConfigPath)
	log.Info("Using MCP config", loggerv2.String("path", mcpConfigPath))

	// Step 4: Pre-allocate TCP listener to know the port before creating executors
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("allocate TCP listener: %w", err)
	}
	addr := listener.Addr().String()
	log.Info("Pre-allocated listener", loggerv2.String("addr", addr))

	// Step 5: Generate API token + derive URLs
	// Two URLs for two different consumers:
	//   hostURL:   mcpbridge binary (runs on host as stdio process)
	//   dockerURL: shell commands + OpenAPI specs (code runs inside Docker container)
	apiToken := executor.GenerateAPIToken()
	_, port, _ := net.SplitHostPort(addr)
	hostURL := "http://127.0.0.1:" + port
	dockerURL := "http://host.docker.internal:" + port

	// Step 6: Set env vars
	// MCP_API_URL = Docker URL (for shell commands in Docker + OpenAPI spec base URLs)
	// MCP_BRIDGE_API_URL = host URL (for mcpbridge binary which runs on the host)
	os.Setenv("MCP_API_URL", dockerURL)
	os.Setenv("MCP_API_TOKEN", apiToken)
	os.Setenv("MCP_BRIDGE_API_URL", hostURL)
	log.Info("MCP env vars set",
		loggerv2.String("MCP_API_URL", dockerURL),
		loggerv2.String("MCP_BRIDGE_API_URL", hostURL))

	// Step 7: Create executors + optional folder guard (captures Docker URL)
	wrappedShell, browserExec, wrappedMap, err := createExecutors(log)
	if err != nil {
		listener.Close()
		return err
	}

	// Step 8: Start executor HTTP server on pre-allocated listener
	serverURL, executorShutdown, err := startExecutorServer(log, wrappedMap, mcpConfigPath, listener, apiToken)
	if err != nil {
		return fmt.Errorf("start executor server: %w", err)
	}
	defer executorShutdown()
	log.Info("Executor server started", loggerv2.String("url", serverURL))

	time.Sleep(500 * time.Millisecond)

	// Step 10: Create agent with code execution mode (populates allMCPToolDefs + tool index)
	// Bridge always exposes only shell + browser + get_api_spec.
	// Agent reads MCP_API_URL from env (now set to host URL for bridge)
	agent, configCleanup, err := createAgent(ctx, log, mcpConfigPath)
	if err != nil {
		return err
	}
	defer agent.Close()
	defer configCleanup()

	// Step 11: Register custom tools (shell + browser + dummy human tool for testing)
	if err := registerTools(agent, wrappedShell, browserExec, log); err != nil {
		return err
	}
	log.Info("Tools registered")

	// Register a dummy "human" category tool to test that custom tools appear
	// in the tool index when UseCodeExecutionMode is true.
	if err := agent.RegisterCustomTool(
		"human_feedback",
		"Request feedback from a human operator",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{
					"type":        "string",
					"description": "The question to ask the human",
				},
			},
			"required": []string{"question"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			question, _ := args["question"].(string)
			fmt.Fprintf(os.Stderr, "\n>>> HUMAN_FEEDBACK TOOL CALLED! question=%q <<<\n", question)
			return fmt.Sprintf(`{"status":"ok","feedback":"This is a dummy human feedback response to: %s"}`, question), nil
		},
		"human",
	); err != nil {
		return fmt.Errorf("register human_feedback tool: %w", err)
	}
	log.Info("Registered dummy human_feedback tool (category: human)")

	// Step 12: Register CLI event listener (shows tool calls in terminal)
	agent.AddEventListener(&cliEventListener{})

	// Step 13: Verify bridge config (should have exactly shell + browser + get_api_spec)
	if err := verifyBridge(agent, log); err != nil {
		return err
	}

	// Print tool index at startup so we can verify custom tools are included
	fmt.Fprintf(os.Stderr, "\n=== Tool Index (UseCodeExecutionMode=%v) ===\n", agent.UseCodeExecutionMode)
	if sp := agent.SystemPrompt; sp != "" {
		if start := strings.Index(sp, "```json\n"); start != -1 {
			jsonStart := start + 8
			if end := strings.Index(sp[jsonStart:], "\n```"); end != -1 {
				fmt.Fprintf(os.Stderr, "%s\n", sp[jsonStart:jsonStart+end])
			}
		}
	}
	fmt.Fprintf(os.Stderr, "=== End Tool Index ===\n\n")

	fmt.Println("Claude Code Chat — type /help for commands, /exit to quit")
	fmt.Println()

	return runChat(ctx, agent)
}

// ---------- Workspace API ----------

func checkWorkspaceAPI(url string) error {
	payload, _ := json.Marshal(map[string]interface{}{
		"command":           "echo 'alive'",
		"working_directory": ".",
	})
	resp, err := http.Post(url+"/api/execute", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("workspace API not reachable at %s (is the Docker container running?): %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace API returned %d at %s", resp.StatusCode, url)
	}
	return nil
}

// ---------- Executors ----------

func createExecutors(log loggerv2.Logger) (
	wrappedShell func(ctx context.Context, args map[string]interface{}) (string, error),
	browserExec func(ctx context.Context, args map[string]interface{}) (string, error),
	wrappedMap map[string]func(ctx context.Context, args map[string]interface{}) (string, error),
	err error,
) {
	advancedExecs := virtualtools.CreateWorkspaceAdvancedToolExecutorsWithUserID("claude-code-chat")
	browserExecs := virtualtools.CreateWorkspaceBrowserToolExecutors()

	shellExec, ok := advancedExecs["execute_shell_command"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("execute_shell_command executor not found")
	}
	browserExec, ok = browserExecs["agent_browser"]
	if !ok {
		return nil, nil, nil, fmt.Errorf("agent_browser executor not found")
	}

	shellOnlyMap := map[string]func(ctx context.Context, args map[string]interface{}) (string, error){
		"execute_shell_command": shellExec,
	}

	if *flagNoGuard {
		log.Info("Folder guard DISABLED (--no-guard)")
		wrappedMap = shellOnlyMap
	} else {
		wrappedMap = server.ApplyChatModeFolderGuard(shellOnlyMap)
		log.Info("Folder guard applied to shell executor")
	}

	wrappedShell = wrappedMap["execute_shell_command"]
	wrappedMap["agent_browser"] = browserExec
	return wrappedShell, browserExec, wrappedMap, nil
}

// ---------- Bridge binary ----------

func ensureBridgeBinary(log loggerv2.Logger) (string, error) {
	if *flagBridgeBinary != "" {
		if _, err := os.Stat(*flagBridgeBinary); err == nil {
			return *flagBridgeBinary, nil
		}
		return "", fmt.Errorf("specified bridge binary not found: %s", *flagBridgeBinary)
	}
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
	buildCmd.Stdout = os.Stderr
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

// ---------- MCP config resolution ----------

func resolveMCPConfig() string {
	if *flagMCPConfig != "" {
		if _, err := os.Stat(*flagMCPConfig); err == nil {
			return *flagMCPConfig
		}
		return ""
	}
	for _, candidate := range []string{
		"examples/claude-code-chat/mcp_servers.json",
		"mcp_servers.json",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// ---------- Executor HTTP server ----------

func startExecutorServer(
	log loggerv2.Logger,
	wrappedExecutors map[string]func(ctx context.Context, args map[string]interface{}) (string, error),
	mcpConfigPath string,
	listener net.Listener,
	apiToken string,
) (serverURL string, shutdown func(), err error) {
	handlers := executor.NewExecutorHandlers(mcpConfigPath, log)

	mux := http.NewServeMux()
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
		srv := parts[0]
		tool := parts[1]
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
		sCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		srv.Shutdown(sCtx)
		log.Info("executor server stopped")
	}

	return serverURL, shutdown, nil
}

// ---------- Agent creation ----------

func createAgent(
	ctx context.Context,
	log loggerv2.Logger,
	mcpConfigPath string,
) (*mcpagent.Agent, func(), error) {
	model, err := llm.InitializeLLM(llm.Config{
		Provider: llm.ProviderClaudeCode,
		ModelID:  llm.GetDefaultModel(llm.ProviderClaudeCode),
		Logger:   log,
	})
	if err != nil {
		return nil, func() {}, fmt.Errorf("initialize LLM: %w", err)
	}

	cleanup := func() {}
	agentOpts := []mcpagent.AgentOption{
		mcpagent.WithLogger(log),
		mcpagent.WithProvider(llm.ProviderClaudeCode),
		mcpagent.WithCodeExecutionMode(true), // populates allMCPToolDefs + tool index in system prompt
		// No WithAPIConfig — agent reads MCP_API_URL/MCP_API_TOKEN from env (Docker-accessible URL)
	}
	if *flagMaxTurns > 0 {
		agentOpts = append(agentOpts, mcpagent.WithMaxTurns(*flagMaxTurns))
	}
	agent, err := mcpagent.NewAgent(ctx, model, mcpConfigPath, agentOpts...)
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("create agent: %w", err)
	}

	log.Info("Claude Code agent created",
		loggerv2.String("MCP_API_URL", os.Getenv("MCP_API_URL")),
		loggerv2.Any("code_exec_mode", true))
	return agent, cleanup, nil
}

// ---------- Tool registration ----------

func registerTools(
	agent *mcpagent.Agent,
	wrappedShell func(ctx context.Context, args map[string]interface{}) (string, error),
	browserExec func(ctx context.Context, args map[string]interface{}) (string, error),
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
		return fmt.Errorf("register shell tool: %w", err)
	}
	log.Info("Registered execute_shell_command")

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
		browserExec,
		virtualtools.GetWorkspaceBrowserToolCategory(),
	); err != nil {
		return fmt.Errorf("register browser tool: %w", err)
	}
	log.Info("Registered agent_browser")

	return nil
}

// ---------- Bridge verification ----------

func verifyBridge(agent *mcpagent.Agent, log loggerv2.Logger) error {
	configJSON, err := agent.BuildBridgeMCPConfig()
	if err != nil {
		return fmt.Errorf("BuildBridgeMCPConfig: %w", err)
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

	foundShell, foundBrowser, foundAPISpec := false, false, false
	var toolNames []string
	for _, td := range toolDefs {
		name, _ := td["name"].(string)
		toolNames = append(toolNames, name)
		switch name {
		case "execute_shell_command":
			foundShell = true
		case "agent_browser":
			foundBrowser = true
		case "get_api_spec":
			foundAPISpec = true
		}
	}
	if !foundShell {
		return fmt.Errorf("execute_shell_command not found in bridge config")
	}
	if !foundBrowser {
		return fmt.Errorf("agent_browser not found in bridge config")
	}
	if !foundAPISpec {
		return fmt.Errorf("get_api_spec not found in bridge config (is code execution mode enabled?)")
	}

	log.Info("Bridge config verified",
		loggerv2.Int("tools", len(toolDefs)),
		loggerv2.String("tool_names", strings.Join(toolNames, ", ")))
	fmt.Fprintf(os.Stderr, "Bridge tools: %s\n", strings.Join(toolNames, ", "))
	return nil
}

// ---------- CLI event listener ----------

// cliEventListener prints tool call start/end events to the terminal so the
// user can see what Claude Code is doing while waiting for a response.
type cliEventListener struct{}

func (l *cliEventListener) Name() string { return "cli-events" }

func (l *cliEventListener) HandleEvent(_ context.Context, event *events.AgentEvent) error {
	switch e := event.Data.(type) {
	case *events.ToolCallStartEvent:
		// Show tool name and abbreviated arguments
		args := e.ToolParams.Arguments
		if len(args) > 120 {
			args = args[:120] + "..."
		}
		fmt.Fprintf(os.Stderr, "  ▶ %s(%s)\n", e.ToolName, args)

	case *events.ToolCallEndEvent:
		// Show tool name, duration, and abbreviated result
		result := e.Result
		if len(result) > 200 {
			result = result[:200] + "..."
		}
		// Replace newlines in result preview for compact display
		result = strings.ReplaceAll(result, "\n", "\\n")
		fmt.Fprintf(os.Stderr, "  ✓ %s (%s) → %s\n", e.ToolName, e.Duration.Round(time.Millisecond), result)

	case *events.ToolCallErrorEvent:
		fmt.Fprintf(os.Stderr, "  ✗ %s — %s\n", e.ToolName, e.Error)
	}
	return nil
}

// ---------- Interactive chat loop ----------

func runChat(ctx context.Context, agent *mcpagent.Agent) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var history []llm.MessageContent

	for {
		fmt.Print("you> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if handled, exit := handleCommand(input, &history, agent); handled {
			if exit {
				return nil
			}
			continue
		}

		// Add user message
		history = append(history, llm.MessageContent{
			Role:  llm.ChatMessageTypeHuman,
			Parts: []llm.ContentPart{llm.TextContent{Text: input}},
		})

		turnCtx, turnCancel := context.WithTimeout(ctx, 5*time.Minute)
		answer, updated, err := agent.AskWithHistory(turnCtx, history)
		turnCancel()

		if err != nil {
			// Remove the failed user message so history stays clean
			history = history[:len(history)-1]
			fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
			continue
		}

		history = updated
		fmt.Printf("\nassistant> %s\n\n", answer)

		// Show per-turn usage summary
		printUsageSummary(agent)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("stdin read error: %w", err)
	}
	return nil
}

func printUsageSummary(agent *mcpagent.Agent) {
	promptTokens, completionTokens, totalTokens, cacheTokens, _, llmCalls, _ := agent.GetTokenUsage()

	// Show session ID if captured
	sessionTag := ""
	if agent.ClaudeCodeSessionID != "" {
		sid := agent.ClaudeCodeSessionID
		if len(sid) > 12 {
			sid = sid[:12] + "..."
		}
		sessionTag = fmt.Sprintf("  session=%s", sid)
	}

	// Show cache hit ratio if cache tokens exist
	cacheInfo := ""
	if cacheTokens > 0 && promptTokens > 0 {
		pct := float64(cacheTokens) / float64(promptTokens) * 100
		cacheInfo = fmt.Sprintf(" (%.0f%% cached)", pct)
	}

	fmt.Fprintf(os.Stderr, "  [usage] %d in / %d out / %d total%s | calls: %d%s\n",
		promptTokens, completionTokens, totalTokens, cacheInfo, llmCalls, sessionTag)
}

// handleCommand processes slash commands. Returns (handled, shouldExit).
func handleCommand(input string, history *[]llm.MessageContent, agent *mcpagent.Agent) (bool, bool) {
	switch strings.ToLower(input) {
	case "/exit", "/quit":
		fmt.Println("Goodbye!")
		return true, true
	case "/clear":
		*history = (*history)[:0]
		agent.ClaudeCodeSessionID = "" // Reset session so next turn starts fresh
		fmt.Println("Conversation cleared.")
		return true, false
	case "/history":
		fmt.Printf("Messages in history: %d\n", len(*history))
		return true, false
	case "/usage":
		promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, llmCalls, _ := agent.GetTokenUsage()
		fmt.Printf("Token Usage (cumulative):\n")
		fmt.Printf("  Input tokens:     %d", promptTokens)
		if cacheTokens > 0 {
			fmt.Printf(" (%d cached, %d new)", cacheTokens, promptTokens-cacheTokens)
		}
		fmt.Printf("\n")
		fmt.Printf("  Output tokens:    %d\n", completionTokens)
		if reasoningTokens > 0 {
			fmt.Printf("  Reasoning tokens: %d\n", reasoningTokens)
		}
		fmt.Printf("  Total tokens:     %d\n", totalTokens)
		fmt.Printf("  LLM calls:        %d\n", llmCalls)
		if agent.ClaudeCodeSessionID != "" {
			fmt.Printf("  Session ID:       %s\n", agent.ClaudeCodeSessionID)
		}
		return true, false
	case "/index":
		// Extract and print the tool index JSON from the system prompt.
		// The tool index is embedded inside <available_tools>...</available_tools>
		// with a ```json code block containing the actual JSON.
		sp := agent.SystemPrompt
		start := strings.Index(sp, "<available_tools>")
		end := strings.Index(sp, "</available_tools>")
		if start == -1 || end == -1 {
			fmt.Println("No <available_tools> section found in system prompt.")
			fmt.Printf("System prompt length: %d chars\n", len(sp))
			fmt.Printf("UseCodeExecutionMode: %v\n", agent.UseCodeExecutionMode)
		} else {
			block := sp[start : end+len("</available_tools>")]
			// Try to extract just the JSON from the ```json ... ``` block
			jsonStart := strings.Index(block, "```json\n")
			jsonEnd := strings.Index(block[jsonStart+8:], "\n```")
			if jsonStart != -1 && jsonEnd != -1 {
				jsonStr := block[jsonStart+8 : jsonStart+8+jsonEnd]
				var parsed interface{}
				if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
					pretty, _ := json.MarshalIndent(parsed, "", "  ")
					fmt.Printf("Tool Index:\n%s\n", string(pretty))
				} else {
					fmt.Printf("Tool index (raw JSON):\n%s\n", jsonStr)
				}
			} else {
				fmt.Printf("Available tools section:\n%s\n", block)
			}
		}
		return true, false
	case "/help":
		fmt.Println("Commands:")
		fmt.Println("  /exit, /quit  — exit the chat")
		fmt.Println("  /clear        — reset conversation history + session")
		fmt.Println("  /history      — show message count")
		fmt.Println("  /usage        — show cumulative token usage")
		fmt.Println("  /index        — show tool index from system prompt")
		fmt.Println("  /help         — show this help")
		return true, false
	}
	return false, false
}
