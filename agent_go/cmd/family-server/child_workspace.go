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

// currentActivityItems returns the CURRENT activity's real workspace-relative
// item paths (always under shared/ — there is no separate child-owned copy):
// a multi-item activity's Items, a single-file handoff's Path, or nil if
// nothing has been handed off yet. This is the ONE definition of "what's
// active right now" — childCanSee, childCanWrite, handleChildWorkspaceTree,
// and childShellTool's Read/WritePaths all build their scoping from this, so
// they can never drift out of sync with each other.
func currentActivityItems() []string {
	ct := loadCurrentTask()
	if len(ct.Items) > 0 {
		return ct.Items
	}
	if p := strings.TrimSpace(ct.Path); p != "" {
		return []string{p}
	}
	return nil
}

// childCanSee reports whether a workspace-relative path is something the
// child is allowed to have opened on their screen: their own attempts, the
// current-task pointer, or an item of the CURRENT activity. Once a new
// activity is handed off, an older one's files are no longer reachable, so
// the tutor/child can't drift back into stale content.
func childCanSee(rel string) bool {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return false
	}
	if _, ok := resolveWorkspacePath(rel); !ok {
		return false
	}
	if rel == "child/current-task.json" || strings.HasPrefix(rel, "child/attempts") {
		return true
	}
	for _, p := range currentActivityItems() {
		if p == rel {
			return true
		}
	}
	return false
}

// childCanWrite reports whether the child agent may write to rel directly —
// their own child/attempts/ scratch space, or an item of the CURRENT
// activity (this is what lets the tutor record "✓ Answered" progress notes
// straight onto the parent's own file — see childSystemPrompt). Must stay in
// sync with childShellTool's WritePaths, which grants the sandboxed shell
// this exact same scope.
func childCanWrite(rel string) bool {
	rel = strings.TrimPrefix(strings.TrimSpace(rel), "/")
	if rel == "" {
		return false
	}
	if _, ok := resolveWorkspacePath(rel); !ok {
		return false
	}
	if strings.HasPrefix(rel, "child/attempts") {
		return true
	}
	for _, p := range currentActivityItems() {
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

// GET /api/child/workspace/tree — the child's own view of the workspace:
// only the CURRENT activity's files plus the child's own attempts — the same
// "current activity only" boundary as childCanSee and childShellTool's own
// readPaths (see childCanSee's doc comment; keep all three in sync). This
// really is the security boundary now, not just a display filter: the child
// agent's own sandbox (childShellTool, StrictAllowlist) is scoped exactly
// this narrowly too, so a file outside this set is neither shown here nor
// actually readable by the tutor.
func handleChildWorkspaceTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	root := workspaceRoot()
	_ = os.MkdirAll(root, 0o700)
	nodes := buildFilteredTree(root, currentActivityItems(), []string{"child/attempts"})
	writeJSON(w, http.StatusOK, nodes)
}
