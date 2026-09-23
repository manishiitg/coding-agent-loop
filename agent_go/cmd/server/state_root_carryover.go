package server

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stateRootCarryoverMarker = "state-root-carryover-v1.done"

// The legacy mixed-scope journal is retained only for rollback in its original
// location; copying it would duplicate hundreds of MB the new server never opens.
var stateRootCarryoverSkippedTopLevel = map[string]bool{
	"structured-chat-events.sqlite":     true,
	"structured-chat-events.sqlite-wal": true,
	"structured-chat-events.sqlite-shm": true,
}

// carryOverLegacyStateRoot runs once when a launcher starts pinning
// AGENTWORKS_STATE_ROOT. Without it every PAT, native-resume recovery record,
// CLI runtime, and the chat journal silently reset to empty on that deploy.
// Files are copied only where the new root lacks them; nothing is overwritten
// and the legacy root is left intact for rollback.
func carryOverLegacyStateRoot() {
	configured := strings.TrimSpace(os.Getenv("AGENTWORKS_STATE_ROOT"))
	if configured == "" {
		return
	}
	target, err := workflowCLIStateRoot()
	if err != nil {
		log.Printf("[STATE_ROOT] cannot resolve configured state root: %v", err)
		return
	}
	legacy, err := defaultWorkflowCLIStateRoot()
	if err != nil {
		return
	}
	copied, err := carryOverStateRoot(legacy, target)
	if err != nil {
		log.Printf("[STATE_ROOT] carrying state from %s to %s failed; will retry on next start: %v", legacy, target, err)
		return
	}
	if copied > 0 {
		log.Printf("[STATE_ROOT] carried %d files from legacy state root %s to %s", copied, legacy, target)
	}
}

func carryOverStateRoot(legacy, target string) (int, error) {
	legacy, target = filepath.Clean(legacy), filepath.Clean(target)
	if legacy == target {
		return 0, nil
	}
	markerPath := filepath.Join(target, "migrations", stateRootCarryoverMarker)
	if _, err := os.Stat(markerPath); err == nil {
		return 0, nil
	}
	info, err := os.Stat(legacy)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("legacy state root %s is not a directory", legacy)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return 0, err
	}
	// SQLite sidecars are only valid next to the database they belong to, so
	// a -wal/-shm is copied only when its base database is copied in this pass.
	copiedBases := make(map[string]bool)
	var copied int
	walkErr := filepath.WalkDir(legacy, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(legacy, path)
		if err != nil || rel == "." {
			return err
		}
		if stateRootCarryoverSkippedTopLevel[rel] {
			return nil
		}
		destination := filepath.Join(target, rel)
		if entry.IsDir() {
			if rel == "migrations" {
				return fs.SkipDir // Markers describe work done in the legacy root, not the target.
			}
			return os.MkdirAll(destination, 0o700)
		}
		if !entry.Type().IsRegular() {
			return nil // Symlinks and devices are never followed into the new root.
		}
		base, sidecar := sqliteSidecarBase(rel)
		if sidecar && !copiedBases[base] {
			return nil
		}
		if _, statErr := os.Lstat(destination); statErr == nil {
			return nil
		} else if !os.IsNotExist(statErr) {
			return statErr
		}
		if err := copyFileNoOverwrite(path, destination); err != nil {
			return err
		}
		copiedBases[rel] = true
		copied++
		return nil
	})
	if walkErr != nil {
		return copied, walkErr
	}
	if err := writeStateRootCarryoverMarker(markerPath, legacy, copied); err != nil {
		return copied, err
	}
	return copied, nil
}

func sqliteSidecarBase(rel string) (string, bool) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if !strings.HasSuffix(rel, suffix) {
			continue
		}
		base := strings.TrimSuffix(rel, suffix)
		if strings.HasSuffix(base, ".sqlite") || strings.HasSuffix(base, ".db") {
			return base, true
		}
	}
	return "", false
}

// copyFileNoOverwrite publishes with a hard link so a file that appeared
// concurrently is never replaced; os.Rename would overwrite it.
func copyFileNoOverwrite(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".carryover-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, input); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return nil
}

func writeStateRootCarryoverMarker(markerPath, legacy string, copied int) error {
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(markerPath), ".state-root-carryover.*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := fmt.Fprintf(temporary, "completed_at=%s\nsource=%s\ncopied=%d\n", time.Now().UTC().Format(time.RFC3339), legacy, copied); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, markerPath)
}
