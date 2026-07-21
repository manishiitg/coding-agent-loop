package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
)

// familyDataDir is the isolated data root for Family Learning. It never touches
// AgentWorks' workspace-docs. Override with FAMILY_DATA_DIR; defaults to
// ~/.sunlit-learning.
func familyDataDir() string {
	if d := strings.TrimSpace(os.Getenv("FAMILY_DATA_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".sunlit-learning"
	}
	return filepath.Join(home, ".sunlit-learning")
}

func statePath() string { return filepath.Join(familyDataDir(), "family.json") }

// Child is the single MVP child profile.
type Child struct {
	Name      string `json:"name"`
	Grade     string `json:"grade"`
	Board     string `json:"board"`
	Language  string `json:"language"`
	CreatedAt string `json:"created_at"`
	// TeachingStyle controls how the tutor handles a stuck child: "hints-first"
	// (default — never reveal the answer until a genuine attempt), "guided"
	// (a hint, then the answer sooner if still stuck), or "direct" (answer
	// plainly, then explain). Empty means "hints-first" (unchanged behavior for
	// existing families that predate this setting).
	TeachingStyle string `json:"teaching_style,omitempty"`
	// Stars is a simple running encouragement count Quill awards for genuine
	// effort/progress (celebrate tool). Purely positive reinforcement — never
	// decremented, no numeric "score" implication.
	Stars int `json:"stars,omitempty"`
}

// familyState is the persisted onboarding/config state.
type familyState struct {
	Engine  string `json:"engine"`
	Child   *Child `json:"child"`
	PinHash string `json:"pin_hash,omitempty"`
	// ParentLabel is how the parent wants to be referred to when Quill talks
	// ABOUT them to the child — "mom", "dad", "grandma", a first name, etc.
	// Empty means not yet asked/known; Quill asks for it conversationally
	// (parentSystemPrompt's parentLabelNudge) rather than via a setup form.
	ParentLabel string `json:"parent_label,omitempty"`
}

var stateMu sync.Mutex

func loadState() familyState {
	var s familyState
	if b, err := os.ReadFile(statePath()); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveState(s familyState) error {
	if err := os.MkdirAll(familyDataDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(), b, 0o600)
}

// scaffoldFamilyFolders creates the parent/child/shared workspace folders.
// These are the scope roots the FolderGuard will later confine sessions to.
func scaffoldFamilyFolders() error {
	base := filepath.Join(familyDataDir(), "workspace")
	// A clean, role-scoped, content-typed layout (workflow-style: files in folders).
	dirs := []string{
		"parent/notes",         // parent-private notes
		"parent/answer-keys",   // answer keys / marking — parent only
		"parent/conversations", // parent chat history
		"shared/inbox",         // uploads land here; the agent files them (process-file skill)
		"shared/materials",     // uploaded school material (by subject/topic)
		"shared/study",         // generated study material (child-visible once approved)
		"shared/tests",         // generated practice tests
		"shared/reports",       // generated HTML progress reports (parent + child)
		"child/attempts",       // child's submitted work
		"child/conversations",  // child chat history
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, filepath.FromSlash(d)), 0o700); err != nil {
			return err
		}
	}
	return nil
}

type setupResponse struct {
	Engine        string `json:"engine"`
	Child         *Child `json:"child"`
	PinSet        bool   `json:"pin_set"`
	SetupComplete bool   `json:"setup_complete"`
	NextStep      string `json:"next_step"` // "engine" | "child" | "pin" | "done"
	ParentLabel   string `json:"parent_label,omitempty"`
}

func computeSetup(s familyState) setupResponse {
	resp := setupResponse{Engine: s.Engine, Child: s.Child, PinSet: s.PinHash != "", ParentLabel: s.ParentLabel}
	switch {
	case s.Engine == "":
		resp.NextStep = "engine"
	case s.Child == nil:
		resp.NextStep = "child"
	case s.PinHash == "":
		resp.NextStep = "pin"
	default:
		resp.NextStep = "done"
		resp.SetupComplete = true
	}
	return resp
}

// GET /api/setup — where should the app land on launch?
func handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	writeJSON(w, http.StatusOK, computeSetup(s))
}

type selectEngineRequest struct {
	Engine string `json:"engine"`
}

// POST /api/engine/selection — persist the chosen engine.
func handleSelectEngine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req selectEngineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Engine) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "engine is required"})
		return
	}
	stateMu.Lock()
	s := loadState()
	s.Engine = strings.TrimSpace(req.Engine)
	err := saveState(s)
	stateMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, computeSetup(s))
}

type createChildRequest struct {
	Name  string `json:"name"`
	Grade string `json:"grade"`
	Board string `json:"board"`
}

// POST /api/child — create the single child + scaffold the workspace folders.
func handleCreateChild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req createChildRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if err := scaffoldFamilyFolders(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	stateMu.Lock()
	s := loadState()
	s.Child = &Child{
		Name:      strings.TrimSpace(req.Name),
		Grade:     strings.TrimSpace(req.Grade),
		Board:     strings.TrimSpace(req.Board),
		Language:  "en",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	err := saveState(s)
	stateMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	seedWorkspace(s.Child)
	writeJSON(w, http.StatusOK, computeSetup(s))
}

type setPinRequest struct {
	Pin string `json:"pin"`
}

// POST /api/parent/pin — set the parent PIN. Stored as a SHA-256 hash in the
// 0600 state file (secure-file decision); a real OS keychain can replace this.
func handleSetPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req setPinRequest
	pin := ""
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		pin = strings.TrimSpace(req.Pin)
	}
	if len(pin) < 4 || len(pin) > 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "PIN must be 4–8 digits"})
		return
	}
	sum := sha256.Sum256([]byte(pin))
	stateMu.Lock()
	s := loadState()
	s.PinHash = hex.EncodeToString(sum[:])
	err := saveState(s)
	stateMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, computeSetup(s))
}

// POST /api/parent/pin/verify — check a PIN against the stored hash. Gates the
// child→parent transition so a child can't return to Parent Mode (answer keys,
// private notes) just by tapping the button. Returns {"ok":true} on match.
func handleVerifyPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req setPinRequest
	pin := ""
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
		pin = strings.TrimSpace(req.Pin)
	}
	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	if s.PinHash == "" {
		// No PIN set yet — nothing to protect; allow through.
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	sum := sha256.Sum256([]byte(pin))
	ok := subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(s.PinHash)) == 1
	if !ok {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// POST /api/reset — clear setup (dev convenience to re-run onboarding).
func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Close any warm coding-agent (tmux) sessions so a fresh setup starts clean.
	agentsession.CloseAllInteractiveSessions()
	stateMu.Lock()
	err := os.Remove(statePath())
	stateMu.Unlock()
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, computeSetup(familyState{}))
}
