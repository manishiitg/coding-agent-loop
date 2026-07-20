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
	// SessionID, when set, makes turns RESUME the coding agent's own session
	// (warm tmux/session resume) instead of cold-starting a fresh one. Use a
	// stable id per conversation (e.g. the conversation id). Empty -> fresh
	// throwaway session each turn (full-history replay).
	SessionID string
}

// Session bundles a live agent with its in-process executor server. Not safe
// for concurrent Ask calls; create one Session per conversation turn (cheap for
// a low-QPS local app) or serialize access.
type Session struct {
	agent    *mcpagent.Agent
	logger   loggerv2.Logger
	shutdown func()
	closed   bool
	resume   bool      // warm-resume mode: the coding agent keeps context across turns
	id       string    // cache key (conversation id); "" for uncached one-off sessions
	lastUsed time.Time // for LRU eviction of the session cache
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

	// A stable SessionID resumes the coding agent's own session across turns;
	// otherwise use a throwaway id (fresh session each turn).
	resume := strings.TrimSpace(cfg.SessionID) != ""
	sessionID := strings.TrimSpace(cfg.SessionID)
	if sessionID == "" {
		sessionID = "agentsession-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	opts := []mcpagent.AgentOption{
		mcpagent.WithLogger(logger),
		mcpagent.WithProvider(cfg.Provider),
		mcpagent.WithCodeExecutionMode(true),
		mcpagent.WithSessionID(sessionID),
	}
	if resume {
		// Keep the coding agent's interactive (tmux) session alive so the next
		// turn resumes it with full context instead of cold-starting.
		switch cfg.Provider {
		case llm.ProviderClaudeCode:
			opts = append(opts, mcpagent.WithClaudeCodePersistentInteractiveSession(true))
		case llm.ProviderCodexCLI:
			opts = append(opts, mcpagent.WithCodexPersistentInteractiveSession(true))
		case llm.ProviderCursorCLI:
			opts = append(opts, mcpagent.WithCursorPersistentInteractiveSession(true))
		case llm.ProviderPiCLI:
			opts = append(opts, mcpagent.WithPiPersistentInteractiveSession(true))
		}
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
		resume: resume,
		shutdown: func() {
			agent.Close()
			execShutdown()
			cleanupConfig()
		},
	}
	return s, nil
}

// ---------- warm-resume session cache ----------
//
// A warm-resume session keeps a live coding-agent CLI (tmux) AND its in-process
// executor/bridge server alive together across turns. Because New() allocates a
// fresh executor port every call and Close() tears it down, creating a new
// Session per HTTP request would leave the resumed CLI pointing at the previous
// turn's dead bridge ("connection refused" the moment a resumed turn needs a
// tool). Acquire fixes that: for a given conversation id it builds the Session
// once and returns the SAME live Session (executor + tmux intact) on later turns.
//
// Turns are serialized by the caller (a global agent-turn mutex), so a cached
// Session is only ever touched single-threaded.

const maxCachedSessions = 8

var (
	sessCacheMu sync.Mutex
	sessCache   = map[string]*Session{}
)

// Acquire returns a warm, reusable Session for cfg.SessionID: it is created on
// the first turn and reused (with its executor + tmux session alive) on later
// turns, so warm resume works even when a resumed turn needs a bridge tool. The
// returned bool is true when the Session is cache-owned — the caller must NOT
// Close it (the cache owns its lifecycle). When cfg.SessionID is empty there is
// no warm resume to preserve, so Acquire returns a fresh uncached Session and
// false; that caller must Close it as before.
func Acquire(ctx context.Context, cfg Config) (*Session, bool, error) {
	id := strings.TrimSpace(cfg.SessionID)
	if id == "" {
		s, err := New(ctx, cfg)
		return s, false, err
	}

	sessCacheMu.Lock()
	defer sessCacheMu.Unlock()

	if s, ok := sessCache[id]; ok && !s.closed {
		s.lastUsed = time.Now()
		return s, true, nil
	}

	s, err := New(ctx, cfg)
	if err != nil {
		return nil, false, err
	}
	s.id = id
	s.lastUsed = time.Now()
	sessCache[id] = s
	evictLocked()
	return s, true, nil
}

// evictLocked closes the least-recently-used sessions beyond the cap. Caller
// holds sessCacheMu. The just-inserted session is the most-recently-used, so it
// is never the eviction target.
func evictLocked() {
	for len(sessCache) > maxCachedSessions {
		var oldestID string
		var oldest time.Time
		for id, s := range sessCache {
			if oldestID == "" || s.lastUsed.Before(oldest) {
				oldestID, oldest = id, s.lastUsed
			}
		}
		victim := sessCache[oldestID]
		delete(sessCache, oldestID)
		if victim != nil {
			victim.teardown()
		}
	}
}

// CloseAllSessions tears down every cached session. Call on reset/shutdown.
func CloseAllSessions() {
	sessCacheMu.Lock()
	victims := make([]*Session, 0, len(sessCache))
	for id, s := range sessCache {
		victims = append(victims, s)
		delete(sessCache, id)
	}
	sessCacheMu.Unlock()
	for _, s := range victims {
		s.teardown()
	}
}

// Ask runs one turn over the supplied history and returns the assistant reply.
// In warm-resume mode the coding agent already holds the prior context, so only
// the newest message is sent; otherwise the full history is replayed.
func (s *Session) Ask(ctx context.Context, history []Message) (string, error) {
	if s.resume && len(history) > 0 {
		history = history[len(history)-1:]
	}
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
	return strings.TrimSpace(sanitizeReply(reply)), nil
}

// sanitizeReply strips internal CLI/transport notices that occasionally bleed
// into the captured assistant text. The coding CLI prints a line like
// "Shell cwd was reset to <dir>" when a command leaves the working directory
// changed; it is machine chatter, never meant for the parent, so drop it.
func sanitizeReply(reply string) string {
	if !strings.Contains(reply, "cwd was reset") {
		return reply
	}
	lines := strings.Split(reply, "\n")
	kept := lines[:0]
	for _, ln := range lines {
		if strings.Contains(ln, "cwd was reset") {
			continue
		}
		kept = append(kept, ln)
	}
	return strings.Join(kept, "\n")
}

// Agent exposes the underlying agent for advanced callers (event listeners,
// usage stats). May be nil after Close.
func (s *Session) Agent() *mcpagent.Agent { return s.agent }

// Close tears down the agent and the executor server. Safe to call more than
// once. A cached (warm-resume) session is normally closed only by the cache
// (eviction / CloseAllSessions); if closed directly it also drops its cache
// entry so a later Acquire rebuilds it instead of handing back a dead session.
func (s *Session) Close() {
	if s == nil {
		return
	}
	if s.id != "" {
		sessCacheMu.Lock()
		if sessCache[s.id] == s {
			delete(sessCache, s.id)
		}
		sessCacheMu.Unlock()
	}
	s.teardown()
}

// teardown shuts down the agent + executor without touching the cache. Callers
// that already hold sessCacheMu (evictLocked, CloseAllSessions) use this to
// avoid re-locking the non-reentrant mutex.
func (s *Session) teardown() {
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
