package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	agycliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/agycli"
	claudecodeadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/claudecode"
	codexcliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/codexcli"
	geminicliadapter "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/geminicli"

	"mcp-agent-builder-go/agent_go/internal/terminals"
)

type terminalPipeRecorderRealCLICase struct {
	name         string
	envGate      string
	binaries     []string
	modelEnvKeys []string
	defaultModel string
	cleanup      func(context.Context) error
	build        func(t *testing.T, ownerSessionID, model, workingDir string) (llmtypes.Model, []llmtypes.CallOption)
}

// TestTerminalPipeRecorderHTTPWithRealGeminiCLITmux drives the real Gemini CLI
// interactive tmux transport through the production terminal stream -> store ->
// pipe recorder -> HTTP detail path. It is opt-in because it requires local CLI
// auth/subscription state.
func TestTerminalPipeRecorderHTTPWithRealGeminiCLITmux(t *testing.T) {
	runTerminalPipeRecorderRealCLIE2E(t, terminalPipeRecorderRealCLICase{
		name:         "gemini-cli",
		envGate:      "RUN_GEMINI_CLI_INTERACTIVE_E2E",
		binaries:     []string{"gemini", "tmux", "node"},
		modelEnvKeys: []string{"GEMINI_CLI_CONTRACT_MODEL", "GEMINI_CLI_REAL_E2E_MODEL"},
		defaultModel: "low",
		cleanup:      geminicliadapter.CleanupGeminiCLIInteractiveSessions,
		build: func(t *testing.T, ownerSessionID, model, workingDir string) (llmtypes.Model, []llmtypes.CallOption) {
			t.Helper()
			return geminicliadapter.NewGeminiCLIAdapter("", model, &e2eMockLogger{}), []llmtypes.CallOption{
				geminicliadapter.WithInteractiveSessionID(ownerSessionID),
				geminicliadapter.WithPersistentInteractiveSession(true),
				geminicliadapter.WithWorkingDir(workingDir),
				geminicliadapter.WithProjectSettings(`{}`),
				geminicliadapter.WithApprovalMode("yolo"),
			}
		},
	})
}

// TestTerminalPipeRecorderHTTPWithRealCodexCLITmux is the same live path for
// Codex CLI. The default mirrors the repo's existing Codex tmux e2e model.
func TestTerminalPipeRecorderHTTPWithRealCodexCLITmux(t *testing.T) {
	runTerminalPipeRecorderRealCLIE2E(t, terminalPipeRecorderRealCLICase{
		name:         "codex-cli",
		envGate:      "RUN_CODEX_CLI_INTERACTIVE_E2E",
		binaries:     []string{"codex", "tmux", "node"},
		modelEnvKeys: []string{"CODEX_CLI_REAL_CONTRACT_MODEL", "CODEX_REAL_E2E_MODEL"},
		defaultModel: "gpt-5.3-codex-spark",
		cleanup:      codexcliadapter.CleanupCodexCLIInteractiveSessions,
		build: func(t *testing.T, ownerSessionID, model, workingDir string) (llmtypes.Model, []llmtypes.CallOption) {
			t.Helper()
			return codexcliadapter.NewCodexCLIAdapter("", model, &e2eMockLogger{}), []llmtypes.CallOption{
				codexcliadapter.WithInteractiveSessionID(ownerSessionID),
				codexcliadapter.WithPersistentInteractiveSession(true),
				codexcliadapter.WithProjectDirID(workingDir),
				codexcliadapter.WithDisableShellTool(),
				codexcliadapter.WithApprovalPolicy("never"),
				codexcliadapter.WithReasoningEffort("low"),
			}
		},
	})
}

