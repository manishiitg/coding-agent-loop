package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// approvedForChildPath is the parent-only manifest of workspace-relative paths
// the parent has explicitly approved for the child to see. File-based, like
// every other piece of family state — no database. Absence of a path here is
// the default (not yet shown to the child); it is NOT a security boundary —
// the child's sandbox already permits reading all of shared/ — it is a pacing
// gate so the child's screen only shows what the parent has chosen to hand off.
func approvedForChildPath() string {
	return filepath.Join(workspaceRoot(), "parent", "approved-for-child.json")
}

var approvedForChildMu sync.Mutex

func loadApprovedForChild() []string {
	approvedForChildMu.Lock()
	defer approvedForChildMu.Unlock()
	b, err := os.ReadFile(approvedForChildPath())
	if err != nil {
		return nil
	}
	var paths []string
	if err := json.Unmarshal(b, &paths); err != nil {
		return nil
	}
	return paths
}

// approveForChild records path as approved for the child, deduplicating.
// Rejects anything not under shared/ — parent/ must never be exposed this way,
// and child/ is already always visible to the child, so approving it is a no-op.
func approveForChild(path string) error {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if !strings.HasPrefix(path, "shared/") {
		return fmt.Errorf("only files under shared/ can be approved for the child")
	}
	if abs, ok := resolveWorkspacePath(path); !ok {
		return fmt.Errorf("invalid path")
	} else if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return fmt.Errorf("file not found")
	}

	approvedForChildMu.Lock()
	defer approvedForChildMu.Unlock()

	existing := loadApprovedForChildLocked()
	for _, p := range existing {
		if p == path {
			return nil // already approved
		}
	}
	existing = append(existing, path)
	sort.Strings(existing)
	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(approvedForChildPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(approvedForChildPath(), b, 0o600)
}

// loadApprovedForChildLocked is loadApprovedForChild's body without re-taking
// the mutex, for callers that already hold it (approveForChild).
func loadApprovedForChildLocked() []string {
	b, err := os.ReadFile(approvedForChildPath())
	if err != nil {
		return nil
	}
	var paths []string
	if err := json.Unmarshal(b, &paths); err != nil {
		return nil
	}
	return paths
}
