// Package agentsession is a reusable runtime that gives a coding-agent (Claude
// Code, Codex, Cursor, ...) access to app-specific custom tools through the
// mcpagent MCP bridge — the same mechanism AgentWorks workflows use to expose
// execute_shell_command.
//
// It encapsulates the wiring that the examples/claude-code-chat template spells
// out by hand: ensure the mcpbridge binary, generate a minimal MCP config,
// stand up the executor HTTP server, create the agent (bridge-only + code
// execution mode via the provider integration appenders), and register the
// caller's custom tools into the session-scoped codeexec registry so the bridge
// can resolve /tools/custom/{name} calls back to Go handlers running in THIS
// process.
//
// Agent and executor server run in the same process by construction — that is
// the whole point: RegisterCustomTool publishes handlers into a registry keyed
// by session id, and the executor server resolves them via the X-Session-ID
// header the bridge injects.
package agentsession

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/executor"
	"github.com/manishiitg/mcpagent/llm"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Tool is one app-specific custom tool exposed to the agent through the bridge.
type Tool struct {
	Name        string
	Description string
	Category    string
	Params      map[string]interface{}
	Handler     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// Message is one conversation turn.
type Message struct {
	Role string // "user" | "assistant"
	Text string
}

// Config parameterizes a Session. Only Provider, WorkingDir and Tools are
// really required for a useful session.
type Config struct {
	Provider     llm.Provider   // e.g. llm.ProviderClaudeCode
	ModelID      string         // "" -> llm.GetDefaultModel(provider)
	WorkingDir   string         // scope root (Family/parent). "" -> process cwd
	SystemPrompt string         // agent persona / instructions
	Tools        []Tool         // app-specific custom tools
	Logger       loggerv2.Logger
	MaxTurns     int // 0 -> provider default
}

// Session bundles a live agent with its in-process executor server. Not safe
// for concurrent Ask calls; create one Session per conversation turn (cheap for
// a low-QPS local app) or serialize access.
type Session struct {
	agent    *mcpagent.Agent
	logger   loggerv2.Logger
	shutdown func()
	closed   bool
}

// New builds a ready-to-use Session: bridge binary + MCP config + executor
// server + agent + registered tools. The caller must Close() it.
func New(ctx context.Context, cfg Config) (*Session, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = loggerv2.NewNoop()
	}

	// 1. Ensure the mcpbridge binary is available and advertise it via env so
	//    BuildBridgeMCPConfig (called by the provider appender) can find it.
	bridgePath, err := ensureBridgeBinary(logger)
	if err != nil {
		return nil, err
	}
	os.Setenv("MCP_BRIDGE_BINARY", bridgePath)

	// 2. Generate a minimal MCP servers config. This session has no upstream MCP
	//    servers — all tools are custom and resolved in-process — so an empty
	//    server map is correct and makes the package cwd-independent.
	mcpConfigPath, cleanupConfig, err := writeMinimalMCPConfig()
	if err != nil {
		return nil, err
	}

	// 3. Pre-allocate the executor listener so we know the port before we build
	//    the bridge env.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cleanupConfig()
		return nil, fmt.Errorf("allocate executor listener: %w", err)
	}
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	hostURL := "http://127.0.0.1:" + port

	// 4. Token + env. Our custom tools run on the host (not in Docker), so both
	//    the in-Docker URL and the bridge (host) URL point at the same local
	//    executor server.
	apiToken := executor.GenerateAPIToken()
	os.Setenv("MCP_API_URL", hostURL)
	os.Setenv("MCP_API_TOKEN", apiToken)
	os.Setenv("MCP_BRIDGE_API_URL", hostURL)

	// 5. Start the executor HTTP server on the pre-allocated listener.
	execShutdown, err := startExecutorServer(logger, mcpConfigPath, listener, apiToken)
	if err != nil {
		listener.Close()
		cleanupConfig()
		return nil, fmt.Errorf("start executor server: %w", err)
	}
	// Give the server a moment to begin serving before the agent runs.
	time.Sleep(300 * time.Millisecond)

	// 6. Create the agent. The provider integration appenders apply bridge-only
	//    access automatically at generation time; WithCodeExecutionMode(true)
	//    also builds the tool index. WithSessionID scopes the custom-tool
	//    registry the bridge resolves against.
	modelID := cfg.ModelID
	if strings.TrimSpace(modelID) == "" {
		modelID = llm.GetDefaultModel(cfg.Provider)
	}
	model, err := llm.InitializeLLM(llm.Config{
		Provider: cfg.Provider,
		ModelID:  modelID,
		Logger:   logger,
		Context:  ctx,
	})
	if err != nil {
		execShutdown()
		cleanupConfig()
		return nil, fmt.Errorf("initialize LLM: %w", err)
	}

	sessionID := "agentsession-" + fmt.Sprintf("%d", time.Now().UnixNano())
	opts := []mcpagent.AgentOption{
		mcpagent.WithLogger(logger),
		mcpagent.WithProvider(cfg.Provider),
		mcpagent.WithCodeExecutionMode(true),
		mcpagent.WithSessionID(sessionID),
	}
	if strings.TrimSpace(cfg.SystemPrompt) != "" {
		opts = append(opts, mcpagent.WithSystemPrompt(cfg.SystemPrompt))
	}
	if strings.TrimSpace(cfg.WorkingDir) != "" {
		opts = append(opts, mcpagent.WithCodingAgentWorkingDir(cfg.WorkingDir))
	}
	if cfg.MaxTurns > 0 {
		opts = append(opts, mcpagent.WithMaxTurns(cfg.MaxTurns))
	}

	agent, err := mcpagent.NewAgent(ctx, model, mcpConfigPath, opts...)
	if err != nil {
		execShutdown()
		cleanupConfig()
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// 7. Register the app-specific custom tools. This publishes them into the
	//    session-scoped codeexec registry (agent.go: InitRegistryForSession) so
	//    the executor server resolves /tools/custom/{name} calls to these
	//    handlers, and adds them to the native bridge tool set (they must also
	//    be listed in mcpagent bridgeTools to be exposed natively).
	for _, t := range cfg.Tools {
		category := t.Category
		if strings.TrimSpace(category) == "" {
			category = "family_tools"
		}
		if err := agent.RegisterCustomTool(t.Name, t.Description, t.Params, t.Handler, category); err != nil {
			agent.Close()
			execShutdown()
			cleanupConfig()
			return nil, fmt.Errorf("register tool %q: %w", t.Name, err)
		}
	}

	s := &Session{
		agent:  agent,
		logger: logger,
		shutdown: func() {
			agent.Close()
			execShutdown()
			cleanupConfig()
		},
	}
	return s, nil
}

