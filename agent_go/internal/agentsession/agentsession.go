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
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
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

// Handle is a provider-native continuation handle (mcpagent's AgentSessionHandle)
// — for Claude Code it carries the CLI's own `--resume` session UUID. Persist it
// as opaque JSON per conversation and hand it back via Config.SessionHandle on the
// next turn: that is what lets a coding-agent restore full prior context after a
// process restart WITHOUT replaying the transcript (the CLI reloads it from its
// own on-disk session store). This is exactly how AgentWorks survives restarts.
type Handle = mcpagent.AgentSessionHandle

// Config parameterizes a Session. Only Provider, WorkingDir and Tools are
// really required for a useful session.
type Config struct {
	Provider llm.Provider // e.g. llm.ProviderClaudeCode
	ModelID  string       // "" -> llm.GetDefaultModel(provider)
	// ReasoningEffort, when set ("low"|"medium"|"high"|"max"), sets the model's
	// reasoning/thinking effort for providers that support it (Claude Code does).
	// Empty leaves the provider/model default. Plumbed via mcpagent.WithLLMConfig
	// as the primary model's Options["reasoning_effort"].
	ReasoningEffort string
	WorkingDir      string // scope root (Family/parent). "" -> process cwd
	SystemPrompt    string // agent persona / instructions
	Tools           []Tool // app-specific custom tools
	Logger          loggerv2.Logger
	MaxTurns        int // 0 -> provider default
	// SessionID, when set, makes turns RESUME the coding agent's own session
	// (warm tmux/session resume) instead of cold-starting a fresh one. Use a
	// stable id per conversation (e.g. the conversation id). Empty -> fresh
	// throwaway session each turn (full-history replay).
	SessionID string
	// SessionHandle, when non-nil, restores the coding agent's provider-native
	// continuation state (for Claude Code: its `--resume` session UUID) BEFORE
	// the turn runs — so the CLI reloads full prior context from its own on-disk
	// session store instead of us replaying the transcript. This is the durable,
	// cross-restart context mechanism (the warm tmux session is only a
	// same-process speed path and dies on restart). Capture the fresh handle
	// after each turn via Session.Handle(), persist it per conversation, and pass
	// it back here on the next turn. Exactly the AgentWorks model. Nil on the very
	// first turn of a brand-new conversation (nothing to resume yet).
	SessionHandle *Handle
	// BridgeRoutingInstructions, when non-nil, overrides mcpagent's default
	// per-provider bridge-tool-routing system-prompt text (see
	// mcpagent.WithBridgeRoutingInstructions and
	// docs/core/mcp_bridge_layer.md) — nil keeps mcpagent's default
	// (unconditionally applied for every provider); a pointer to "" suppresses
	// the block entirely; a non-empty string replaces it with this app's own
	// wording. Left unset (nil) for now — the default is left unchanged
	// everywhere this Config is built; this field only exists so a caller can
	// opt into custom wording later without further agentsession changes.
	BridgeRoutingInstructions *string
}

// Session bundles a live agent with its in-process executor server. Not safe
// for concurrent Ask calls; create one Session per conversation turn (cheap for
// a low-QPS local app) or serialize access.
type Session struct {
	agent    *mcpagent.Agent
	logger   loggerv2.Logger
	shutdown func()
	closed   bool
	resume   bool // warm-resume mode: the coding agent keeps context across turns
}

