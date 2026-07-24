package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// whisperModelSizeMB is the approximate download size of ggml-base.en.bin —
// shown in the UI before it's installed (the real on-disk size is reported
// once it's actually there, via actualOrApproxModelSizeMB).
const whisperModelSizeMB = 148

// voiceTranscriptionStatus is what the WhatsApp settings UI needs to render
// the voice-transcription toggle: whether it's on, whether the (~148MB)
// model is actually present, whether an install is in flight, and the last
// install error if any.
type voiceTranscriptionStatus struct {
	Enabled     bool   `json:"enabled"`
	Installed   bool   `json:"installed"`
	Installing  bool   `json:"installing"`
	ModelSizeMB int    `json:"model_size_mb"`
	Error       string `json:"error,omitempty"`
}

var (
	voiceInstallMu    sync.Mutex
	voiceInstalling   bool
	voiceInstallError string
)

func voiceModelInstalled() bool {
	_, err := os.Stat(whisperModelPath())
	return err == nil
}

// actualOrApproxModelSizeMB reports the real on-disk size once the model is
// downloaded, falling back to the known approximate size beforehand (so the
// UI can show "~148MB" before the parent ever enables it).
func actualOrApproxModelSizeMB() int {
	if fi, err := os.Stat(whisperModelPath()); err == nil {
		if mb := int(fi.Size() / (1024 * 1024)); mb > 0 {
			return mb
		}
	}
	return whisperModelSizeMB
}

// whatsAppVoiceEnabled resolves the parent's effective choice — see the
// WhatsAppVoiceEnabled field doc comment in state.go for the nil-vs-false
// distinction this depends on.
func whatsAppVoiceEnabled(s familyState) bool {
	if s.WhatsAppVoiceEnabled != nil {
		return *s.WhatsAppVoiceEnabled
	}
	return voiceModelInstalled()
}

func currentVoiceTranscriptionStatus(s familyState) voiceTranscriptionStatus {
	voiceInstallMu.Lock()
	installing := voiceInstalling
	lastErr := voiceInstallError
	voiceInstallMu.Unlock()
	return voiceTranscriptionStatus{
		Enabled:     whatsAppVoiceEnabled(s),
		Installed:   voiceModelInstalled(),
		Installing:  installing,
		ModelSizeMB: actualOrApproxModelSizeMB(),
		Error:       lastErr,
	}
}

// handleWhatsAppVoiceToggle turns on-device WhatsApp voice-note transcription
// on or off. Turning it on kicks off a background install (whisper-cli +
// ffmpeg via Homebrew if either is missing, then the ~148MB whisper.cpp model
// download) and returns immediately — the frontend polls /api/whatsapp/status
// (which now includes voice_transcription) to show install progress. Turning
// it off deletes the model file right away to reclaim the ~148MB; whisper-cli
// and ffmpeg themselves are tiny system tools and are left installed.
func handleWhatsAppVoiceToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	stateMu.Lock()
	s := loadState()
	enabled := req.Enabled
	s.WhatsAppVoiceEnabled = &enabled
	err := saveState(s)
	stateMu.Unlock()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if enabled {
		go installVoiceTranscription()
	} else {
		voiceInstallMu.Lock()
		voiceInstallError = ""
		voiceInstallMu.Unlock()
		if err := os.Remove(whisperModelPath()); err != nil && !os.IsNotExist(err) {
			log.Printf("[voice] failed to delete model: %v", err)
		} else {
			log.Printf("[voice] deleted whisper model (transcription disabled)")
		}
	}

	stateMu.Lock()
	s = loadState()
	stateMu.Unlock()
	writeJSON(w, http.StatusOK, currentVoiceTranscriptionStatus(s))
}

// installVoiceTranscription best-effort installs whisper-cli/ffmpeg (via
// Homebrew, only if not already on PATH) and downloads the whisper.cpp model,
// so enabling the toggle "just works" without the parent touching a terminal.
// Runs in its own goroutine; progress/errors are polled via
// currentVoiceTranscriptionStatus.
func installVoiceTranscription() {
	voiceInstallMu.Lock()
	if voiceInstalling {
		voiceInstallMu.Unlock()
		return
	}
	voiceInstalling = true
	voiceInstallError = ""
	voiceInstallMu.Unlock()
	defer func() {
		voiceInstallMu.Lock()
		voiceInstalling = false
		voiceInstallMu.Unlock()
	}()

	brewInstall := func(formula string) error {
		log.Printf("[voice] installing %s via Homebrew...", formula)
		out, err := exec.Command("brew", "install", formula).CombinedOutput()
		if err != nil {
			return fmt.Errorf("brew install %s: %w (%s)", formula, err, lastLines(string(out), 300))
		}
		return nil
	}

	if _, err := exec.LookPath("whisper-cli"); err != nil {
		if err := brewInstall("whisper-cpp"); err != nil {
			log.Printf("[voice] %v", err)
			voiceInstallMu.Lock()
			voiceInstallError = err.Error()
			voiceInstallMu.Unlock()
			return
		}
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		if err := brewInstall("ffmpeg"); err != nil {
			log.Printf("[voice] %v", err)
			voiceInstallMu.Lock()
			voiceInstallError = err.Error()
			voiceInstallMu.Unlock()
			return
		}
	}

	if voiceModelInstalled() {
		return
	}
	if err := downloadWhisperModel(); err != nil {
		msg := fmt.Sprintf("failed to download whisper model: %v", err)
		log.Printf("[voice] %s", msg)
		voiceInstallMu.Lock()
		voiceInstallError = msg
		voiceInstallMu.Unlock()
		return
	}
	log.Printf("[voice] whisper model installed at %s", whisperModelPath())
}

// lastLines trims brew's (often noisy) output down to a short tail for a
// readable error message/log line.
func lastLines(s string, maxChars int) string {
	if len(s) > maxChars {
		return s[len(s)-maxChars:]
	}
	return s
}

// downloadWhisperModel fetches the ggml-base.en.bin GGML model used for
// on-device transcription (see transcribeAudioFile) into a temp file, then
// renames it into place — so a failed/partial download never leaves a
// half-written model file that voiceModelInstalled() would wrongly trust.
func downloadWhisperModel() error {
	modelPath := whisperModelPath()
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o700); err != nil {
		return err
	}
	const url = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	tmpPath := modelPath + ".download"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		os.Remove(tmpPath)
		return copyErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}
	return os.Rename(tmpPath, modelPath)
}
