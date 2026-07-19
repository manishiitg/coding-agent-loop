package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
}

// familyState is the persisted onboarding/config state.
type familyState struct {
	Engine  string `json:"engine"`
	Child   *Child `json:"child"`
	PinHash string `json:"pin_hash,omitempty"`
	// Subject/Topic are written by the parent agent's set_subject_topic tool —
	// the first real MCP bridge tool wired into Family Learning.
	Subject string `json:"subject,omitempty"`
	Topic   string `json:"topic,omitempty"`
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
	for _, sub := range []string{"parent", "child", "shared"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o700); err != nil {
			return err
		}
	}
	return nil
}

type setupResponse struct {
	Engine        string `json:"engine"`
	Child         *Child `json:"child"`
	Subject       string `json:"subject,omitempty"`
	Topic         string `json:"topic,omitempty"`
	PinSet        bool   `json:"pin_set"`
	SetupComplete bool   `json:"setup_complete"`
	NextStep      string `json:"next_step"` // "engine" | "child" | "pin" | "done"
}

func computeSetup(s familyState) setupResponse {
	resp := setupResponse{Engine: s.Engine, Child: s.Child, Subject: s.Subject, Topic: s.Topic, PinSet: s.PinHash != ""}
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

// POST /api/reset — clear setup (dev convenience to re-run onboarding).
func handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stateMu.Lock()
	err := os.Remove(statePath())
	stateMu.Unlock()
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, computeSetup(familyState{}))
}