// Ask runs one turn over the supplied history and returns the assistant reply.
func (s *Session) Ask(ctx context.Context, history []Message) (string, error) {
	msgs := make([]llmtypes.MessageContent, 0, len(history))
	for _, m := range history {
		role := llmtypes.ChatMessageTypeHuman
		if strings.EqualFold(m.Role, "assistant") || strings.EqualFold(m.Role, "ai") {
			role = llmtypes.ChatMessageTypeAI
		}
		msgs = append(msgs, llmtypes.MessageContent{
			Role:  role,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: m.Text}},
		})
	}
	reply, _, err := s.agent.AskWithHistory(ctx, msgs)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reply), nil
}

// Agent exposes the underlying agent for advanced callers (event listeners,
// usage stats). May be nil after Close.
func (s *Session) Agent() *mcpagent.Agent { return s.agent }

// Close tears down the agent and the executor server.
func (s *Session) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.shutdown != nil {
		s.shutdown()
	}
}

// ---------- helpers ----------

// ensureBridgeBinary resolves the mcpbridge binary, building it into
// ~/go/bin/mcpbridge from the sibling mcpagent module if necessary.
func ensureBridgeBinary(logger loggerv2.Logger) (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("MCP_BRIDGE_BINARY")); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}
	if path, err := exec.LookPath("mcpbridge"); err == nil {
		return path, nil
	}
	home, _ := os.UserHomeDir()
	goBin := filepath.Join(home, "go", "bin", "mcpbridge")
	if _, err := os.Stat(goBin); err == nil {
		return goBin, nil
	}
	// Attempt to build from the mcpagent module root.
	root := findMcpagentRoot()
	if root == "" {
		return "", fmt.Errorf("mcpbridge binary not found and mcpagent source not located; build it: go build -o ~/go/bin/mcpbridge ./cmd/mcpbridge/")
	}
	logger.Info("Building mcpbridge", loggerv2.String("root", root), loggerv2.String("out", goBin))
	cmd := exec.Command("go", "build", "-o", goBin, "./cmd/mcpbridge/")
	cmd.Dir = root
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build mcpbridge: %w", err)
	}
	return goBin, nil
}

func findMcpagentRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 6 && dir != "" && dir != "/"; i++ {
		if _, err := os.Stat(filepath.Join(dir, "cmd", "mcpbridge")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	for _, c := range []string{"../mcpagent", "../../mcpagent", "../../../mcpagent"} {
		if _, err := os.Stat(filepath.Join(c, "cmd", "mcpbridge")); err == nil {
			return c
		}
	}
	return ""
}

// writeMinimalMCPConfig writes an empty MCP servers config to a temp file so
// NewAgent has a valid config path regardless of cwd.
func writeMinimalMCPConfig() (path string, cleanup func(), err error) {
	f, err := os.CreateTemp("", "agentsession-mcp-*.json")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp MCP config: %w", err)
	}
	if _, err := f.WriteString(`{"mcpServers":{}}`); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", func() {}, fmt.Errorf("write temp MCP config: %w", err)
	}
	f.Close()
	name := f.Name()
	return name, func() { os.Remove(name) }, nil
}

// startExecutorServer stands up the per-tool executor HTTP server on the given
// listener. Custom tool resolution flows through the session-scoped codeexec
// registry populated by RegisterCustomTool.
func startExecutorServer(logger loggerv2.Logger, mcpConfigPath string, listener net.Listener, apiToken string) (func(), error) {
	handlers := executor.NewExecutorHandlers(mcpConfigPath, logger)

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
		handlers.HandlePerToolMCPRequest(w, r, parts[0], parts[1])
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

	srv := &http.Server{
		Handler:           executor.AuthMiddleware(apiToken)(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.Error("executor server error", err)
		}
	}()

	return func() {
		sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sCtx)
	}, nil
}

// serialize guards process-global MCP env vars while a Session is being built.
// Callers running concurrent Sessions should hold this via NewSerialized.
var serialize sync.Mutex

// NewSerialized is New wrapped in a package mutex, for callers that may build
// Sessions concurrently (the executor env vars are process-global).
func NewSerialized(ctx context.Context, cfg Config) (*Session, error) {
	serialize.Lock()
	defer serialize.Unlock()
	return New(ctx, cfg)
}
