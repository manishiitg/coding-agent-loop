package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	providerSetupOutputLimit = 1024 * 1024
	providerSetupMaxDuration = 20 * time.Minute
	providerSetupRetention   = 10 * time.Minute
)

type providerSetupCommand struct {
	command string
	args    []string
}

// Provider setup never accepts a command from the browser. Each supported
// action maps to one reviewed argv so this feature cannot become a general
// remote shell.
var providerSetupCommands = map[string]map[string]providerSetupCommand{
	"claude-code": {
		"authenticate": {command: "claude"},
	},
	"codex-cli": {
		"authenticate": {command: "codex", args: []string{"login"}},
	},
	"cursor-cli": {
		"authenticate": {command: "cursor-agent", args: []string{"login"}},
	},
	"muse-cli": {
		"authenticate": {command: "muse", args: []string{"login"}},
	},
}

type providerSetupSnapshot struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	ExitCode  *int      `json:"exit_code,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type providerSetupSession struct {
	mu          sync.Mutex
	id          string
	provider    string
	action      string
	ownerID     string
	status      string
	exitCode    *int
	errMessage  string
	createdAt   time.Time
	updatedAt   time.Time
	terminal    *os.File
	cancel      context.CancelFunc
	output      []byte
	subscribers map[chan []byte]struct{}
}

func (s *providerSetupSession) snapshot() providerSetupSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return providerSetupSnapshot{
		ID:        s.id,
		Provider:  s.provider,
		Action:    s.action,
		Status:    s.status,
		ExitCode:  s.exitCode,
		Error:     s.errMessage,
		CreatedAt: s.createdAt,
		UpdatedAt: s.updatedAt,
	}
}

func (s *providerSetupSession) isOwnedBy(userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ownerID == userID
}

func (s *providerSetupSession) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == "running"
}

func (s *providerSetupSession) appendOutput(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	copyChunk := append([]byte(nil), chunk...)
	s.mu.Lock()
	s.output = append(s.output, copyChunk...)
	if len(s.output) > providerSetupOutputLimit {
		s.output = append([]byte(nil), s.output[len(s.output)-providerSetupOutputLimit:]...)
	}
	s.updatedAt = time.Now().UTC()
	for subscriber := range s.subscribers {
		select {
		case subscriber <- copyChunk:
		default:
			// A reconnect receives the bounded transcript seed. Never let a slow
			// browser block the provider command or its authentication prompts.
		}
	}
	s.mu.Unlock()
}

func (s *providerSetupSession) finish(err error) {
	s.mu.Lock()
	if s.status != "running" {
		s.mu.Unlock()
		return
	}
	code := 0
	if err != nil {
		code = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		s.status = "failed"
		s.errMessage = err.Error()
	} else {
		s.status = "completed"
	}
	s.exitCode = &code
	s.updatedAt = time.Now().UTC()
	for subscriber := range s.subscribers {
		close(subscriber)
		delete(s.subscribers, subscriber)
	}
	s.mu.Unlock()
}

func (s *providerSetupSession) subscribe() ([]byte, <-chan []byte, func()) {
	channel := make(chan []byte, 128)
	s.mu.Lock()
	seed := append([]byte(nil), s.output...)
	if s.status == "running" {
		s.subscribers[channel] = struct{}{}
	} else {
		close(channel)
	}
	s.mu.Unlock()
	return seed, channel, func() {
		s.mu.Lock()
		if _, ok := s.subscribers[channel]; ok {
			delete(s.subscribers, channel)
			close(channel)
		}
		s.mu.Unlock()
	}
}

func (s *providerSetupSession) write(data string) error {
	s.mu.Lock()
	terminal := s.terminal
	running := s.status == "running"
	s.mu.Unlock()
	if !running || terminal == nil {
		return errors.New("provider setup is not running")
	}
	_, err := io.WriteString(terminal, data)
	return err
}

func (s *providerSetupSession) resize(cols, rows int) error {
	cols, rows, ok := clampLiveAttachGeometry(cols, rows)
	if !ok {
		return errors.New("invalid terminal size")
	}
	s.mu.Lock()
	terminal := s.terminal
	running := s.status == "running"
	s.mu.Unlock()
	if !running || terminal == nil {
		return nil
	}
	return pty.Setsize(terminal, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

func (s *providerSetupSession) stop() {
	s.mu.Lock()
	if s.status != "running" {
		s.mu.Unlock()
		return
	}
	s.status = "cancelled"
	s.errMessage = "Cancelled by user"
	s.updatedAt = time.Now().UTC()
	cancel := s.cancel
	terminal := s.terminal
	for subscriber := range s.subscribers {
		close(subscriber)
		delete(s.subscribers, subscriber)
	}
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if terminal != nil {
		_ = terminal.Close()
	}
}

type providerSetupManager struct {
	mu       sync.Mutex
	sessions map[string]*providerSetupSession
}

func newProviderSetupManager() *providerSetupManager {
	return &providerSetupManager{sessions: make(map[string]*providerSetupSession)}
}

func (m *providerSetupManager) start(ownerID, provider, action string, cols, rows int) (*providerSetupSession, error) {
	actions, ok := providerSetupCommands[provider]
	if !ok {
		return nil, fmt.Errorf("guided setup is not available for provider %q", provider)
	}
	spec, ok := actions[action]
	if !ok {
		return nil, fmt.Errorf("guided %s is not available for provider %q", action, provider)
	}
	if _, err := exec.LookPath(spec.command); err != nil {
		return nil, fmt.Errorf("required command %q is not available on the server", spec.command)
	}

	m.mu.Lock()
	for _, existing := range m.sessions {
		if existing.provider == provider && existing.isRunning() {
			m.mu.Unlock()
			return nil, fmt.Errorf("a %s setup session is already running", provider)
		}
	}
	m.mu.Unlock()

	id, err := randomProviderSetupID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), providerSetupMaxDuration)
	command := exec.CommandContext(ctx, spec.command, spec.args...)
	command.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	cols, rows, validSize := clampLiveAttachGeometry(cols, rows)
	if !validSize {
		cols, rows = liveAttachDefaultCols, liveAttachDefaultRows
	}
	terminal, err := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start %s %s: %w", provider, action, err)
	}
	now := time.Now().UTC()
	session := &providerSetupSession{
		id:          id,
		provider:    provider,
		action:      action,
		ownerID:     ownerID,
		status:      "running",
		createdAt:   now,
		updatedAt:   now,
		terminal:    terminal,
		cancel:      cancel,
		subscribers: make(map[chan []byte]struct{}),
	}
	m.mu.Lock()
	m.sessions[id] = session
	m.mu.Unlock()

	go func() {
		buffer := make([]byte, 32*1024)
		for {
			n, readErr := terminal.Read(buffer)
			if n > 0 {
				session.appendOutput(buffer[:n])
			}
			if readErr != nil {
				break
			}
		}
		waitErr := command.Wait()
		cancel()
		_ = terminal.Close()
		session.finish(waitErr)
		time.AfterFunc(providerSetupRetention, func() { m.remove(id, false) })
	}()

	return session, nil
}

func (m *providerSetupManager) get(id string) (*providerSetupSession, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	return session, ok
}

func (m *providerSetupManager) remove(id string, stop bool) {
	m.mu.Lock()
	session := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if stop && session != nil {
		session.stop()
	}
}

func randomProviderSetupID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("create provider setup id: %w", err)
	}
	return "provider-setup-" + hex.EncodeToString(value), nil
}

func (api *StreamingAPI) providerSetupManager() *providerSetupManager {
	api.providerSetupMu.Lock()
	defer api.providerSetupMu.Unlock()
	if api.providerSetups == nil {
		api.providerSetups = newProviderSetupManager()
	}
	return api.providerSetups
}

type startProviderSetupRequest struct {
	Provider string `json:"provider"`
	Action   string `json:"action"`
	Cols     int    `json:"cols,omitempty"`
	Rows     int    `json:"rows,omitempty"`
}

func (api *StreamingAPI) handleStartProviderSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	var request startProviderSetupRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&request); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}
	request.Provider = strings.TrimSpace(request.Provider)
	request.Action = strings.TrimSpace(request.Action)
	session, err := api.providerSetupManager().start(GetUserIDFromContext(r.Context()), request.Provider, request.Action, request.Cols, request.Rows)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"session": session.snapshot()})
}

func (api *StreamingAPI) resolveProviderSetup(w http.ResponseWriter, r *http.Request) (*providerSetupSession, bool) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	session, ok := api.providerSetupManager().get(id)
	if !ok {
		http.Error(w, `{"error":"provider setup session not found"}`, http.StatusNotFound)
		return nil, false
	}
	if !session.isOwnedBy(GetUserIDFromContext(r.Context())) {
		http.Error(w, `{"error":"provider setup session not found"}`, http.StatusNotFound)
		return nil, false
	}
	return session, true
}

func (api *StreamingAPI) handleProviderSetupSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	session, ok := api.resolveProviderSetup(w, r)
	if !ok {
		return
	}
	if r.Method == http.MethodDelete {
		api.providerSetupManager().remove(session.id, true)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"session": session.snapshot()})
}

type providerSetupClientMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (api *StreamingAPI) handleProviderSetupStream(w http.ResponseWriter, r *http.Request) {
	if !api.checkLiveAttachOrigin(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	session, ok := api.resolveProviderSetup(w, r)
	if !ok {
		return
	}
	upgrader := api.liveAttachUpgrader()
	connection, err := upgrader.Upgrade(unwrapResponseWriter(w), r, nil)
	if err != nil {
		return
	}
	defer connection.Close()

	seed, output, unsubscribe := session.subscribe()
	defer unsubscribe()
	if len(seed) > 0 {
		if err := connection.WriteMessage(websocket.BinaryMessage, seed); err != nil {
			return
		}
	}

	clientClosed := make(chan struct{})
	go func() {
		defer close(clientClosed)
		for {
			var message providerSetupClientMessage
			if err := connection.ReadJSON(&message); err != nil {
				return
			}
			switch message.Type {
			case "input":
				if len(message.Data) <= 16*1024 {
					_ = session.write(message.Data)
				}
			case "resize":
				_ = session.resize(message.Cols, message.Rows)
			}
		}
	}()

	for {
		select {
		case chunk, open := <-output:
			if !open {
				_ = connection.WriteJSON(map[string]interface{}{"type": "exit", "session": session.snapshot()})
				return
			}
			if err := connection.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		case <-clientClosed:
			return
		}
	}
}
