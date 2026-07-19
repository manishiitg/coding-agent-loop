package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func workspaceRoot() string { return filepath.Join(familyDataDir(), "workspace") }

// materialEntry is one uploaded file, tagged by scope/subject/topic.
type materialEntry struct {
	Path    string `json:"path"` // workspace-relative
	Name    string `json:"name"`
	Scope   string `json:"scope,omitempty"`
	Subject string `json:"subject,omitempty"`
	Topic   string `json:"topic,omitempty"`
	Size    int64  `json:"size"`
}

// listMaterials walks the workspace and returns every file under a
// <scope>/materials/<subject>/<topic>/ path. Shared by the HTTP endpoint and
// the agent's list_materials tool.
func listMaterials() []materialEntry {
	root := workspaceRoot()
	out := []materialEntry{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		mi := -1
		for i, seg := range parts {
			if seg == "materials" {
				mi = i
				break
			}
		}
		if mi == -1 {
			return nil
		}
		e := materialEntry{Path: rel, Name: parts[len(parts)-1]}
		if mi > 0 {
			e.Scope = parts[0]
		}
		if len(parts) > mi+1 {
			e.Subject = parts[mi+1]
		}
		if len(parts) > mi+2 {
			e.Topic = parts[mi+2]
		}
		if info, e2 := d.Info(); e2 == nil {
			e.Size = info.Size()
		}
		out = append(out, e)
		return nil
	})
	return out
}

// resolveWorkspacePath cleans a workspace-relative path and guarantees it stays
// inside the workspace (no traversal).
func resolveWorkspacePath(rel string) (string, bool) {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	clean := filepath.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", false
	}
	root := workspaceRoot()
	abs := filepath.Join(root, clean)
	if abs != root && !strings.HasPrefix(abs, root+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}
