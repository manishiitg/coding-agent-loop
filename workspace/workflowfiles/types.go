// Package workflowfiles defines the internal, workflow-confined file API shared
// by the agent server and workspace service. It is not a public client API.
package workflowfiles

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"
)

const MaxFileBytes = 2 << 20
const MissingRevision = "missing"

type Request struct {
	Root             string `json:"root"`
	Operation        string `json:"operation"`
	Path             string `json:"path,omitempty"`
	Query            string `json:"query,omitempty"`
	Content          string `json:"content,omitempty"`
	Diff             string `json:"diff,omitempty"`
	ExpectedRevision string `json:"expected_revision,omitempty"`
	Offset           int    `json:"offset,omitempty"`
	Limit            int    `json:"limit,omitempty"`
	Depth            int    `json:"depth,omitempty"`
	// Managed is set only by the authenticated agent server for typed tools.
	Managed bool              `json:"managed,omitempty"`
	Checks  map[string]string `json:"checks,omitempty"`
	Writes  map[string]string `json:"writes,omitempty"`
}
type File struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	Revision string `json:"revision"`
	Size     int64  `json:"size"`
}
type Entry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	Line int    `json:"line,omitempty"`
	Text string `json:"text,omitempty"`
}
type Result struct {
	File
	Entries    []Entry           `json:"entries,omitempty"`
	NextOffset int               `json:"next_offset,omitempty"`
	Truncated  bool              `json:"truncated,omitempty"`
	Revisions  map[string]string `json:"revisions,omitempty"`
}

func Revision(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
func CleanRelative(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	if strings.ContainsAny(p, "\\\x00\r\n") || strings.HasPrefix(p, "/") || strings.Contains(p, ":") {
		return "", fmt.Errorf("path must be workflow-relative")
	}
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return "", fmt.Errorf("parent traversal is not allowed")
		}
	}
	return path.Clean(p), nil
}

// Private paths never appear in the public file API. Builder transcripts have
// stricter ownership than the shared workflow and use the chat API instead.
func Private(p string) bool {
	for _, part := range strings.Split(strings.ToLower(p), "/") {
		if part == ".git" || part == "builder" || part == "secrets" || part == "keys" || part == ".ssh" || part == ".agentworks" || strings.HasPrefix(part, ".agentworks-") || strings.HasPrefix(part, ".env") {
			return true
		}
	}
	return false
}
func Protected(p string) bool {
	p = strings.ToLower(p)
	first := strings.Split(p, "/")[0]
	return Private(p) || first == "planning" || first == "evaluation" || first == "runs" || p == "workflow.json"
}
