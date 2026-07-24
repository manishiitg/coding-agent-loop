package main

import (
	"fmt"
	"os"
	"strings"
)

// validateSharedFile checks that path is a real, existing file under shared/
// — the only place child-facing activity content is allowed to live. Used when
// building an activity (create_learning_activity) and handing one off, so a
// bad or parent-private path can never become part of what the child sees.
//
// There is no longer any "approved-for-child" list: what the child can reach
// is defined entirely by the CURRENT activity's items (see currentActivityItems
// / childCanSee / childCanWrite). Handing off just records those item paths in
// child/current-task.json; nothing else needs to be marked.
func validateSharedFile(path string) error {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if !strings.HasPrefix(path, "shared/") {
		return fmt.Errorf("only files under shared/ can be given to the child")
	}
	abs, ok := resolveWorkspacePath(path)
	if !ok {
		return fmt.Errorf("invalid path")
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return fmt.Errorf("file not found")
	}
	return nil
}
