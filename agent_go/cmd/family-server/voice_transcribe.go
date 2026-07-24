package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// whisperModelPath resolves the local whisper.cpp GGML model file used for
// WhatsApp voice-note transcription. Override with WHISPER_MODEL_PATH;
// defaults to a model downloaded once into the app's own data root (a sibling
// of workspace/, so it's never swept into the workspace backup skill).
func whisperModelPath() string {
	if p := strings.TrimSpace(os.Getenv("WHISPER_MODEL_PATH")); p != "" {
		return p
	}
	return filepath.Join(familyDataDir(), "whisper-models", "ggml-base.en.bin")
}

// transcribeAudioFile transcribes a local audio file to text entirely
// on-device via whisper.cpp (the `whisper-cli` binary, installed separately
// e.g. `brew install whisper-cpp`) — no cloud API key, no per-use cost.
// whisper.cpp's own decoder can't read WhatsApp's ogg/opus voice notes
// directly, so ffmpeg first transposes the audio into a plain 16kHz mono WAV
// it can. Both tools are optional: if either is missing from PATH, this
// returns an error and the caller treats transcription as unavailable
// (the audio file itself is still saved to the inbox regardless).
func transcribeAudioFile(ctx context.Context, audioPath string) (string, error) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not installed (brew install ffmpeg)")
	}
	whisperBin, err := exec.LookPath("whisper-cli")
	if err != nil {
		return "", fmt.Errorf("whisper-cli not installed (brew install whisper-cpp)")
	}
	modelPath := whisperModelPath()
	if _, err := os.Stat(modelPath); err != nil {
		return "", fmt.Errorf("whisper model not found at %s", modelPath)
	}

	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	wavPath := audioPath + ".transcribe.wav"
	defer os.Remove(wavPath)
	convert := exec.CommandContext(ctx, ffmpegBin, "-y", "-i", audioPath, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", wavPath)
	if out, err := convert.CombinedOutput(); err != nil {
		return "", fmt.Errorf("ffmpeg convert failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	run := exec.CommandContext(ctx, whisperBin, "-m", modelPath, "-f", wavPath, "-nt", "-np", "-l", "en")
	out, err := run.Output()
	if err != nil {
		return "", fmt.Errorf("whisper-cli failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