// New builds a per-turn Session. Following the AgentWorks model, it reuses the
// process-global executor / MCP bridge (started once, on first use) and creates
// only the lightweight coding-agent for this turn. The bridge is long-lived and
// shared; the Session is cheap and disposable. The caller must Close() it, which
// closes ONLY the per-turn agent — never the shared bridge, and never the
// provider-owned interactive (tmux) session, so a warm-resume conversation stays
// warm across turns. Create one Session per turn (as AgentWorks rebuilds its
// per-turn agent wrapper); a stable SessionID makes the provider resume the same
// coding-agent CLI from its own owner registry.
func New(ctx context.Context, cfg Config) (*Session, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = loggerv2.NewNoop()
	}

	// Reuse the one shared executor/bridge (binary + MCP config + executor HTTP
	// server + env), started once for the whole process.
	b, err := ensureSharedBridge(logger)
	if err != nil {
		return nil, err
	}

	// Create the agent. The provider integration appenders apply bridge-only
	// access automatically at generation time; WithCodeExecutionMode(true) also
	// builds the tool index. WithSessionID scopes the custom-tool registry the
	// bridge resolves against.
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
	if effort := strings.TrimSpace(cfg.ReasoningEffort); effort != "" {
		// Set the primary model's reasoning/thinking effort. GetLLMModelConfig
		// returns LLMConfig.Primary when its Provider is set, so specify the full
		// model here (provider + id) alongside the Options.
		opts = append(opts, mcpagent.WithLLMConfig(mcpagent.AgentLLMConfiguration{
			Primary: mcpagent.LLMModel{
				Provider: string(cfg.Provider),
				ModelID:  modelID,
				Options:  map[string]interface{}{"reasoning_effort": effort},
			},
		}))
	}
	if resume {
		// Keep the coding agent's interactive (tmux) session alive so the next
		// turn resumes it with full context instead of cold-starting. The
		// provider owns that session in its registry and reaps it on idle.
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
	if cfg.BridgeRoutingInstructions != nil {
		opts = append(opts, mcpagent.WithBridgeRoutingInstructions(*cfg.BridgeRoutingInstructions))
	}
	if len(cfg.Tools) > 0 {
		// Expose every app-registered custom tool as a NATIVE bridge tool for
		// THIS agent — scoped to this session only, never touching mcpagent's
		// shared package-level bridgeTools list (which stays fixed across
		// every consumer of that module; see docs/core/mcp_bridge_layer.md).
		names := make([]string, 0, len(cfg.Tools))
		for _, t := range cfg.Tools {
			names = append(names, t.Name)
		}
		opts = append(opts, mcpagent.WithAdditionalBridgeTools(names...))
	}

	agent, err := mcpagent.NewAgent(ctx, model, b.mcpConfigPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	// Register the app-specific custom tools. This publishes them into the
	// session-scoped codeexec registry (agent.go: InitRegistryForSession) so the
	// shared executor server resolves /tools/custom/{name} calls to these
	// handlers. Native bridge exposure is handled above via
	// WithAdditionalBridgeTools — scoped to this agent, not the shared
	// mcpagent bridgeTools list.
	for _, t := range cfg.Tools {
		category := t.Category
		if strings.TrimSpace(category) == "" {
			category = "family_tools"
		}
		if err := agent.RegisterCustomTool(t.Name, t.Description, t.Params, t.Handler, category); err != nil {
			agent.Close()
			return nil, fmt.Errorf("register tool %q: %w", t.Name, err)
		}
	}

	// Restore provider-native continuation state (Claude Code's `--resume` UUID,
	// etc.) BEFORE the first generation so the CLI reloads full prior context
	// from its own session store — the durable, cross-restart path. Applied even
	// though the warm tmux may already hold context in-process: the two coexist
	// (the provider reuses a live tmux when present, else mints a fresh one and
	// resumes via this handle). This is what AgentWorks does after a restart.
	if cfg.SessionHandle != nil && !cfg.SessionHandle.Empty() {
		agent.ApplyAgentSessionHandle(cfg.SessionHandle)
	}

	// Track the warm-resume owner so /api/reset can proactively close its tmux
	// session (the provider otherwise reaps it on idle).
	if resume {
		rememberInteractiveOwner(sessionID, cfg.Provider)
	}

	s := &Session{
		agent:    agent,
		logger:   logger,
		resume:   resume,
		shutdown: agent.Close, // per-turn agent only; shared bridge + tmux persist
	}
	return s, nil
}

// ---------- process-global executor / MCP bridge + warm-owner tracking ----------
//
// AgentWorks runs ONE executor / MCP bridge for the whole process (its bridge is
// the main server's own route set, wired once at startup) and keeps warm
// coding-agent (tmux) sessions in the provider's owner registry, reaped by an
// idle timeout — there is no LRU or size cap. SparkQuill mirrors that: a single
// shared bridge (below), per-turn Sessions, and warm resume owned by the
// provider. We keep only a set of owner ids so reset can close their tmux.

// sharedBridge is the process-global executor/MCP bridge, created once.
type sharedBridge struct {
	mcpConfigPath string
	shutdown      func() // executor + config cleanup; only run at process exit
}

var (
	bridgeOnce sync.Once
	bridge     *sharedBridge
	bridgeErr  error

	ownerMu    sync.Mutex
	warmOwners = map[string]llm.Provider{}
)

// ensureSharedBridge starts the process-global executor / MCP bridge exactly
// once and returns it on every later call. Following AgentWorks — whose bridge
// is the main server's own route set, wired once at startup — the bridge binary,
// MCP config, executor HTTP server, and the MCP_* env the CLIs read are set up a
// single time and shared by every conversation and skill run. The persistent
// coding-agent CLIs call back into this always-alive endpoint, so a resumed turn
// never hits a dead bridge. It is deliberately never torn down per turn.
func ensureSharedBridge(logger loggerv2.Logger) (*sharedBridge, error) {
	bridgeOnce.Do(func() {
		bridgePath, err := ensureBridgeBinary(logger)
		if err != nil {
			bridgeErr = err
			return
		}
		os.Setenv("MCP_BRIDGE_BINARY", bridgePath)

		// No upstream MCP servers — all tools are custom and resolved in-process.
		mcpConfigPath, cleanupConfig, err := writeMinimalMCPConfig()
		if err != nil {
			bridgeErr = err
			return
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			cleanupConfig()
			bridgeErr = fmt.Errorf("allocate executor listener: %w", err)
			return
		}
		_, port, _ := net.SplitHostPort(listener.Addr().String())
		hostURL := "http://127.0.0.1:" + port

		// Custom tools run on the host, so the in-Docker URL and the bridge (host)
		// URL both point at this one executor server.
		apiToken := executor.GenerateAPIToken()
		os.Setenv("MCP_API_URL", hostURL)
		os.Setenv("MCP_API_TOKEN", apiToken)
		os.Setenv("MCP_BRIDGE_API_URL", hostURL)

		execShutdown, err := startExecutorServer(logger, mcpConfigPath, listener, apiToken)
		if err != nil {
			listener.Close()
			cleanupConfig()
			bridgeErr = fmt.Errorf("start executor server: %w", err)
			return
		}
		time.Sleep(300 * time.Millisecond) // let it begin serving before first turn

		bridge = &sharedBridge{
			mcpConfigPath: mcpConfigPath,
			shutdown: func() {
				execShutdown()
				cleanupConfig()
			},
		}
	})
	return bridge, bridgeErr
}

// rememberInteractiveOwner records a warm-resume owner (conversation id + its
// provider) so CloseAllInteractiveSessions can proactively close its tmux
// session on reset. This is just a set of ids, not a session cache.
func rememberInteractiveOwner(sessionID string, provider llm.Provider) {
	ownerMu.Lock()
	warmOwners[sessionID] = provider
	ownerMu.Unlock()
}

// CloseAllInteractiveSessions closes every warm coding-agent (tmux) session we
// have started, via the provider's owner-scoped close. Use on reset/shutdown for
// a clean slate; absent this call the provider reaps them on idle anyway. There
// is no LRU or size cap here — matching AgentWorks.
func CloseAllInteractiveSessions() {
	ownerMu.Lock()
	owners := make(map[string]llm.Provider, len(warmOwners))
	for id, p := range warmOwners {
		owners[id] = p
	}
	warmOwners = map[string]llm.Provider{}
	ownerMu.Unlock()

	for id, p := range owners {
		switch p {
		case llm.ProviderClaudeCode:
			llmproviders.CloseClaudeCodeInteractiveSessionForOwner(id, "reset")
		case llm.ProviderCodexCLI:
			llmproviders.CloseCodexCLIInteractiveSessionForOwner(id, "reset")
		case llm.ProviderCursorCLI:
			llmproviders.CloseCursorCLIInteractiveSessionForOwner(id, "reset")
		case llm.ProviderPiCLI:
			llmproviders.ClosePiCLIInteractiveSessionForOwner(id, "reset")
		}
	}
}

// Ask runs one turn over the supplied history and returns the assistant reply.
// In resume mode the coding agent holds prior context itself — via the live warm
// tmux session (same process) or the restored SessionHandle's `--resume` state
// (after a restart) — so only the newest message is sent. This mirrors
// AgentWorks: the persistent/resumed CLI reconstructs history from its own store;
// the provider adapter only ever forwards the latest message in this mode, so
// replaying the full thread here would be dropped anyway. Non-resume (throwaway)
// sessions send the whole thread, since there is nothing to resume.
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

// Handle returns the coding agent's latest provider-native continuation handle
// (Claude Code's `--resume` UUID, etc.), captured from the just-completed turn.
// Persist it per conversation and pass it back via Config.SessionHandle next turn
// so context survives a process restart. Returns nil if the provider produced no
// resumable handle (e.g. a throwaway non-resume session). Call after Ask, before
// Close (it reads live agent state).
func (s *Session) Handle() *Handle {
	if s == nil || s.agent == nil {
		return nil
	}
	return s.agent.CurrentAgentSessionHandle()
}

// Close disposes the per-turn agent. Safe to call more than once. It closes ONLY
// this turn's agent — never the process-global bridge and never the provider's
// interactive (tmux) session, which is owned by the provider registry and
// persists so a warm-resume conversation stays warm across turns.
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
