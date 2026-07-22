package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type uploadResponse struct {
	Name    string `json:"name,omitempty"`
	Path    string `json:"path,omitempty"` // workspace-relative
	Size    int64  `json:"size,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Subject string `json:"subject,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Error   string `json:"error,omitempty"`
}

// saveCurrentUpload writes a small pointer file naming the exact path of a
// child's just-uploaded photo — the SAME pattern as child/current-task.json
// for handoffs. A prompt instruction telling the child agent to proactively
// "ls child/inbox/" for a new upload proved unreliable in testing (the model
// kept defaulting to checking shared/inbox instead); pointing it at one
// specific, deterministic file to read removes the guessing entirely.
func saveCurrentUpload(rel string) {
	abs, ok := resolveWorkspacePath("child/current-upload.json")
	if !ok {
		return
	}
	_ = os.MkdirAll(filepath.Dir(abs), 0o700)
	b, _ := json.Marshal(struct {
		Path string `json:"path"`
	}{Path: rel})
	_ = os.WriteFile(abs, b, 0o600)
}

// safeName sanitizes an uploaded filename to its base, no traversal.
func safeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, "..", "-")
	name = strings.Trim(name, ". ")
	if name == "" {
		return "upload.bin"
	}
	return name
}

// POST /api/upload (multipart/form-data) — add school material to the workspace,
// organized by scope/subject/topic. Fields:
//
//	file    (required) the uploaded file
//	scope   parent|child|shared (default shared)
//	subject, topic (optional; fall back to the persisted Subject/Topic)
//
// Files land at workspace/<scope>/materials/<subject>/<topic>/<filename>.
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil { // 64 MB
		writeJSON(w, http.StatusBadRequest, uploadResponse{Error: "invalid upload (expected multipart/form-data)"})
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, uploadResponse{Error: "a file is required"})
		return
	}
	defer file.Close()

	// Uploads land in a staging inbox. The agent then reads each file, classifies
	// it, and files it into shared/materials/<subject>/<topic>/ with a metadata
	// JSON — see skills/process-file/SKILL.md. Subject/topic are decided from the
	// file's content by the agent, not guessed from stale header state.
	//
	// scope=child routes instead to child/inbox/ — the child's OWN sandbox
	// (childShellTool's WritePaths/ReadPaths are scoped to child/ only), so a
	// child-uploaded photo (e.g. a scan of their answer) is immediately visible
	// to their own agent turn without needing parent approval, unlike shared/
	// which the child cannot see until explicitly approved.
	relDir := filepath.Join("shared", "inbox")
	scope := "shared"
	if strings.TrimSpace(r.FormValue("scope")) == "child" {
		relDir = filepath.Join("child", "inbox")
		scope = "child"
	}
	absDir := filepath.Join(familyDataDir(), "workspace", relDir)
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, uploadResponse{Error: err.Error()})
		return
	}

	name := safeName(hdr.Filename)
	absPath := filepath.Join(absDir, name)
	out, err := os.Create(absPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, uploadResponse{Error: err.Error()})
		return
	}
	size, copyErr := io.Copy(out, file)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		writeJSON(w, http.StatusInternalServerError, uploadResponse{Error: "failed to save the file"})
		return
	}
	relPath := filepath.ToSlash(filepath.Join(relDir, name))
	if scope == "child" {
		saveCurrentUpload(relPath)
	}

	writeJSON(w, http.StatusOK, uploadResponse{
		Name:  name,
		Path:  relPath,
		Size:  size,
		Scope: scope,
	})
}
