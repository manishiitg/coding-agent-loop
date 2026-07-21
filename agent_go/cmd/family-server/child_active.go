package main

import (
	"os"
	"path/filepath"
	"strings"
)

// mirrorToChildActive gives the child their own live, freely-editable copy of
// an approved shared/ file under child/active/ — copied once, on first open,
// and never overwritten again so progress markers Quill adds later (via the
// child's own shell tool) survive. The child sandbox's WritePaths is scoped to
// child/ only, never shared/ (that stays the parent's clean, canonical copy,
// reusable and untouched) — mirroring here means the child can record
// progress on their OWN copy without ever needing write access into shared/.
// Non-shared/ paths (already under child/, or a package manifest) pass
// through unchanged.
func mirrorToChildActive(rel string) string {
	rel = strings.TrimSpace(rel)
	if !strings.HasPrefix(rel, "shared/") {
		return rel
	}
	dest := "child/active/" + filepath.Base(rel)
	destAbs, ok := resolveWorkspacePath(dest)
	if !ok {
		return rel
	}
	if _, err := os.Stat(destAbs); err == nil {
		return dest // already mirrored — never clobber progress already recorded
	}
	srcAbs, ok := resolveWorkspacePath(rel)
	if !ok {
		return rel
	}
	b, err := os.ReadFile(srcAbs)
	if err != nil {
		return rel
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o700); err != nil {
		return rel
	}
	if err := os.WriteFile(destAbs, b, 0o600); err != nil {
		return rel
	}
	return dest
}
