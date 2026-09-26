package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Real Claude Code: a crew's native session survives the move to Crew/.
// `claude --resume` only looks in ~/.claude/projects/<slug(cwd)>, so without
// the migration's session copy a moved crew silently starts over.
//
//	RUN_CREW_ROOT_CLAUDE_E2E=1 go test ./cmd/server/ -run TestMigrateCrewRootKeepsClaudeSession -v -count=1
func TestMigrateCrewRootKeepsClaudeSession(t *testing.T) {
	if os.Getenv("RUN_CREW_ROOT_CLAUDE_E2E") != "1" {
		t.Skip("set RUN_CREW_ROOT_CLAUDE_E2E=1 (uses the real claude CLI and its login)")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude not installed: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	docs, _ := filepath.EvalSymlinks(t.TempDir())
	state := t.TempDir()
	claude := func(dir string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "claude", append([]string{"-p", "--model", "claude-haiku-4-5-20251001"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	crew := func(owner, dir string) (legacy, shared string) {
		legacy = filepath.Join(docs, "_users", owner, "Chats", "Work", "projects", dir)
		writeMigrationFixture(t, docs, filepath.ToSlash(filepath.Join("_users", owner, "Chats", "Work", "projects", dir, "product.json")), `{"product":"work","id":"`+dir+`","title":"E2E"}`)
		return legacy, filepath.Join(docs, "Crew", dir)
	}
	cleanup := func(dirs ...string) {
		for _, dir := range dirs {
			_ = os.RemoveAll(filepath.Join(home, ".claude", "projects", claudeProjectDirName(dir)))
		}
	}

	oldDir, newDir := crew("alice", "e2e-"+uuid.NewString()[:8])
	sessionID := uuid.NewString()
	t.Cleanup(func() { cleanup(oldDir, newDir) })
	if out, err := claude(oldDir, "--session-id", sessionID, "Remember this token for later: ZEBRA-4242. Reply only with OK."); err != nil {
		t.Fatalf("turn 1: %v\n%s", err, out)
	}

	// Control: a moved folder without the session copy cannot resume.
	controlOld, controlNew := crew("bob", "ctl-"+uuid.NewString()[:8])
	controlSession := uuid.NewString()
	t.Cleanup(func() { cleanup(controlOld, controlNew) })
	if out, err := claude(controlOld, "--session-id", controlSession, "Reply only with OK."); err != nil {
		t.Fatalf("control turn 1: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Dir(controlNew), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(controlOld, controlNew); err != nil {
		t.Fatal(err)
	}
	if out, err := claude(controlNew, "--resume", controlSession, "Reply only with OK."); err == nil && !strings.Contains(strings.ToLower(out), "no conversation found") {
		t.Logf("note: plain move resumed anyway (%q); the migration copy is still exercised below", out)
	} else {
		t.Logf("control: plain move cannot resume, as expected: %s", out)
	}

	report, err := migrateCrewRoot(crewRootMigrationOptions{DocsRoot: docs, StateRoot: state, Apply: true, CLIHomes: []string{home}})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Logf("migration: moves=%d copied_session_dirs=%d warnings=%v", len(report.Moves), report.CLISessionsCopy, report.Warnings)
	if report.CLISessionsCopy < 1 {
		t.Fatal("no Claude session store was carried over")
	}
	out, err := claude(newDir, "--resume", sessionID, "What token did I ask you to remember? Reply with the token only.")
	if err != nil {
		t.Fatalf("resume after move: %v\n%s", err, out)
	}
	t.Logf("resumed answer: %q", out)
	if !strings.Contains(out, "ZEBRA-4242") {
		t.Fatalf("the moved crew lost its Claude conversation: %q", out)
	}
}
