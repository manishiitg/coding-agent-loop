package server

import (
	"os"
	"path/filepath"
	"testing"
)

func writeStateFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readStateFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestStateRootCarryoverKeepsAccessTokensAcrossRootChange(t *testing.T) {
	legacy := filepath.Join(t.TempDir(), "AgentWorks", "state")
	target := filepath.Join(t.TempDir(), "pinned-state")
	writeStateFile(t, filepath.Join(legacy, "auth", "abc.sqlite"), "tokens")
	writeStateFile(t, filepath.Join(legacy, "auth", "abc.sqlite-wal"), "tokens-wal")
	writeStateFile(t, filepath.Join(legacy, "chat-native-recovery", "r.json"), "recovery")
	writeStateFile(t, filepath.Join(legacy, "cli-runtimes", "codex", "state.json"), "runtime")
	writeStateFile(t, filepath.Join(legacy, "structured-chat-events.sqlite"), "legacy mixed journal")
	writeStateFile(t, filepath.Join(legacy, "migrations", "chat-events-v2.done"), "legacy marker")

	copied, err := carryOverStateRoot(legacy, target)
	if err != nil {
		t.Fatal(err)
	}
	if copied != 4 {
		t.Fatalf("copied %d files, want 4", copied)
	}
	if got := readStateFile(t, filepath.Join(target, "auth", "abc.sqlite")); got != "tokens" {
		t.Fatalf("access-token store not carried over: %q", got)
	}
	if got := readStateFile(t, filepath.Join(target, "auth", "abc.sqlite-wal")); got != "tokens-wal" {
		t.Fatalf("token WAL not carried with its database: %q", got)
	}
	if got := readStateFile(t, filepath.Join(target, "chat-native-recovery", "r.json")); got != "recovery" {
		t.Fatalf("recovery journal not carried over: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target, "structured-chat-events.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("rollback-only mixed journal was copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "migrations", "chat-events-v2.done")); !os.IsNotExist(err) {
		t.Fatalf("legacy migration marker was copied, which would skip the target's own import: %v", err)
	}
	if got := readStateFile(t, filepath.Join(legacy, "auth", "abc.sqlite")); got != "tokens" {
		t.Fatalf("legacy root was modified: %q", got)
	}
}

func TestStateRootCarryoverNeverOverwritesAndRunsOnce(t *testing.T) {
	legacy := t.TempDir()
	target := t.TempDir()
	writeStateFile(t, filepath.Join(legacy, "auth", "abc.sqlite"), "old tokens")
	writeStateFile(t, filepath.Join(legacy, "auth", "abc.sqlite-wal"), "old wal")
	writeStateFile(t, filepath.Join(target, "auth", "abc.sqlite"), "new tokens")

	if _, err := carryOverStateRoot(legacy, target); err != nil {
		t.Fatal(err)
	}
	if got := readStateFile(t, filepath.Join(target, "auth", "abc.sqlite")); got != "new tokens" {
		t.Fatalf("existing target file overwritten: %q", got)
	}
	if _, err := os.Stat(filepath.Join(target, "auth", "abc.sqlite-wal")); !os.IsNotExist(err) {
		t.Fatalf("legacy WAL copied next to a different database: %v", err)
	}

	writeStateFile(t, filepath.Join(legacy, "late.json"), "added after carryover")
	copied, err := carryOverStateRoot(legacy, target)
	if err != nil {
		t.Fatal(err)
	}
	if copied != 0 {
		t.Fatalf("marker-backed rerun copied %d files", copied)
	}
}

func TestStateRootCarryoverOnlyWhenRootIsPinned(t *testing.T) {
	userData := t.TempDir()
	t.Setenv("RUNLOOP_USER_DATA_DIR", userData)
	writeStateFile(t, filepath.Join(userData, "state", "auth", "abc.sqlite"), "tokens")

	t.Setenv("AGENTWORKS_STATE_ROOT", "")
	carryOverLegacyStateRoot() // No pinned root: nothing to do and nothing to fail.

	pinned := filepath.Join(t.TempDir(), "pinned")
	t.Setenv("AGENTWORKS_STATE_ROOT", pinned)
	carryOverLegacyStateRoot()
	if got := readStateFile(t, filepath.Join(pinned, "auth", "abc.sqlite")); got != "tokens" {
		t.Fatalf("pinned root did not receive legacy tokens: %q", got)
	}
}
