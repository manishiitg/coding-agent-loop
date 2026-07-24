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
	ParentLabel string      `json:"parent_label,omitempty"`
	Pulse       PulseConfig `json:"pulse,omitempty"`
	// WhatsAppVoiceEnabled is the parent's explicit choice for on-device
	// WhatsApp voice-note transcription (see voice_transcribe.go). nil means
	// "never explicitly chosen" — whatsAppVoiceEnabled() then defaults to
	// whether the whisper model already happens to be installed, so shipping
	// this toggle doesn't silently turn off transcription that was already
	// working. A pointer (not a plain bool) so "explicitly disabled" and
	// "never asked" are distinguishable — a plain bool's zero value can't tell
	// those apart.
	WhatsAppVoiceEnabled *bool `json:"whatsapp_voice_enabled,omitempty"`
}

// PulseConfig is the parent-configurable settings for the Pulse background
// check-in (see pulse.go). Opt-in by default (Enabled starts false) — Quill
// should never start proactively messaging the parent until they turn it on.
type PulseConfig struct {
	Enabled bool `json:"enabled"`
	// CadenceHours is how often Pulse runs, in hours. 0 means "not yet
	// configured" and is treated as the default (24h) rather than "never".
	CadenceHours int `json:"cadence_hours,omitempty"`
	// LastRunAt (RFC3339 UTC) is when Pulse last actually ran, so the ticker
	// knows whether the cadence window has elapsed.
	LastRunAt string `json:"last_run_at,omitempty"`
	// SchoolGmailQuery is a Gmail search filter (e.g. "from:school.edu") the
	// parent configures so Quill can check for new school email — deliberately
	// server-side config, never a free-form parameter the model controls, so
	// Quill can only ever search within what the parent explicitly scoped.
	SchoolGmailQuery string `json:"school_gmail_query,omitempty"`
	// WatchSites are websites the parent wants Quill to check on each Pulse via
	// agent_browser (reusing the parent's own signed-in CDP Chrome — see
	// browser_status.go): a school portal, a class site, any third-party page —
	// generic and multiple, not one fixed school portal. Only checked when the
	// parent has set them, never URLs the model picks on its own.
	WatchSites []string `json:"watch_sites,omitempty"`
	// SchoolPortalURL is the legacy single-portal field, kept so older saved
	// state still parses; folded into the effective site list (see Sites()).
	SchoolPortalURL string `json:"school_portal_url,omitempty"`
	// NotifyEmails are additional recipient addresses the parent wants Pulse
	// digest emails sent to (e.g. both parents). Empty = send only to the
	// connected Gmail account's own address (the default self-notify).
	NotifyEmails []string `json:"notify_emails,omitempty"`
}

// Sites returns the effective de-duplicated list of websites to check —
// WatchSites plus the legacy single SchoolPortalURL if it's still set.
func (p PulseConfig) Sites() []string {
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		out = append(out, u)
	}
	for _, u := range p.WatchSites {
		add(u)
	}
	add(p.SchoolPortalURL)
	return out
}

func (p PulseConfig) cadence() time.Duration {
	hours := p.CadenceHours
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
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

// scaffoldFamilyFolders creates the non-activity workspace areas — Subject/
// activity folders (and their in-folder answer keys, conversations, attempts)
// are created on demand as activities are made, not scaffolded up front.
func scaffoldFamilyFolders() error {
	base := filepath.Join(familyDataDir(), "workspace")
	dirs := []string{
		"materials",     // parent's uploaded raw source material, by subject/topic
		"inbox",         // uploads land here; the agent files them (process-file skill)
		"reports",       // generated HTML progress reports (parent + child)
		"memory",        // preferences, browser notes, child interests/profile
		"conversations", // the one parent<->Quill thread (conversations/parent.json)
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