// TestTerminalPipeRecorderHTTPWithRealClaudeCodeTmux keeps the original
// reported provider covered by the same HTTP-level assertion.
func TestTerminalPipeRecorderHTTPWithRealClaudeCodeTmux(t *testing.T) {
	runTerminalPipeRecorderRealCLIE2E(t, terminalPipeRecorderRealCLICase{
		name:         "claude-code",
		envGate:      "RUN_CLAUDE_CODE_EXPERIMENTAL_LIVE_E2E",
		binaries:     []string{"claude", "tmux"},
		modelEnvKeys: []string{"CLAUDE_CODE_EXPERIMENTAL_MODEL", "CLAUDECODE_REAL_E2E_MODEL"},
		defaultModel: "claude-haiku-4-5-20251001",
		cleanup:      claudecodeadapter.CleanupClaudeCodeTmuxSessions,
		build: func(t *testing.T, ownerSessionID, model, workingDir string) (llmtypes.Model, []llmtypes.CallOption) {
			t.Helper()
			return claudecodeadapter.NewClaudeCodeInteractiveAdapter(model, &e2eMockLogger{}), []llmtypes.CallOption{
				claudecodeadapter.WithInteractiveSessionID(ownerSessionID),
				claudecodeadapter.WithPersistentInteractiveSession(true),
				claudecodeadapter.WithWorkingDir(workingDir),
			}
		},
	})
}

// TestTerminalPipeRecorderHTTPWithRealAgyCLITmux covers Antigravity/Agy for
// teams that run the Agy live gate.
func TestTerminalPipeRecorderHTTPWithRealAgyCLITmux(t *testing.T) {
	runTerminalPipeRecorderRealCLIE2E(t, terminalPipeRecorderRealCLICase{
		name:         "agy-cli",
		envGate:      "RUN_AGY_CLI_REAL_E2E",
		binaries:     []string{"agy", "tmux"},
		modelEnvKeys: []string{"AGY_CLI_REAL_E2E_MODEL"},
		defaultModel: "agy-cli",
		cleanup:      agycliadapter.CleanupAgyCLIInteractiveSessions,
		build: func(t *testing.T, ownerSessionID, model, workingDir string) (llmtypes.Model, []llmtypes.CallOption) {
			t.Helper()
			return agycliadapter.NewAgyCLIAdapter("", model, &e2eMockLogger{}), []llmtypes.CallOption{
				agycliadapter.WithInteractiveSessionID(ownerSessionID),
				agycliadapter.WithPersistentInteractiveSession(true),
				agycliadapter.WithWorkingDir(workingDir),
			}
		},
	})
}

