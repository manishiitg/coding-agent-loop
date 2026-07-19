package main

import (
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

// safeSeg sanitizes a string for use as a single path segment (no traversal,
// no separators). Empty result becomes "misc".
func safeSeg(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	s = strings.ReplaceAll(s, "..", "-")
	s = strings.Trim(s, ". ")
	if s == "" {
		return "misc"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
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

	scope := strings.ToLower(strings.TrimSpace(r.FormValue("scope")))
	switch scope {
	case "parent", "child", "shared":
	default:
		scope = "shared"
	}

	stateMu.Lock()
	s := loadState()
	stateMu.Unlock()
	subject := strings.TrimSpace(r.FormValue("subject"))
	if subject == "" {
		subject = s.Subject
	}
	topic := strings.TrimSpace(r.FormValue("topic"))
	if topic == "" {
		topic = s.Topic
	}

	relParts := []string{scope, "materials"}
	if subject != "" {
		relParts = append(relParts, safeSeg(subject))
	}
	if topic != "" {
		relParts = append(relParts, safeSeg(topic))
	}
	relDir := filepath.Join(relParts...)
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

	writeJSON(w, http.StatusOK, uploadResponse{
		Name:    name,
		Path:    filepath.ToSlash(filepath.Join(relDir, name)),
		Size:    size,
		Scope:   scope,
		Subject: subject,
		Topic:   topic,
	})
}
