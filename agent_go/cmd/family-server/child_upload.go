package main

import (
	"encoding/json"
	"os"
	"strings"
)

// currentUploadPath is the current-upload.json pointer file written by
// handleUpload (scope=child) — same pattern as current-activity.json for
// handoffs.
func currentUploadPath() (string, bool) { return resolveWorkspacePath("current-upload.json") }

func loadCurrentUpload() (string, bool) {
	abs, ok := currentUploadPath()
	if !ok {
		return "", false
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	var v struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(b, &v) != nil || strings.TrimSpace(v.Path) == "" {
		return "", false
	}
	return v.Path, true
}

func clearCurrentUpload() {
	if abs, ok := currentUploadPath(); ok {
		_ = os.Remove(abs)
	}
}

// pendingChildUploadSuffix checks for a just-uploaded child photo and, if one
// exists, returns text to APPEND onto this turn's own last message naming its
// EXACT path — rather than relying on the model to proactively guess which
// folder to check (testing showed it unreliably defaults to the top-level
// inbox), or pre-computing the transcription for it to just trust (testing showed the
// model treats an unverifiable claim embedded in a "user" message with the
// same skepticism it would show the child fabricating an answer, and insists
// on checking the real file itself anyway — reasonable caution, but it fights
// a pre-computed answer). Stating the real path lets the model's OWN
// read_image call succeed on the first try — verification stays genuinely the
// model's, only the "which folder" guess is removed. Always clears the
// pointer so the same photo is never re-flagged as new on a later turn.
func pendingChildUploadSuffix() string {
	path, ok := loadCurrentUpload()
	if !ok {
		return ""
	}
	clearCurrentUpload()
	return "\n\n(I uploaded it to " + path + ")"
}