func runTerminalPipeRecorderRealCLIE2E(t *testing.T, tc terminalPipeRecorderRealCLICase) {
	t.Helper()
	if os.Getenv(tc.envGate) == "" {
		t.Skipf("set %s=1 to run the %s terminal pipe HTTP e2e", tc.envGate, tc.name)
	}
	for _, binary := range tc.binaries {
		if _, err := exec.LookPath(binary); err != nil {
			t.Skipf("%s requires %s in PATH: %v", tc.name, binary, err)
		}
	}
	if tc.cleanup != nil {
		t.Cleanup(func() { _ = tc.cleanup(context.Background()) })
	}

	model := terminalPipeRecorderRealModel(tc.modelEnvKeys, tc.defaultModel)
	ownerSessionID := terminalPipeRecorderRealID(tc.name)
	sessionID := "terminal-pipe-real-http-" + ownerSessionID
	terminalOwnerID := "main:" + sessionID
	terminalID := sessionID + ":" + terminalOwnerID
	workingDir := t.TempDir()
	adapter, extraOpts := tc.build(t, ownerSessionID, model, workingDir)

	store := terminals.NewStore()
	recorder := &terminalPipeRecorder{
		root:     t.TempDir(),
		sessions: make(map[string]*terminalPipeRecording),
	}
	api := &StreamingAPI{
		terminalStore:        store,
		terminalPipeRecorder: recorder,
	}

	firstMarker := "RUNLOOP_PIPE_SCROLL_FIRST_" + terminalPipeRecorderRealSuffix()
	lastMarker := "RUNLOOP_PIPE_SCROLL_LAST_" + terminalPipeRecorderRealSuffix()
	prompt := terminalPipeRecorderRealPrompt(firstMarker, lastMarker)

	streamChan := make(chan llmtypes.StreamChunk, 256)
	drain := newTerminalPipeRecorderRealDrain(store, recorder, sessionID, terminalOwnerID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for chunk := range streamChan {
			drain.add(chunk)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	callOpts := append([]llmtypes.CallOption{}, extraOpts...)
	callOpts = append(callOpts, llmtypes.WithStreamingChan(streamChan), llmtypes.WithMaxTokens(512))
	resp, err := adapter.GenerateContent(ctx, []llmtypes.MessageContent{
		{Role: llmtypes.ChatMessageTypeSystem, Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{Text: "Do not use tools. Follow the user's formatting request directly."},
		}},
		{Role: llmtypes.ChatMessageTypeHuman, Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{Text: prompt},
		}},
	}, callOpts...)
	safeCloseTerminalPipeRecorderRealStream(streamChan)
	<-done
	if err != nil {
		t.Fatalf("%s GenerateContent: %v", tc.name, err)
	}

	drain.seedFromResponseIfNeeded(t, resp)
	if drain.terminalChunkCount() == 0 {
		t.Fatalf("%s emitted zero terminal chunks; HTTP terminal detail would have no live pane to show", tc.name)
	}
	if strings.TrimSpace(drain.tmuxSession()) == "" {
		t.Fatalf("%s terminal chunks did not expose tmux_session metadata", tc.name)
	}

	detail := waitForTerminalPipeRecorderRealDetail(t, api, terminalID, []string{firstMarker, lastMarker})
	if detail.ContentSource != "tmux_pipe" {
		t.Fatalf("%s content_source = %q, want tmux_pipe", tc.name, detail.ContentSource)
	}
	if !strings.Contains(detail.Content, firstMarker) || !strings.Contains(detail.Content, lastMarker) {
		t.Fatalf("%s pipe HTTP detail lost scrollback markers first=%t last=%t content:\n%s",
			tc.name,
			strings.Contains(detail.Content, firstMarker),
			strings.Contains(detail.Content, lastMarker),
			detail.Content,
		)
	}

	t.Logf("%s pipe HTTP e2e: terminal_chunks=%d tmux=%q bytes=%d model=%q",
		tc.name, drain.terminalChunkCount(), drain.tmuxSession(), len(detail.Content), model)
}

type terminalPipeRecorderRealDrain struct {
	mu                sync.Mutex
	store             *terminals.Store
	recorder          *terminalPipeRecorder
	sessionID         string
	terminalOwnerID   string
	tmux              string
	terminalChunks    int
	lastTerminalChunk string
	lastChunkIndex    int
}

func newTerminalPipeRecorderRealDrain(store *terminals.Store, recorder *terminalPipeRecorder, sessionID, terminalOwnerID string) *terminalPipeRecorderRealDrain {
	return &terminalPipeRecorderRealDrain{
		store:           store,
		recorder:        recorder,
		sessionID:       sessionID,
		terminalOwnerID: terminalOwnerID,
	}
}

func (d *terminalPipeRecorderRealDrain) add(chunk llmtypes.StreamChunk) {
	if chunk.Type == llmtypes.StreamChunkTypeStatusLine && chunk.StatusLine != nil {
		if tmux := tmuxSessionFromMetadata(chunk.StatusLine.Metadata); tmux != "" {
			d.setTmux(tmux)
		}
		return
	}
	if chunk.Type != llmtypes.StreamChunkTypeTerminal {
		return
	}
	tmuxSession := tmuxSessionFromMetadata(chunk.Metadata)
	d.mu.Lock()
	if tmuxSession != "" {
		d.tmux = tmuxSession
	}
	if strings.TrimSpace(chunk.Content) != "" {
		d.terminalChunks++
		d.lastChunkIndex++
		d.lastTerminalChunk = chunk.Content
		d.store.HandleEvent(d.sessionID, terminalRouteChunkEvent(d.sessionID, d.terminalOwnerID, d.tmux, chunk.Content, d.lastChunkIndex))
		d.recorder.ObserveSnapshots(d.store.List(d.sessionID))
	}
	d.mu.Unlock()
}

