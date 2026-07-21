package main

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// buildFilteredTree returns a pruned copy of the workspace tree containing
// ONLY the given paths (and their ancestor directories) plus everything under
// alwaysInclude prefixes (e.g. the child's own attempts). Empty directories
// are dropped. This is what makes the child's screen show just what the
// parent has approved, instead of everything under shared/.
func buildFilteredTree(root string, paths []string, alwaysInclude []string) []treeNode {
	included := map[string]bool{}
	mark := func(p string) {
		p = strings.Trim(p, "/")
		if p == "" {
			return
		}
		parts := strings.Split(p, "/")
		cur := ""
		for _, part := range parts {
			if cur == "" {
				cur = part
			} else {
				cur = cur + "/" + part
			}
			included[cur] = true
		}
	}
	for _, p := range paths {
		mark(p)
	}
	for _, prefix := range alwaysInclude {
		absPrefix := filepath.Join(root, filepath.FromSlash(prefix))
		_ = filepath.WalkDir(absPrefix, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			mark(filepath.ToSlash(rel))
			return nil
		})
	}

	var walk func(absDir, rel string) []treeNode
	walk = func(absDir, rel string) []treeNode {
		entries, err := os.ReadDir(absDir)
		if err != nil {
			return nil
		}
		sort.Slice(entries, func(i, j int) bool {
			di, dj := entries[i].IsDir(), entries[j].IsDir()
			if di != dj {
				return di
			}
			return entries[i].Name() < entries[j].Name()
		})
		var nodes []treeNode
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			childRel := name
			if rel != "" {
				childRel = rel + "/" + name
			}
			if !included[childRel] {
				continue
			}
			if e.IsDir() {
				kids := walk(filepath.Join(absDir, name), childRel)
				if len(kids) == 0 {
					continue // prune directories with nothing approved inside
				}
				nodes = append(nodes, treeNode{Name: name, Path: childRel, Type: "dir", Children: kids})
			} else {
				nodes = append(nodes, treeNode{Name: name, Path: childRel, Type: "file"})
			}
		}
		return nodes
	}
	return walk(root, "")
}

// childCanSee reports whether a workspace-relative path is something the child
// is allowed to have opened on their screen: their own work under child/, or a
// file the parent has explicitly approved (approved-for-child.json). This is
// the same allow-list the child's file panel is built from, so the agent's
// open_file can never surface something the parent hasn't handed off.
func childCanSee(rel string) bool {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return false
	}
	if _, ok := resolveWorkspacePath(rel); !ok {
		return false
	}
	if strings.HasPrefix(rel, "child/") {
		return true
	}
	for _, p := range loadApprovedForChild() {
		if p == rel {
			return true
		}
	}
	return false
}

// childDisplayName is the child's first name for prompt wording, or a neutral
// fallback when no profile exists yet.
func childDisplayName(child *Child) string {
	if child != nil && strings.TrimSpace(child.Name) != "" {
		return child.Name
	}
	return "your student"
}

// GET /api/child/workspace/tree — the child's own view of the workspace: only
// files the parent has approved (parent/approved-for-child.json) plus the
// child's own attempts. This is a pacing/UX gate, not the security boundary —
// the child agent's sandbox already permits reading all of shared/; this just
// controls what shows up unprompted on the child's screen.
func handleChildWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := workspaceRoot()
	_ = os.MkdirAll(root, 0o700)
	approved := loadApprovedForChild()
	nodes := buildFilteredTree(root, approved, []string{"child/attempts"})
	writeJSON(w, http.StatusOK, nodes)
}
