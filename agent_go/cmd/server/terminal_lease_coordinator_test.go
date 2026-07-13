package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/internal/terminalleases"
)

func TestSweepOrphanedOwnedTmuxSessionsKeepsLiveAndUntaggedOwners(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldRun := runTerminalTmuxCommand
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		runTerminalTmuxCommand = oldRun
	})

	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
		startedAt, ok := processStartedAt(os.Getpid())
		if !ok {
			t.Fatal("could not resolve current process start time")
		}
		return strings.Join([]string{
			fmt.Sprintf("mlp-codex-cli-int-live\tinstance-live\t%d\t%d\t1", os.Getpid(), startedAt.Unix()),
			"mlp-claude-code-legacy\t\t\t\t",
			"unrelated-session\tdead-instance\t999999\t1\t1",
		}, "\n"), nil
	}
	var killed []string
	runTerminalTmuxCommand = func(_ context.Context, _ string, args ...string) error {
		killed = append(killed, strings.Join(args, " "))
		return nil
	}

	if got := sweepOrphanedOwnedTmuxSessions(context.Background()); got != 0 {
		t.Fatalf("closed = %d, want 0", got)
	}
	if len(killed) != 0 {
		t.Fatalf("unexpected kills: %v", killed)
	}
}

func TestSweepOrphanedOwnedTmuxSessionsKillsOnlyDeadTaggedOwner(t *testing.T) {
	oldOutput := runTerminalTmuxOutputCommand
	oldRun := runTerminalTmuxCommand
	t.Cleanup(func() {
		runTerminalTmuxOutputCommand = oldOutput
		runTerminalTmuxCommand = oldRun
	})

	runTerminalTmuxOutputCommand = func(context.Context, ...string) (string, error) {
		return "mlp-pi-cli-int-orphan\tdead-instance\t999999\t1\t1", nil
	}
	var killed string
	runTerminalTmuxCommand = func(_ context.Context, _ string, args ...string) error {
		killed = strings.Join(args, " ")
		return nil
	}

	if got := sweepOrphanedOwnedTmuxSessions(context.Background()); got != 1 {
		t.Fatalf("closed = %d, want 1", got)
	}
	if killed != "kill-session -t mlp-pi-cli-int-orphan" {
		t.Fatalf("kill args = %q", killed)
	}
}

func TestParseOwnedTmuxSessionsRejectsIncompleteRows(t *testing.T) {
	rows := parseOwnedTmuxSessions("short\trow\nmlp-agy-cli-int-1\tinstance\t42\t100\t200")
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].OwnerPID != 42 || rows[0].Heartbeat != 200 {
		t.Fatalf("parsed row = %#v", rows[0])
	}
}

func TestMarkTerminalLeaseOwnershipWritesOwnerMetadata(t *testing.T) {
	oldRun := runTerminalTmuxCommand
	t.Cleanup(func() { runTerminalTmuxCommand = oldRun })
	var commands []string
	runTerminalTmuxCommand = func(_ context.Context, _ string, args ...string) error {
		commands = append(commands, strings.Join(args, " "))
		return nil
	}

	markTerminalLeaseOwnership(testTerminalLease("mlp-cursor-cli-int-owned"))

	if len(commands) != 4 {
		t.Fatalf("commands = %d, want 4: %v", len(commands), commands)
	}
	for _, option := range []string{
		tmuxOptionRunloopInstanceID,
		tmuxOptionRunloopOwnerPID,
		tmuxOptionRunloopOwnerStartedAt,
		tmuxOptionRunloopHeartbeat,
	} {
		found := false
		for _, command := range commands {
			if strings.Contains(command, option) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing option %s in %v", option, commands)
		}
	}
}

func testTerminalLease(tmuxSession string) terminalleases.Lease {
	return terminalleases.Lease{
		TmuxSession:    tmuxSession,
		InstanceID:     "instance-1",
		OwnerPID:       1234,
		OwnerStartedAt: time.Unix(100, 0),
	}
}
