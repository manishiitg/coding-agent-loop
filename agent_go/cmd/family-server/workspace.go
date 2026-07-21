package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// treeNode is one entry in the workspace file tree.
type treeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"` // relative to the workspace root
	Type     string     `json:"type"` // "dir" | "file"
	Children []treeNode `json:"children,omitempty"`
}

func buildTree(absDir, rel string) []treeNode {
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di // directories first
		}
		return entries[i].Name() < entries[j].Name()
	})
	nodes := make([]treeNode, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if e.IsDir() {
			nodes = append(nodes, treeNode{
				Name:     name,
				Path:     childRel,
				Type:     "dir",
				Children: buildTree(filepath.Join(absDir, name), childRel),
			})
		} else {
			nodes = append(nodes, treeNode{Name: name, Path: childRel, Type: "file"})
		}
	}
	return nodes
}

// GET /api/workspace/tree — the live family workspace as a hierarchical tree.
func handleWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := filepath.Join(familyDataDir(), "workspace")
	_ = os.MkdirAll(root, 0o700)
	writeJSON(w, http.StatusOK, buildTree(root, ""))
}

// seedWorkspace writes a couple of starter files so the tree is meaningful and
// the file-first model has real content. Later, agent tools write here too.
func seedWorkspace(child *Child) {
	base := filepath.Join(familyDataDir(), "workspace")
	if child != nil {
		if b, err := json.MarshalIndent(child, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(base, "parent", "child-profile.json"), b, 0o600)
		}
	}
	name := "your child"
	if child != nil && strings.TrimSpace(child.Name) != "" {
		name = child.Name
	}
	mapPath := filepath.Join(base, "shared", "academic-map.html")
	if _, err := os.Stat(mapPath); os.IsNotExist(err) {
		html := "<!doctype html>\n<meta charset=\"utf-8\">\n<title>Academic map</title>\n<h1>" + name + "’s academic map</h1>\n<p>This living view grows as " + name + " learns.</p>\n"
		_ = os.WriteFile(mapPath, []byte(html), 0o600)
	}
	progressPath := filepath.Join(base, "shared", "reports", "progress.html")
	if _, err := os.Stat(progressPath); os.IsNotExist(err) {
		_ = os.MkdirAll(filepath.Dir(progressPath), 0o700)
		html := "<!doctype html>\n<meta charset=\"utf-8\">\n<title>Progress</title>\n<h1>" + name + "’s progress</h1>\n<p>This living report grows as " + name + " learns.</p>\n"
		_ = os.WriteFile(progressPath, []byte(html), 0o600)
	}
	prefsPath := filepath.Join(base, "parent", "preferences.md")
	if _, err := os.Stat(prefsPath); os.IsNotExist(err) {
		md := "# What Quill has learned about teaching " + name + "\n\n" +
			"Notes Quill adds here — things you've said directly (`[stated]`) and patterns Quill has noticed " +
			"over time (`[inferred]`) — teaching style, question style, pacing, what to avoid. Each line ends " +
			"with a date; newer notes take priority over older ones on the same topic.\n"
		_ = os.WriteFile(prefsPath, []byte(md), 0o600)
	}
}