func (d *terminalPipeRecorderRealDrain) seedFromResponseIfNeeded(t *testing.T, resp *llmtypes.ContentResponse) {
	t.Helper()
	handle, ok := llmtypes.ExtractCodingProviderSessionHandleFromResponse(resp)
	if !ok || strings.TrimSpace(handle.TmuxSession) == "" {
		return
	}
	d.mu.Lock()
	if d.tmux == "" {
		d.tmux = handle.TmuxSession
	}
	if d.terminalChunks > 0 {
		if strings.TrimSpace(d.lastTerminalChunk) != "" {
			d.lastChunkIndex++
			d.store.HandleEvent(d.sessionID, terminalRouteChunkEvent(d.sessionID, d.terminalOwnerID, handle.TmuxSession, d.lastTerminalChunk, d.lastChunkIndex))
			d.recorder.ObserveSnapshots(d.store.List(d.sessionID))
		}
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
}

func (d *terminalPipeRecorderRealDrain) setTmux(tmuxSession string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(tmuxSession) != "" {
		d.tmux = tmuxSession
	}
}

func (d *terminalPipeRecorderRealDrain) tmuxSession() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.tmux
}

func (d *terminalPipeRecorderRealDrain) terminalChunkCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.terminalChunks
}

func waitForTerminalPipeRecorderRealDetail(t *testing.T, api *StreamingAPI, terminalID string, markers []string) terminals.Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var latest terminals.Snapshot
	for time.Now().Before(deadline) {
		latest = getTerminalPipeRecorderRealDetail(t, api, terminalID)
		allMarkers := true
		for _, marker := range markers {
			if !strings.Contains(latest.Content, marker) {
				allMarkers = false
				break
			}
		}
		if latest.ContentSource == "tmux_pipe" && allMarkers {
			return latest
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for pipe-backed terminal detail to contain markers %v; source=%q latest=%q", markers, latest.ContentSource, latest.Content)
	return terminals.Snapshot{}
}

func getTerminalPipeRecorderRealDetail(t *testing.T, api *StreamingAPI, terminalID string) terminals.Snapshot {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/terminals/"+terminalID+"?content=history&debug=1", nil)
	req = mux.SetURLVars(req, map[string]string{"terminal_id": terminalID})
	rec := httptest.NewRecorder()
	api.handleGetTerminal(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get terminal status = %d body=%s", rec.Code, rec.Body.String())
	}
	var snapshot terminals.Snapshot
	if err := json.NewDecoder(rec.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode terminal detail: %v", err)
	}
	return snapshot
}

func terminalPipeRecorderRealPrompt(firstMarker, lastMarker string) string {
	var b strings.Builder
	b.WriteString("Print these lines as plain text. Do not use a code block.\n")
	for i := 1; i <= 30; i++ {
		marker := fmt.Sprintf("middle-%02d", i)
		if i == 1 {
			marker = firstMarker
		}
		if i == 30 {
			marker = lastMarker
		}
		fmt.Fprintf(&b, "line %02d %s\n", i, marker)
	}
	return b.String()
}

func terminalPipeRecorderRealModel(envKeys []string, fallback string) string {
	for _, key := range envKeys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}

func terminalPipeRecorderRealID(provider string) string {
	provider = strings.ToLower(strings.NewReplacer(" ", "-", "_", "-", "/", "-").Replace(provider))
	return provider + "-" + terminalPipeRecorderRealSuffix()
}

func terminalPipeRecorderRealSuffix() string {
	return strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
}

func tmuxSessionFromMetadata(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	for _, key := range []string{"tmux_session", "tmux_session_name"} {
		if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeCloseTerminalPipeRecorderRealStream(streamChan chan llmtypes.StreamChunk) {
	defer func() { _ = recover() }()
	close(streamChan)
}
